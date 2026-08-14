package queue

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/barats/xlistman/internal/store/sqlite"
)

type failingSender struct{ err error }

func (f failingSender) Send(string, string, []byte) error { return f.err }

type okSender struct{}

func (okSender) Send(string, string, []byte) error { return nil }

func testWorker(t *testing.T, maxRetries int, smtp Sender) (*Worker, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	w := &Worker{
		Store:      s,
		SMTP:       smtp,
		MaxRetries: maxRetries,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return w, s
}

func TestBounceAfterMaxRetries(t *testing.T) {
	w, s := testWorker(t, 2, failingSender{err: errBoom})
	ctx := context.Background()

	// A post delivery carrying the original poster's address.
	if err := s.Enqueue(ctx, 1, "dev@example.com", "bob@x.com", []byte("post"), "dev-bounces+bob=x.com@example.com", "alice@x.com"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// First attempt fails and requeues (retries 0 -> 1).
	now := time.Now()
	w.processBatch(ctx, now, w.Logger)
	items, _ := s.ListQueued(ctx)
	if len(items) != 1 {
		t.Fatalf("after first failure, len(queue) = %d, want 1", len(items))
	}
	if items[0].Retries != 1 {
		t.Errorf("retries = %d, want 1", items[0].Retries)
	}

	// Second attempt (past the backoff) fails and hits the cap: bounce + drop.
	w.processBatch(ctx, now.Add(time.Hour), w.Logger)
	items, _ = s.ListQueued(ctx)
	if len(items) != 1 {
		t.Fatalf("after cap, len(queue) = %d, want 1 (the bounce)", len(items))
	}
	b := items[0]
	if b.To != "alice@x.com" {
		t.Errorf("bounce To = %q, want alice@x.com", b.To)
	}
	if b.OriginalSender != "" {
		t.Errorf("bounce OriginalSender = %q, want empty", b.OriginalSender)
	}
	body := string(b.Body)
	if !strings.Contains(body, "bob@x.com") {
		t.Errorf("bounce body does not mention failed recipient: %q", body)
	}
	if !strings.Contains(body, errBoom.Error()) {
		t.Errorf("bounce body does not include the error: %q", body)
	}
}

func TestDropNotificationAfterMaxRetries(t *testing.T) {
	w, s := testWorker(t, 2, failingSender{err: errBoom})
	ctx := context.Background()

	// A list-originated notice: no original sender.
	if err := s.Enqueue(ctx, 1, "dev@example.com", "bob@x.com", []byte("notice"), "dev@example.com", ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// First failure requeues.
	now := time.Now()
	w.processBatch(ctx, now, w.Logger)
	items, _ := s.ListQueued(ctx)
	if len(items) != 1 {
		t.Fatalf("after first failure, len(queue) = %d, want 1", len(items))
	}

	// Second failure (past the backoff) hits the cap: dropped, nothing bounced.
	w.processBatch(ctx, now.Add(time.Hour), w.Logger)
	items, _ = s.ListQueued(ctx)
	if len(items) != 0 {
		t.Fatalf("after cap, len(queue) = %d, want 0 (dropped)", len(items))
	}
}

func TestSuccessfulSend(t *testing.T) {
	w, s := testWorker(t, 3, okSender{})
	ctx := context.Background()

	if err := s.Enqueue(ctx, 1, "dev@example.com", "bob@x.com", []byte("post"), "dev-bounces+bob=x.com@example.com", "alice@x.com"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w.processBatch(ctx, time.Now(), w.Logger)
	items, _ := s.ListQueued(ctx)
	if len(items) != 0 {
		t.Fatalf("after successful send, len(queue) = %d, want 0", len(items))
	}
}

var errBoom = &sendError{}

type sendError struct{}

func (e *sendError) Error() string { return "delivery failed: connection refused" }
