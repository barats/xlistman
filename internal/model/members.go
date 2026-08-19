package model

// MemberView is one member (or role holder) of a List, as assembled for
// export and console display: the subscriber's email, their Subscription
// state when subscribed, and every List Role they hold.
type MemberView struct {
	SubscriberID   int64
	Email          string
	SubscriptionID *int64 // nil when the Subscriber holds a role but is not subscribed
	Status         string // "" when not subscribed
	DeliveryMode   string // "" when not subscribed
	BounceCount    int
	Roles          []string
}
