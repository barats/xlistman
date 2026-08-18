package model

// WebSettings is the instance-wide web access control state (ADR 0020). A
// single row (ID 1) holds the two switches; both default to enabled. The
// server reads them per request, so a CLI toggle takes effect immediately
// with no restart, and the state is shared by all instances on one database.
type WebSettings struct {
	ID                int64 `gorm:"primaryKey"`
	LoginEnabled      bool  `gorm:"not null;default:true"`
	ManagementEnabled bool  `gorm:"not null;default:true"`
}
