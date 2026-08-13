package model

import "time"

// Domain is a virtual email domain hosted by an xListman instance.
type Domain struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	Name        string    `gorm:"uniqueIndex;not null"`
	Description string    `gorm:"not null;default:''"`
	CreatedAt   time.Time
}
