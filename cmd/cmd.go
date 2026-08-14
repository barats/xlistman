// Package cmd implements the xListman CLI.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/barats/xlistman/internal/config"
	"github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/queue"
	"github.com/barats/xlistman/internal/server"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// Version is set at build time.
var Version = "dev"

// Run dispatches CLI commands. webBuild carries the embedded SvelteKit SPA
// (ADR 0007); it is passed to the HTTP server for static serving.
func Run(args []string, webBuild fs.FS) int {
	if len(args) < 1 {
		printUsage()
		return 1
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "serve":
		return cmdServe(rest, webBuild)
	case "deliver":
		return cmdDeliver(rest)
	case "domain":
		return cmdDomain(rest)
	case "list":
		return cmdList(rest)
	case "owner":
		return cmdOwner(rest)
	case "subscriber":
		return cmdSubscriber(rest)
	case "moderation":
		return cmdModeration(rest)
	case "queue":
		return cmdQueue(rest)
	case "config":
		return cmdConfig(rest)
	case "version":
		fmt.Println("xListman", Version)
		return 0
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `xListman - mailing list manager

Usage: xlistman <command> [args]

Commands:
  serve                          Start the daemon
  deliver <list-address>         Pipe mode: read message from stdin, relay to daemon
  domain add <name> [desc]       Add a virtual domain
  domain remove <name>           Remove a virtual domain
  domain list                    List all domains
  list create <addr> --type <t>  Create a list (type: discussion or newsletter)
       [--owner <email>] [--moderate]  Assign first owner / enable moderation
  list delete <addr>             Delete a list
  list list [--domain <d>]       List lists
  list info <addr>               Show list details
  owner add <list> <email>       Add an owner
  owner remove <list> <email>    Remove an owner
  owner list <list>              List owners
  subscriber add <list> <email>  Manually add subscriber
  subscriber remove <list> <email>  Remove subscriber
  subscriber list <list>         List subscribers
  subscriber import <list> <file>  Import from CSV
  moderation list <list>           List held messages awaiting approval
  moderation approve <id>          Approve and deliver a held message
  moderation reject <id>           Reject a held message (notifies sender)
  moderation discard <id>          Discard a held message silently
  queue list                     List pending outbound messages
  queue discard <id>             Discard a stuck message
  config check                   Validate config file
  version                        Print version
`)
}

func loadConfig() (*config.Config, error) {
	configPath := os.Getenv("XLISTMAN_CONFIG")
	if configPath == "" {
		configPath = "xlistman.yaml"
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nUse 'xlistman config init' to generate one, or set XLISTMAN_CONFIG", configPath)
	}
	return config.LoadFromFile(configPath)
}

func openStore(cfg *config.Config) (*sqlite.Store, error) {
	return sqlite.Open(cfg.Database.Path)
}

// parseListAddr splits "dev@example.com" into ("dev", "example.com").
func parseListAddr(addr string) (listName, domain string, err error) {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid list address: %s", addr)
	}
	return parts[0], parts[1], nil
}

// --- serve ---

