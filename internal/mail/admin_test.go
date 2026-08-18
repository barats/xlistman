package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// adminFixture sets up an in-memory store with a discussion list and a
// pipeline for the shared administration actions (ADR 0016).
func adminFixture(t *testing.T) (*Pipeline, *sqlite.Store, *model.List) {
	t.Helper()
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d, err := s.CreateDomain(ctx, "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.CreateList(ctx, "dev", d.ID, "example.com", "Dev", model.ListTypeDiscussion)
	if err != nil {
		t.Fatal(err)
	}
	return &Pipeline{Store: s, WebBaseURL: "http://localhost:8080"}, s, l
}

// testActor is a fixed Subscriber actor used when exercising audited actions.
func testActor() model.AuditActor {
	return model.AuditActor{Kind: model.AuditActorSubscriber, ID: 1, Email: "owner@example.com"}
}

// queuedTo returns every queued recipient address.
func queuedTo(t *testing.T, s *sqlite.Store, ctx context.Context) []string {
	t.Helper()
	queued, err := s.ListQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var to []string
	for _, q := range queued {
		to = append(to, q.To)
	}
	return to
}

// hasMailTo reports whether a queued message to `to` contains `needle`.
func hasMailTo(t *testing.T, s *sqlite.Store, ctx context.Context, to, needle string) bool {
	t.Helper()
	queued, err := s.ListQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queued {
		if q.To == to && strings.Contains(string(q.Body), needle) {
			return true
		}
	}
	return false
}

func TestAddMemberAuthoritative(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	subscription, err := p.AddMember(ctx, l.ListName, l.Domain, "bob@example.com", testActor())
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	subscr, err := s.GetSubscription(ctx, l.ID, subscription.SubscriberID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if subscr.Status != model.SubscriptionStatusActive {
		t.Errorf("status = %q, want active (no double opt-in)", subscr.Status)
	}
	if !hasMailTo(t, s, ctx, "bob@example.com", "Welcome") {
		t.Errorf("welcome email not sent to bob; queued=%v", queuedTo(t, s, ctx))
	}

	// Adding an already-subscribed address errors.
	if _, err := p.AddMember(ctx, l.ListName, l.Domain, "bob@example.com", testActor()); err == nil {
		t.Errorf("AddMember on existing member: want error")
	}
}

func TestRemoveMemberSendsGoodbye(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	subscription, err := p.AddMember(ctx, l.ListName, l.Domain, "erin@example.com", testActor())
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := p.RemoveMember(ctx, l.ID, subscription.SubscriberID, testActor()); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if _, err := s.GetSubscription(ctx, l.ID, subscription.SubscriberID); err == nil {
		t.Errorf("subscription still exists after RemoveMember")
	}
	if !hasMailTo(t, s, ctx, "erin@example.com", "unsubscribed") {
		t.Errorf("goodbye email not sent to erin; queued=%v", queuedTo(t, s, ctx))
	}

	// Removing a non-member errors.
	if err := p.RemoveMember(ctx, l.ID, subscription.SubscriberID, testActor()); err == nil {
		t.Errorf("RemoveMember on non-member: want error")
	}
}

func TestApproveSubscription(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	carol, err := s.GetOrCreateSubscriber(ctx, "carol@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscr, err := s.CreateSubscription(ctx, l.ID, carol.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSubscription(ctx, subscr.ID, model.SubscriptionStatusHeld); err != nil {
		t.Fatal(err)
	}

	if err := p.ApproveSubscription(ctx, l.ID, carol.ID, testActor()); err != nil {
		t.Fatalf("ApproveSubscription: %v", err)
	}
	updated, _ := s.GetSubscription(ctx, l.ID, carol.ID)
	if updated.Status != model.SubscriptionStatusActive {
		t.Errorf("status = %q, want active after approval", updated.Status)
	}
	if !hasMailTo(t, s, ctx, "carol@example.com", "Welcome") {
		t.Errorf("welcome email not sent on approval")
	}

	// Approving a subscription that is not held errors.
	dave, _ := s.GetOrCreateSubscriber(ctx, "dave@example.com")
	if _, err := s.CreateSubscription(ctx, l.ID, dave.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.ApproveSubscription(ctx, l.ID, dave.ID, testActor()); err == nil {
		t.Errorf("ApproveSubscription on non-held: want error")
	}
}

func TestRejectSubscription(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	frank, err := s.GetOrCreateSubscriber(ctx, "frank@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscr, err := s.CreateSubscription(ctx, l.ID, frank.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmSubscription(ctx, subscr.ID, model.SubscriptionStatusHeld); err != nil {
		t.Fatal(err)
	}

	if err := p.RejectSubscription(ctx, l.ID, frank.ID, testActor()); err != nil {
		t.Fatalf("RejectSubscription: %v", err)
	}
	if _, err := s.GetSubscription(ctx, l.ID, frank.ID); err == nil {
		t.Errorf("subscription still exists after rejection")
	}
	if !hasMailTo(t, s, ctx, "frank@example.com", "not approved") {
		t.Errorf("rejection notice not sent to frank")
	}

	// Rejecting a subscription that is not held errors.
	gina, _ := s.GetOrCreateSubscriber(ctx, "gina@example.com")
	if _, err := s.CreateSubscription(ctx, l.ID, gina.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.RejectSubscription(ctx, l.ID, gina.ID, testActor()); err == nil {
		t.Errorf("RejectSubscription on non-held: want error")
	}
}

func TestRoleLastOwnerGuard(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	owner, _ := s.GetOrCreateSubscriber(ctx, "owner@example.com")
	other, _ := s.GetOrCreateSubscriber(ctx, "other@example.com")
	if err := p.GrantRole(ctx, l.ID, owner.ID, RoleOwner, testActor()); err != nil {
		t.Fatalf("grant owner: %v", err)
	}

	// The last owner cannot be removed.
	if err := p.RevokeRole(ctx, l.ID, owner.ID, RoleOwner, testActor()); err == nil {
		t.Errorf("revoking the last owner: want error")
	}

	// With a second owner, revocation works.
	if err := p.GrantRole(ctx, l.ID, other.ID, RoleOwner, testActor()); err != nil {
		t.Fatalf("grant second owner: %v", err)
	}
	if err := p.RevokeRole(ctx, l.ID, owner.ID, RoleOwner, testActor()); err != nil {
		t.Fatalf("revoke non-last owner: %v", err)
	}

	// Moderators are freely added and removed.
	if err := p.GrantRole(ctx, l.ID, other.ID, RoleModerator, testActor()); err != nil {
		t.Fatalf("grant moderator: %v", err)
	}
	if ok, _ := s.IsModerator(ctx, l.ID, other.ID); !ok {
		t.Errorf("other is not a moderator after grant")
	}
	if err := p.RevokeRole(ctx, l.ID, other.ID, RoleModerator, testActor()); err != nil {
		t.Fatalf("revoke moderator: %v", err)
	}

	// Unknown roles error.
	if err := p.GrantRole(ctx, l.ID, other.ID, "boss", testActor()); err == nil {
		t.Errorf("grant unknown role: want error")
	}
}

// TestAuditTrailRecorded verifies the shared Pipeline records Audit Events
// (ADR 0018) for member and role actions, newest-first, scoped to the list.
func TestAuditTrailRecorded(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	if _, err := p.AddMember(ctx, l.ListName, l.Domain, "bob@example.com", testActor()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	carol, _ := s.GetOrCreateSubscriber(ctx, "carol@example.com")
	if err := p.GrantRole(ctx, l.ID, carol.ID, RoleModerator, testActor()); err != nil {
		t.Fatalf("GrantRole: %v", err)
	}

	listID := l.ID
	events, err := s.ListAuditEvents(ctx, &listID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2; %+v", len(events), events)
	}
	// Newest first: the role grant, then the member add.
	if events[0].Action != model.ActionRoleGrant || events[0].Target != "carol@example.com" ||
		events[0].ActorEmail != "owner@example.com" || events[0].ActorKind != string(model.AuditActorSubscriber) {
		t.Errorf("events[0] = %+v, want role.grant on carol by owner", events[0])
	}
	if events[1].Action != model.ActionMemberAdd || events[1].Target != "bob@example.com" {
		t.Errorf("events[1] = %+v, want member.add on bob", events[1])
	}
	if events[1].ListAddr != "dev@example.com" {
		t.Errorf("events[1] list_addr = %q, want dev@example.com", events[1].ListAddr)
	}

	// A second list sees none of these events (per-list scoping).
	d, _ := s.GetDomain(ctx, "example.com")
	other, _ := s.CreateList(ctx, "other", d.ID, "example.com", "", model.ListTypeDiscussion)
	otherID := other.ID
	otherEvents, _ := s.ListAuditEvents(ctx, &otherID, "", 0)
	if len(otherEvents) != 0 {
		t.Errorf("other list audit = %d events, want 0", len(otherEvents))
	}
}

// TestRecordBounceAutoDisables verifies the shared auto-disable flow
// (ADR 0019): increments accumulate, the subscription disables at the list's
// threshold, and owners are notified only when OwnerAutoDisableNotice is on.
func TestRecordBounceAutoDisables(t *testing.T) {
	s, _, p, l := moderationFixture(t)
	ctx := context.Background()
	alice, _ := s.GetSubscriber(ctx, "alice@example.com")
	subscr, _ := s.GetSubscription(ctx, l.ID, alice.ID)
	threshold := l.Settings.BounceThreshold

	// Below threshold: increments but stays active, no owner notice.
	for i := 0; i < threshold-1; i++ {
		if err := p.RecordBounce(ctx, l, subscr); err != nil {
			t.Fatalf("RecordBounce: %v", err)
		}
	}
	subscr, _ = s.GetSubscription(ctx, l.ID, alice.ID)
	if subscr.Status != model.SubscriptionStatusActive || subscr.BounceCount != threshold-1 {
		t.Fatalf("below threshold: status=%q count=%d, want active/%d", subscr.Status, subscr.BounceCount, threshold-1)
	}
	if rcpts := strings.Join(queuedRecipients(t, s, ctx), ","); strings.Contains(rcpts, "admin@example.com") {
		t.Errorf("owner notified below threshold: %q", rcpts)
	}

	// At threshold: disabled. OwnerAutoDisableNotice is off by default, so no notice.
	if err := p.RecordBounce(ctx, l, subscr); err != nil {
		t.Fatalf("RecordBounce: %v", err)
	}
	subscr, _ = s.GetSubscription(ctx, l.ID, alice.ID)
	if subscr.Status != model.SubscriptionStatusDisabled {
		t.Errorf("status at threshold = %q, want disabled", subscr.Status)
	}
	if rcpts := strings.Join(queuedRecipients(t, s, ctx), ","); strings.Contains(rcpts, "admin@example.com") {
		t.Errorf("owner notified with notice off: %q", rcpts)
	}

	// With the notice on, a fresh auto-disable emails the owner (deduped).
	settings := l.Settings
	settings.OwnerAutoDisableNotice = true
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.GetList(ctx, l.ListName, l.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReenableSubscription(ctx, subscr.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < threshold; i++ {
		if err := p.RecordBounce(ctx, fresh, subscr); err != nil {
			t.Fatalf("RecordBounce: %v", err)
		}
	}
	rcpts := strings.Join(queuedRecipients(t, s, ctx), ",")
	if count(rcpts, "admin@example.com") != 1 {
		t.Errorf("owner notified %d times (owner+moderator should dedupe); got %q", count(rcpts, "admin@example.com"), rcpts)
	}
	if !hasMailTo(t, s, ctx, "admin@example.com", "alice@example.com") {
		t.Errorf("auto-disable notice does not mention alice")
	}
}

func TestConfirmModeratedNotifiesPending(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	settings := l.Settings
	settings.SubscriptionPolicy = model.SubscriptionPolicyModerated
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	srv := &LMTPServer{Store: s, Pipeline: p}
	parsed, err := ParseAddress("dev-subscribe@example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: alice@example.com\r\nTo: dev-subscribe@example.com\r\nSubject: subscribe\r\n\r\nsubscribe\r\n")
	if err := srv.handleSubscribe(ctx, parsed, raw); err != nil {
		t.Fatalf("handleSubscribe: %v", err)
	}

	// Confirming into a moderated list holds the subscription and tells the
	// requester it awaits approval.
	queued, _ := s.ListQueued(ctx)
	confirmParsed, err := ParseAddress(confirmReplyTo(t, queued[0].Body))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.handleConfirm(ctx, confirmParsed); err != nil {
		t.Fatalf("handleConfirm: %v", err)
	}

	alice, _ := s.GetSubscriber(ctx, "alice@example.com")
	subscr, _ := s.GetSubscription(ctx, l.ID, alice.ID)
	if subscr.Status != model.SubscriptionStatusHeld {
		t.Errorf("status = %q, want held on moderated list", subscr.Status)
	}
	if !hasMailTo(t, s, ctx, "alice@example.com", "awaiting approval") {
		t.Errorf("pending-approval notice not sent to alice; queued=%v", queuedTo(t, s, ctx))
	}
}
