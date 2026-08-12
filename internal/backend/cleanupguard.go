package backend

import (
	"sync"
	"time"
)

const (
	// cleanupGuardTTL is how long a card's cleanup round suppresses a repeat.
	//
	// It only has to span a single round, not the backend's whole replay
	// window. The webhook signature covers a timestamp measured in whole
	// seconds, so two rounds collide in the backend's replay cache only when
	// they share the same second; rounds further apart sign differently and
	// both authenticate. Five seconds therefore covers the duplicate this
	// guard exists to stop, while leaving any genuine later cleanup - the
	// reconcile sweep minutes afterwards, or a card that becomes terminal
	// again - free to proceed.
	cleanupGuardTTL = 5 * time.Second

	// cleanupGuardMaxEntries bounds the map so a long-lived process cannot
	// accumulate one entry per card seen. Entries expire on their own; this
	// is the backstop for a burst larger than the expiry can drain.
	cleanupGuardMaxEntries = 4096
)

// cleanupGuard collapses duplicate end-session rounds for the same card.
//
// A single terminal card publishes both CardStateChanged and CardReleased, and
// the subscriber handles each in its own goroutine, so one completion produced
// two identical /end-session + /kill rounds within the same second. The second
// round signs byte-for-byte identically to the first - same key, method, URI,
// body and second-resolution timestamp - so the backend's replay cache
// correctly rejected it, logging an authentication failure indistinguishable
// from a wrong shared secret.
//
// The duplicate was always redundant: /kill is idempotent and the first round
// has already done the work. Suppressing it here keeps the backend's replay
// protection intact rather than loosening it, and keeps the retry path open -
// a round that fails leaves nothing behind once the entry expires.
type cleanupGuard struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
	now  func() time.Time
}

func newCleanupGuard(ttl time.Duration) *cleanupGuard {
	if ttl <= 0 {
		ttl = cleanupGuardTTL
	}

	return &cleanupGuard{ttl: ttl, seen: make(map[string]time.Time), now: time.Now}
}

// claim reports whether the caller owns this card's cleanup round. It returns
// false when another round claimed the same card within the TTL, which is the
// caller's signal to skip the round entirely.
func (g *cleanupGuard) claim(project, cardID string) bool {
	if g == nil {
		return true
	}

	key := project + "/" + cardID
	now := g.now()

	g.mu.Lock()
	defer g.mu.Unlock()

	if at, ok := g.seen[key]; ok && now.Sub(at) < g.ttl {
		return false
	}

	g.prune(now)

	g.seen[key] = now

	return true
}

// prune drops expired entries, then clears the map wholesale if it is still
// over the cap. Wholesale is acceptable because an over-cap map means a burst
// far larger than the TTL can drain, and the only cost of a dropped entry is
// one redundant cleanup round. Callers hold g.mu.
func (g *cleanupGuard) prune(now time.Time) {
	for k, at := range g.seen {
		if now.Sub(at) >= g.ttl {
			delete(g.seen, k)
		}
	}

	if len(g.seen) >= cleanupGuardMaxEntries {
		clear(g.seen)
	}
}
