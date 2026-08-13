// Package cmd implements the xListman CLI.
package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/barat/xlistman/internal/config"
	"github.com/barat/xlistman/internal/model"
	"github.com/barat/xlistman/internal/store/sqlite"
)

// Version is set at build time.
var Version = "dev"

// Run dispatches CLI commands.
func Run(args []string) int {
	if len(args) < 1 {
		printUsage()
		return 1
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "serve":
		return cmdServe(rest)
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
	case "queue":
		return cmdQueue(rest)
	case "migrate":
		return cmdMigrate(rest)
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
       --owner <email>           Assign first owner
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
  queue list                     List pending outbound messages
  queue discard <id>             Discard a stuck message
  migrate                        Run database migrations
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

func cmdServe(args []string) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config validation:", err)
		return 1
	}
	// TODO: start daemon (LMTP + HTTP + queue worker)
	fmt.Println("xListman daemon starting...")
	fmt.Printf("  HTTP: %s\n", cfg.HTTP.Listen)
	fmt.Printf("  LMTP: %s\n", cfg.LMTP.Listen)
	fmt.Println("Daemon not yet fully implemented. Use 'xlistman domain/list/owner' commands to manage lists.")
	return 0
}

// --- deliver ---

func cmdDeliver(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: xlistman deliver <list-address>")
		return 1
	}
	// TODO: read stdin, relay to daemon via Unix socket
	fmt.Fprintln(os.Stderr, "pipe mode not yet implemented")
	return 1
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
			fmt.Fprintln(os.Stderr, "usage: xlistman list create <addr> --type <discussion|newsletter> --owner <email>")
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
			s.CreateSubscription(ctx, l.ID, sub.ID)
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
				status := "active"
				if sub.Disabled {
					status = "disabled"
				}
				fmt.Printf("%s\t%s\t%s\n", subr.Email, sub.DeliveryMode, status)
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
			s.CreateSubscription(ctx, l.ID, sub.ID)
			count++
		}
		fmt.Printf("Imported %d subscribers to %s\n", count, args[1])
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

// --- migrate ---

func cmdMigrate(args []string) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	defer s.Close()
	fmt.Println("Migrations complete.")
	return 0
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
