package mail

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// TestSubscribeConfirmFlow drives the email double opt-in loop: a subscribe
// request creates a Pending subscription and enqueues a confirmation email,
// and confirming via the confirm address activates it.
func TestSubscribeConfirmFlow(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)

	srv := &LMTPServer{Store: s, Pipeline: &Pipeline{Store: s}}
	parsed, err := ParseAddress("dev-subscribe@example.com")
	if err != nil {
		t.Fatal(err)
	}

	raw := []byte("From: alice@example.com\r\nTo: dev-subscribe@example.com\r\nSubject: subscribe\r\n\r\nsubscribe\r\n")
	if err := srv.handleSubscribe(ctx, parsed, raw); err != nil {
		t.Fatalf("handleSubscribe: %v", err)
	}

	// Subscription exists and is pending.
	sub, _ := s.GetSubscriber(ctx, "alice@example.com")
	subscr, err := s.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if subscr.Status != model.SubscriptionStatusPending {
		t.Errorf("Status = %q, want pending", subscr.Status)
	}

	// Confirmation email is enqueued with a Reply-To confirm address.
	queued, _ := s.ListQueued(ctx)
	if len(queued) != 1 {
		t.Fatalf("len(queued) = %d, want 1", len(queued))
	}
	confirmAddr := confirmReplyTo(t, queued[0].Body)
	confirmParsed, err := ParseAddress(confirmAddr)
	if err != nil {
		t.Fatal(err)
	}
	if confirmParsed.Type != AddressTypeConfirm {
		t.Errorf("confirm address type = %v, want confirm", confirmParsed.Type)
	}

	// Confirm via the confirm address.
	if err := srv.handleConfirm(ctx, confirmParsed); err != nil {
		t.Fatalf("handleConfirm: %v", err)
	}
	subscr, _ = s.GetSubscription(ctx, l.ID, sub.ID)
	if subscr.Status != model.SubscriptionStatusActive {
		t.Errorf("Status = %q, want active", subscr.Status)
	}
	if subscr.ConfirmedAt == nil {
		t.Errorf("ConfirmedAt = nil, want set")
	}
}

// TestSubscribeResendWhenPending verifies a repeat subscribe request while a
// subscription is pending re-sends the confirmation email instead of erroring.
func TestSubscribeResendWhenPending(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	_, _ = s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)

	srv := &LMTPServer{Store: s, Pipeline: &Pipeline{Store: s}}
	parsed, _ := ParseAddress("dev-subscribe@example.com")
	raw := []byte("From: alice@example.com\r\nTo: dev-subscribe@example.com\r\nSubject: subscribe\r\n\r\nsubscribe\r\n")

	if err := srv.handleSubscribe(ctx, parsed, raw); err != nil {
		t.Fatalf("first handleSubscribe: %v", err)
	}
	if err := srv.handleSubscribe(ctx, parsed, raw); err != nil {
		t.Fatalf("second handleSubscribe: %v", err)
	}

	queued, _ := s.ListQueued(ctx)
	if len(queued) != 2 {
		t.Errorf("len(queued) = %d, want 2 (confirmation re-sent)", len(queued))
	}
}

// TestSubscribeModeratedConfirmHeld verifies a Moderated list holds the
// subscription for owner approval after confirmation.
func TestSubscribeModeratedConfirmHeld(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	settings := l.Settings
	settings.SubscriptionPolicy = model.SubscriptionPolicyModerated
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	srv := &LMTPServer{Store: s, Pipeline: &Pipeline{Store: s}}
	parsed, _ := ParseAddress("dev-subscribe@example.com")
	raw := []byte("From: alice@example.com\r\nTo: dev-subscribe@example.com\r\nSubject: subscribe\r\n\r\nsubscribe\r\n")
	if err := srv.handleSubscribe(ctx, parsed, raw); err != nil {
		t.Fatalf("handleSubscribe: %v", err)
	}

	queued, _ := s.ListQueued(ctx)
	confirmAddr := confirmReplyTo(t, queued[0].Body)
	confirmParsed, _ := ParseAddress(confirmAddr)
	if err := srv.handleConfirm(ctx, confirmParsed); err != nil {
		t.Fatalf("handleConfirm: %v", err)
	}

	sub, _ := s.GetSubscriber(ctx, "alice@example.com")
	subscr, _ := s.GetSubscription(ctx, l.ID, sub.ID)
	if subscr.Status != model.SubscriptionStatusHeld {
		t.Errorf("Status = %q, want held", subscr.Status)
	}
}

// confirmReplyTo extracts the Reply-To header from a queued message body.
func confirmReplyTo(t *testing.T, body []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(body), "\r\n") {
		if strings.HasPrefix(line, "Reply-To:") {
			addr := strings.TrimSpace(strings.TrimPrefix(line, "Reply-To:"))
			if addr == "" {
				t.Fatal("confirmation email has empty Reply-To")
			}
			return addr
		}
	}
	t.Fatal("confirmation email missing Reply-To header")
	return ""
}

// TestLMTPNullSenderBounce verifies a bounce DSN with the null envelope
// sender ("MAIL FROM:<>", RFC 3464) is accepted and attributed. Regression:
// the server stored the null sender as an empty string and rejected RCPT with
// "503 MAIL first", silently dropping every real bounce from the MTA.
func TestLMTPNullSenderBounce(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	sub, _ := s.GetOrCreateSubscriber(ctx, "alice@example.com")
	if _, err := s.CreateSubscription(ctx, l.ID, sub.ID); err != nil {
		t.Fatal(err)
	}

	srv := &LMTPServer{Store: s, Pipeline: &Pipeline{Store: s}}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &lmtpConn{server: srv, conn: server, r: bufio.NewReader(server), w: bufio.NewWriter(server)}
	done := make(chan struct{})
	go func() { defer close(done); c.serve(ctx) }()

	cr := bufio.NewReader(client)
	write := func(line string) {
		if _, err := io.WriteString(client, line+"\r\n"); err != nil {
			t.Fatal(err)
		}
	}
	readResp := func() string {
		resp, err := cr.ReadString('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		return strings.TrimSpace(resp)
	}
	expect := func(prefix, got string) {
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("response = %q, want prefix %q", got, prefix)
		}
	}

	expect("220", readResp()) // greeting; net.Pipe is synchronous so read first
	write("LHLO test")
	expect("250", readResp())
	write("MAIL FROM:<>") // null sender: how Postfix delivers DSNs
	expect("250", readResp())
	write("RCPT TO:<dev-bounces+alice=example.com@example.com>")
	expect("250", readResp()) // was "503 MAIL first"
	write("DATA")
	expect("354", readResp())
	write("From: MAILER-DAEMON@bMacAir.local")
	write("Subject: Undelivered Mail")
	write("")
	write("Delivery failed")
	write(".")
	expect("250", readResp())
	write("QUIT")
	expect("221", readResp())
	<-done

	subscr, err := s.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if subscr.BounceCount != 1 {
		t.Errorf("BounceCount = %d, want 1 (DSN attributed via VERP)", subscr.BounceCount)
	}
}
