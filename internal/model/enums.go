// Package model defines xListman's domain entities.
//
// These types represent the core concepts from the project's ubiquitous language
// (see CONTEXT.md). They are pure data structures annotated with GORM tags for
// persistence, with no business logic.
package model

// ListType determines who can post to a list and how posts are handled.
type ListType string

const (
	ListTypeDiscussion ListType = "discussion"
	ListTypeNewsletter ListType = "newsletter"
)

// SubscriptionPolicy determines how subscription requests are handled after
// double opt-in confirmation.
type SubscriptionPolicy string

const (
	SubscriptionPolicyOpen      SubscriptionPolicy = "open"
	SubscriptionPolicyModerated SubscriptionPolicy = "moderated"
	SubscriptionPolicyClosed    SubscriptionPolicy = "closed"
)

// SubscriptionStatus is the state of a Subscription.
type SubscriptionStatus string

const (
	SubscriptionStatusPending  SubscriptionStatus = "pending"
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusHeld     SubscriptionStatus = "held"
	SubscriptionStatusDisabled SubscriptionStatus = "disabled"
)

// DeliveryMode controls how a subscriber receives posts.
type DeliveryMode string

const (
	DeliveryModeRegular DeliveryMode = "regular"
	DeliveryModeDigest  DeliveryMode = "digest"
	DeliveryModeNomail  DeliveryMode = "nomail"
)

// ReplyToMode controls the Reply-To header on delivered posts.
type ReplyToMode string

const (
	ReplyToList      ReplyToMode = "list"
	ReplyToSender    ReplyToMode = "sender"
	ReplyToSpecified ReplyToMode = "specified"
)

// DigestFrequency controls how often digests are sent.
type DigestFrequency string

const (
	DigestDaily  DigestFrequency = "daily"
	DigestWeekly DigestFrequency = "weekly"
)
