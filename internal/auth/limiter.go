package auth

import (
	"sync"
	"time"
)

const (
	limiterFreeFailures = 3
	limiterMaxBlock     = 5 * time.Minute
	limiterPruneAfter   = time.Hour
	limiterPruneAbove   = 1024
)

// Limiter is an in-memory login rate limiter keyed on account+IP. Deliberately
// not persisted: an attacker who can restart the server has host access, which
// is game over anyway — and memory keeps the login path allocation-free.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

type limiterEntry struct {
	failures     int
	blockedUntil time.Time
	lastActivity time.Time
}

// NewLimiter returns a Limiter on the real clock.
func NewLimiter() *Limiter {
	return &Limiter{entries: map[string]*limiterEntry{}, now: time.Now}
}

// Allow reports whether an attempt for key may proceed. When blocked, it
// returns how long until the next attempt is accepted.
func (l *Limiter) Allow(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		return 0, true
	}

	now := l.now()
	if now.Before(e.blockedUntil) {
		return e.blockedUntil.Sub(now), false
	}

	return 0, true
}

// Failure records a failed attempt. From the third failure on, the key is
// blocked for 2^(failures-3) seconds, capped at limiterMaxBlock.
func (l *Limiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneLocked(now)

	e, ok := l.entries[key]
	if !ok {
		e = &limiterEntry{}
		l.entries[key] = e
	}

	e.failures++
	e.lastActivity = now

	if e.failures >= limiterFreeFailures {
		block := time.Second << uint(e.failures-limiterFreeFailures) //nolint:gosec // bounded below by the cap check
		if block > limiterMaxBlock || block <= 0 {
			block = limiterMaxBlock
		}

		e.blockedUntil = now.Add(block)
	}
}

// Reset clears a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.entries, key)
}

// pruneLocked drops entries idle beyond limiterPruneAfter once the map is
// large. Called with l.mu held.
func (l *Limiter) pruneLocked(now time.Time) {
	if len(l.entries) <= limiterPruneAbove {
		return
	}

	for k, e := range l.entries {
		if now.Sub(e.lastActivity) > limiterPruneAfter && now.After(e.blockedUntil) {
			delete(l.entries, k)
		}
	}
}
