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
	case "admin":
		return cmdAdmin(rest)
	case "list":
		return cmdList(rest)
	case "owner":
		return cmdOwner(rest)
	case "moderator":
		return cmdModerator(rest)
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
  list delete <addr>             Delete a list (and all its data)
  list type <addr> <t>           Change a list's type (discussion|newsletter)
  list list [--domain <d>]       List lists
  list info <addr>               Show list details
  list allowlist <addr>          List designated senders (newsletter)
  list add-sender <addr> <email>  Designate a sender for a newsletter list
  list remove-sender <addr> <id>  Remove a designated sender
  list config <addr> <k>=<v>...  Edit list settings (keys match list info)
  owner add <list> <email>       Add an owner
  owner remove <list> <email>    Remove an owner
  owner list <list>              List owners
  admin add <email>              Designate a subscriber as Administrator (ADR 0017)
  admin remove <email>           Revoke Administrator
  admin list                     List Administrators
  moderator add <list> <email>   Add a moderator
  moderator remove <list> <email>  Remove a moderator
  moderator list <list>          List moderators
  subscriber add <list> <email>  Manually add subscriber
  subscriber remove <list> <email>  Remove subscriber
  subscriber approve <list> <email>  Approve a held subscription request
  subscriber reject <list> <email>   Reject a held subscription request
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

// applyListSetting applies one key=value pair from `list config` to the
// list's settings, returning ok=false for an unknown key. Keys match the
// ListSettings JSON names; description is a list-level field.
func applyListSetting(s *model.ListSettings, desc *string, descSet *bool, key, val string) (bool, error) {
	switch key {
	case "description":
		*desc = val
		*descSet = true
	case "moderation_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.ModerationEnabled = b
	case "subject_prefix":
		s.SubjectPrefix = val
	case "footer_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.FooterEnabled = b
	case "max_message_size":
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil || n < 0 {
			return true, fmt.Errorf("expected a non-negative integer")
		}
		s.MaxMessageSize = n
	case "archive_max_age_days":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return true, fmt.Errorf("expected a non-negative integer")
		}
		s.ArchiveMaxAgeDays = n
	case "digest_frequency":
		if val != string(model.DigestDaily) && val != string(model.DigestWeekly) {
			return true, fmt.Errorf("expected daily or weekly")
		}
		s.DigestFrequency = model.DigestFrequency(val)
	case "subscription_policy":
		switch val {
		case string(model.SubscriptionPolicyOpen), string(model.SubscriptionPolicyModerated), string(model.SubscriptionPolicyClosed):
			s.SubscriptionPolicy = model.SubscriptionPolicy(val)
		default:
			return true, fmt.Errorf("expected open, moderated, or closed")
		}
	case "reply_to_mode":
		switch val {
		case string(model.ReplyToList), string(model.ReplyToSender), string(model.ReplyToSpecified):
			s.ReplyToMode = model.ReplyToMode(val)
		default:
			return true, fmt.Errorf("expected list, sender, or specified")
		}
	case "reply_to_address":
		s.ReplyToAddress = val
	case "welcome_email":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.WelcomeEmail = b
	case "goodbye_email":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.GoodbyeEmail = b
	case "sender_held_notice":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.SenderHeldNotice = b
	case "owner_auto_disable_notice":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("expected true or false")
		}
		s.OwnerAutoDisableNotice = b
	case "bounce_threshold":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return true, fmt.Errorf("expected a non-negative integer")
		}
		s.BounceThreshold = n
	case "held_expiry_days":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return true, fmt.Errorf("expected a non-negative integer")
		}
		s.HeldExpiryDays = n
	default:
		return false, nil
	}
	return true, nil
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

// --- admin ---

