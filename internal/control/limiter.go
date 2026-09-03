package control

import (
	"sync"
	"time"
)

// limiter counts failures per key inside a sliding window and, past a
// threshold, refuses attempts until an exponential backoff has passed.
// It is used for logins (per user) and enrollments (per remote IP).
type limiter struct {
	now       func() time.Time
	window    time.Duration
	threshold int
	maxDelay  time.Duration

	mu      sync.Mutex
	entries map[string]*limitEntry
}

type limitEntry struct {
	failures []time.Time
	last     time.Time
}

func newLimiter(now func() time.Time, window time.Duration, threshold int, maxDelay time.Duration) *limiter {
	return &limiter{now: now, window: window, threshold: threshold, maxDelay: maxDelay, entries: map[string]*limitEntry{}}
}

// allow reports whether key may attempt now.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	now := l.now()
	e.prune(now.Add(-l.window))
	n := len(e.failures)
	if n < l.threshold {
		return true
	}
	delay := time.Duration(1<<uint(min(n-l.threshold, 30))) * time.Second
	if delay > l.maxDelay {
		delay = l.maxDelay
	}
	return now.Sub(e.last) >= delay
}

// fail records a failed attempt for key.
func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok {
		e = &limitEntry{}
		l.entries[key] = e
	}
	e.prune(now.Add(-l.window))
	e.failures = append(e.failures, now)
	e.last = now
	// Drop stale keys so the map does not grow without bound.
	for k, other := range l.entries {
		if k != key && other.last.Before(now.Add(-l.window)) {
			delete(l.entries, k)
		}
	}
}

// reset clears the record for key after a success.
func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (e *limitEntry) prune(cutoff time.Time) {
	i := 0
	for i < len(e.failures) && e.failures[i].Before(cutoff) {
		i++
	}
	e.failures = e.failures[i:]
}

// rateLimit allows at most max attempts per key inside a sliding window.
type rateLimit struct {
	now    func() time.Time
	window time.Duration
	max    int

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newRateLimit(now func() time.Time, window time.Duration, max int) *rateLimit {
	return &rateLimit{now: now, window: window, max: max, attempts: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it is within the limit.
func (r *rateLimit) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-r.window)
	kept := r.attempts[key][:0]
	for _, t := range r.attempts[key] {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	for k, ts := range r.attempts {
		if k != key && (len(ts) == 0 || ts[len(ts)-1].Before(cutoff)) {
			delete(r.attempts, k)
		}
	}
	if len(kept) >= r.max {
		r.attempts[key] = kept
		return false
	}
	r.attempts[key] = append(kept, now)
	return true
}