func cmdServe(args []string, webBuild fs.FS) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config validation:", err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Outbound queue worker sends via SMTP, or writes to a sink directory in
	// development (smtp.mode: sink).
	worker := &queue.Worker{
		Store:      s,
		SMTP:       &mail.SMTPClient{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, Mode: cfg.SMTP.Mode, SinkDir: cfg.SMTP.SinkDir},
		MaxRetries: cfg.Queue.MaxRetries,
		Logger:     logger,
	}
	go worker.Run(ctx)

	// Digest worker: compile and enqueue per-list digests for digest-mode
	// subscribers.
	digestWorker := &queue.DigestWorker{Store: s, Logger: logger}
	go digestWorker.Run(ctx)

	// Inbound: LMTP server (primary MTA path).
	pipeline := &mail.Pipeline{Store: s, WebBaseURL: cfg.Web.BaseURL}
	lmtpServer := &mail.LMTPServer{Addr: cfg.LMTP.Listen, Store: s, Pipeline: pipeline}

	// Inbound: pipe-mode Unix socket (fallback MTA path, ADR 0002).
	socketServer := &mail.SocketServer{Path: cfg.Socket.Path, Server: lmtpServer}

	// HTTP API server (also serves the embedded SPA).
	httpServer := server.New(cfg, s, logger, pipeline, webBuild)

	errCh := make(chan error, 3)
	go func() { errCh <- lmtpServer.ListenAndServe(ctx) }()
	go func() { errCh <- socketServer.ListenAndServe(ctx) }()
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	// Background sweeper: silently discard expired held messages, magic links,
	// and sessions.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := s.DeleteExpiredHeldMessages(ctx, time.Now()); err != nil {
					logger.Error("expire held messages", "error", err)
				} else if n > 0 {
					logger.Info("expired held messages discarded", "count", n)
				}
				if n, err := s.DeleteExpiredMagicLinks(ctx, time.Now()); err != nil {
					logger.Error("expire magic links", "error", err)
				} else if n > 0 {
					logger.Info("expired magic links discarded", "count", n)
				}
				if n, err := s.DeleteExpiredSessions(ctx, time.Now()); err != nil {
					logger.Error("expire sessions", "error", err)
				} else if n > 0 {
					logger.Info("expired sessions discarded", "count", n)
				}
			}
		}
	}()

	logger.Info("xListman daemon started",
		"http", cfg.HTTP.Listen, "lmtp", cfg.LMTP.Listen, "socket", cfg.Socket.Path, "smtp_mode", cfg.SMTP.Mode)

	select {
	case <-ctx.Done():
		// Normal shutdown (SIGINT/SIGTERM).
	case err := <-errCh:
		if err != nil {
			logger.Error("server error", "error", err)
			return 1
		}
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err)
	}
	return 0
}

// --- deliver ---

func cmdDeliver(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman deliver <list-address>")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	conn, err := net.Dial("unix", cfg.Socket.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to daemon: %v (is 'xlistman serve' running?)\n", err)
		return 1
	}
	defer conn.Close()

	// Send the recipient address, then the raw message from stdin.
	fmt.Fprintf(conn, "%s\n", args[0])
	if _, err := io.Copy(conn, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "send message:", err)
		return 1
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	resp, _ := io.ReadAll(conn)
	text := strings.TrimSpace(string(resp))
	if strings.HasPrefix(text, "ERR") {
		fmt.Fprintln(os.Stderr, text)
		return 1
	}
	fmt.Println(text)
	return 0
}

// --- domain ---

func cmdDomain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman domain <add|remove|list> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman domain add <name> [description]")
			return 1
		}
		desc := ""
		if len(args) >= 3 {
			desc = args[2]
		}
		d, err := s.CreateDomain(ctx, args[1], desc)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create domain:", err)
			return 1
		}
		fmt.Printf("Created domain: %s (id=%d)\n", d.Name, d.ID)
		return 0

	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman domain remove <name>")
			return 1
		}
		if err := s.DeleteDomain(ctx, args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "delete domain:", err)
			return 1
		}
		fmt.Printf("Removed domain: %s\n", args[1])
		return 0

	case "list":
		domains, err := s.ListDomains(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list domains:", err)
			return 1
		}
		if len(domains) == 0 {
			fmt.Println("No domains.")
			return 0
		}
		for _, d := range domains {
			fmt.Printf("%s\t%s\n", d.Name, d.Description)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown domain subcommand:", args[0])
	return 1
}

// --- list ---

