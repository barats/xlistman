package model

import "time"

// Subscriber is a verified email address known to xMailman.
type Subscriber struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Email     string `gorm:"uniqueIndex;not null"`
	Name      string `gorm:"not null;default:''"`
	CreatedAt time.Time
}
