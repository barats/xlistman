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
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/barats/xlistman/internal/config"
	"github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/members"
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
	case "audit":
		return cmdAudit(rest)
	case "queue":
		return cmdQueue(rest)
	case "config":
		return cmdConfig(rest)
	case "enable":
		return cmdSetWebToggle(rest, true)
	case "disable":
		return cmdSetWebToggle(rest, false)
	case "web":
		return cmdWeb(rest)
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
       [--owner <email>] [--desc <text>] [--moderate]  Assign first owner / set description / enable moderation
  list delete <addr>             Delete a list (and all its data)
  list type <addr> <t>           Change a list's type (discussion|newsletter)
  list list [--domain <d>]       List lists
  list info <addr>               Show list details and all settings
  list allowlist <addr>          List designated senders (newsletter)
  list add-sender <addr> <email>  Designate a sender for a newsletter list
  list remove-sender <addr> <subscriber-id>  Remove a designated sender
  list config <addr> <k>=<v>...  Edit list settings (keys match list info)
  owner add <list> <email>       Add an owner
  owner remove <list> <email>    Remove an owner
  owner list <list>              List owners
  admin add <email>              Designate a subscriber as Administrator
  admin remove <email>           Revoke Administrator
  admin list                     List Administrators
  moderator add <list> <email>   Add a moderator
  moderator remove <list> <email>  Remove a moderator
  moderator list <list>          List moderators
  subscriber add <list> <email>  Manually add subscriber
  subscriber remove <list> <email>  Remove subscriber
  subscriber approve <list> <email>  Approve a held subscription request
  subscriber reject <list> <email>   Reject a held subscription request
  subscriber re-enable <list> <email>  Re-enable a bounced-out subscriber (resets bounces)
  subscriber reset-bounces <list> <email>  Reset a member's bounce count
  subscriber list <list>         List subscribers
  subscriber import <list> <file>  Import members from a CSV file
  subscriber export <list>         Export members to CSV on stdout
  moderation list <list>           List held messages awaiting approval
  moderation approve <id>          Approve and deliver a held message
  moderation reject <id>           Reject a held message (notifies sender)
  moderation discard <id>          Discard a held message silently
  audit list <addr> [action]      Show audit events for a list
  audit server [action]           Show all audit events instance-wide
  queue list                     List pending outbound messages
  queue discard <id>             Discard a stuck message
  enable login                   Enable web login (magic-link sign-in)
  disable login                  Disable web login (block new sign-ins and log everyone out)
  enable management              Enable web management (role console + server admin)
  disable management             Disable web management (block both consoles)
  web status                     Show web access control state
  config check                   Validate config file
  config init                    Generate a default xlistman.yaml
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

// cliActor returns the local CLI operator as the actor for Audit Events
// (ADR 0018): the CLI has no subscriber identity, so its OS user is captured.
func cliActor() model.AuditActor {
	name := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return model.AuditActor{Kind: model.AuditActorCLI, Detail: "run by " + name + "@" + host}
	}
	return model.AuditActor{Kind: model.AuditActorCLI, Detail: "run by " + name}
}

// recordAudit writes an Audit Event for a store-direct CLI action. The state
// change is already committed; a failure is reported to stderr rather than
// rolled back, since the schema has no cross-row transaction.
func recordAudit(ctx context.Context, st *sqlite.Store, l *model.List, action, target, detail string) {
	var listID *int64
	listAddr := ""
	if l != nil {
		id := l.ID
		listID = &id
		listAddr = l.Address()
	}
	e := model.NewAuditEvent(listID, listAddr, action, cliActor(), target, detail)
	if err := st.CreateAuditEvent(ctx, e); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record audit event (%s): %v\n", action, err)
	}
}

