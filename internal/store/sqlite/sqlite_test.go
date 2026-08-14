package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/barats/xlistman/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDomainCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Create
	d, err := s.CreateDomain(ctx, "example.com", "Test domain")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if d.ID == 0 || d.Name != "example.com" {
		t.Errorf("created domain = %+v", d)
	}

	// Get
	got, err := s.GetDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.Name != "example.com" || got.Description != "Test domain" {
		t.Errorf("got domain = %+v", got)
	}

	// List
	domains, err := s.ListDomains(ctx)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 1 {
		t.Errorf("len(domains) = %d, want 1", len(domains))
	}

	// Delete
	if err := s.DeleteDomain(ctx, "example.com"); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	domains, _ = s.ListDomains(ctx)
	if len(domains) != 0 {
		t.Errorf("after delete, len(domains) = %d, want 0", len(domains))
	}
}

func TestListCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")

	// Create
	l, err := s.CreateList(ctx, "dev", d.ID, "example.com", "Dev list", model.ListTypeDiscussion)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if l.ListName != "dev" || l.Domain != "example.com" {
		t.Errorf("created list = %+v", l)
	}
	if l.Settings.SubjectPrefix != "[dev]" {
		t.Errorf("SubjectPrefix = %q, want %q", l.Settings.SubjectPrefix, "[dev]")
	}
	if l.Settings.FooterEnabled != true {
		t.Errorf("FooterEnabled = false, want true")
	}
	if l.ListType != model.ListTypeDiscussion {
		t.Errorf("ListType = %q, want %q", l.ListType, model.ListTypeDiscussion)
	}

	// Get
	got, err := s.GetList(ctx, "dev", "example.com")
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if got.Address() != "dev@example.com" {
		t.Errorf("Address() = %q, want %q", got.Address(), "dev@example.com")
	}
	if got.Settings.SubjectPrefix != "[dev]" {
		t.Errorf("SubjectPrefix after get = %q", got.Settings.SubjectPrefix)
	}

	// GetListByID
	gotByID, err := s.GetListByID(ctx, l.ID)
	if err != nil {
		t.Fatalf("GetListByID: %v", err)
	}
	if gotByID.ListName != "dev" {
		t.Errorf("GetListByID name = %q", gotByID.ListName)
	}

	// List lists
	lists, err := s.ListLists(ctx, "")
	if err != nil {
		t.Fatalf("ListLists: %v", err)
	}
	if len(lists) != 1 {
		t.Errorf("len(lists) = %d, want 1", len(lists))
	}

	// Update settings
	newSettings := l.Settings
	newSettings.ModerationEnabled = true
	if err := s.UpdateListSettings(ctx, l.ID, newSettings); err != nil {
		t.Fatalf("UpdateListSettings: %v", err)
	}
	updated, _ := s.GetListByID(ctx, l.ID)
	if !updated.Settings.ModerationEnabled {
		t.Errorf("ModerationEnabled = false, want true")
	}

	// Delete
	s.DeleteList(ctx, "dev", "example.com")
	lists, _ = s.ListLists(ctx, "")
	if len(lists) != 0 {
		t.Errorf("after delete, len(lists) = %d, want 0", len(lists))
	}
}

func TestSubscriberGetOrCreate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Create
	sub, err := s.GetOrCreateSubscriber(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetOrCreateSubscriber: %v", err)
	}
	if sub.Email != "alice@example.com" {
		t.Errorf("email = %q", sub.Email)
	}

	// Get existing (should return same subscriber, not create duplicate)
	sub2, err := s.GetOrCreateSubscriber(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetOrCreateSubscriber (existing): %v", err)
	}
	if sub2.ID != sub.ID {
		t.Errorf("subscriber ID = %d, want %d (should be same)", sub2.ID, sub.ID)
	}
}

func TestSubscriptionCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	sub, _ := s.GetOrCreateSubscriber(ctx, "alice@example.com")

	// Create
	subscr, err := s.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subscr.DeliveryMode != model.DeliveryModeRegular {
		t.Errorf("DeliveryMode = %q, want %q", subscr.DeliveryMode, model.DeliveryModeRegular)
	}
	if subscr.Status != model.SubscriptionStatusPending {
		t.Errorf("Status = %q, want %q", subscr.Status, model.SubscriptionStatusPending)
	}

	// Get
	got, err := s.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.ID != subscr.ID {
		t.Errorf("subscription ID mismatch")
	}

	// Update delivery
	s.UpdateSubscriptionDelivery(ctx, subscr.ID, model.DeliveryModeDigest)
	got, _ = s.GetSubscription(ctx, l.ID, sub.ID)
	if got.DeliveryMode != model.DeliveryModeDigest {
		t.Errorf("DeliveryMode = %q, want %q", got.DeliveryMode, model.DeliveryModeDigest)
	}

	// Set status and confirm
	s.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusDisabled)
	got, _ = s.GetSubscription(ctx, l.ID, sub.ID)
	if got.Status != model.SubscriptionStatusDisabled {
		t.Errorf("Status = %q, want %q", got.Status, model.SubscriptionStatusDisabled)
	}
	s.ConfirmSubscription(ctx, subscr.ID, model.SubscriptionStatusActive)
	got, _ = s.GetSubscription(ctx, l.ID, sub.ID)
	if got.Status != model.SubscriptionStatusActive {
		t.Errorf("Status = %q, want %q", got.Status, model.SubscriptionStatusActive)
	}
	if got.ConfirmedAt == nil {
		t.Errorf("ConfirmedAt = nil, want set")
	}

	// Bounce count
	s.IncrementBounceCount(ctx, subscr.ID)
	s.IncrementBounceCount(ctx, subscr.ID)
	got, _ = s.GetSubscription(ctx, l.ID, sub.ID)
	if got.BounceCount != 2 {
		t.Errorf("BounceCount = %d, want 2", got.BounceCount)
	}

	// List subscriptions
	subs, _ := s.ListSubscriptions(ctx, l.ID)
	if len(subs) != 1 {
		t.Errorf("len(subs) = %d, want 1", len(subs))
	}

	// Delete
	s.DeleteSubscription(ctx, l.ID, sub.ID)
	subs, _ = s.ListSubscriptions(ctx, l.ID)
	if len(subs) != 0 {
		t.Errorf("after delete, len(subs) = %d, want 0", len(subs))
	}
}

func TestOwnerOperations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)
	sub, _ := s.GetOrCreateSubscriber(ctx, "alice@example.com")

	// Add
	if err := s.AddOwner(ctx, l.ID, sub.ID); err != nil {
		t.Fatalf("AddOwner: %v", err)
	}

	// IsOwner
	isOwner, _ := s.IsOwner(ctx, l.ID, sub.ID)
	if !isOwner {
		t.Errorf("IsOwner = false, want true")
	}

	// List
	owners, _ := s.ListOwners(ctx, l.ID)
	if len(owners) != 1 {
		t.Errorf("len(owners) = %d, want 1", len(owners))
	}

	// Duplicate add should not error
	if err := s.AddOwner(ctx, l.ID, sub.ID); err != nil {
		t.Errorf("duplicate AddOwner returned error: %v", err)
	}
	owners, _ = s.ListOwners(ctx, l.ID)
	if len(owners) != 1 {
		t.Errorf("after duplicate add, len(owners) = %d, want 1", len(owners))
	}

	// Remove
	s.RemoveOwner(ctx, l.ID, sub.ID)
	isOwner, _ = s.IsOwner(ctx, l.ID, sub.ID)
	if isOwner {
		t.Errorf("after remove, IsOwner = true, want false")
	}
}

func TestQueueEnqueueClaimSend(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Enqueue
	err := s.Enqueue(ctx, 1, "dev@example.com", "alice@work.com", []byte("test message"), "dev-bounces+alice=work.com@example.com")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Queue depth
	depth, _ := s.QueueDepth(ctx)
	if depth != 1 {
		t.Errorf("depth = %d, want 1", depth)
	}

	// Claim
	now := time.Now()
	q, err := s.ClaimNextQueued(ctx, now)
	if err != nil {
		t.Fatalf("ClaimNextQueued: %v", err)
	}
	if q == nil {
		t.Fatal("ClaimNextQueued returned nil, want a queued message")
	}
	if q.To != "alice@work.com" {
		t.Errorf("To = %q, want %q", q.To, "alice@work.com")
	}

	// After claim, depth should be 0 (claimed items not counted)
	depth, _ = s.QueueDepth(ctx)
	if depth != 0 {
		t.Errorf("after claim, depth = %d, want 0", depth)
	}

	// Mark sent (deletes from queue)
	s.MarkQueuedSent(ctx, q.ID)

	// List queued should be empty
	items, _ := s.ListQueued(ctx)
	if len(items) != 0 {
		t.Errorf("after send, len(items) = %d, want 0", len(items))
	}
}

