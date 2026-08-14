package queue

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/textproto"
	"time"

	"github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

// DigestWorker compiles per-list digests from the archive and enqueues them
// to digest-mode subscribers. Scheduling is elapsed-based (ADR 0014): a list
// is due when its watermark is older than its frequency (24h daily, 7d
// weekly). The watermark is advanced atomically so multiple instances cannot
// send the same digest (ADR 0008).
type DigestWorker struct {
	Store    store.Store
	Interval time.Duration // polling interval, default 1m
	Logger   *slog.Logger
}

// Run checks for due digests until ctx is cancelled.
func (w *DigestWorker) Run(ctx context.Context) {
	interval := w.Interval
	if interval == 0 {
		interval = time.Minute
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
			w.processDue(ctx, time.Now(), logger)
		}
	}
}

func (w *DigestWorker) processDue(ctx context.Context, now time.Time, logger *slog.Logger) {
	lists, err := w.Store.ListLists(ctx, "")
	if err != nil {
		logger.Error("list lists for digest", "error", err)
		return
	}
	for i := range lists {
		l := &lists[i]
		if !digestDue(l, now) {
			continue
		}
		if err := w.digestList(ctx, l, now, logger); err != nil {
			logger.Error("digest list", "list", l.Address(), "error", err)
		}
	}
}

// digestDue reports whether a list's digest period has elapsed.
func digestDue(l *model.List, now time.Time) bool {
	period := digestPeriod(l.Settings.DigestFrequency)
	if period == 0 {
		return false
	}
	if l.LastDigestSentAt == nil {
		return true // never sent
	}
	return now.Sub(*l.LastDigestSentAt) >= period
}

// digestPeriod returns the elapsed period for a digest frequency: 24h daily,
// 7d weekly. Unknown frequencies return 0 (never due).
func digestPeriod(f model.DigestFrequency) time.Duration {
	switch f {
	case model.DigestWeekly:
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// digestList compiles and enqueues one list's digest. The watermark is
// claimed first so a second instance cannot also send this window; the
// trade-off is that a crash between claim and enqueue can skip a digest, but
// the posts remain in the archive (ADR 0014).
func (w *DigestWorker) digestList(ctx context.Context, l *model.List, now time.Time, logger *slog.Logger) error {
	entries, err := w.Store.ListArchiveSince(ctx, l.ID, sinceWatermark(l.LastDigestSentAt))
	if err != nil {
		return fmt.Errorf("list archive since watermark: %w", err)
	}

	claimed, err := w.Store.AdvanceDigestWatermark(ctx, l.ID, l.LastDigestSentAt, now)
	if err != nil {
		return fmt.Errorf("advance digest watermark: %w", err)
	}
	if !claimed {
		logger.Warn("digest window already claimed by another instance", "list", l.Address())
		return nil
	}

	if len(entries) == 0 {
		return nil // nothing to send; the empty window is closed by the claim
	}

	subject, mimeHeader, mimeBody, err := buildDigest(l, entries)
	if err != nil {
		return fmt.Errorf("build digest: %w", err)
	}

	subs, err := w.Store.ListSubscriptions(ctx, l.ID)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}
	for _, sub := range subs {
		if sub.Status != model.SubscriptionStatusActive || sub.DeliveryMode != model.DeliveryModeDigest {
			continue
		}
		subscriber, err := w.Store.GetSubscriberByID(ctx, sub.SubscriberID)
		if err != nil {
			continue
		}
		verpAddr, err := mail.EncodeVERP(l.Address(), subscriber.Email)
		if err != nil {
			continue
		}
		msg := buildDigestMessage(l.Address(), subscriber.Email, subject, mimeHeader, mimeBody)
		if err := w.Store.Enqueue(ctx, l.ID, l.Address(), subscriber.Email, msg, verpAddr, ""); err != nil {
			logger.Error("enqueue digest", "list", l.Address(), "to", subscriber.Email, "error", err)
			continue
		}
	}
	return nil
}

// buildDigest builds the subject, the multipart/digest Content-Type header,
// and the multipart body for a list digest: a text/plain table of contents
// followed by each archived post as a message/rfc822 part.
func buildDigest(l *model.List, entries []model.ArchiveEntry) (subject, mimeHeader string, mimeBody []byte, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	toc, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"text/plain; charset=utf-8"},
	})
	if err != nil {
		return "", "", nil, err
	}
	if _, err := fmt.Fprintf(toc, "The following %d messages were posted to %s since the last digest:\n\n", len(entries), l.Address()); err != nil {
		return "", "", nil, err
	}
	for i, e := range entries {
		if _, err := fmt.Fprintf(toc, "%3d. %s\n     %s\n", i+1, e.Subject, e.From); err != nil {
			return "", "", nil, err
		}
	}

	for _, e := range entries {
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": []string{"message/rfc822"},
		})
		if err != nil {
			return "", "", nil, err
		}
		if _, err := part.Write(e.Body); err != nil {
			return "", "", nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return "", "", nil, err
	}

	prefix := l.Settings.SubjectPrefix
	if prefix == "" {
		prefix = "[" + l.ListName + "]"
	}
	subject = fmt.Sprintf("%s Digest (%d messages)", prefix, len(entries))
	mimeHeader = "Content-Type: multipart/digest; boundary=\"" + mw.Boundary() + "\""
	return subject, mimeHeader, buf.Bytes(), nil
}

// buildDigestMessage assembles the full RFC 822 digest message for one
// subscriber, wrapping the shared multipart content in the outer headers.
func buildDigestMessage(from, to, subject, mimeHeader string, mimeBody []byte) []byte {
	date := time.Now().UTC().Format(time.RFC1123Z)
	head := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n%s\r\n",
		from, to, subject, date, mimeHeader)
	return append([]byte(head), mimeBody...)
}

// sinceWatermark returns the archive cut-off for a digest: the watermark, or
// the zero time if no digest has been sent yet.
func sinceWatermark(w *time.Time) time.Time {
	if w == nil {
		return time.Time{}
	}
	return *w
}