// printAuditEvents prints audit events newest-first in a tabular form.
func printAuditEvents(events []model.AuditEvent) {
	if len(events) == 0 {
		fmt.Println("No audit events.")
		return
	}
	for _, e := range events {
		actor := e.ActorLabel()
		if e.ActorKind == string(model.AuditActorCLI) && e.ActorDetail != "" {
			actor += " (" + e.ActorDetail + ")"
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n",
			e.At.Format("2006-01-02 15:04:05"), e.ListAddr, e.Action, actor, e.Target, e.Detail)
	}
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
		recordAudit(ctx, s, nil, model.ActionDomainCreate, d.Name, desc)
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
		recordAudit(ctx, s, nil, model.ActionDomainDelete, args[1], "")
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
		recordAudit(ctx, s, nil, model.ActionAdminDesignate, sub.Email, "")
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
		recordAudit(ctx, s, nil, model.ActionAdminRevoke, sub.Email, "")
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
		fmt.Fprintln(os.Stderr, "usage: xlistman list <create|delete|type|list|info|config|allowlist|add-sender|remove-sender> [args]")
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
		recordAudit(ctx, s, l, model.ActionListCreate, l.Address(), string(listType))
		fmt.Printf("Created list: %s (type=%s, id=%d)\n", l.Address(), l.ListType, l.ID)
		return 0

	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman list delete <addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		if err := s.DeleteList(ctx, listName, domain); err != nil {
			fmt.Fprintln(os.Stderr, "delete list:", err)
			return 1
		}
		recordAudit(ctx, s, l, model.ActionListDelete, l.Address(), "")
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
		recordAudit(ctx, s, l, model.ActionListType, l.Address(), string(listType))
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
		// Settings are printed with their ListSettings JSON names, matching
		// the keys accepted by `list config` (type/description are list-level
		// fields shown for context).
		st := l.Settings
		fmt.Printf("List: %s\n", l.Address())
		fmt.Printf("type: %s\n", l.ListType)
		fmt.Printf("description: %s\n", l.Description)
		fmt.Printf("moderation_enabled: %v\n", st.ModerationEnabled)
		fmt.Printf("subject_prefix: %s\n", st.SubjectPrefix)
		fmt.Printf("footer_enabled: %v\n", st.FooterEnabled)
		fmt.Printf("max_message_size: %d\n", st.MaxMessageSize)
		fmt.Printf("archive_max_age_days: %d\n", st.ArchiveMaxAgeDays)
		fmt.Printf("digest_frequency: %s\n", st.DigestFrequency)
		fmt.Printf("subscription_policy: %s\n", st.SubscriptionPolicy)
		fmt.Printf("reply_to_mode: %s\n", st.ReplyToMode)
		fmt.Printf("reply_to_address: %s\n", st.ReplyToAddress)
		fmt.Printf("welcome_email: %v\n", st.WelcomeEmail)
		fmt.Printf("goodbye_email: %v\n", st.GoodbyeEmail)
		fmt.Printf("sender_held_notice: %v\n", st.SenderHeldNotice)
		fmt.Printf("owner_auto_disable_notice: %v\n", st.OwnerAutoDisableNotice)
		fmt.Printf("bounce_threshold: %d\n", st.BounceThreshold)
		fmt.Printf("held_expiry_days: %d\n", st.HeldExpiryDays)
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
		oldSettings := l.Settings
		desc := l.Description
		oldDesc := l.Description
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
		changed := settings.ChangedFrom(oldSettings)
		if descSet && desc != oldDesc {
			changed = append(changed, "description")
		}
		recordAudit(ctx, s, l, model.ActionSettingsUpdate, l.Address(), strings.Join(changed, ", "))
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
		recordAudit(ctx, s, l, model.ActionSenderAdd, sub.Email, "")
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
		sub, _ := s.GetSubscriberByID(ctx, subID)
		email := ""
		if sub != nil {
			email = sub.Email
		}
		if err := s.RemoveDesignatedSender(ctx, l.ID, subID); err != nil {
			fmt.Fprintln(os.Stderr, "remove sender:", err)
			return 1
		}
		recordAudit(ctx, s, l, model.ActionSenderRemove, email, "")
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
			recordAudit(ctx, s, l, model.ActionRoleGrant, sub.Email, "owner")
			fmt.Printf("Added owner: %s to %s\n", args[2], args[1])
		} else {
			s.RemoveOwner(ctx, l.ID, sub.ID)
			recordAudit(ctx, s, l, model.ActionRoleRevoke, sub.Email, "owner")
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
			err = p.GrantRole(ctx, l.ID, sub.ID, mail.RoleModerator, cliActor())
		} else {
			err = p.RevokeRole(ctx, l.ID, sub.ID, mail.RoleModerator, cliActor())
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
		fmt.Fprintln(os.Stderr, "usage: xlistman subscriber <add|remove|approve|reject|re-enable|reset-bounces|list|import|export> [args]")
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
			recordAudit(ctx, s, l, model.ActionMemberAdd, sub.Email, "")
			fmt.Printf("Added subscriber: %s to %s\n", args[2], args[1])
		} else {
			s.DeleteSubscription(ctx, l.ID, sub.ID)
			recordAudit(ctx, s, l, model.ActionMemberRemove, sub.Email, "")
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
			err = p.ApproveSubscription(ctx, l.ID, sub.ID, cliActor())
		} else {
			err = p.RejectSubscription(ctx, l.ID, sub.ID, cliActor())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, args[0]+" subscription:", err)
			return 1
		}
		verb := map[string]string{"approve": "approved", "reject": "rejected"}[args[0]]
		fmt.Printf("Subscription of %s to %s %s\n", sub.Email, l.Address(), verb)
		return 0

	case "re-enable", "reset-bounces":
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
		subscr, err := s.GetSubscription(ctx, l.ID, sub.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s is not a member of %s\n", args[2], args[1])
			return 1
		}
		switch args[0] {
		case "re-enable":
			if subscr.Status != model.SubscriptionStatusDisabled {
				fmt.Fprintf(os.Stderr, "subscription of %s is not disabled (status: %s)\n", sub.Email, subscr.Status)
				return 1
			}
			if err := s.ReenableSubscription(ctx, subscr.ID); err != nil {
				fmt.Fprintln(os.Stderr, "re-enable:", err)
				return 1
			}
			recordAudit(ctx, s, l, model.ActionMemberReenable, sub.Email, "")
			fmt.Printf("Re-enabled %s on %s (bounce count reset)\n", sub.Email, l.Address())
		case "reset-bounces":
			if err := s.ResetBounceCount(ctx, subscr.ID); err != nil {
				fmt.Fprintln(os.Stderr, "reset bounces:", err)
				return 1
			}
			recordAudit(ctx, s, l, model.ActionMemberResetBounces, sub.Email, "")
			fmt.Printf("Reset bounce count for %s on %s\n", sub.Email, l.Address())
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
		f, err := os.Open(args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "open file:", err)
			return 1
		}
		defer f.Close()
		src, err := members.ParseImport(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "import:", err)
			return 1
		}
		p := &mail.Pipeline{Store: s, WebBaseURL: cfg.Web.BaseURL}
		res, err := p.ImportMembers(ctx, l.ListName, l.Domain, src, cliActor())
		if err != nil {
			fmt.Fprintln(os.Stderr, "import:", err)
			return 1
		}
		fmt.Printf("Imported %d subscribers to %s (skipped %d: %d already subscribed, %d disabled, %d invalid)\n",
			res.Added, args[1], res.Skipped(), res.Already, res.Disabled, res.Invalid)
		return 0

	case "export":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman subscriber export <list-addr>")
			return 1
		}
		listName, domain, _ := parseListAddr(args[1])
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		views, err := s.ListMembers(ctx, l.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list members:", err)
			return 1
		}
		rows := make([]members.MemberRow, 0, len(views))
		for _, v := range views {
			rows = append(rows, members.MemberRow{Email: v.Email, Status: v.Status, DeliveryMode: v.DeliveryMode, Roles: v.Roles})
		}
		if _, err := os.Stdout.Write(members.ExportCSV(rows)); err != nil {
			fmt.Fprintln(os.Stderr, "write output:", err)
			return 1
		}
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
			err = p.ApproveHeld(ctx, id, cliActor())
		case "reject":
			err = p.RejectHeld(ctx, id, cliActor())
		case "discard":
			err = p.DiscardHeld(ctx, id, cliActor())
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