func cmdList(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman list <create|delete|list|info> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "create":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list create <addr> --type <discussion|newsletter> [--owner <email>] [--moderate]")
			return 1
		}
		listName, domain, err := parseListAddr(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		listType := model.ListTypeDiscussion
		ownerEmail := ""
		desc := ""
		moderate := false
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--type":
				i++
				if i < len(args) {
					listType = model.ListType(args[i])
				}
			case "--owner":
				i++
				if i < len(args) {
					ownerEmail = args[i]
				}
			case "--desc":
				i++
				if i < len(args) {
					desc = args[i]
				}
			case "--moderate":
				moderate = true
			}
		}
		d, err := s.GetDomain(ctx, domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain %s not found: %v\n", domain, err)
			return 1
		}
		l, err := s.CreateList(ctx, listName, d.ID, d.Name, desc, listType)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create list:", err)
			return 1
		}
		if moderate {
			settings := l.Settings
			settings.ModerationEnabled = true
			if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
				fmt.Fprintln(os.Stderr, "set moderation:", err)
				return 1
			}
		}
		if ownerEmail != "" {
			owner, _ := s.GetOrCreateSubscriber(ctx, ownerEmail)
			s.AddOwner(ctx, l.ID, owner.ID)
		}
		fmt.Printf("Created list: %s (type=%s, id=%d)\n", l.Address(), l.ListType, l.ID)
		return 0

	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list delete <addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		if err := s.DeleteList(ctx, listName, domain); err != nil {
			fmt.Fprintln(os.Stderr, "delete list:", err)
			return 1
		}
		fmt.Printf("Deleted list: %s\n", args[1])
		return 0

	case "list":
		domainFilter := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--domain" && i+1 < len(args) {
				domainFilter = args[i+1]
			}
		}
		lists, err := s.ListLists(ctx, domainFilter)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list lists:", err)
			return 1
		}
		if len(lists) == 0 {
			fmt.Println("No lists.")
			return 0
		}
		for _, l := range lists {
			fmt.Printf("%s\t%s\t%s\n", l.Address(), l.ListType, l.Description)
		}
		return 0

	case "info":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list info <addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		fmt.Printf("List: %s\n", l.Address())
		fmt.Printf("Type: %s\n", l.ListType)
		fmt.Printf("Description: %s\n", l.Description)
		fmt.Printf("Moderation: %v\n", l.Settings.ModerationEnabled)
		fmt.Printf("Subscription Policy: %s\n", l.Settings.SubscriptionPolicy)
		fmt.Printf("Digest: %s\n", l.Settings.DigestFrequency)
		fmt.Printf("Subject Prefix: %s\n", l.Settings.SubjectPrefix)
		fmt.Printf("Footer: %v\n", l.Settings.FooterEnabled)
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown list subcommand:", args[0])
	return 1
}

// --- owner ---

func cmdOwner(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman owner <add|remove|list> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "add", "remove":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman owner add|remove <list-addr> <email>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		sub, err := s.GetOrCreateSubscriber(ctx, args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "get subscriber:", err)
			return 1
		}
		if args[0] == "add" {
			s.AddOwner(ctx, l.ID, sub.ID)
			fmt.Printf("Added owner: %s to %s\n", args[2], args[1])
		} else {
			s.RemoveOwner(ctx, l.ID, sub.ID)
			fmt.Printf("Removed owner: %s from %s\n", args[2], args[1])
		}
		return 0

	case "list":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman owner list <list-addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		owners, _ := s.ListOwners(ctx, l.ID)
		for _, o := range owners {
			sub, _ := s.GetSubscriberByID(ctx, o.SubscriberID)
			if sub != nil {
				fmt.Println(sub.Email)
			}
		}
		return 0
	}
	return 1
}

// --- subscriber ---

