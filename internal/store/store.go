// Package store defines the storage interface for xListman.
//
// The Store interface is the primary seam for testing. All domain operations
// go through this interface, allowing the SQLite implementation to be swapped
// for a different backend (e.g., PostgreSQL) in the future.
package store

import (
	"context"
	"time"

	"github.com/barats/xlistman/internal/model"
)

// Store is the storage interface for all xListman domain operations.
type Store interface {
	// Domain operations
	CreateDomain(ctx context.Context, name, description string) (*model.Domain, error)
	GetDomain(ctx context.Context, name string) (*model.Domain, error)
	ListDomains(ctx context.Context) ([]model.Domain, error)
	DeleteDomain(ctx context.Context, name string) error

	// List operations
	CreateList(ctx context.Context, listName string, domainID int64, domainName, description string, listType model.ListType) (*model.List, error)
	GetList(ctx context.Context, listName, domainName string) (*model.List, error)
	GetListByID(ctx context.Context, id int64) (*model.List, error)
	ListLists(ctx context.Context, domainName string) ([]model.List, error)
	DeleteList(ctx context.Context, listName, domainName string) error
	UpdateListSettings(ctx context.Context, listID int64, settings model.ListSettings) error

	// Subscriber operations
	GetOrCreateSubscriber(ctx context.Context, email string) (*model.Subscriber, error)
	GetSubscriber(ctx context.Context, email string) (*model.Subscriber, error)
	GetSubscriberByID(ctx context.Context, id int64) (*model.Subscriber, error)

	// Subscription operations
	CreateSubscription(ctx context.Context, listID, subscriberID int64) (*model.Subscription, error)
	GetSubscription(ctx context.Context, listID, subscriberID int64) (*model.Subscription, error)
	ListSubscriptions(ctx context.Context, listID int64) ([]model.Subscription, error)
	ListSubscriptionsBySubscriber(ctx context.Context, subscriberID int64) ([]model.Subscription, error)
	UpdateSubscriptionDelivery(ctx context.Context, subID int64, mode model.DeliveryMode) error
	SetSubscriptionStatus(ctx context.Context, subID int64, status model.SubscriptionStatus) error
	ConfirmSubscription(ctx context.Context, subID int64, status model.SubscriptionStatus) error
	IncrementBounceCount(ctx context.Context, subID int64) error
	DeleteSubscription(ctx context.Context, listID, subscriberID int64) error

	// Owner operations
	AddOwner(ctx context.Context, listID, subscriberID int64) error
	RemoveOwner(ctx context.Context, listID, subscriberID int64) error
	ListOwners(ctx context.Context, listID int64) ([]model.Owner, error)
	IsOwner(ctx context.Context, listID, subscriberID int64) (bool, error)

	// Moderator operations
	AddModerator(ctx context.Context, listID, subscriberID int64) error
	RemoveModerator(ctx context.Context, listID, subscriberID int64) error
	ListModerators(ctx context.Context, listID int64) ([]model.Moderator, error)
	IsModerator(ctx context.Context, listID, subscriberID int64) (bool, error)

	// Designated sender operations
	AddDesignatedSender(ctx context.Context, listID, subscriberID int64) error
	RemoveDesignatedSender(ctx context.Context, listID, subscriberID int64) error
	ListDesignatedSenders(ctx context.Context, listID int64) ([]model.DesignatedSender, error)
	IsDesignatedSender(ctx context.Context, listID, subscriberID int64) (bool, error)

	// Held message operations
	CreateHeldMessage(ctx context.Context, listID int64, sender, subject string, body []byte, expiresAt time.Time) (*model.HeldMessage, error)
	ListHeldMessages(ctx context.Context, listID int64) ([]model.HeldMessage, error)
	GetHeldMessageByToken(ctx context.Context, token string) (*model.HeldMessage, error)
	GetHeldMessageByID(ctx context.Context, id int64) (*model.HeldMessage, error)
	DeleteHeldMessage(ctx context.Context, id int64) error
	DeleteExpiredHeldMessages(ctx context.Context, now time.Time) (int64, error)

	// Queue operations
	Enqueue(ctx context.Context, listID int64, from, to string, body []byte, envelopeSender string) error
	ClaimNextQueued(ctx context.Context, now time.Time) (*model.QueuedMessage, error)
	MarkQueuedSent(ctx context.Context, id int64) error
	RequeueWithBackoff(ctx context.Context, id int64, nextAttempt time.Time) error
	ListQueued(ctx context.Context) ([]model.QueuedMessage, error)
	DiscardQueued(ctx context.Context, id int64) error
	QueueDepth(ctx context.Context) (int, error)

	// Archive operations
	ArchiveMessage(ctx context.Context, listID int64, msgID, subject, from string, body []byte, threadID string) error
	ListArchive(ctx context.Context, listID int64, limit, offset int) ([]model.ArchiveEntry, error)
	SearchArchive(ctx context.Context, listID int64, query string, limit int) ([]model.ArchiveEntry, error)
	GetArchiveEntry(ctx context.Context, id int64) (*model.ArchiveEntry, error)

	// Confirmation token operations
	CreateConfirmationToken(ctx context.Context, listID, subscriberID int64, email string, expiresAt time.Time) (string, error)
	GetConfirmationToken(ctx context.Context, token string) (*model.ConfirmationToken, error)
	DeleteConfirmationToken(ctx context.Context, token string) error

	// Magic link operations
	CreateMagicLink(ctx context.Context, subscriberID int64, email string, expiresAt time.Time) (string, error)
	GetMagicLink(ctx context.Context, token string) (*model.MagicLink, error)
	DeleteMagicLink(ctx context.Context, token string) error

	// Session operations
	CreateSession(ctx context.Context, subscriberID int64, email string, expiresAt time.Time) (string, error)
	GetSession(ctx context.Context, id string) (*model.Session, error)
	DeleteSession(ctx context.Context, id string) error

	// Database maintenance
	Close() error
}
