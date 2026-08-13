package model

import "time"

// ConfirmationToken is a one-time token for double opt-in confirmation.
type ConfirmationToken struct {
	Token        string    `gorm:"primaryKey"`
	ListID       int64     `gorm:"not null"`
	SubscriberID int64     `gorm:"not null"`
	Email        string    `gorm:"not null"`
	ExpiresAt    time.Time `gorm:"not null"`
	CreatedAt    time.Time
}

// MagicLink is a one-time login token sent to a subscriber's email.
type MagicLink struct {
	Token        string    `gorm:"primaryKey"`
	SubscriberID int64     `gorm:"not null"`
	Email        string    `gorm:"not null"`
	ExpiresAt    time.Time `gorm:"not null"`
	CreatedAt    time.Time
}

// Session is an authenticated web session.
type Session struct {
	ID           string    `gorm:"primaryKey"`
	SubscriberID int64     `gorm:"not null"`
	Email        string    `gorm:"not null"`
	ExpiresAt    time.Time `gorm:"not null"`
	CreatedAt    time.Time
}
