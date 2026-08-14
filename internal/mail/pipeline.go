package mail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

// Pipeline processes inbound mail messages and routes them to the appropriate
// handler (post, subscribe, unsubscribe, bounce, etc.).
type Pipeline struct {
	Store      store.Store
	WebBaseURL string
}

// ProcessPost handles a message posted to a list address (listname@domain).
// It applies the posting policy, modifies the message, archives it, and
// enqueues delivery to all active subscribers.
func (p *Pipeline) ProcessPost(ctx context.Context, listName, domain, senderAddr string, rawMsg []byte) error {
	// Get the list.
	l, err := p.Store.GetList(ctx, listName, domain)
	if err != nil {
		return fmt.Errorf("get list: %w", err)
	}

	// Determine sender authorization.
	sub, err := p.Store.GetSubscriber(ctx, senderAddr)
	isSubscriber := err == nil && sub != nil
	if isSubscriber {
		_, subErr := p.Store.GetSubscription(ctx, l.ID, sub.ID)
		isSubscriber = subErr == nil
	}

	isOwner := false
	isDesignatedSender := false
	if isSubscriber {
		isOwner, _ = p.Store.IsOwner(ctx, l.ID, sub.ID)
		isDesignatedSender, _ = p.Store.IsDesignatedSender(ctx, l.ID, sub.ID)
	} else {
		// Check by email for non-subscribers (owners may not be subscribed).
		if s, err := p.Store.GetSubscriber(ctx, senderAddr); err == nil {
			isOwner, _ = p.Store.IsOwner(ctx, l.ID, s.ID)
			isDesignatedSender, _ = p.Store.IsDesignatedSender(ctx, l.ID, s.ID)
		}
	}

	// Apply posting policy.
	action := DecidePostAction(*l, isSubscriber, isOwner, isDesignatedSender)

	switch action {
	case PostActionAccept:
		return p.deliverToList(ctx, l, senderAddr, rawMsg)
	case PostActionHold:
		return p.holdMessage(ctx, l, senderAddr, rawMsg)
	case PostActionReject:
		return p.rejectMessage(ctx, l, senderAddr, rawMsg)
	}
	return nil
}

// deliverToList modifies the message, archives it, and enqueues delivery
// to all active (non-disabled, non-nomail) subscribers with regular delivery.
func (p *Pipeline) deliverToList(ctx context.Context, l *model.List, senderAddr string, rawMsg []byte) error {
	// Archive the original message.
	subject := extractSubject(rawMsg)
	msgID := extractHeader(rawMsg, "Message-ID")
	threadID := extractThreadID(rawMsg)
	if err := p.Store.ArchiveMessage(ctx, l.ID, msgID, subject, senderAddr, rawMsg, threadID); err != nil {
		return fmt.Errorf("archive message: %w", err)
	}

	// Get all active subscriptions.
	subs, err := p.Store.ListSubscriptions(ctx, l.ID)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}

	// Enqueue delivery for each subscriber.
	for _, sub := range subs {
		if sub.Status != model.SubscriptionStatusActive {
			continue // only confirmed, active subscriptions receive posts
		}
		if sub.DeliveryMode == model.DeliveryModeNomail {
			continue
		}
		if sub.DeliveryMode == model.DeliveryModeDigest {
			continue // digests are handled by the digest worker
		}

		subscriber, err := p.Store.GetSubscriberByID(ctx, sub.SubscriberID)
		if err != nil {
			continue
		}
		if strings.EqualFold(subscriber.Email, senderAddr) {
			continue // don't send posters their own message back
		}

		// Build VERP envelope sender.
		verpAddr, err := EncodeVERP(l.Address(), subscriber.Email)
		if err != nil {
			continue
		}

		// Modify message per-subscriber (footer with unsubscribe URL).
		unsubURL := fmt.Sprintf("%s/unsubscribe?list=%s&email=%s", p.WebBaseURL, l.Address(), subscriber.Email)
		modified, err := ModifyMessage(rawMsg, ModifyMessageOptions{
			List:           *l,
			WebBaseURL:     p.WebBaseURL,
			UnsubscribeURL: unsubURL,
		})
		if err != nil {
			continue
		}

		if err := p.Store.Enqueue(ctx, l.ID, l.Address(), subscriber.Email, modified, verpAddr); err != nil {
			// Skip a failed enqueue so one bad recipient doesn't block the rest.
			continue
		}
	}

	return nil
}