func cmdSubscriber(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman subscriber <add|remove|list|import> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "add", "remove":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman subscriber add|remove <list-addr> <email>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		sub, err := s.GetOrCreateSubscriber(ctx, args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "get subscriber:", err)
			return 1
		}
		if args[0] == "add" {
			subscr, err := s.CreateSubscription(ctx, l.ID, sub.ID)
			if err != nil {
				fmt.Fprintln(os.Stderr, "add subscriber:", err)
				return 1
			}
			// Manual owner action: bypasses double opt-in (CLI add/import
			// create Active subscriptions directly).
			s.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusActive)
			fmt.Printf("Added subscriber: %s to %s\n", args[2], args[1])
		} else {
			s.DeleteSubscription(ctx, l.ID, sub.ID)
			fmt.Printf("Removed subscriber: %s from %s\n", args[2], args[1])
		}
		return 0

	case "list":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman subscriber list <list-addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		subs, _ := s.ListSubscriptions(ctx, l.ID)
		for _, sub := range subs {
			subr, _ := s.GetSubscriberByID(ctx, sub.SubscriberID)
			if subr != nil {
				fmt.Printf("%s\t%s\t%s\n", subr.Email, sub.DeliveryMode, sub.Status)
			}
		}
		return 0

	case "import":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman subscriber import <list-addr> <file>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		data, err := os.ReadFile(args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "read file:", err)
			return 1
		}
		lines := strings.Split(string(data), "\n")
		count := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			email := line
			if idx := strings.Index(line, ","); idx >= 0 {
				email = strings.TrimSpace(line[:idx])
			}
			sub, err := s.GetOrCreateSubscriber(ctx, email)
			if err != nil {
				continue
			}
			subscr, err := s.CreateSubscription(ctx, l.ID, sub.ID)
			if err != nil {
				continue // already subscribed
			}
			// Manual import bypasses double opt-in, like subscriber add.
			s.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusActive)
			count++
		}
		fmt.Printf("Imported %d subscribers to %s\n", count, args[1])
		return 0
	}
	return 1
}

// --- moderation ---

func cmdModeration(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman moderation <list|approve|reject|discard> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "list":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman moderation list <list-addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		held, err := s.ListHeldMessages(ctx, l.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list held messages:", err)
			return 1
		}
		if len(held) == 0 {
			fmt.Println("No held messages.")
			return 0
		}
		for _, m := range held {
			fmt.Printf("%d\t%s\t%s\texpires=%s\n", m.ID, m.Sender, m.Subject, m.ExpiresAt.Format("2006-01-02 15:04"))
		}
		return 0

	case "approve", "reject", "discard":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "usage: xlistman moderation %s <id>\n", args[0])
			return 1
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid id:", err)
			return 1
		}
		p := &mail.Pipeline{Store: s, WebBaseURL: cfg.Web.BaseURL}
		switch args[0] {
		case "approve":
			err = p.ApproveHeld(ctx, id)
		case "reject":
			err = p.RejectHeld(ctx, id)
		case "discard":
			err = p.DiscardHeld(ctx, id)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "moderation:", err)
			return 1
		}
		fmt.Printf("Held message %d %s\n", id, map[string]string{
			"approve": "approved", "reject": "rejected", "discard": "discarded",
		}[args[0]])
		return 0
	}
	return 1
}

// --- queue ---

func cmdQueue(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman queue <list|discard> [args]")
		return 1
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		return 1
	}
	defer s.Close()
	ctx := context.Background()

	switch args[0] {
	case "list":
		items, err := s.ListQueued(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list queue:", err)
			return 1
		}
		if len(items) == 0 {
			fmt.Println("Queue is empty.")
			return 0
		}
		for _, q := range items {
			fmt.Printf("%d\t%s\t%s\tretries=%d\tnext=%s\n", q.ID, q.To, q.From, q.Retries, q.NextAttempt.Format("2006-01-02 15:04"))
		}
		return 0

	case "discard":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman queue discard <id>")
			return 1
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid id:", err)
			return 1
		}
		s.DiscardQueued(ctx, id)
		fmt.Printf("Discarded queue item: %d\n", id)
		return 0
	}
	return 1
}

// --- config ---

func cmdConfig(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman config <check|init>")
		return 1
	}
	switch args[0] {
	case "check":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := cfg.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, "config invalid:", err)
			return 1
		}
		fmt.Println("Config is valid.")
		return 0

	case "init":
		data, err := config.GenerateDefault()
		if err != nil {
			fmt.Fprintln(os.Stderr, "generate config:", err)
			return 1
		}
		if err := os.WriteFile("xlistman.yaml", data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write config:", err)
			return 1
		}
		fmt.Println("Generated xlistman.yaml. Edit it and set web.base_url.")
		return 0
	}
	return 1
}
