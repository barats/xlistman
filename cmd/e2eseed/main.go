// Command e2eseed inserts fixture states that the CLI and the mail pipeline
// cannot create: Disabled members (bounce count at the list's threshold) and
// Held subscriptions awaiting owner approval. Everything else is seeded
// through the xListman CLI and `xlistman deliver` (see scripts/e2e-seed.sh).
//
// Usage (run against a fresh database, with XLISTMAN_CONFIG=e2e.yaml):
//
//	go run ./cmd/e2eseed                          # canonical fixtures: disabled@ on dev, heldsub@ on mod
//	go run ./cmd/e2eseed disabled <list> <email>  # Disabled member of <list>
//	go run ./cmd/e2eseed held-sub <list> <email>  # Held subscription on <list>
//
// The flag-less form seeds the two fixtures the Batch 1 suite relies on; the
// subcommands mint per-test throwaway fixtures.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/barats/xlistman/internal/config"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

func main() {
	path := os.Getenv("XLISTMAN_CONFIG")
	if path == "" {
		path = "xlistman.yaml"
	}
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		fatalf("load config: %v", err)
	}
	st, err := sqlite.Open(cfg.Database.Path)
	if err != nil {
		fatalf("open store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	args := os.Args[1:]
	if len(args) == 0 {
		if err := seedDisabledMember(ctx, st, "dev", "lists.test", "disabled@lists.test"); err != nil {
			fatalf("disabled member: %v", err)
		}
		if err := seedHeldSubscription(ctx, st, "mod", "lists.test", "heldsub@lists.test"); err != nil {
			fatalf("held subscription: %v", err)
		}
		fmt.Println("e2eseed: seeded canonical disabled member and held subscription")
		return
	}

	switch args[0] {
	case "disabled":
		if len(args) != 3 {
			fatalf("usage: e2eseed disabled <list-addr> <email>")
		}
		name, domain, err := splitAddr(args[1])
		if err != nil {
			fatalf("%v", err)
		}
		if err := seedDisabledMember(ctx, st, name, domain, args[2]); err != nil {
			fatalf("disabled member: %v", err)
		}
		fmt.Printf("e2eseed: %s is a Disabled member of %s\n", args[2], args[1])
	case "held-sub":
		if len(args) != 3 {
			fatalf("usage: e2eseed held-sub <list-addr> <email>")
		}
		name, domain, err := splitAddr(args[1])
		if err != nil {
			fatalf("%v", err)
		}
		if err := seedHeldSubscription(ctx, st, name, domain, args[2]); err != nil {
			fatalf("held subscription: %v", err)
		}
		fmt.Printf("e2eseed: %s holds a pending subscription on %s\n", args[2], args[1])
	default:
		fatalf("unknown subcommand %q (want: disabled | held-sub)", args[0])
	}
}

func splitAddr(addr string) (name, domain string, err error) {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid list address: %s", addr)
	}
	return parts[0], parts[1], nil
}

// seedDisabledMember makes email a Disabled member of the list with
// bounce_count at the list's threshold — the end state the mail pipeline
// produces through RecordBounce (ADR 0019).
func seedDisabledMember(ctx context.Context, st *sqlite.Store, listName, domain, email string) error {
	l, err := st.GetList(ctx, listName, domain)
	if err != nil {
		return err
	}
	sub, err := st.GetOrCreateSubscriber(ctx, email)
	if err != nil {
		return err
	}
	subscr, err := st.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return err
	}
	for i := 0; i < l.Settings.BounceThreshold; i++ {
		if err := st.IncrementBounceCount(ctx, subscr.ID); err != nil {
			return err
		}
	}
	return st.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusDisabled)
}

// seedHeldSubscription makes email a confirmed-but-Held member of the list —
// the moderated subscription policy path awaiting owner approval.
func seedHeldSubscription(ctx context.Context, st *sqlite.Store, listName, domain, email string) error {
	l, err := st.GetList(ctx, listName, domain)
	if err != nil {
		return err
	}
	sub, err := st.GetOrCreateSubscriber(ctx, email)
	if err != nil {
		return err
	}
	subscr, err := st.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return err
	}
	return st.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusHeld)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "e2eseed: "+format+"\n", args...)
	os.Exit(1)
}
