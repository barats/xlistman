// Package sqlite implements the store.Store interface using GORM with SQLite.
package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// onConflictDoNothing is a GORM clause for SQLite ON CONFLICT DO NOTHING.
var onConflictDoNothing = clause.OnConflict{DoNothing: true}

// Store implements store.Store using GORM with SQLite.
type Store struct {
	db *gorm.DB
}

// Open opens a SQLite database at path and auto-migrates the schema.
func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return s, nil
}

// OpenInMemory opens an in-memory SQLite database (for tests).
func OpenInMemory() (*Store, error) {
	return Open(":memory:")
}

func (s *Store) migrate() error {
	if err := s.db.AutoMigrate(
		&model.Domain{},
		&model.List{},
		&model.Subscriber{},
		&model.Subscription{},
		&model.Owner{},
		&model.Moderator{},
		&model.DesignatedSender{},
		&model.HeldMessage{},
		&model.QueuedMessage{},
		&model.ArchiveEntry{},
		&model.ConfirmationToken{},
		&model.MagicLink{},
		&model.Session{},
	); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}

	// FTS5 virtual table for archive full-text search (not managed by AutoMigrate).
	// Uses a standalone FTS table (not external-content) for simplicity and
	// reliability. The ArchiveMessage method inserts into both tables.
	if err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS archive_fts USING fts5(
		subject, body_text
	)`).Error; err != nil {
		return fmt.Errorf("create archive_fts: %w", err)
	}

	return nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// populateDomain fills the Domain field by looking up the domain name.
func (s *Store) populateDomain(ctx context.Context, l *model.List) {
	if l == nil {
		return
	}
	var d model.Domain
	if err := s.db.WithContext(ctx).First(&d, l.DomainID).Error; err == nil {
		l.Domain = d.Name
	}
}

// --- Domain operations ---

func (s *Store) CreateDomain(ctx context.Context, name, description string) (*model.Domain, error) {
	d := model.Domain{Name: name, Description: description}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return nil, fmt.Errorf("create domain: %w", err)
	}
	return &d, nil
}

func (s *Store) GetDomain(ctx context.Context, name string) (*model.Domain, error) {
	var d model.Domain
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&d).Error; err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}
	return &d, nil
}

func (s *Store) ListDomains(ctx context.Context) ([]model.Domain, error) {
	var domains []model.Domain
	if err := s.db.WithContext(ctx).Order("name").Find(&domains).Error; err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	return domains, nil
}

func (s *Store) DeleteDomain(ctx context.Context, name string) error {
	return s.db.WithContext(ctx).Where("name = ?", name).Delete(&model.Domain{}).Error
}

// --- List operations ---

func (s *Store) CreateList(ctx context.Context, listName string, domainID int64, domainName, description string, listType model.ListType) (*model.List, error) {
	settings := model.DefaultListSettings(listType)
	settings.SubjectPrefix = "[" + listName + "]"
	l := model.List{
		ListName:    listName,
		DomainID:    domainID,
		Description: description,
		ListType:    listType,
		Settings:    settings,
	}
	if err := s.db.WithContext(ctx).Create(&l).Error; err != nil {
		return nil, fmt.Errorf("create list: %w", err)
	}
	l.Domain = domainName
	return &l, nil
}

func (s *Store) GetList(ctx context.Context, listName, domainName string) (*model.List, error) {
	var l model.List
	err := s.db.WithContext(ctx).
		Joins("JOIN domains ON domains.id = lists.domain_id").
		Where("lists.list_name = ? AND domains.name = ?", listName, domainName).
		First(&l).Error
	if err != nil {
		return nil, fmt.Errorf("get list: %w", err)
	}
	s.populateDomain(ctx, &l)
	return &l, nil
}

func (s *Store) GetListByID(ctx context.Context, id int64) (*model.List, error) {
	var l model.List
	if err := s.db.WithContext(ctx).First(&l, id).Error; err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}
	s.populateDomain(ctx, &l)
	return &l, nil
}

func (s *Store) ListLists(ctx context.Context, domainName string) ([]model.List, error) {
	var lists []model.List
	query := s.db.WithContext(ctx).Joins("JOIN domains ON domains.id = lists.domain_id")
	if domainName != "" {
		query = query.Where("domains.name = ?", domainName)
	}
	if err := query.Order("domains.name, lists.list_name").Find(&lists).Error; err != nil {
		return nil, fmt.Errorf("list lists: %w", err)
	}
	for i := range lists {
		s.populateDomain(ctx, &lists[i])
	}
	return lists, nil
}

func (s *Store) DeleteList(ctx context.Context, listName, domainName string) error {
	return s.db.WithContext(ctx).
		Where("list_name = ? AND domain_id = (SELECT id FROM domains WHERE name = ?)",
			listName, domainName).
		Delete(&model.List{}).Error
}

func (s *Store) UpdateListSettings(ctx context.Context, listID int64, settings model.ListSettings) error {
	var l model.List
	if err := s.db.WithContext(ctx).First(&l, listID).Error; err != nil {
		return err
	}
	l.Settings = settings
	return s.db.WithContext(ctx).Save(&l).Error
}

func (s *Store) UpdateListDescription(ctx context.Context, listID int64, description string) error {
	return s.db.WithContext(ctx).Model(&model.List{}).Where("id = ?", listID).
		Update("description", description).Error
}

// --- Subscriber operations ---

func (s *Store) GetOrCreateSubscriber(ctx context.Context, email string) (*model.Subscriber, error) {
	var sub model.Subscriber
	result := s.db.WithContext(ctx).Where("email = ?", email).First(&sub)
	if result.Error == nil {
		return &sub, nil
	}
	if result.Error != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("get subscriber: %w", result.Error)
	}

	sub = model.Subscriber{Email: email}
	if err := s.db.WithContext(ctx).Create(&sub).Error; err != nil {
		return nil, fmt.Errorf("create subscriber: %w", err)
	}
	return &sub, nil
}

func (s *Store) GetSubscriber(ctx context.Context, email string) (*model.Subscriber, error) {
	var sub model.Subscriber
	if err := s.db.WithContext(ctx).Where("email = ?", email).First(&sub).Error; err != nil {
		return nil, fmt.Errorf("get subscriber: %w", err)
	}
	return &sub, nil
}

func (s *Store) GetSubscriberByID(ctx context.Context, id int64) (*model.Subscriber, error) {
	var sub model.Subscriber
	if err := s.db.WithContext(ctx).First(&sub, id).Error; err != nil {
		return nil, fmt.Errorf("get subscriber by id: %w", err)
	}
	return &sub, nil
}

// --- Subscription operations ---

func (s *Store) CreateSubscription(ctx context.Context, listID, subscriberID int64) (*model.Subscription, error) {
	sub := model.Subscription{
		ListID:       listID,
		SubscriberID: subscriberID,
		DeliveryMode: model.DeliveryModeRegular,
		Status:       model.SubscriptionStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(&sub).Error; err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return &sub, nil
}

func (s *Store) GetSubscription(ctx context.Context, listID, subscriberID int64) (*model.Subscription, error) {
	var sub model.Subscription
	if err := s.db.WithContext(ctx).Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).First(&sub).Error; err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return &sub, nil
}

func (s *Store) GetSubscriptionByID(ctx context.Context, id int64) (*model.Subscription, error) {
	var sub model.Subscription
	if err := s.db.WithContext(ctx).First(&sub, id).Error; err != nil {
		return nil, fmt.Errorf("get subscription by id: %w", err)
	}
	return &sub, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, listID int64) ([]model.Subscription, error) {
	var subs []model.Subscription
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).Find(&subs).Error; err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	return subs, nil
}

func (s *Store) ListSubscriptionsBySubscriber(ctx context.Context, subscriberID int64) ([]model.Subscription, error) {
	var subs []model.Subscription
	if err := s.db.WithContext(ctx).Where("subscriber_id = ?", subscriberID).Find(&subs).Error; err != nil {
		return nil, fmt.Errorf("list subscriptions by subscriber: %w", err)
	}
	return subs, nil
}

func (s *Store) UpdateSubscriptionDelivery(ctx context.Context, subID int64, mode model.DeliveryMode) error {
	return s.db.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", subID).
		Update("delivery_mode", mode).Error
}

func (s *Store) SetSubscriptionStatus(ctx context.Context, subID int64, status model.SubscriptionStatus) error {
	return s.db.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", subID).
		Update("status", status).Error
}

// ConfirmSubscription marks a subscription confirmed: it sets Status and stamps
// ConfirmedAt in a single update.
func (s *Store) ConfirmSubscription(ctx context.Context, subID int64, status model.SubscriptionStatus) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", subID).
		Updates(map[string]any{"status": status, "confirmed_at": now}).Error
}

func (s *Store) IncrementBounceCount(ctx context.Context, subID int64) error {
	return s.db.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", subID).
		UpdateColumn("bounce_count", gorm.Expr("bounce_count + 1")).Error
}

func (s *Store) DeleteSubscription(ctx context.Context, listID, subscriberID int64) error {
	return s.db.WithContext(ctx).Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).
		Delete(&model.Subscription{}).Error
}

// --- Owner operations ---

func (s *Store) AddOwner(ctx context.Context, listID, subscriberID int64) error {
	owner := model.Owner{ListID: listID, SubscriberID: subscriberID}
	return s.db.WithContext(ctx).Clauses(onConflictDoNothing).Create(&owner).Error
}

func (s *Store) RemoveOwner(ctx context.Context, listID, subscriberID int64) error {
	return s.db.WithContext(ctx).Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).
		Delete(&model.Owner{}).Error
}

func (s *Store) ListOwners(ctx context.Context, listID int64) ([]model.Owner, error) {
	var owners []model.Owner
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).Find(&owners).Error; err != nil {
		return nil, err
	}
	return owners, nil
}

// ListOwnerLists returns the Owner rows for the lists a Subscriber owns.
func (s *Store) ListOwnerLists(ctx context.Context, subscriberID int64) ([]model.Owner, error) {
	var owners []model.Owner
	if err := s.db.WithContext(ctx).Where("subscriber_id = ?", subscriberID).Find(&owners).Error; err != nil {
		return nil, err
	}
	return owners, nil
}

func (s *Store) IsOwner(ctx context.Context, listID, subscriberID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Owner{}).
		Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).Count(&count).Error
	return count > 0, err
}

// --- Moderator operations ---

func (s *Store) AddModerator(ctx context.Context, listID, subscriberID int64) error {
	m := model.Moderator{ListID: listID, SubscriberID: subscriberID}
	return s.db.WithContext(ctx).Clauses(onConflictDoNothing).Create(&m).Error
}

func (s *Store) RemoveModerator(ctx context.Context, listID, subscriberID int64) error {
	return s.db.WithContext(ctx).Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).
		Delete(&model.Moderator{}).Error
}

func (s *Store) ListModerators(ctx context.Context, listID int64) ([]model.Moderator, error) {
	var mods []model.Moderator
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).Find(&mods).Error; err != nil {
		return nil, err
	}
	return mods, nil
}

// ListModeratorLists returns the Moderator rows for the lists a Subscriber
// moderates.
func (s *Store) ListModeratorLists(ctx context.Context, subscriberID int64) ([]model.Moderator, error) {
	var mods []model.Moderator
	if err := s.db.WithContext(ctx).Where("subscriber_id = ?", subscriberID).Find(&mods).Error; err != nil {
		return nil, err
	}
	return mods, nil
}

func (s *Store) IsModerator(ctx context.Context, listID, subscriberID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Moderator{}).
		Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).Count(&count).Error
	return count > 0, err
}

// --- Designated sender operations ---

func (s *Store) AddDesignatedSender(ctx context.Context, listID, subscriberID int64) error {
	ds := model.DesignatedSender{ListID: listID, SubscriberID: subscriberID}
	return s.db.WithContext(ctx).Clauses(onConflictDoNothing).Create(&ds).Error
}

func (s *Store) RemoveDesignatedSender(ctx context.Context, listID, subscriberID int64) error {
	return s.db.WithContext(ctx).Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).
		Delete(&model.DesignatedSender{}).Error
}

func (s *Store) ListDesignatedSenders(ctx context.Context, listID int64) ([]model.DesignatedSender, error) {
	var senders []model.DesignatedSender
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).Find(&senders).Error; err != nil {
		return nil, err
	}
	return senders, nil
}

func (s *Store) IsDesignatedSender(ctx context.Context, listID, subscriberID int64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.DesignatedSender{}).
		Where("list_id = ? AND subscriber_id = ?", listID, subscriberID).Count(&count).Error
	return count > 0, err
}

// --- Held message operations ---

func (s *Store) CreateHeldMessage(ctx context.Context, listID int64, sender, subject string, body []byte, expiresAt time.Time) (*model.HeldMessage, error) {
	m := model.HeldMessage{
		ListID:     listID,
		Token:      generateToken(),
		Sender:     sender,
		Subject:    subject,
		Body:       body,
		ReceivedAt: time.Now(),
		ExpiresAt:  expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("create held message: %w", err)
	}
	return &m, nil
}

func (s *Store) ListHeldMessages(ctx context.Context, listID int64) ([]model.HeldMessage, error) {
	var msgs []model.HeldMessage
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).Order("received_at").Find(&msgs).Error; err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *Store) GetHeldMessageByToken(ctx context.Context, token string) (*model.HeldMessage, error) {
	var m model.HeldMessage
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) GetHeldMessageByID(ctx context.Context, id int64) (*model.HeldMessage, error) {
	var m model.HeldMessage
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteHeldMessage(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.HeldMessage{}, id).Error
}

// DeleteExpiredHeldMessages removes held messages past their expiry and
// returns how many were removed.
func (s *Store) DeleteExpiredHeldMessages(ctx context.Context, now time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&model.HeldMessage{})
	return res.RowsAffected, res.Error
}

// DeleteExpiredMagicLinks removes magic links past their expiry and returns
// how many were removed.
func (s *Store) DeleteExpiredMagicLinks(ctx context.Context, now time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&model.MagicLink{})
	return res.RowsAffected, res.Error
}

// DeleteExpiredSessions removes sessions past their expiry and returns how
// many were removed.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&model.Session{})
	return res.RowsAffected, res.Error
}

// --- Queue operations ---

func (s *Store) Enqueue(ctx context.Context, listID int64, from, to string, body []byte, envelopeSender, originalSender string) error {
	q := model.QueuedMessage{
		ListID:         listID,
		From:           from,
		To:             to,
		Body:           body,
		EnvelopeSender: envelopeSender,
		OriginalSender: originalSender,
		NextAttempt:    time.Now(),
	}
	return s.db.WithContext(ctx).Create(&q).Error
}

// ClaimNextQueued atomically claims the next available queue item using
// a transaction (multi-instance-safe pattern).
func (s *Store) ClaimNextQueued(ctx context.Context, now time.Time) (*model.QueuedMessage, error) {
	var q model.QueuedMessage
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_at IS NULL AND next_attempt <= ?", now).
			Order("next_attempt").First(&q).Error; err != nil {
			return err
		}
		return tx.Model(&model.QueuedMessage{}).Where("id = ? AND claimed_at IS NULL", q.ID).
			Update("claimed_at", now).Error
	})
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim queued: %w", err)
	}
	return &q, nil
}

func (s *Store) MarkQueuedSent(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.QueuedMessage{}, id).Error
}

func (s *Store) RequeueWithBackoff(ctx context.Context, id int64, nextAttempt time.Time) error {
	return s.db.WithContext(ctx).Model(&model.QueuedMessage{}).Where("id = ?", id).
		Updates(map[string]any{
			"claimed_at":   nil,
			"retries":      gorm.Expr("retries + 1"),
			"next_attempt": nextAttempt,
		}).Error
}

func (s *Store) ListQueued(ctx context.Context) ([]model.QueuedMessage, error) {
	var items []model.QueuedMessage
	if err := s.db.WithContext(ctx).Order("next_attempt").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) DiscardQueued(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.QueuedMessage{}, id).Error
}

func (s *Store) QueueDepth(ctx context.Context) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.QueuedMessage{}).
		Where("claimed_at IS NULL").Count(&count).Error
	return int(count), err
}

// --- Archive operations ---

func (s *Store) ArchiveMessage(ctx context.Context, listID int64, msgID, subject, from string, body []byte, threadID string) error {
	e := model.ArchiveEntry{
		ListID:     listID,
		MessageID:  msgID,
		Subject:    subject,
		From:       from,
		Body:       body,
		ThreadID:   threadID,
		ReceivedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		return err
	}
	// Insert into FTS table for full-text search.
	return s.db.WithContext(ctx).Exec(
		`INSERT INTO archive_fts (rowid, subject, body_text) VALUES (?, ?, ?)`,
		e.ID, subject, string(body),
	).Error
}

func (s *Store) ListArchive(ctx context.Context, listID int64, limit, offset int) ([]model.ArchiveEntry, error) {
	var entries []model.ArchiveEntry
	if err := s.db.WithContext(ctx).Where("list_id = ?", listID).
		Order("received_at DESC").Limit(limit).Offset(offset).Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// ListArchiveSince returns archive entries for a list received after `since`,
// in chronological order (used by the digest worker).
func (s *Store) ListArchiveSince(ctx context.Context, listID int64, since time.Time) ([]model.ArchiveEntry, error) {
	var entries []model.ArchiveEntry
	if err := s.db.WithContext(ctx).Where("list_id = ? AND received_at > ?", listID, since).
		Order("received_at ASC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) SearchArchive(ctx context.Context, listID int64, query string, limit int) ([]model.ArchiveEntry, error) {
	var entries []model.ArchiveEntry
	err := s.db.WithContext(ctx).Raw(`
		SELECT a.* FROM archive_entries a
		JOIN archive_fts f ON a.id = f.rowid
		WHERE a.list_id = ? AND archive_fts MATCH ?
		ORDER BY a.received_at DESC LIMIT ?`,
		listID, query, limit).Scan(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("search archive: %w", err)
	}
	return entries, nil
}

func (s *Store) GetArchiveEntry(ctx context.Context, id int64) (*model.ArchiveEntry, error) {
	var e model.ArchiveEntry
	if err := s.db.WithContext(ctx).First(&e, id).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

// --- Digest operations ---

// AdvanceDigestWatermark moves a list's digest watermark to `to` only if it is
// still `from` (or still nil), returning whether the claim succeeded.
func (s *Store) AdvanceDigestWatermark(ctx context.Context, listID int64, from *time.Time, to time.Time) (bool, error) {
	query := s.db.WithContext(ctx).Model(&model.List{}).Where("id = ?", listID)
	if from == nil {
		query = query.Where("last_digest_sent_at IS NULL")
	} else {
		query = query.Where("last_digest_sent_at <= ?", from)
	}
	res := query.Updates(map[string]any{"last_digest_sent_at": to})
	return res.RowsAffected > 0, res.Error
}

// --- Confirmation token operations ---

func (s *Store) CreateConfirmationToken(ctx context.Context, listID, subscriberID int64, email string, expiresAt time.Time) (string, error) {
	token := generateToken()
	t := model.ConfirmationToken{
		Token:        token,
		ListID:       listID,
		SubscriberID: subscriberID,
		Email:        email,
		ExpiresAt:    expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&t).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) GetConfirmationToken(ctx context.Context, token string) (*model.ConfirmationToken, error) {
	var t model.ConfirmationToken
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) DeleteConfirmationToken(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&model.ConfirmationToken{}).Error
}

// --- Magic link operations ---

func (s *Store) CreateMagicLink(ctx context.Context, subscriberID int64, email string, expiresAt time.Time) (string, error) {
	token := generateToken()
	m := model.MagicLink{
		Token:        token,
		SubscriberID: subscriberID,
		Email:        email,
		ExpiresAt:    expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) GetMagicLink(ctx context.Context, token string) (*model.MagicLink, error) {
	var m model.MagicLink
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteMagicLink(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&model.MagicLink{}).Error
}

// --- Session operations ---

func (s *Store) CreateSession(ctx context.Context, subscriberID int64, email string, expiresAt time.Time) (string, error) {
	id := generateToken()
	sess := model.Session{
		ID:           id,
		SubscriberID: subscriberID,
		Email:        email,
		ExpiresAt:    expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*model.Session, error) {
	var sess model.Session
	if err := s.db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Session{}).Error
}

// --- helpers ---

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