// holdMessage stores the message in the moderation queue and notifies the
// list's owners and moderators, plus the sender when sender_held_notice is on.
func (p *Pipeline) holdMessage(ctx context.Context, l *model.List, senderAddr string, rawMsg []byte) error {
	subject := extractSubject(rawMsg)
	expiresAt := time.Now().Add(time.Duration(l.Settings.HeldExpiryDays) * 24 * time.Hour)
	held, err := p.Store.CreateHeldMessage(ctx, l.ID, senderAddr, subject, rawMsg, expiresAt)
	if err != nil {
		return fmt.Errorf("create held message: %w", err)
	}

	if err := p.notifyModerators(ctx, l, held); err != nil {
		return fmt.Errorf("notify moderators: %w", err)
	}
	if l.Settings.SenderHeldNotice {
		if err := p.notifySenderHeld(ctx, l, senderAddr, subject); err != nil {
			return fmt.Errorf("notify sender: %w", err)
		}
	}
	return nil
}

// notifyModerators emails the full held message to every owner and moderator
// of the list (deduplicated), with a Reply-To that routes their reply to the
// moderation action handler.
func (p *Pipeline) notifyModerators(ctx context.Context, l *model.List, held *model.HeldMessage) error {
	moderateAddr := fmt.Sprintf("%s-moderate+%s@%s", l.ListName, held.Token, l.Domain)

	seen := map[int64]bool{}
	emails := []string{}
	add := func(id int64) {
		if seen[id] {
			return
		}
		seen[id] = true
		if sub, err := p.Store.GetSubscriberByID(ctx, id); err == nil {
			emails = append(emails, sub.Email)
		}
	}
	if owners, err := p.Store.ListOwners(ctx, l.ID); err == nil {
		for _, o := range owners {
			add(o.SubscriberID)
		}
	}
	if mods, err := p.Store.ListModerators(ctx, l.ID); err == nil {
		for _, m := range mods {
			add(m.SubscriberID)
		}
	}

	bodyText := fmt.Sprintf("A message from %s to %s is awaiting approval.\n\n"+
		"Subject: %s\n\n"+
		"To approve, reply to this message with: approve\n"+
		"To reject, reply with: reject\n"+
		"To discard, reply with: discard\n\n"+
		"---- original message ----\n%s\n", held.Sender, l.Address(), held.Subject, held.Body)

	for _, to := range emails {
		if err := p.Store.Enqueue(ctx, l.ID, l.Address(), to,
			buildNotice(l.Address(), to, moderateAddr, "Held message for approval: "+held.Subject, bodyText),
			l.Address()); err != nil {
			return err
		}
	}
	return nil
}

// notifySenderHeld emails the post's sender that their message is awaiting
// moderator approval.
func (p *Pipeline) notifySenderHeld(ctx context.Context, l *model.List, senderAddr, subject string) error {
	bodyText := fmt.Sprintf("Your message to %s has been received and is awaiting moderator approval.\n"+
		"If it is not approved, you will not receive a further notification.\n", l.Address())
	return p.Store.Enqueue(ctx, l.ID, l.Address(), senderAddr,
		buildNotice(l.Address(), senderAddr, l.Address()+"-owner@"+l.Domain, "Your message to "+l.Address()+" is being reviewed", bodyText),
		l.Address())
}

