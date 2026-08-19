package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/barats/xlistman/internal/model"
)

// Shared administration actions used by the web console and the CLI (ADR 0016),
// so the two admin surfaces exercise the same store functions and cannot drift.

// List role names used by GrantRole/RevokeRole.
const (
	RoleOwner      = "owner"
	RoleModerator  = "moderator"
)

// AddMember subscribes an address directly, bypassing double opt-in: the
// caller (an Owner or server administrator) is the trusted actor. An unknown
// address is created as a Subscriber first. Sends the list's welcome email
// when enabled.
func (p *Pipeline) AddMember(ctx context.Context, listName, domain, email string, actor model.AuditActor) (*model.Subscription, error) {
	sub, err := p.Store.GetOrCreateSubscriber(ctx, email)
	if err != nil {
		return nil, err
	}
	l, err := p.Store.GetList(ctx, listName, domain)
	if err != nil {
		return nil, err
	}
	if _, err := p.Store.GetSubscription(ctx, l.ID, sub.ID); err == nil {
		return nil, fmt.Errorf("already subscribed")
	}
	subscription, err := p.Store.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return nil, err
	}
	if err := p.Store.SetSubscriptionStatus(ctx, subscription.ID, model.SubscriptionStatusActive); err != nil {
		return nil, err
	}
	if l.Settings.WelcomeEmail {
		if err := p.enqueueWelcome(ctx, l, sub); err != nil {
			return nil, err
		}
	}
	return subscription, p.recordAudit(ctx, l, model.ActionMemberAdd, actor, sub.Email, "")
}

// RemoveMember unsubscribes a member, sending the goodbye email when enabled.
func (p *Pipeline) RemoveMember(ctx context.Context, listID, subscriberID int64, actor model.AuditActor) error {
	if _, err := p.Store.GetSubscription(ctx, listID, subscriberID); err != nil {
		return fmt.Errorf("not subscribed")
	}
	sub, err := p.Store.GetSubscriberByID(ctx, subscriberID)
	if err != nil {
		return err
	}
	l, err := p.Store.GetListByID(ctx, listID)
	if err != nil {
		return err
	}
	if err := p.Store.DeleteSubscription(ctx, listID, subscriberID); err != nil {
		return err
	}
	if l.Settings.GoodbyeEmail {
		if err := p.enqueueGoodbye(ctx, l, sub); err != nil {
			return err
		}
	}
	return p.recordAudit(ctx, l, model.ActionMemberRemove, actor, sub.Email, "")
}

// ApproveSubscription activates a Held Subscription (the Owner's Subscription
// Approval) and sends the list's welcome email when enabled.
func (p *Pipeline) ApproveSubscription(ctx context.Context, listID, subscriberID int64, actor model.AuditActor) error {
	sub, l, subscriber, err := p.subscriptionContext(ctx, listID, subscriberID)
	if err != nil {
		return err
	}
	if sub.Status != model.SubscriptionStatusHeld {
		return fmt.Errorf("subscription is not awaiting approval")
	}
	if err := p.Store.SetSubscriptionStatus(ctx, sub.ID, model.SubscriptionStatusActive); err != nil {
		return err
	}
	if l.Settings.WelcomeEmail {
		if err := p.enqueueWelcome(ctx, l, subscriber); err != nil {
			return err
		}
	}
	return p.recordAudit(ctx, l, model.ActionSubscriptionApprove, actor, subscriber.Email, "")
}

// RejectSubscription removes a Held Subscription and notifies the requester
// that their request was not approved.
func (p *Pipeline) RejectSubscription(ctx context.Context, listID, subscriberID int64, actor model.AuditActor) error {
	sub, l, subscriber, err := p.subscriptionContext(ctx, listID, subscriberID)
	if err != nil {
		return err
	}
	if sub.Status != model.SubscriptionStatusHeld {
		return fmt.Errorf("subscription is not awaiting approval")
	}
	if err := p.Store.DeleteSubscription(ctx, listID, subscriberID); err != nil {
		return err
	}
	if err := p.enqueueSubscriptionRejected(ctx, l, subscriber); err != nil {
		return err
	}
	return p.recordAudit(ctx, l, model.ActionSubscriptionReject, actor, subscriber.Email, "")
}

// NotifySubscriptionPending emails a subscriber that their confirmed request
// awaits Owner approval on a Moderated list.
func (p *Pipeline) NotifySubscriptionPending(ctx context.Context, l *model.List, sub *model.Subscriber) error {
	bodyText := fmt.Sprintf("Your subscription request to %s has been received and is awaiting approval by the list owners.\n",
		l.Address())
	return p.Store.Enqueue(ctx, l.ID, l.Address(), sub.Email,
		buildNotice(l.Address(), sub.Email, l.Address()+"-owner@"+l.Domain, "Your subscription to "+l.Address()+" is awaiting approval", bodyText),
		l.Address(), "")
}

