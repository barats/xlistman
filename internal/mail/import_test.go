package mail

import (
	"context"
	"testing"

	"github.com/barats/xlistman/internal/members"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

func TestImportMembersAuthoritative(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	src := &members.ImportSource{Emails: []string{"alice@example.com", "bob@example.com"}}
	res, err := p.ImportMembers(ctx, l.ListName, l.Domain, src, testActor())
	if err != nil {
		t.Fatalf("ImportMembers: %v", err)
	}
	if res.Added != 2 || res.Skipped() != 0 {
		t.Fatalf("result = %+v, want added 2 skipped 0", res)
	}

	// Both are Active subscribers with no double opt-in and no welcome mail.
	for _, email := range src.Emails {
		sub, err := s.GetSubscriber(ctx, email)
		if err != nil {
			t.Fatalf("GetSubscriber(%s): %v", email, err)
		}
		subscr, err := s.GetSubscription(ctx, l.ID, sub.ID)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", email, err)
		}
		if subscr.Status != model.SubscriptionStatusActive {
			t.Errorf("%s status = %q, want active", email, subscr.Status)
		}
	}
	if got := queuedTo(t, s, ctx); len(got) != 0 {
		t.Errorf("queued recipients = %v, want none (import sends no welcome mail)", got)
	}
}

func TestImportMembersSkipsExistingAndDisabled(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	// Existing Active member.
	existing := makeSubscriberRow(t, s, "carol@example.com")
	exSub, err := s.CreateSubscription(ctx, l.ID, existing.ID)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := s.SetSubscriptionStatus(ctx, exSub.ID, model.SubscriptionStatusActive); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Disabled member.
	disabled := makeSubscriberRow(t, s, "dan@example.com")
	disSub, err := s.CreateSubscription(ctx, l.ID, disabled.ID)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := s.SetSubscriptionStatus(ctx, disSub.ID, model.SubscriptionStatusDisabled); err != nil {
		t.Fatalf("disable: %v", err)
	}

	src := &members.ImportSource{Emails: []string{"carol@example.com", "dan@example.com", "eve@example.com"}}
	res, err := p.ImportMembers(ctx, l.ListName, l.Domain, src, testActor())
	if err != nil {
		t.Fatalf("ImportMembers: %v", err)
	}
	if res.Added != 1 {
		t.Errorf("added = %d, want 1 (eve)", res.Added)
	}
	if res.Already != 1 {
		t.Errorf("already = %d, want 1 (carol)", res.Already)
	}
	if res.Disabled != 1 {
		t.Errorf("disabled = %d, want 1 (dan)", res.Disabled)
	}
	// Dan stays Disabled — import never re-enables.
	danSub, err := s.GetSubscription(ctx, l.ID, disabled.ID)
	if err != nil {
		t.Fatalf("GetSubscription(dan): %v", err)
	}
	if danSub.Status != model.SubscriptionStatusDisabled {
		t.Errorf("dan status = %q, want disabled (import does not re-enable)", danSub.Status)
	}
}

func TestImportMembersRecordsOneAuditEvent(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	src := &members.ImportSource{Emails: []string{"alice@example.com", "bob@example.com"}, Invalid: 1}
	if _, err := p.ImportMembers(ctx, l.ListName, l.Domain, src, testActor()); err != nil {
		t.Fatalf("ImportMembers: %v", err)
	}

	events, err := s.ListAuditEvents(ctx, &l.ID, model.ActionMemberImport, 0, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("member.import events = %d, want exactly 1", len(events))
	}
	e := events[0]
	if e.Action != model.ActionMemberImport {
		t.Errorf("action = %q, want member.import", e.Action)
	}
	if e.Detail != "added 2, skipped 1" {
		t.Errorf("detail = %q, want %q", e.Detail, "added 2, skipped 1")
	}
	if e.Target != l.Address() {
		t.Errorf("target = %q, want list address %q", e.Target, l.Address())
	}
	if e.ActorEmail != testActor().Email {
		t.Errorf("actor email = %q, want %q", e.ActorEmail, testActor().Email)
	}
}

func TestImportMembersRespectsWelcomeEmailSetting(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	settings := l.Settings
	settings.WelcomeEmail = true
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatalf("enable welcome email: %v", err)
	}

	src := &members.ImportSource{Emails: []string{"alice@example.com"}}
	if _, err := p.ImportMembers(ctx, l.ListName, l.Domain, src, testActor()); err != nil {
		t.Fatalf("ImportMembers: %v", err)
	}
	// Even with welcome_email on, a bulk import sends nothing — a migration
	// must not flood N members.
	if got := queuedTo(t, s, ctx); len(got) != 0 {
		t.Errorf("queued recipients = %v, want none (bulk import sends no welcome mail)", got)
	}
}

// makeSubscriberRow creates a Subscriber row without subscribing.
func makeSubscriberRow(t *testing.T, s *sqlite.Store, email string) *model.Subscriber {
	t.Helper()
	sub, err := s.GetOrCreateSubscriber(context.Background(), email)
	if err != nil {
		t.Fatalf("GetOrCreateSubscriber(%s): %v", email, err)
	}
	return sub
}
