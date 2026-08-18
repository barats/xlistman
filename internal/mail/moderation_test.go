package mail

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

func TestParseAddress_Moderate(t *testing.T) {
	p, err := ParseAddress("dev-moderate+abc123@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != AddressTypeModerate {
		t.Errorf("Type = %v, want AddressTypeModerate", p.Type)
	}
	if p.ListName != "dev" {
		t.Errorf("ListName = %q, want dev", p.ListName)
	}
	if p.Domain != "example.com" {
		t.Errorf("Domain = %q, want example.com", p.Domain)
	}
	if p.EncodedPart != "abc123" {
		t.Errorf("EncodedPart = %q, want abc123", p.EncodedPart)
	}
}

// moderationFixture sets up a moderated discussion list with an owner (who is
// also a moderator), a separate moderator, and an active subscriber.
func moderationFixture(t *testing.T) (*sqlite.Store, *LMTPServer, *Pipeline, *model.List) {
	t.Helper()
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	settings := l.Settings
	settings.ModerationEnabled = true
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	admin, _ := s.GetOrCreateSubscriber(ctx, "admin@example.com")
	s.AddOwner(ctx, l.ID, admin.ID)
	s.AddModerator(ctx, l.ID, admin.ID) // admin is both owner and moderator (dedup check)
	mod, _ := s.GetOrCreateSubscriber(ctx, "mod@example.com")
	s.AddModerator(ctx, l.ID, mod.ID)

	alice, _ := s.GetOrCreateSubscriber(ctx, "alice@example.com")
	subscr, _ := s.CreateSubscription(ctx, l.ID, alice.ID)
	s.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusActive)

	p := &Pipeline{Store: s, WebBaseURL: "https://lists.example.com"}
	srv := &LMTPServer{Store: s, Pipeline: p}
	return s, srv, p, l
}

func queuedRecipients(t *testing.T, s *sqlite.Store, ctx context.Context) []string {
	t.Helper()
	queued, err := s.ListQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rcpts []string
	for _, q := range queued {
		rcpts = append(rcpts, q.To)
	}
	return rcpts
}

func TestHoldNotifiesModeratorsAndSender(t *testing.T) {
	s, _, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: please approve\r\n\r\nhello\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatalf("ProcessPost: %v", err)
	}

	// Message is held.
	held, err := s.ListHeldMessages(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 {
		t.Fatalf("len(held) = %d, want 1", len(held))
	}
	if held[0].Token == "" {
		t.Errorf("held message missing moderation token")
	}

	// Notifications: admin (deduped across owner+moderator), mod, and the sender.
	rcpts := strings.Join(queuedRecipients(t, s, ctx), ",")
	for _, want := range []string{"admin@example.com", "mod@example.com", "charlie@example.com"} {
		if !strings.Contains(rcpts, want) {
			t.Errorf("queue missing notification to %s; got %q", want, rcpts)
		}
	}
	if count(rcpts, "admin@example.com") != 1 {
		t.Errorf("admin notified more than once (owner+moderator should dedupe); got %q", rcpts)
	}
}

func TestModerateApprove(t *testing.T) {
	s, srv, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: please approve\r\n\r\nhello\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	held, _ := s.ListHeldMessages(ctx, l.ID)
	heldMsg := held[0]

	reply := []byte("From: mod@example.com\r\nTo: dev-moderate+" + heldMsg.Token + "@example.com\r\nSubject: Re: held\r\n\r\napprove\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + heldMsg.Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err != nil {
		t.Fatalf("handleModerate: %v", err)
	}

	// Held message gone; delivered to the active subscriber.
	held, _ = s.ListHeldMessages(ctx, l.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after approve")
	}
	rcpts := strings.Join(queuedRecipients(t, s, ctx), ",")
	if !strings.Contains(rcpts, "alice@example.com") {
		t.Errorf("approved post not delivered to alice; got %q", rcpts)
	}
}

func TestModerateReject(t *testing.T) {
	s, srv, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: bad post\r\n\r\nspam\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	held, _ := s.ListHeldMessages(ctx, l.ID)

	reply := []byte("From: admin@example.com\r\nTo: dev-moderate+" + held[0].Token + "@example.com\r\nSubject: Re: held\r\n\r\nreject\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + held[0].Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err != nil {
		t.Fatalf("handleModerate: %v", err)
	}

	held, _ = s.ListHeldMessages(ctx, l.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after reject")
	}
	// Rejection notice enqueued to the original sender.
	queued, _ := s.ListQueued(ctx)
	found := false
	for _, q := range queued {
		if q.To == "charlie@example.com" && strings.Contains(string(q.Body), "rejected") {
			found = true
		}
	}
	if !found {
		t.Errorf("rejection notice not sent to charlie")
	}
}

