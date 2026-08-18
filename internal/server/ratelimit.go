package server

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// This file implements in-memory, per-instance rate limiting for the web API
// (ADR 0023). The limiter is a keyed token bucket: each key (a client IP or an
// email address) gets its own *rate.Limiter with a fixed refill rate and burst
// capacity. State lives only in this process — an explicit deviation from
// ADR 0008's "rate limiting counters are stored in the database" phrasing,
// because DB-backed counters would add writes to the exact database this
// protects. When xListman becomes multi-instance, rate limiting belongs in
// front (a reverse proxy), not the app DB.
type keyedRateLimiter struct {
	mu         sync.Mutex
	limit      rate.Limit
	burst      int
	pruneAfter time.Duration
	buckets    map[string]*limiterEntry
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// newKeyedRateLimiter builds a limiter for a per-hour allowance with an equal
// burst capacity (e.g., 50/hour allows 50 immediately, then refills 50/hour).
// A token bucket refills continuously between requests, so it smooths bursts
// instead of resetting at hour boundaries (no 6-in-two-minutes window edge).
func newKeyedRateLimiter(perHour int, pruneAfter time.Duration) *keyedRateLimiter {
	if perHour <= 0 {
		perHour = 1
	}
	return &keyedRateLimiter{
		limit:      rate.Every(time.Hour / time.Duration(perHour)),
		burst:      perHour,
		pruneAfter: pruneAfter,
		buckets:    make(map[string]*limiterEntry),
	}
}

func (l *keyedRateLimiter) entry(key string) *rate.Limiter {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.buckets[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = e
		if len(l.buckets) > 4096 {
			l.sweepLocked(now)
		}
	}
	e.lastSeen = now
	return e.lim
}

func (l *keyedRateLimiter) sweepLocked(now time.Time) {
	cutoff := now.Add(-l.pruneAfter)
	for k, e := range l.buckets {
		if e.lastSeen.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// allow consumes a token for key if available immediately, reporting whether
// it did. Used for per-IP flood gates that count every request.
func (l *keyedRateLimiter) allow(key string) bool {
	return l.entry(key).Allow()
}

// reserve holds a token for key for a count-on-success decision: Consume it
// once the work is actually performed, Cancel it if not (so a failed attempt
// does not count against the key).
func (l *keyedRateLimiter) reserve(key string) *rate.Reservation {
	return l.entry(key).Reserve()
}

// retryAfter reports the wait until a token is available for key (0 if one is
// available now), for the Retry-After header on 429 responses.
func (l *keyedRateLimiter) retryAfter(key string) time.Duration {
	r := l.entry(key).Reserve()
	defer r.Cancel()
	return r.Delay()
}

// keyCount reports the number of live buckets, used by tests.
func (l *keyedRateLimiter) keyCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
