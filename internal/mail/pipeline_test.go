package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// TestPipelineDeliversOnlyToActive verifies that a post is delivered to Active
// subscriptions only: Pending (unconfirmed), Held, and Disabled must not
// receive posts.
func TestPipelineDeliversOnlyToActive(t *testing.T) {
	s, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	d, _ := s.CreateDomain(ctx, "example.com", "")
	l, _ := s.CreateList(ctx, "dev", d.ID, "example.com", "", model.ListTypeDiscussion)

	addSub := func(email string, status model.SubscriptionStatus) {
		sub, err := s.GetOrCreateSubscriber(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		subscr, err := s.CreateSubscription(ctx, l.ID, sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status != model.SubscriptionStatusPending {
			if err := s.SetSubscriptionStatus(ctx, subscr.ID, status); err != nil {
				t.Fatal(err)
			}
		}
	}

	addSub("bob@example.com", model.SubscriptionStatusActive)
	addSub("alice@example.com", model.SubscriptionStatusActive)
	addSub("charlie@example.com", model.SubscriptionStatusPending)
	addSub("dave@example.com", model.SubscriptionStatusDisabled)
	addSub("erin@example.com", model.SubscriptionStatusHeld)

	raw := []byte("From: bob@example.com\r\nTo: dev@example.com\r\nSubject: hello\r\n\r\nhi everyone\r\n")
	p := &Pipeline{Store: s, WebBaseURL: "https://lists.example.com"}
	if err := p.ProcessPost(ctx, "dev", "example.com", "bob@example.com", raw); err != nil {
		t.Fatalf("ProcessPost: %v", err)
	}

	queued, err := s.ListQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var recipients []string
	for _, q := range queued {
		recipients = append(recipients, q.To)
	}
	got := strings.Join(recipients, ",")

	for _, want := range []string{"bob@example.com", "alice@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("queue missing delivery to %s; got %q", want, got)
		}
	}
	for _, notWant := range []string{"charlie@example.com", "dave@example.com", "erin@example.com"} {
		if strings.Contains(got, notWant) {
			t.Errorf("queue delivered to non-active subscriber %s; got %q", notWant, got)
		}
	}
}