func TestModerateDiscard(t *testing.T) {
	s, srv, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: junk\r\n\r\nspam\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	held, _ := s.ListHeldMessages(ctx, l.ID)
	before := len(queuedRecipients(t, s, ctx))

	reply := []byte("From: mod@example.com\r\nTo: dev-moderate+" + held[0].Token + "@example.com\r\nSubject: Re: held\r\n\r\ndiscard\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + held[0].Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err != nil {
		t.Fatalf("handleModerate: %v", err)
	}

	held, _ = s.ListHeldMessages(ctx, l.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after discard")
	}
	if after := len(queuedRecipients(t, s, ctx)); after != before {
		t.Errorf("discard enqueued messages: before=%d after=%d, want no change", before, after)
	}
}

// TestEmailBounceAutoDisables drives the VERP bounce address through the LMTP
// handler and confirms the subscription auto-disables at the threshold
// (ADR 0019).
func TestEmailBounceAutoDisables(t *testing.T) {
	s, srv, _, l := moderationFixture(t)
	ctx := context.Background()
	alice, _ := s.GetSubscriber(ctx, "alice@example.com")
	threshold := l.Settings.BounceThreshold

	for i := 0; i < threshold; i++ {
		parsed, err := ParseAddress("dev-bounces+alice=example.com@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.handleBounce(ctx, parsed); err != nil {
			t.Fatalf("handleBounce: %v", err)
		}
	}
	subscr, _ := s.GetSubscription(ctx, l.ID, alice.ID)
	if subscr.Status != model.SubscriptionStatusDisabled {
		t.Errorf("status = %q, want disabled after %d bounces", subscr.Status, threshold)
	}
	if subscr.BounceCount != threshold {
		t.Errorf("bounce count = %d, want %d", subscr.BounceCount, threshold)
	}
}

func TestModerateEmailRecordsAudit(t *testing.T) {
	s, srv, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: held for audit\r\n\r\nhello\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	held, _ := s.ListHeldMessages(ctx, l.ID)

	// A moderator approves via the email path (listname-moderate+token@domain).
	reply := []byte("From: mod@example.com\r\nTo: dev-moderate+" + held[0].Token + "@example.com\r\nSubject: Re: held\r\n\r\napprove\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + held[0].Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err != nil {
		t.Fatalf("handleModerate: %v", err)
	}

	listID := l.ID
	events, err := s.ListAuditEvents(ctx, &listID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1; %+v", len(events), events)
	}
	if events[0].Action != model.ActionModerationApprove || events[0].Target != "held for audit" {
		t.Errorf("event = %+v, want moderation.approve on the held subject", events[0])
	}
	// The email path resolves the actor from the reply's From address.
	if events[0].ActorEmail != "mod@example.com" || events[0].ActorKind != string(model.AuditActorSubscriber) {
		t.Errorf("event actor = %+v, want mod@example.com", events[0])
	}
}

func TestModerateRejectsNonModerator(t *testing.T) {
	s, srv, p, l := moderationFixture(t)
	ctx := context.Background()

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: post\r\n\r\nbody\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	held, _ := s.ListHeldMessages(ctx, l.ID)

	// A random (non-owner, non-moderator) subscriber tries to approve.
	reply := []byte("From: alice@example.com\r\nTo: dev-moderate+" + held[0].Token + "@example.com\r\nSubject: Re: held\r\n\r\napprove\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + held[0].Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err == nil {
		t.Fatal("non-moderator approve succeeded, want error")
	}

	// Message still held.
	held, _ = s.ListHeldMessages(ctx, l.ID)
	if len(held) != 1 {
		t.Errorf("held message removed by unauthorized action")
	}
}

func TestModerateInvalidToken(t *testing.T) {
	_, srv, _, _ := moderationFixture(t)
	ctx := context.Background()

	reply := []byte("From: mod@example.com\r\nSubject: Re: held\r\n\r\napprove\r\n")
	parsed, _ := ParseAddress("dev-moderate+nonsense@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err == nil {
		t.Fatal("invalid token approve succeeded, want error")
	}
}

func TestModerationExpiredHeld(t *testing.T) {
	s, srv, _, l := moderationFixture(t)
	ctx := context.Background()

	// A held message whose expiry has already passed.
	held, err := s.CreateHeldMessage(ctx, l.ID, "charlie@example.com", "old", []byte("From: charlie@example.com\r\nSubject: old\r\n\r\nx\r\n"), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// Acting on it fails.
	reply := []byte("From: mod@example.com\r\nSubject: Re: held\r\n\r\napprove\r\n")
	parsed, _ := ParseAddress("dev-moderate+" + held.Token + "@example.com")
	if err := srv.handleModerate(ctx, parsed, reply); err == nil {
		t.Fatal("approving an expired held message succeeded, want error")
	}

	// The sweeper discards it.
	n, err := s.DeleteExpiredHeldMessages(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("DeleteExpiredHeldMessages removed %d, want 1", n)
	}
	remaining, _ := s.ListHeldMessages(ctx, l.ID)
	if len(remaining) != 0 {
		t.Errorf("expired held message not swept")
	}
}

func count(s, sub string) int {
	n := 0
	for i := 0; ; {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			break
		}
		n++
		i += j + len(sub)
	}
	return n
}
