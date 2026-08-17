package model

import "time"

// Administrator is a Subscriber with instance-wide server privileges on an
// xListman instance: creating domains and lists, managing other
// Administrators, deleting lists, and changing ListType (ADR 0017). The
// instance-wide counterpart to Owner, which is scoped to a single List.
type Administrator struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	SubscriberID int64 `gorm:"not null;uniqueIndex"`
	CreatedAt    time.Time
}
