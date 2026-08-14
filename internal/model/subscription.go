package model

import "time"

// Subscription is the relationship between a Subscriber and a List that causes
// posts to be delivered to that address while it is Active.
type Subscription struct {
	ID           int64              `gorm:"primaryKey;autoIncrement"`
	ListID       int64              `gorm:"not null;index:idx_sub_list_sub,unique"`
	SubscriberID int64              `gorm:"not null;index:idx_sub_list_sub,unique;index"`
	DeliveryMode DeliveryMode       `gorm:"not null;default:'regular'"`
	Status       SubscriptionStatus `gorm:"not null;default:'pending'"`
	BounceCount  int                `gorm:"not null;default:0"`
	ConfirmedAt  *time.Time
	CreatedAt    time.Time
}
