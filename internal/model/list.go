package model

import (
	"reflect"
	"time"
)

// ListSettings holds per-list configuration. All fields have sensible defaults
// applied when a list is created. Serialized as JSON in the database.
type ListSettings struct {
	// Discussion: moderation toggle. Newsletter: unused.
	ModerationEnabled bool `json:"moderation_enabled"`

	// Subject prefix, e.g., "[dev]". Empty string means no prefix.
	SubjectPrefix string `json:"subject_prefix"`

	// Whether to append a footer to delivered messages.
	FooterEnabled bool `json:"footer_enabled"`

	// Maximum message size in bytes. 0 means no limit (but see global default).
	MaxMessageSize int64 `json:"max_message_size"`

	// Whether the list accepts attachments (ADR 0025).
	AllowAttachments bool `json:"allow_attachments"`

	// Maximum size of a single attachment in bytes. 0 means no limit.
	MaxAttachmentSize int64 `json:"max_attachment_size"`

	// Archive retention in days. 0 means unlimited.
	ArchiveMaxAgeDays int `json:"archive_max_age_days"`

	// Digest frequency for this list.
	DigestFrequency DigestFrequency `json:"digest_frequency"`

	// How subscription requests are handled.
	SubscriptionPolicy SubscriptionPolicy `json:"subscription_policy"`

	// Reply-To behavior for delivered posts.
	ReplyToMode    ReplyToMode `json:"reply_to_mode"`
	ReplyToAddress string      `json:"reply_to_address"` // used when ReplyToMode is "specified"

	// Notification toggles.
	WelcomeEmail           bool `json:"welcome_email"`
	GoodbyeEmail           bool `json:"goodbye_email"`
	SenderHeldNotice       bool `json:"sender_held_notice"`
	OwnerAutoDisableNotice bool `json:"owner_auto_disable_notice"`

	// Bounce handling: consecutive bounces before auto-disable.
	BounceThreshold int `json:"bounce_threshold"`

	// Held message expiry in days.
	HeldExpiryDays int `json:"held_expiry_days"`
}

// DefaultListSettings returns settings with sensible defaults for a new list.
func DefaultListSettings(listType ListType) ListSettings {
	s := ListSettings{
		SubjectPrefix:          "", // set by caller based on list name
		FooterEnabled:          true,
		MaxMessageSize:         0, // no enforced total cap by default (ADR 0025)
		AllowAttachments:       true,
		MaxAttachmentSize:      0, // no per-attachment limit
		ArchiveMaxAgeDays:      0, // unlimited
		DigestFrequency:        DigestDaily,
		SubscriptionPolicy:     SubscriptionPolicyOpen,
		ReplyToMode:            ReplyToList,
		WelcomeEmail:           true,
		GoodbyeEmail:           true,
		SenderHeldNotice:       true,
		OwnerAutoDisableNotice: false,
		BounceThreshold:        5,
		HeldExpiryDays:         14,
	}

	if listType == ListTypeDiscussion {
		s.ModerationEnabled = false
	}

	return s
}

// ChangedFrom returns the JSON names of the settings fields that differ
// between the receiver and old, used for settings.update audit trail detail.
func (s ListSettings) ChangedFrom(old ListSettings) []string {
	ov := reflect.ValueOf(old)
	cv := reflect.ValueOf(s)
	ot := ov.Type()
	var changed []string
	for i := 0; i < ot.NumField(); i++ {
		if reflect.DeepEqual(ov.Field(i).Interface(), cv.Field(i).Interface()) {
			continue
		}
		name := ot.Field(i).Tag.Get("json")
		if name == "" {
			name = ot.Field(i).Name
		}
		changed = append(changed, name)
	}
	return changed
}

// List is a mailing list, identified by (listname, domain).
type List struct {
	ID          int64        `gorm:"primaryKey;autoIncrement"`
	ListName    string       `gorm:"not null;index:idx_list_domain,unique"`
	DomainID    int64        `gorm:"not null;index:idx_list_domain,unique"`
	Description string       `gorm:"not null;default:''"`
	ListType    ListType     `gorm:"not null;default:'discussion'"`
	Settings    ListSettings `gorm:"serializer:json"`
	CreatedAt   time.Time
	// LastDigestSentAt is the digest watermark: archive posts after this
	// timestamp are candidates for the next digest. Nil means no digest has
	// been sent yet.
	LastDigestSentAt *time.Time

	// Domain is populated via join, not stored directly.
	Domain string `gorm:"-"`
}

// Address returns the full list email address, e.g., "dev@example.com".
func (l List) Address() string {
	return l.ListName + "@" + l.Domain
}
