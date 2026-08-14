package mail

import (
	"context"
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