func TestQueueRequeueWithBackoff(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	s.Enqueue(ctx, 1, "list@x.com", "sub@x.com", []byte("msg"), "")
	now := time.Now()
	q, _ := s.ClaimNextQueued(ctx, now)

	// Requeue with backoff
	nextAttempt := now.Add(5 * time.Minute)
	if err := s.RequeueWithBackoff(ctx, q.ID, nextAttempt); err != nil {
		t.Fatalf("RequeueWithBackoff: %v", err)
	}

	// Should not be claimable yet (next_attempt is in the future)
	q2, _ := s.ClaimNextQueued(ctx, now)
	if q2 != nil {
		t.Error("expected nil claim (next_attempt in future)")
	}

	// Should be claimable after the backoff time
	q3, _ := s.ClaimNextQueued(ctx, nextAttempt.Add(time.Second))
	if q3 == nil {
		t.Fatal("expected claim after backoff")
	}
	if q3.Retries != 1 {
		t.Errorf("Retries = %d, want 1", q3.Retries)
	}
}

func TestArchiveOperations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Archive a message
	err := s.ArchiveMessage(ctx, 1, "<msg1@x.com>", "Hello world", "alice@x.com", []byte("body text"), "thread1")
	if err != nil {
		t.Fatalf("ArchiveMessage: %v", err)
	}

	// List archive
	entries, err := s.ListArchive(ctx, 1, 10, 0)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Subject != "Hello world" {
		t.Errorf("Subject = %q", entries[0].Subject)
	}

	// Get by ID
	e, err := s.GetArchiveEntry(ctx, entries[0].ID)
	if err != nil {
		t.Fatalf("GetArchiveEntry: %v", err)
	}
	if e.From != "alice@x.com" {
		t.Errorf("From = %q", e.From)
	}
}

func TestConfirmationToken(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	expires := time.Now().Add(48 * time.Hour)
	token, err := s.CreateConfirmationToken(ctx, 1, 1, "alice@x.com", expires)
	if err != nil {
		t.Fatalf("CreateConfirmationToken: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	// Get
	ct, err := s.GetConfirmationToken(ctx, token)
	if err != nil {
		t.Fatalf("GetConfirmationToken: %v", err)
	}
	if ct.Email != "alice@x.com" {
		t.Errorf("Email = %q", ct.Email)
	}

	// Delete
	s.DeleteConfirmationToken(ctx, token)
	_, err = s.GetConfirmationToken(ctx, token)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMagicLinkAndSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sub, _ := s.GetOrCreateSubscriber(ctx, "alice@x.com")

	// Magic link
	expires := time.Now().Add(15 * time.Minute)
	token, err := s.CreateMagicLink(ctx, sub.ID, "alice@x.com", expires)
	if err != nil {
		t.Fatalf("CreateMagicLink: %v", err)
	}

	ml, err := s.GetMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("GetMagicLink: %v", err)
	}
	if ml.SubscriberID != sub.ID {
		t.Errorf("SubscriberID = %d, want %d", ml.SubscriberID, sub.ID)
	}

	// Session
	sessExpires := time.Now().Add(30 * 24 * time.Hour)
	sessID, err := s.CreateSession(ctx, sub.ID, "alice@x.com", sessExpires)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess, err := s.GetSession(ctx, sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Email != "alice@x.com" {
		t.Errorf("Email = %q", sess.Email)
	}

	// Delete session
	s.DeleteSession(ctx, sessID)
	_, err = s.GetSession(ctx, sessID)
	if err == nil {
		t.Error("expected error after session delete")
	}
}
