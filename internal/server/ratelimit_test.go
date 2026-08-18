package server

import (
	"testing"
	"time"
)

func TestKeyedRateLimiterAllow(t *testing.T) {
	l := newKeyedRateLimiter(2, time.Hour) // burst 2, 2/hour refill
	for i := 1; i <= 2; i++ {
		if !l.allow("k") {
			t.Fatalf("allow %d = false, want true (within burst)", i)
		}
	}
	if l.allow("k") {
		t.Fatal("allow 3 = true, want false (burst exhausted)")
	}
	if l.allow("k") {
		t.Fatal("allow 4 = true, want false")
	}
}

func TestKeyedRateLimiterIndependentKeys(t *testing.T) {
	l := newKeyedRateLimiter(1, time.Hour)
	if !l.allow("a") {
		t.Fatal("allow a = false, want true")
	}
	if l.allow("a") {
		t.Fatal("allow a 2 = true, want false")
	}
	if !l.allow("b") {
		t.Fatal("allow b = false, want true (independent key)")
	}
}

func TestKeyedRateLimiterReserveConsume(t *testing.T) {
	l := newKeyedRateLimiter(3, time.Hour) // burst 3
	// Three "successful sends" (reserve, never cancel) exhaust the bucket.
	for i := 0; i < 3; i++ {
		r := l.reserve("a@example.com")
		if r.Delay() > 0 {
			t.Fatalf("reserve %d over quota, want token available", i+1)
		}
	}
	// A fourth request is over quota.
	r := l.reserve("a@example.com")
	if r.Delay() <= 0 {
		t.Fatal("reserve 4 not over quota, want denial")
	}
	r.Cancel()
	// Canceling a denied reservation gives nothing back: still over quota.
	if r := l.reserve("a@example.com"); r.Delay() <= 0 {
		t.Fatal("reserve after canceling a denied reservation not over quota")
	}
}

func TestKeyedRateLimiterCancelReturnsToken(t *testing.T) {
	l := newKeyedRateLimiter(3, time.Hour)
	r := l.reserve("b@example.com")
	if r.Delay() > 0 {
		t.Fatalf("reserve 1 over quota, want token available")
	}
	r.Cancel() // e.g., unknown address: no mail actually sent
	if r := l.reserve("b@example.com"); r.Delay() > 0 {
		t.Fatal("canceled reservation did not return its token")
	}
}

func TestKeyedRateLimiterRetryAfter(t *testing.T) {
	l := newKeyedRateLimiter(2, time.Hour)
	if d := l.retryAfter("k"); d != 0 {
		t.Fatalf("retryAfter on fresh key = %v, want 0", d)
	}
	l.allow("k")
	l.allow("k")
	if d := l.retryAfter("k"); d <= 0 {
		t.Fatalf("retryAfter after exhaustion = %v, want > 0", d)
	}
}

func TestKeyedRateLimiterSweep(t *testing.T) {
	l := newKeyedRateLimiter(3, time.Hour)
	l.allow("stale")
	l.mu.Lock()
	l.buckets["stale"].lastSeen = time.Now().Add(-2 * time.Hour)
	l.mu.Unlock()
	l.sweepLocked(time.Now())
	if n := l.keyCount(); n != 0 {
		t.Fatalf("keyCount after sweep = %d, want 0", n)
	}
}
