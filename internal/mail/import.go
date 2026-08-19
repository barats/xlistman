package mail

import (
	"context"
	"strings"

	"github.com/barats/xlistman/internal/members"
	"github.com/barats/xlistman/internal/model"
)

// ImportMembers bulk-adds members to a list from a parsed import file (Phase
// 14). Each address is an authoritative add — GetOrCreateSubscriber then an
// Active subscription, bypassing double opt-in and the list's Subscription
// Policy — with trust carried by the caller (an Owner or server
// administrator), exactly like AddMember but in bulk.
//
// Deliberate deviations from single-row AddMember, for a bulk operation:
//   - Already-subscribed addresses are skipped, not errored; Disabled members
//     are skipped and never re-enabled (re-enable stays an explicit action).
//   - No welcome emails are sent; a migration must not flood N members.
//   - One member.import Audit Event records the counts, not N member.add
//     events.
func (p *Pipeline) ImportMembers(ctx context.Context, listName, domain string, src *members.ImportSource, actor model.AuditActor) (members.ImportResult, error) {
	l, err := p.Store.GetList(ctx, listName, domain)
	if err != nil {
		return members.ImportResult{}, err
	}

	res := members.ImportResult{Invalid: src.Invalid}
	for _, email := range src.Emails {
		email = strings.TrimSpace(strings.ToLower(email))
		if email == "" {
			res.Invalid++
			continue
		}
		sub, err := p.Store.GetOrCreateSubscriber(ctx, email)
		if err != nil {
			res.Invalid++
			continue
		}
		existing, err := p.Store.GetSubscription(ctx, l.ID, sub.ID)
		if err == nil {
			if existing.Status == model.SubscriptionStatusDisabled {
				res.Disabled++
			} else {
				res.Already++
			}
			continue
		}
		subscription, err := p.Store.CreateSubscription(ctx, l.ID, sub.ID)
		if err != nil {
			res.Invalid++
			continue
		}
		if err := p.Store.SetSubscriptionStatus(ctx, subscription.ID, model.SubscriptionStatusActive); err != nil {
			res.Invalid++
			continue
		}
		res.Added++
	}

	return res, p.recordAudit(ctx, l, model.ActionMemberImport, actor, l.Address(), res.Detail())
}
