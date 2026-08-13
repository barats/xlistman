package model

import "time"

// QueuedMessage is a message pending delivery to the MTA, stored in the
// database. All outbound mail (posts, digests, notifications, confirmations)
// flows through this queue.
type QueuedMessage struct {
	ID             int64     `gorm:"primaryKey;autoIncrement"`
	ListID         int64     `gorm:"not null;default:0"`
	From           string    `gorm:"column:from_addr;not null"`
	To             string    `gorm:"column:to_addr;not null"`
	Body           []byte    `gorm:"not null"`
	EnvelopeSender string    `gorm:"not null;default:''"`
	Retries        int       `gorm:"not null;default:0"`
	NextAttempt    time.Time `gorm:"not null"`
	ClaimedAt      *time.Time
	CreatedAt      time.Time
}
