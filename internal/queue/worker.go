package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

// Worker processes the outbound queue, sending messages to the MTA via SMTP.
type Worker struct {
	Store    store.Store
	SMTP     *mail.SMTPClient
	Interval time.Duration // polling interval, default 5s
	Logger   *slog.Logger
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
			w.processBatch(ctx, logger)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context, logger *slog.Logger) {
	for {
		q, err := w.Store.ClaimNextQueued(ctx, time.Now())
		if err != nil {
			logger.Error("claim queued message", "error", err)
			return
		}
		if q == nil {
			return // queue empty
		}

		if err := w.sendQueued(q); err != nil {
			logger.Error("send queued message", "id", q.ID, "to", q.To, "error", err)
			// Requeue with exponential backoff.
			backoff := backoffDuration(q.Retries)
			nextAttempt := time.Now().Add(backoff)
			if err := w.Store.RequeueWithBackoff(ctx, q.ID, nextAttempt); err != nil {
				logger.Error("requeue message", "id", q.ID, "error", err)
			}
		} else {
			w.Store.MarkQueuedSent(ctx, q.ID)
		}
	}
}

func (w *Worker) sendQueued(q *model.QueuedMessage) error {
	if w.SMTP == nil {
		return fmt.Errorf("SMTP client not configured")
	}
	return w.SMTP.Send(q.EnvelopeSender, q.To, q.Body)
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