func cmdAdmin(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman admin <add|remove|list> [email]")
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
			fmt.Fprintln(os.Stderr, "usage: xlistman admin add <email>")
			return 1
		}
		sub, err := s.GetOrCreateSubscriber(ctx, strings.ToLower(args[1]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "get subscriber:", err)
			return 1
		}
		if err := s.AddAdministrator(ctx, sub.ID); err != nil {
			fmt.Fprintln(os.Stderr, "add administrator:", err)
			return 1
		}
		fmt.Printf("Designated %s as Administrator\n", sub.Email)
		return 0

	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman admin remove <email>")
			return 1
		}
		sub, err := s.GetSubscriber(ctx, strings.ToLower(args[1]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "unknown subscriber: %s\n", args[1])
			return 1
		}
		if err := s.RemoveAdministrator(ctx, sub.ID); err != nil {
			fmt.Fprintln(os.Stderr, "remove administrator:", err)
			return 1
		}
		fmt.Printf("Revoked Administrator from %s\n", sub.Email)
		return 0

	case "list":
		admins, err := s.ListAdministrators(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list administrators:", err)
			return 1
		}
		if len(admins) == 0 {
			fmt.Println("No administrators. Designate one with `xlistman admin add <email>`.")
			return 0
		}
		for _, a := range admins {
			sub, err := s.GetSubscriberByID(ctx, a.SubscriberID)
			if err != nil {
				continue
			}
			fmt.Println(sub.Email)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown admin subcommand:", args[0])
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

	case "type":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list type <addr> <discussion|newsletter>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		listType := model.ListType(strings.ToLower(args[2]))
		if listType != model.ListTypeDiscussion && listType != model.ListTypeNewsletter {
			fmt.Fprintln(os.Stderr, "list type must be discussion or newsletter")
			return 1
		}
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		if err := s.UpdateListType(ctx, l.ID, listType); err != nil {
			fmt.Fprintln(os.Stderr, "change list type:", err)
			return 1
		}
		fmt.Printf("Changed %s to type %s\n", l.Address(), listType)
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

	case "config":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list config <addr> <key>=<value> [<key>=<value> ...]")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		settings := l.Settings
		desc := l.Description
		descSet := false
		for _, kv := range args[2:] {
			eq := strings.Index(kv, "=")
			if eq <= 0 {
				fmt.Fprintf(os.Stderr, "invalid setting (want key=value): %s\n", kv)
				return 1
			}
			key := strings.ToLower(strings.TrimSpace(kv[:eq]))
			val := strings.TrimSpace(kv[eq+1:])
			ok, err := applyListSetting(&settings, &desc, &descSet, key, val)
			if err != nil {
				fmt.Fprintln(os.Stderr, "setting", key+":", err)
				return 1
			}
			if !ok {
				fmt.Fprintf(os.Stderr, "unknown setting: %s\n", key)
				return 1
			}
		}
		if settings.ReplyToMode == model.ReplyToSpecified && strings.TrimSpace(settings.ReplyToAddress) == "" {
			fmt.Fprintln(os.Stderr, "reply_to_address is required when reply_to_mode is specified")
			return 1
		}
		if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
			fmt.Fprintln(os.Stderr, "update settings:", err)
			return 1
		}
		if descSet {
			if err := s.UpdateListDescription(ctx, l.ID, desc); err != nil {
				fmt.Fprintln(os.Stderr, "update description:", err)
				return 1
			}
		}
		fmt.Printf("Updated settings for %s\n", l.Address())
		return 0

	case "allowlist":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list allowlist <addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		senders, err := s.ListDesignatedSenders(ctx, l.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list senders:", err)
			return 1
		}
		if len(senders) == 0 {
			fmt.Println("No designated senders.")
			return 0
		}
		for _, d := range senders {
			sub, err := s.GetSubscriberByID(ctx, d.SubscriberID)
			if err != nil {
				continue
			}
			fmt.Printf("%d\t%s\n", sub.ID, sub.Email)
		}
		return 0

	case "add-sender":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list add-sender <addr> <email>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		if l.ListType != model.ListTypeNewsletter {
			fmt.Fprintln(os.Stderr, "only newsletter lists have designated senders")
			return 1
		}
		// Subscriber-first: only a known (verified) Subscriber can be designated.
		sub, err := s.GetSubscriber(ctx, strings.ToLower(args[2]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "unknown subscriber: %s. Add them first with `xlistman subscriber add`, or have them subscribe to a list.\n", args[2])
			return 1
		}
		if err := s.AddDesignatedSender(ctx, l.ID, sub.ID); err != nil {
			fmt.Fprintln(os.Stderr, "add sender:", err)
			return 1
		}
		fmt.Printf("Added %s as a designated sender of %s\n", sub.Email, l.Address())
		return 0

	case "remove-sender":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list remove-sender <addr> <subscriber-id>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		subID, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid subscriber id:", args[2])
			return 1
		}
		if err := s.RemoveDesignatedSender(ctx, l.ID, subID); err != nil {
			fmt.Fprintln(os.Stderr, "remove sender:", err)
			return 1
		}
		fmt.Printf("Removed designated sender %d from %s\n", subID, l.Address())
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

// --- moderator ---

func cmdModerator(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman moderator <add|remove|list> [args]")
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
			fmt.Fprintf(os.Stderr, "usage: xlistman moderator %s <list-addr> <email>\n", args[0])
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		sub, err := s.GetOrCreateSubscriber(ctx, strings.ToLower(args[2]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "get subscriber:", err)
			return 1
		}
		p := &mail.Pipeline{Store: s, WebBaseURL: cfg.Web.BaseURL}
		if args[0] == "add" {
			err = p.GrantRole(ctx, l.ID, sub.ID, mail.RoleModerator)
		} else {
			err = p.RevokeRole(ctx, l.ID, sub.ID, mail.RoleModerator)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, args[0]+" moderator:", err)
			return 1
		}
		verb := map[string]string{"add": "Added", "remove": "Removed"}[args[0]]
		fmt.Printf("%s moderator: %s on %s\n", verb, args[2], args[1])
		return 0

	case "list":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman moderator list <list-addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		mods, err := s.ListModerators(ctx, l.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list moderators:", err)
			return 1
		}
		if len(mods) == 0 {
			fmt.Println("No moderators.")
			return 0
		}
		for _, m := range mods {
			sub, err := s.GetSubscriberByID(ctx, m.SubscriberID)
			if err != nil {
				continue
			}
			fmt.Println(sub.Email)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown moderator subcommand:", args[0])
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

	case "approve", "reject":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: xlistman subscriber %s <list-addr> <email>\n", args[0])
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		sub, err := s.GetSubscriber(ctx, strings.ToLower(args[2]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "unknown subscriber: %s\n", args[2])
			return 1
		}
		p := &mail.Pipeline{Store: s, WebBaseURL: cfg.Web.BaseURL}
		if args[0] == "approve" {
			err = p.ApproveSubscription(ctx, l.ID, sub.ID)
		} else {
			err = p.RejectSubscription(ctx, l.ID, sub.ID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, args[0]+" subscription:", err)
			return 1
		}
		verb := map[string]string{"approve": "approved", "reject": "rejected"}[args[0]]
		fmt.Printf("Subscription of %s to %s %s\n", sub.Email, l.Address(), verb)
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
