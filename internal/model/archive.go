package model

import "time"

// ArchiveEntry is a stored post in a list's archive.
type ArchiveEntry struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	ListID     int64     `gorm:"not null;index"`
	MessageID  string    `gorm:"not null;default:''"`
	Subject    string    `gorm:"not null;default:''"`
	From       string    `gorm:"column:from_addr;not null;default:''"`
	Body       []byte    `gorm:"not null"`
	ThreadID   string    `gorm:"not null;default:'';index:idx_archive_thread"`
	ReceivedAt time.Time
}