// GrantRole grants an Owner or Moderator List Role to a Subscriber.
func (p *Pipeline) GrantRole(ctx context.Context, listID, subscriberID int64, role string, actor model.AuditActor) error {
	l, err := p.Store.GetListByID(ctx, listID)
	if err != nil {
		return err
	}
	sub, err := p.Store.GetSubscriberByID(ctx, subscriberID)
	if err != nil {
		return err
	}
	switch role {
	case RoleOwner:
		if err := p.Store.AddOwner(ctx, listID, subscriberID); err != nil {
			return err
		}
	case RoleModerator:
		if err := p.Store.AddModerator(ctx, listID, subscriberID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown role: %s", role)
	}
	return p.recordAudit(ctx, l, model.ActionRoleGrant, actor, sub.Email, role)
}

// RevokeRole revokes an Owner or Moderator List Role, refusing to remove the
// last Owner so a list can never become ownerless.
func (p *Pipeline) RevokeRole(ctx context.Context, listID, subscriberID int64, role string, actor model.AuditActor) error {
	l, err := p.Store.GetListByID(ctx, listID)
	if err != nil {
		return err
	}
	sub, err := p.Store.GetSubscriberByID(ctx, subscriberID)
	if err != nil {
		return err
	}
	switch role {
	case RoleOwner:
		owners, err := p.Store.ListOwners(ctx, listID)
		if err != nil {
			return err
		}
		if len(owners) == 1 && owners[0].SubscriberID == subscriberID {
			return fmt.Errorf("cannot remove the last owner")
		}
		if err := p.Store.RemoveOwner(ctx, listID, subscriberID); err != nil {
			return err
		}
	case RoleModerator:
		if err := p.Store.RemoveModerator(ctx, listID, subscriberID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown role: %s", role)
	}
	return p.recordAudit(ctx, l, model.ActionRoleRevoke, actor, sub.Email, role)
}

// subscriptionContext loads a subscription, its list, and its subscriber.
func (p *Pipeline) subscriptionContext(ctx context.Context, listID, subscriberID int64) (*model.Subscription, *model.List, *model.Subscriber, error) {
	sub, err := p.Store.GetSubscription(ctx, listID, subscriberID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("subscription not found")
	}
	l, err := p.Store.GetListByID(ctx, listID)
	if err != nil {
		return nil, nil, nil, err
	}
	subscriber, err := p.Store.GetSubscriberByID(ctx, subscriberID)
	if err != nil {
		return nil, nil, nil, err
	}
	return sub, l, subscriber, nil
}

func (p *Pipeline) enqueueWelcome(ctx context.Context, l *model.List, sub *model.Subscriber) error {
	subject := l.Settings.WelcomeSubject
	body := l.Settings.WelcomeBody
	if subject == "" {
		subject = "Welcome to " + l.Address()
	}
	if body == "" {
		body = fmt.Sprintf("You are now subscribed to %s.\n\n"+
			"To change your delivery preferences or unsubscribe, visit %s/me.\n", l.Address(), p.WebBaseURL)
	}
	subject = renderNoticeTemplate(subject, l.Address(), sub.Email, p.WebBaseURL, "")
	body = renderNoticeTemplate(body, l.Address(), sub.Email, p.WebBaseURL, "")
	return p.Store.Enqueue(ctx, l.ID, l.Address(), sub.Email,
		buildNotice(l.Address(), sub.Email, l.Address()+"-owner@"+l.Domain, subject, body),
		l.Address(), "")
}

func (p *Pipeline) enqueueGoodbye(ctx context.Context, l *model.List, sub *model.Subscriber) error {
	subject := l.Settings.GoodbyeSubject
	body := l.Settings.GoodbyeBody
	if subject == "" {
		subject = "You have been unsubscribed from " + l.Address()
	}
	if body == "" {
		body = fmt.Sprintf("You have been unsubscribed from %s.\n\n"+
			"If this was a mistake, you can resubscribe at %s.\n", l.Address(), p.WebBaseURL)
	}
	subject = renderNoticeTemplate(subject, l.Address(), sub.Email, p.WebBaseURL, "")
	body = renderNoticeTemplate(body, l.Address(), sub.Email, p.WebBaseURL, "")
	return p.Store.Enqueue(ctx, l.ID, l.Address(), sub.Email,
		buildNotice(l.Address(), sub.Email, l.Address()+"-owner@"+l.Domain, subject, body),
		l.Address(), "")
}

// renderNoticeTemplate substitutes the owner-customized notice placeholders:
// {list} (list address), {email} (recipient), {url} (web UI base), and
// {subject} (only meaningful for the sender-held notice).
func renderNoticeTemplate(template, listAddr, email, baseURL, subject string) string {
	return strings.NewReplacer(
		"{list}", listAddr,
		"{email}", email,
		"{url}", baseURL,
		"{subject}", subject,
	).Replace(template)
}

func (p *Pipeline) enqueueSubscriptionRejected(ctx context.Context, l *model.List, sub *model.Subscriber) error {
	bodyText := fmt.Sprintf("Your subscription request to %s was not approved by the list owners.\n", l.Address())
	return p.Store.Enqueue(ctx, l.ID, l.Address(), sub.Email,
		buildNotice(l.Address(), sub.Email, l.Address()+"-owner@"+l.Domain, "Your subscription request to "+l.Address()+" was not approved", bodyText),
		l.Address(), "")
}
