package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/barats/xlistman/internal/config"
	xmail "github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// newProtectServer builds a test server with a config mutation applied, for
// rate-limit tests that need small allowances.
func newProtectServer(t *testing.T, mutate func(*config.Config)) (*Server, *sqlite.Store, string) {
	t.Helper()
	st, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory store: %v", err)
	}
	cfg := &config.Config{Web: config.WebConfig{BaseURL: "http://test.local"}}
	if mutate != nil {
		mutate(cfg)
	}
	pipeline := &xmail.Pipeline{Store: st, WebBaseURL: cfg.Web.BaseURL}
	srv := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), pipeline)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, st, ts.URL
}

func TestMagicLinkPerIPRateLimit(t *testing.T) {
	_, _, baseURL := newProtectServer(t, func(c *config.Config) {
		c.RateLimits.MagicLinkPerIPPerHour = 2
	})
	for i := 0; i < 2; i++ {
		resp, body := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"nobody@example.com"}`, nil)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("request %d status = %d, want 202; body=%s", i+1, resp.StatusCode, body)
		}
	}
	resp, body := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"nobody@example.com"}`, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-limit status = %d, want 429; body=%s", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("429 missing Retry-After header")
	} else if n, err := strconv.Atoi(ra); err != nil || n <= 0 {
		t.Errorf("Retry-After = %q, want a positive second count", ra)
	}
}

func TestMagicLinkPerEmailRateLimit(t *testing.T) {
	_, st, baseURL := newProtectServer(t, nil) // default 3/hour per email
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")

	// Unknown addresses must not consume the per-email allowance: three
	// requests to a non-subscriber send no mail and burn no quota.
	for i := 0; i < 3; i++ {
		resp, _ := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"nobody@example.com"}`, nil)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("unknown request %d status = %d, want 202", i+1, resp.StatusCode)
		}
	}
	if got := queuedCount(t, st); got != 0 {
		t.Fatalf("unknown requests queued %d mail, want 0", got)
	}

	// Three sends to a known subscriber all succeed and each queues mail.
	for i := 0; i < 3; i++ {
		resp, _ := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"alice@example.com"}`, nil)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("alice request %d status = %d, want 202", i+1, resp.StatusCode)
		}
	}
	if got := queuedCount(t, st); got != 3 {
		t.Fatalf("alice requests queued %d mail, want 3", got)
	}

	// A fourth request is silently 202'd: no mail, and no signal that would
	// let an attacker confirm the address is a subscriber.
	resp, body := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("over-quota status = %d, want silent 202; body=%s", resp.StatusCode, body)
	}
	if got := queuedCount(t, st); got != 3 {
		t.Fatalf("over-quota request queued %d mail, want still 3", got)
	}
}

func TestSubscribePerIPRateLimit(t *testing.T) {
	_, st, baseURL := newProtectServer(t, func(c *config.Config) {
		c.RateLimits.SubscribePerHour = 2
	})
	setupList(t, st) // dev@example.com, Open policy
	for i := 0; i < 2; i++ {
		email := "sub" + strconv.Itoa(i+1) + "@example.com"
		resp, body := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"`+email+`"}`, nil)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("subscribe %d status = %d, want 202; body=%s", i+1, resp.StatusCode, body)
		}
	}
	resp, body := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"sub3@example.com"}`, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-limit subscribe status = %d, want 429; body=%s", resp.StatusCode, body)
	}
}

func TestPublicListCacheHeaders(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	setupList(t, st)

	resp, _ := do(t, baseURL, "GET", "/api/lists", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/lists status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("/api/lists Cache-Control = %q, want %q", got, "public, max-age=60")
	}

	resp, _ = do(t, baseURL, "GET", "/api/lists/example.com/dev", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list detail status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("list detail Cache-Control = %q, want %q", got, "public, max-age=60")
	}

	// Errors are not cached.
	resp, _ = do(t, baseURL, "GET", "/api/lists/example.com/missing", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing list status = %d, want 404", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "" {
		t.Errorf("404 Cache-Control = %q, want none", got)
	}
}
