package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

type cmdFixture struct {
	s   *sqlite.Store
	srv *LMTPServer
	l   *model.List
	ctx context.Context
}

func cmdFixtureT(t *testing.T) *cmdFixture {
	t.Helper()
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	return &cmdFixture{
		s:   s,
		srv: &LMTPServer{Store: s, Pipeline: &Pipeline{Store: s}},
		l:   l,
		ctx: ctx,
	}
}

// addActive subscribes email as an Active subscriber to the fixture list.
func (f *cmdFixture) addActive(t *testing.T, email string) {
	t.Helper()
	sub, err := f.s.GetOrCreateSubscriber(f.ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	subscr, err := f.s.CreateSubscription(f.ctx, f.l.ID, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetSubscriptionStatus(f.ctx, subscr.ID, model.SubscriptionStatusActive); err != nil {
		t.Fatal(err)
	}
}

// addOwner makes email an owner of the fixture list.
func (f *cmdFixture) addOwner(t *testing.T, email string) {
	t.Helper()
	sub, err := f.s.GetOrCreateSubscriber(f.ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.AddOwner(f.ctx, f.l.ID, sub.ID); err != nil {
		t.Fatal(err)
	}
}

// runRequest drives handleRequest with the given sender and body, returning
// the reply body sent back to the sender.
func (f *cmdFixture) runRequest(t *testing.T, sender, body string) string {
	t.Helper()
	parsed, err := ParseAddress("dev-request@example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: " + sender + "\r\nTo: dev-request@example.com\r\nSubject: request\r\n\r\n" + body)
	if err := f.srv.handleRequest(f.ctx, parsed, raw); err != nil {
		t.Fatalf("handleRequest: %v", err)
	}
	queued, err := f.s.ListQueued(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(queued) - 1; i >= 0; i-- {
		if queued[i].To == sender {
			return string(queued[i].Body)
		}
	}
	t.Fatalf("no reply to %s; queue: %+v", sender, queued)
	return ""
}

// TestParseCommands verifies body parsing: one command per line, blank and
// quoted lines ignored, case-insensitive, and contact consuming the rest.
func TestParseCommands(t *testing.T) {
	raw := []byte("From: a@b.c\r\nTo: dev-request@example.com\r\nSubject: s\r\n\r\nSet Digest\n\n> quoted line\nWHICH\n")
	cmds, _ := parseCommands(raw)
	if len(cmds) != 2 || cmds[0] != "Set Digest" || cmds[1] != "WHICH" {
		t.Errorf("cmds = %v, want [Set Digest WHICH]", cmds)
	}

	raw = []byte("From: a@b.c\r\nTo: dev-request@example.com\r\nSubject: s\r\n\r\ncontact\n\nPlease help me.\nThanks\n")
	cmds, msg := parseCommands(raw)
	if len(cmds) != 1 || !isContactCommand(cmds[0]) {
		t.Errorf("cmds = %v, want a single contact command", cmds)
	}
	if !strings.Contains(msg, "Please help me.") || !strings.Contains(msg, "Thanks") {
		t.Errorf("contact message = %q, want both body lines", msg)
	}
}

func TestCommandHelpAndInfo(t *testing.T) {
	f := cmdFixtureT(t)

	got := f.runRequest(t, "anyone@example.com", "help")
	for _, want := range []string{"which", "set", "re-enable", "unsubscribe", "who", "contact", "info"} {
		if !strings.Contains(got, want) {
			t.Errorf("help reply missing %q; got %q", want, got)
		}
	}

	got = f.runRequest(t, "anyone@example.com", "info")
	for _, want := range []string{"dev@example.com", "discussion"} {
		if !strings.Contains(got, want) {
			t.Errorf("info reply missing %q; got %q", want, got)
		}
	}
}

func TestCommandWhich(t *testing.T) {
	f := cmdFixtureT(t)
	f.addActive(t, "alice@example.com")

	// Subscribe alice to a second list on another domain.
	d2, _ := f.s.CreateDomain(f.ctx, "other.com", "")
	l2, _ := f.s.CreateList(f.ctx, "news", d2.ID, "other.com", "", model.ListTypeNewsletter)
	sub, _ := f.s.GetSubscriber(f.ctx, "alice@example.com")
	subscr, _ := f.s.CreateSubscription(f.ctx, l2.ID, sub.ID)
	if err := f.s.SetSubscriptionStatus(f.ctx, subscr.ID, model.SubscriptionStatusActive); err != nil {
		t.Fatal(err)
	}

	got := f.runRequest(t, "alice@example.com", "which")
	for _, want := range []string{"dev@example.com", "news@other.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("which reply missing %s; got %q", want, got)
		}
	}

	got = f.runRequest(t, "nobody@example.com", "which")
	if !strings.Contains(got, "not subscribed") {
		t.Errorf("which reply for unknown address = %q, want 'not subscribed'", got)
	}
}

func TestCommandSet(t *testing.T) {
	f := cmdFixtureT(t)
	f.addActive(t, "alice@example.com")

	got := f.runRequest(t, "alice@example.com", "set digest")
	if !strings.Contains(got, "now digest") {
		t.Errorf("set digest reply = %q, want confirmation", got)
	}
	sub, _ := f.s.GetSubscriber(f.ctx, "alice@example.com")
	subscr, _ := f.s.GetSubscription(f.ctx, f.l.ID, sub.ID)
	if subscr.DeliveryMode != model.DeliveryModeDigest {
		t.Errorf("DeliveryMode = %q, want digest", subscr.DeliveryMode)
	}

	got = f.runRequest(t, "alice@example.com", "set bogus")
	if !strings.Contains(got, "Usage: set") {
		t.Errorf("set bogus reply = %q, want usage", got)
	}

	got = f.runRequest(t, "stranger@example.com", "set nomail")
	if !strings.Contains(got, "not subscribed") {
		t.Errorf("set by stranger = %q, want 'not subscribed'", got)
	}
}

func TestCommandReEnable(t *testing.T) {
	f := cmdFixtureT(t)
	f.addActive(t, "alice@example.com")
	sub, _ := f.s.GetSubscriber(f.ctx, "alice@example.com")
	subscr, _ := f.s.GetSubscription(f.ctx, f.l.ID, sub.ID)
	if err := f.s.SetSubscriptionStatus(f.ctx, subscr.ID, model.SubscriptionStatusDisabled); err != nil {
		t.Fatal(err)
	}

	got := f.runRequest(t, "alice@example.com", "re-enable")
	if !strings.Contains(got, "re-enabled") {
		t.Errorf("re-enable reply = %q, want re-enabled", got)
	}
	subscr, _ = f.s.GetSubscription(f.ctx, f.l.ID, sub.ID)
	if subscr.Status != model.SubscriptionStatusActive {
		t.Errorf("Status = %q, want active", subscr.Status)
	}

	got = f.runRequest(t, "alice@example.com", "re-enable")
	if !strings.Contains(got, "not disabled") {
		t.Errorf("re-enable on active reply = %q, want 'not disabled'", got)
	}
}

func TestCommandUnsubscribe(t *testing.T) {
	f := cmdFixtureT(t)
	f.addActive(t, "alice@example.com")

	got := f.runRequest(t, "alice@example.com", "unsubscribe")
	if !strings.Contains(got, "unsubscribed") {
		t.Errorf("unsubscribe reply = %q, want unsubscribed", got)
	}
	sub, _ := f.s.GetSubscriber(f.ctx, "alice@example.com")
	if _, err := f.s.GetSubscription(f.ctx, f.l.ID, sub.ID); err == nil {
		t.Error("subscription still exists after unsubscribe")
	}

	got = f.runRequest(t, "alice@example.com", "unsubscribe")
	if !strings.Contains(got, "not subscribed") {
		t.Errorf("second unsubscribe reply = %q, want 'not subscribed'", got)
	}
}

func TestCommandWho(t *testing.T) {
	f := cmdFixtureT(t)
	f.addActive(t, "alice@example.com")
	f.addActive(t, "bob@example.com")
	f.addOwner(t, "admin@example.com")

	// An owner can view the roster.
	got := f.runRequest(t, "admin@example.com", "who")
	for _, want := range []string{"alice@example.com", "bob@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("who reply missing %s; got %q", want, got)
		}
	}

	// A member (not owner) is refused.
	got = f.runRequest(t, "alice@example.com", "who")
	if !strings.Contains(got, "Only the list owners") {
		t.Errorf("who by member = %q, want refusal", got)
	}

	// A stranger is refused.
	got = f.runRequest(t, "stranger@example.com", "who")
	if !strings.Contains(got, "Only the list owners") {
		t.Errorf("who by stranger = %q, want refusal", got)
	}
}

func TestCommandContact(t *testing.T) {
	f := cmdFixtureT(t)
	f.addOwner(t, "admin@example.com")
	f.addOwner(t, "boss@example.com")

	got := f.runRequest(t, "alice@example.com", "contact\n\nPlease fix my subscription.\nThanks")
	if !strings.Contains(got, "sent to the owners") {
		t.Errorf("contact reply = %q, want confirmation", got)
	}

	// Both owners receive a forward with Reply-To = sender, the message, and
	// the wrap note; the command line itself is not forwarded.
	queued, _ := f.s.ListQueued(f.ctx)
	var adminBody, bossBody string
	for _, q := range queued {
		switch q.To {
		case "admin@example.com":
			adminBody = string(q.Body)
		case "boss@example.com":
			bossBody = string(q.Body)
		}
	}
	if adminBody == "" || bossBody == "" {
		t.Fatalf("owners did not receive the contact; queue: %+v", queued)
	}
	for _, b := range []string{adminBody, bossBody} {
		if !strings.Contains(b, "Reply-To: alice@example.com") {
			t.Errorf("owner forward missing Reply-To; body=%q", b)
		}
		if !strings.Contains(b, "Please fix my subscription.") {
			t.Errorf("owner forward missing contact content; body=%q", b)
		}
		if !strings.Contains(b, "sent to the owners") {
			t.Errorf("owner forward missing wrap note; body=%q", b)
		}
		if strings.Contains(b, "contact") {
			t.Errorf("owner forward still contains the command line; body=%q", b)
		}
	}
}

func TestContactWithoutMessage(t *testing.T) {
	f := cmdFixtureT(t)
	f.addOwner(t, "admin@example.com")

	got := f.runRequest(t, "alice@example.com", "contact")
	if !strings.Contains(got, "Put your message after the contact command") {
		t.Errorf("empty contact reply = %q, want usage", got)
	}
}

func TestHandleOwnerForward(t *testing.T) {
	f := cmdFixtureT(t)
	f.addOwner(t, "admin@example.com")
	f.addOwner(t, "boss@example.com")
	f.addOwner(t, "alice@example.com") // sender is also an owner: no self-copy

	parsed, _ := ParseAddress("dev-owner@example.com")
	raw := []byte("From: alice@example.com\r\nTo: dev-owner@example.com\r\nSubject: hi owners\r\n\r\nImportant message for owners.\r\n")
	if err := f.srv.handleOwnerForward(f.ctx, parsed, raw); err != nil {
		t.Fatalf("handleOwnerForward: %v", err)
	}

	queued, _ := f.s.ListQueued(f.ctx)
	seen := map[string]bool{}
	var body string
	for _, q := range queued {
		seen[q.To] = true
		if q.To == "admin@example.com" {
			body = string(q.Body)
		}
	}
	if !seen["admin@example.com"] || !seen["boss@example.com"] {
		t.Errorf("owners not all notified; got %v", seen)
	}
	if seen["alice@example.com"] {
		t.Error("sender (also an owner) received their own forward")
	}
	if !strings.Contains(body, "Important message for owners.") {
		t.Errorf("owner forward missing message content; body=%q", body)
	}
	if !strings.Contains(body, "Reply-To: alice@example.com") {
		t.Errorf("owner forward missing Reply-To; body=%q", body)
	}
}

func TestCommandReplyFormat(t *testing.T) {
	f := cmdFixtureT(t)

	parsed, _ := ParseAddress("dev-request@example.com")
	raw := []byte("From: alice@example.com\r\nTo: dev-request@example.com\r\nSubject: request\r\n\r\nhelp\nboguscmd\n")
	if err := f.srv.handleRequest(f.ctx, parsed, raw); err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	queued, _ := f.s.ListQueued(f.ctx)
	if len(queued) != 1 {
		t.Fatalf("len(queued) = %d, want a single combined reply", len(queued))
	}
	q := queued[0]
	body := string(q.Body)

	if !strings.Contains(body, "Unknown command \"boguscmd\"") {
		t.Errorf("reply missing unknown-command message; got %q", body)
	}
	if !strings.Contains(body, "From: dev@example.com") {
		t.Errorf("reply From != list address; got %q", body)
	}
	if !strings.Contains(body, "Reply-To: dev-request@example.com") {
		t.Errorf("reply Reply-To != request address; got %q", body)
	}
	// Envelope sender is VERP-encoded to the command sender.
	_, recip, err := DecodeVERP(q.EnvelopeSender)
	if err != nil || !strings.EqualFold(recip, "alice@example.com") {
		t.Errorf("reply envelope sender %q not VERP to alice", q.EnvelopeSender)
	}
}

func TestEmptyBodyFallsBackToHelp(t *testing.T) {
	f := cmdFixtureT(t)
	got := f.runRequest(t, "alice@example.com", "")
	if !strings.Contains(got, "Commands for") {
		t.Errorf("empty body reply = %q, want help", got)
	}
}
