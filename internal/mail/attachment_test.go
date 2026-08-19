package mail

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// buildMultipartWith assembles a raw multipart/mixed post with a text/plain
// body and one attachment part.
func buildMultipartWith(t *testing.T, body, attName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p1, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}})
	p1.Write([]byte(body))
	p2, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        []string{"application/octet-stream"},
		"Content-Disposition": []string{`attachment; filename="` + attName + `"`},
	})
	p2.Write([]byte("file-content-bytes"))
	mw.Close()
	head := "From: bob@example.com\r\nTo: dev@example.com\r\nSubject: hi\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n\r\n"
	return append([]byte(head), buf.Bytes()...)
}

func TestAttachmentPolicyRejects(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	bob, _ := s.GetOrCreateSubscriber(ctx, "bob@example.com")
	subscr, _ := s.CreateSubscription(ctx, l.ID, bob.ID)
	s.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusActive)
	alice, _ := s.GetOrCreateSubscriber(ctx, "alice@example.com")
	asub, _ := s.CreateSubscription(ctx, l.ID, alice.ID)
	s.SetSubscriptionStatus(ctx, asub.ID, model.SubscriptionStatusActive)

	p := &Pipeline{Store: s, WebBaseURL: "https://lists.example.com"}

	// Sanity: the default list accepts an attachment post (delivered).
	raw := buildMultipartWith(t, "hello", "report.pdf")
	if err := p.ProcessPost(ctx, "dev", "example.com", "bob@example.com", raw); err != nil {
		t.Fatalf("default list rejected attachment post: %v", err)
	}
	entries, _ := s.ListArchive(ctx, l.ID, 10, 0)
	if len(entries) != 1 {
		t.Fatalf("archive entries = %d, want 1", len(entries))
	}
	delivered := false
	queued, _ := s.ListQueued(ctx)
	for _, q := range queued {
		if q.To == "alice@example.com" {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("default list did not deliver the attachment post")
	}

	// Disallow attachments.
	settings := l.Settings
	settings.AllowAttachments = false
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	if err := p.ProcessPost(ctx, "dev", "example.com", "bob@example.com", raw); err != nil {
		t.Fatalf("ProcessPost: %v", err)
	}
	// Not archived (which also proves no delivery — archiving precedes
	// delivery in deliverToList), and the sender got a reason-specific notice.
	entries, _ = s.ListArchive(ctx, l.ID, 10, 0)
	if len(entries) != 1 {
		t.Errorf("attachment post archived despite policy: %d entries", len(entries))
	}
	queued, _ = s.ListQueued(ctx)
	var notice string
	for _, q := range queued {
		if q.To == "bob@example.com" && strings.Contains(string(q.Body), "rejected") {
			notice = string(q.Body)
		}
	}
	if !strings.Contains(notice, "does not allow attachments") {
		t.Errorf("rejection notice missing reason: %q", notice)
	}
}

func TestAttachmentSizePolicyRejects(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	settings := l.Settings
	settings.MaxAttachmentSize = 5 // tiny
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	p := &Pipeline{Store: s, WebBaseURL: "https://lists.example.com"}
	raw := buildMultipartWith(t, "hello", "big.pdf")
	if err := p.ProcessPost(ctx, "dev", "example.com", "bob@example.com", raw); err != nil {
		t.Fatalf("ProcessPost: %v", err)
	}
	queued, _ := s.ListQueued(ctx)
	var notice string
	for _, q := range queued {
		if q.To == "bob@example.com" {
			notice = string(q.Body)
		}
	}
	if !strings.Contains(notice, "per-attachment size limit") {
		t.Errorf("rejection notice missing size reason: %q", notice)
	}
	entries, _ := s.ListArchive(ctx, l.ID, 10, 0)
	if len(entries) != 0 {
		t.Errorf("oversized attachment post archived: %d entries", len(entries))
	}
}

func TestTotalSizePolicyRejects(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	settings := l.Settings
	settings.MaxMessageSize = 10
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	p := &Pipeline{Store: s, WebBaseURL: "https://lists.example.com"}
	raw := []byte("From: bob@example.com\r\nTo: dev@example.com\r\nSubject: hi\r\n\r\nthis body is longer than ten bytes")
	if err := p.ProcessPost(ctx, "dev", "example.com", "bob@example.com", raw); err != nil {
		t.Fatalf("ProcessPost: %v", err)
	}
	queued, _ := s.ListQueued(ctx)
	var notice string
	for _, q := range queued {
		if q.To == "bob@example.com" {
			notice = string(q.Body)
		}
	}
	if !strings.Contains(notice, "size limit") {
		t.Errorf("rejection notice missing size reason: %q", notice)
	}
	entries, _ := s.ListArchive(ctx, l.ID, 10, 0)
	if len(entries) != 0 {
		t.Errorf("oversized post archived: %d entries", len(entries))
	}
}
