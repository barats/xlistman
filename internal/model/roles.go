package model

import "time"

// Owner is a Subscriber with administrative authority over a List.
type Owner struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	ListID       int64 `gorm:"not null;index:idx_owner_list_sub,unique"`
	SubscriberID int64 `gorm:"not null;index:idx_owner_list_sub,unique"`
	CreatedAt    time.Time
}

// Moderator is a Subscriber who can approve or reject held messages.
type Moderator struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	ListID       int64 `gorm:"not null;index:idx_mod_list_sub,unique"`
	SubscriberID int64 `gorm:"not null;index:idx_mod_list_sub,unique"`
	CreatedAt    time.Time
}

// DesignatedSender is a Subscriber authorized to post to a Newsletter list.
type DesignatedSender struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	ListID       int64 `gorm:"not null;index:idx_ds_list_sub,unique"`
	SubscriberID int64 `gorm:"not null;index:idx_ds_list_sub,unique"`
	CreatedAt    time.Time
}