// --- audit ---

func cmdAudit(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman audit <list <addr> [action]|server [action]>")
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

	action := ""
	if len(args) >= 3 {
		action = args[2]
	}

	switch args[0] {
	case "list":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xlistman audit list <list-addr> [action]")
			return 1
		}
		listName, domain, err := parseListAddr(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		l, err := s.GetList(ctx, listName, domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "get list:", err)
			return 1
		}
		listID := l.ID
		events, err := s.ListAuditEvents(ctx, &listID, action, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list audit events:", err)
			return 1
		}
		printAuditEvents(events)
		return 0

	case "server":
		events, err := s.ListAuditEvents(ctx, nil, action, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "list audit events:", err)
			return 1
		}
		printAuditEvents(events)
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown audit subcommand:", args[0])
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

// --- web access control (ADR 0020) ---

// cmdSetWebToggle enables or disables one of the two web access switches
// (`xlistman enable|disable login|management`). Disabling login also ends
// every existing Session, so a lockdown logs everyone out. Every toggle is
// recorded as an Audit Event.
func cmdSetWebToggle(args []string, enabled bool) int {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: xlistman %s <login|management>\n", verb)
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
	case "login":
		if err := s.SetWebLoginEnabled(ctx, enabled); err != nil {
			fmt.Fprintln(os.Stderr, "set login:", err)
			return 1
		}
		detail := "enabled=" + strconv.FormatBool(enabled)
		if !enabled {
			n, err := s.DeleteAllSessions(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "delete sessions:", err)
				return 1
			}
			detail += fmt.Sprintf(", sessions_ended=%d", n)
		}
		action := model.ActionWebLoginEnable
		if !enabled {
			action = model.ActionWebLoginDisable
		}
		recordAudit(ctx, s, nil, action, "login", detail)
		fmt.Printf("Web login %sd\n", verb)
		return 0

	case "management":
		if err := s.SetWebManagementEnabled(ctx, enabled); err != nil {
			fmt.Fprintln(os.Stderr, "set management:", err)
			return 1
		}
		action := model.ActionWebManagementEnable
		if !enabled {
			action = model.ActionWebManagementDisable
		}
		recordAudit(ctx, s, nil, action, "management", "enabled="+strconv.FormatBool(enabled))
		fmt.Printf("Web management %sd\n", verb)
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown web toggle: %s (use login or management)\n", args[0])
	return 1
}

// cmdWeb shows the current web access control state (`xlistman web status`).
func cmdWeb(args []string) int {
	if len(args) < 1 || args[0] != "status" {
		fmt.Fprintln(os.Stderr, "usage: xlistman web status")
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

	ws, err := s.GetWebSettings(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "get web status:", err)
		return 1
	}
	state := func(on bool) string {
		if on {
			return "enabled"
		}
		return "disabled"
	}
	fmt.Printf("web login:      %s\n", state(ws.LoginEnabled))
	fmt.Printf("web management: %s\n", state(ws.ManagementEnabled))
	return 0
}
