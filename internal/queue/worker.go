package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

// Sender delivers a single message to the MTA. *mail.SMTPClient implements it.
type Sender interface {
	Send(envelopeSender, recipient string, rawMsg []byte) error
}

// defaultMaxRetries is used when MaxRetries is left at zero.
const defaultMaxRetries = 8

// Worker processes the outbound queue, sending messages to the MTA via SMTP.
type Worker struct {
	Store      store.Store
	SMTP       Sender
	Interval   time.Duration // polling interval, default 5s
	MaxRetries int           // delivery attempts before bounce/drop, default 8
	Logger     *slog.Logger
}

// Run processes the queue until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx, time.Now(), logger)
		}
	}
}

func (w *Worker) maxRetries() int {
	if w.MaxRetries > 0 {
		return w.MaxRetries
	}
	return defaultMaxRetries
}

func (w *Worker) processBatch(ctx context.Context, now time.Time, logger *slog.Logger) {
	for {
		q, err := w.Store.ClaimNextQueued(ctx, now)
		if err != nil {
			logger.Error("claim queued message", "error", err)
			return
		}
		if q == nil {
			return // queue empty
		}

		if err := w.sendQueued(q); err != nil {
			logger.Error("send queued message", "id", q.ID, "to", q.To, "error", err)
			// This failure would exceed the attempt cap: bounce or drop.
			if q.Retries+1 >= w.maxRetries() {
				w.giveUp(ctx, q, err, logger)
				continue
			}
			// Requeue with exponential backoff.
			backoff := backoffDuration(q.Retries)
			nextAttempt := now.Add(backoff)
			if err := w.Store.RequeueWithBackoff(ctx, q.ID, nextAttempt); err != nil {
				logger.Error("requeue message", "id", q.ID, "error", err)
			}
		} else {
			w.Store.MarkQueuedSent(ctx, q.ID)
		}
	}
}

// giveUp handles a message that exhausted its retries. Post deliveries
// (which carry the poster's address) get a failure notice bounced to the
// poster; list-originated notifications are dropped with a warning. The
// queue item itself is always removed.
func (w *Worker) giveUp(ctx context.Context, q *model.QueuedMessage, sendErr error, logger *slog.Logger) {
	if q.OriginalSender != "" {
		body := buildBounceBody(q.From, q.OriginalSender, q.To, sendErr)
		// The bounce is a list-originated notice (no original sender), so a
		// failure of the bounce itself is dropped at max retries, never
		// re-bounced.
		if err := w.Store.Enqueue(ctx, q.ListID, q.From, q.OriginalSender, body, q.From, ""); err != nil {
			logger.Error("enqueue bounce", "id", q.ID, "error", err)
		} else {
			logger.Warn("bounced undeliverable post to original sender", "id", q.ID, "to", q.To, "sender", q.OriginalSender)
		}
	} else {
		logger.Warn("dropped undeliverable message after max retries", "id", q.ID, "to", q.To)
	}
	if err := w.Store.MarkQueuedSent(ctx, q.ID); err != nil {
		logger.Error("drop queue item", "id", q.ID, "error", err)
	}
}

func (w *Worker) sendQueued(q *model.QueuedMessage) error {
	if w.SMTP == nil {
		return fmt.Errorf("SMTP client not configured")
	}
	return w.SMTP.Send(q.EnvelopeSender, q.To, q.Body)
}

// buildBounceBody builds the failure notice sent to the poster of an
// undeliverable post.
func buildBounceBody(listAddr, poster, recipient string, sendErr error) []byte {
	date := time.Now().UTC().Format(time.RFC1123Z)
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Delivery failed: %s\r\nDate: %s\r\n\r\n"+
		"Your message to %s could not be delivered to %s:\n\n%v\n",
		listAddr, poster, listAddr, date, listAddr, recipient, sendErr))
}

// backoffDuration returns exponential backoff: 1m, 5m, 15m, 1h, up to 24h.
func backoffDuration(retries int) time.Duration {
	switch {
	case retries < 1:
		return 1 * time.Minute
	case retries < 2:
		return 5 * time.Minute
	case retries < 3:
		return 15 * time.Minute
	case retries < 4:
		return 1 * time.Hour
	default:
		return 24 * time.Hour
	}
}
