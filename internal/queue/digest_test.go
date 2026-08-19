package queue

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

func testDigestWorker(t *testing.T, s *sqlite.Store) *DigestWorker {
	t.Helper()
	return &DigestWorker{
		Store:  s,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestBuildDigest(t *testing.T) {
	l := &model.List{ListName: "dev", Domain: "example.com", Settings: model.DefaultListSettings(model.ListTypeDiscussion)}
	entries := []model.ArchiveEntry{
		{Subject: "First post", From: "alice@x.com", Body: []byte("From: alice@x.com\r\nSubject: First post\r\n\r\nbody one\r\n")},
		{Subject: "Second post", From: "bob@x.com", Body: []byte("From: bob@x.com\r\nSubject: Second post\r\n\r\nbody two\r\n")},
	}

	subject, mimeHeader, mimeBody, err := buildDigest(l, entries)
	if err != nil {
		t.Fatalf("buildDigest: %v", err)
	}
	if subject != "[dev] Digest (2 messages)" {
		t.Errorf("subject = %q, want %q", subject, "[dev] Digest (2 messages)")
	}
	if !strings.HasPrefix(mimeHeader, "Content-Type: multipart/digest; boundary=") {
		t.Errorf("mimeHeader = %q", mimeHeader)
	}

	_, params, err := mime.ParseMediaType(strings.TrimPrefix(mimeHeader, "Content-Type: "))
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(mimeBody), params["boundary"])

	// Expect a text/plain table of contents followed by two message/rfc822 parts.
	var toc, msgParts int
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		body, _ := io.ReadAll(part)
		ct := part.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			toc++
			if !strings.Contains(string(body), "First post") || !strings.Contains(string(body), "alice@x.com") {
				t.Errorf("toc missing entries: %q", body)
			}
		case strings.HasPrefix(ct, "message/rfc822"):
			msgParts++
		}
	}
	if toc != 1 {
		t.Errorf("text/plain parts = %d, want 1", toc)
	}
	if msgParts != 2 {
		t.Errorf("message/rfc822 parts = %d, want 2", msgParts)
	}
}

func TestDigestWorker(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	w := testDigestWorker(t, s)

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)

	// Alice is digest-mode; Bob is regular.
	alice, _ := s.GetOrCreateSubscriber(ctx, "alice@x.com")
	aliceSub, _ := s.CreateSubscription(ctx, l.ID, alice.ID)
	_ = s.SetSubscriptionStatus(ctx, aliceSub.ID, model.SubscriptionStatusActive)
	_ = s.UpdateSubscriptionDelivery(ctx, aliceSub.ID, model.DeliveryModeDigest)

	bob, _ := s.GetOrCreateSubscriber(ctx, "bob@x.com")
	bobSub, _ := s.CreateSubscription(ctx, l.ID, bob.ID)
	_ = s.SetSubscriptionStatus(ctx, bobSub.ID, model.SubscriptionStatusActive)

	archive := func(subject string) {
		raw := []byte("From: alice@x.com\r\nTo: dev@example.com\r\nSubject: " + subject + "\r\nMessage-ID: <" + subject + "@x.com>\r\n\r\nBody of " + subject + "\r\n")
		if err := s.ArchiveMessage(ctx, l.ID, "<"+subject+"@x.com>", subject, "alice@x.com", raw, "t1", "Body of "+subject); err != nil {
			t.Fatalf("ArchiveMessage: %v", err)
		}
	}
	archive("First post")
	archive("Second post")

	now := time.Now()

	// First run: list is due (never sent); Alice gets a digest, Bob gets nothing.
	w.processDue(ctx, now, w.Logger)
	items, _ := s.ListQueued(ctx)
	if len(items) != 1 {
		t.Fatalf("after first digest, len(queue) = %d, want 1", len(items))
	}
	q := items[0]
	if q.To != "alice@x.com" {
		t.Errorf("digest To = %q, want alice@x.com", q.To)
	}
	if q.OriginalSender != "" {
		t.Errorf("digest OriginalSender = %q, want empty", q.OriginalSender)
	}
	if !strings.Contains(q.EnvelopeSender, "-bounces+") {
		t.Errorf("digest EnvelopeSender = %q, want VERP address", q.EnvelopeSender)
	}
	body := string(q.Body)
	if !strings.Contains(body, "multipart/digest") || !strings.Contains(body, "First post") || !strings.Contains(body, "Second post") {
		t.Errorf("digest body missing content: %q", body)
	}

	// Watermark advanced.
	got, _ := s.GetListByID(ctx, l.ID)
	if got.LastDigestSentAt == nil {
		t.Error("LastDigestSentAt not advanced after digest")
	}

	// Run again within the period: not due, nothing sent.
	w.processDue(ctx, now.Add(time.Minute), w.Logger)
	items, _ = s.ListQueued(ctx)
	if len(items) != 1 {
		t.Fatalf("after in-period run, len(queue) = %d, want 1", len(items))
	}

	// A new post, then the period elapses: a second digest goes out.
	archive("Third post")
	w.processDue(ctx, now.Add(24*time.Hour+time.Minute), w.Logger)
	items, _ = s.ListQueued(ctx)
	if len(items) != 2 {
		t.Fatalf("after second digest, len(queue) = %d, want 2", len(items))
	}
	if !strings.Contains(string(items[1].Body), "Third post") {
		t.Errorf("second digest missing new post: %q", items[1].Body)
	}
}
