package mail

import (
	"github.com/barats/xlistman/internal/model"
)

// PostAction is the decision made by the posting policy for an incoming message.
type PostAction int

const (
	PostActionAccept PostAction = iota // deliver to subscribers
	PostActionHold                     // store in moderation queue
	PostActionReject                   // reject with notification
)

// DecidePostAction determines what happens to a message posted to a list,
// based on the list type, moderation settings, and whether the sender is
// authorized.
//
// Discussion, moderation off: subscribers can post, non-subscribers rejected.
// Discussion, moderation on: all posts held for moderator approval.
// Newsletter: only designated senders (owners or allowlist) can post.
func DecidePostAction(l model.List, isSubscriber, isOwner, isDesignatedSender bool) PostAction {
	switch l.ListType {
	case model.ListTypeDiscussion:
		if l.Settings.ModerationEnabled {
			return PostActionHold
		}
		if isSubscriber {
			return PostActionAccept
		}
		return PostActionReject

	case model.ListTypeNewsletter:
		if isOwner || isDesignatedSender {
			return PostActionAccept
		}
		return PostActionReject
	}

	return PostActionReject
}
