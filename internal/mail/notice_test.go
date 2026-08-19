package mail

import (
	"context"
	"testing"
)

// TestCustomNoticeContent verifies that owner-customized welcome/goodbye
// subject+body with placeholders replace the built-in text, and that empty
// fields fall back to the defaults.
func TestCustomNoticeContent(t *testing.T) {
	p, s, l := adminFixture(t)
	ctx := context.Background()

	settings := l.Settings
	settings.WelcomeSubject = "Welcome to {list}, {email}"
	settings.WelcomeBody = "Hi {email}, visit {url} to manage your subscription."
	settings.GoodbyeSubject = "Goodbye from {list}"
	settings.GoodbyeBody = "Sorry to see {email} go. You can rejoin at {url}."
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	// Welcome with custom content.
	sub, err := p.AddMember(ctx, l.ListName, l.Domain, "bob@example.com", testActor())
	if err != nil {
		t.Fatal(err)
	}
	if !hasMailTo(t, s, ctx, "bob@example.com", "Subject: Welcome to dev@example.com, bob@example.com") {
		t.Errorf("welcome subject not customized")
	}
	if !hasMailTo(t, s, ctx, "bob@example.com", "visit http://localhost:8080 to manage") {
		t.Errorf("welcome body placeholders not substituted")
	}

	// Goodbye with custom content.
	if err := p.RemoveMember(ctx, l.ID, sub.SubscriberID, testActor()); err != nil {
		t.Fatal(err)
	}
	if !hasMailTo(t, s, ctx, "bob@example.com", "Subject: Goodbye from dev@example.com") {
		t.Errorf("goodbye subject not customized")
	}
	if !hasMailTo(t, s, ctx, "bob@example.com", "Sorry to see bob@example.com go") {
		t.Errorf("goodbye body placeholders not substituted")
	}

	// Empty fields fall back to the built-in text.
	settings.WelcomeSubject = ""
	settings.WelcomeBody = ""
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddMember(ctx, l.ListName, l.Domain, "carol@example.com", testActor()); err != nil {
		t.Fatal(err)
	}
	if !hasMailTo(t, s, ctx, "carol@example.com", "Subject: Welcome to dev@example.com") {
		t.Errorf("default welcome subject not used when empty")
	}
}

// TestCustomSenderHeldNotice verifies the sender-held notice uses the
// owner-customized subject+body, including the {subject} placeholder.
func TestCustomSenderHeldNotice(t *testing.T) {
	s, _, p, l := moderationFixture(t)
	ctx := context.Background()

	// Reload fresh settings so the fixture's moderation toggle is preserved.
	fresh, err := s.GetList(ctx, "dev", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	settings := fresh.Settings
	settings.SenderHeldSubject = "Held: {subject} on {list}"
	settings.SenderHeldBody = "Dear {email}, your post to {list} is awaiting approval."
	if err := s.UpdateListSettings(ctx, l.ID, settings); err != nil {
		t.Fatal(err)
	}

	raw := []byte("From: charlie@example.com\r\nTo: dev@example.com\r\nSubject: my proposal\r\n\r\nbody\r\n")
	if err := p.ProcessPost(ctx, "dev", "example.com", "charlie@example.com", raw); err != nil {
		t.Fatal(err)
	}
	if !hasMailTo(t, s, ctx, "charlie@example.com", "Subject: Held: my proposal on dev@example.com") {
		t.Errorf("sender-held subject not customized")
	}
	if !hasMailTo(t, s, ctx, "charlie@example.com", "Dear charlie@example.com, your post to dev@example.com") {
		t.Errorf("sender-held body placeholders not substituted")
	}
}
