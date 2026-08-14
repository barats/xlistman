package model

import "time"

// HeldMessage is a post awaiting moderator approval.
type HeldMessage struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	ListID     int64  `gorm:"not null;index"`
	Sender     string `gorm:"not null"`
	Subject    string `gorm:"not null;default:''"`
	Body       []byte `gorm:"not null"`
	ReceivedAt time.Time
	ExpiresAt  time.Time `gorm:"not null"`
}