// buildNotice builds a plain-text notification email.
func buildNotice(from, to, replyTo, subject, bodyText string) []byte {
	date := time.Now().UTC().Format(time.RFC1123Z)
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nReply-To: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s",
		from, to, replyTo, subject, date, bodyText))
}

// ApproveHeld delivers a held message to the list, overriding the posting
// policy, and removes it from the moderation queue.
func (p *Pipeline) ApproveHeld(ctx context.Context, heldID int64) error {
	held, l, err := p.heldContext(ctx, heldID)
	if err != nil {
		return err
	}
	if err := p.deliverToList(ctx, l, held.Sender, held.Body); err != nil {
		return err
	}
	return p.Store.DeleteHeldMessage(ctx, held.ID)
}

// RejectHeld discards a held message and notifies its original sender.
func (p *Pipeline) RejectHeld(ctx context.Context, heldID int64) error {
	held, l, err := p.heldContext(ctx, heldID)
	if err != nil {
		return err
	}
	if err := p.rejectMessage(ctx, l, held.Sender, held.Body); err != nil {
		return err
	}
	return p.Store.DeleteHeldMessage(ctx, held.ID)
}

// DiscardHeld removes a held message silently.
func (p *Pipeline) DiscardHeld(ctx context.Context, heldID int64) error {
	held, _, err := p.heldContext(ctx, heldID)
	if err != nil {
		return err
	}
	return p.Store.DeleteHeldMessage(ctx, held.ID)
}

// heldContext loads a held message and its list, rejecting expired messages.
func (p *Pipeline) heldContext(ctx context.Context, heldID int64) (*model.HeldMessage, *model.List, error) {
	held, err := p.Store.GetHeldMessageByID(ctx, heldID)
	if err != nil {
		return nil, nil, fmt.Errorf("held message %d: %w", heldID, err)
	}
	if time.Now().After(held.ExpiresAt) {
		return nil, nil, fmt.Errorf("held message %d has expired", heldID)
	}
	l, err := p.Store.GetListByID(ctx, held.ListID)
	if err != nil {
		return nil, nil, err
	}
	return held, l, nil
}

// rejectMessage sends a rejection notification to the sender.
func (p *Pipeline) rejectMessage(ctx context.Context, l *model.List, senderAddr string, rawMsg []byte) error {
	// Enqueue a rejection notification.
	rejectionBody := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Your post to %s was rejected\r\n\r\n"+
		"Your message to %s was not accepted.\n\n"+
		"If this is a discussion list, only subscribers may post.\n"+
		"If this is a newsletter list, only designated senders may post.\n",
		l.ListName+"-owner@"+l.Domain, senderAddr, l.Address(), l.Address())

	return p.Store.Enqueue(ctx, 0, l.ListName+"-owner@"+l.Domain, senderAddr, []byte(rejectionBody), "")
}

// --- helpers ---

func extractSubject(raw []byte) string {
	return extractHeader(raw, "Subject")
}

func extractHeader(raw []byte, header string) string {
	lines := splitLines(raw)
	prefix := header + ":"
	for i, line := range lines {
		if len(line) >= len(prefix) && equalFold(line[:len(prefix)], prefix) {
			val := trimSpace(line[len(prefix):])
			// Handle header continuation lines.
			for j := i + 1; j < len(lines); j++ {
				if len(lines[j]) > 0 && (lines[j][0] == ' ' || lines[j][0] == '\t') {
					val += trimSpace(lines[j])
				} else {
					break
				}
			}
			return val
		}
		// Stop at empty line (end of headers).
		if line == "" {
			break
		}
	}
	return ""
}

func extractThreadID(raw []byte) string {
	inReplyTo := extractHeader(raw, "In-Reply-To")
	if inReplyTo != "" {
		return inReplyTo
	}
	return extractHeader(raw, "Message-ID")
}

func splitLines(raw []byte) []string {
	var lines []string
	start := 0
	for i, b := range raw {
		if b == '\n' {
			line := string(raw[start:i])
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(raw) {
		line := string(raw[start:])
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
