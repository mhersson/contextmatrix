package backend

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupGuardClaim(t *testing.T) {
	t.Run("first claim wins, immediate repeat is suppressed", func(t *testing.T) {
		g := newCleanupGuard(time.Minute)

		assert.True(t, g.claim("proj", "C-001"), "the first round owns the card")
		assert.False(t, g.claim("proj", "C-001"), "the duplicate round must be suppressed")
	})

	t.Run("different cards and projects do not collide", func(t *testing.T) {
		g := newCleanupGuard(time.Minute)

		require.True(t, g.claim("proj", "C-001"))
		assert.True(t, g.claim("proj", "C-002"), "a different card is a different round")
		assert.True(t, g.claim("other", "C-001"), "the same card ID in another project is unrelated")
	})

	t.Run("a claim past the TTL is allowed again", func(t *testing.T) {
		// A genuine later cleanup - the reconcile sweep minutes afterwards, or a
		// card that becomes terminal a second time - must never be suppressed.
		now := time.Now()
		g := newCleanupGuard(5 * time.Second)
		g.now = func() time.Time { return now }

		require.True(t, g.claim("proj", "C-001"))
		require.False(t, g.claim("proj", "C-001"), "still inside the window")

		now = now.Add(5 * time.Second)

		assert.True(t, g.claim("proj", "C-001"), "the window has elapsed, so the round may proceed")
	})

	t.Run("a nil guard claims everything", func(t *testing.T) {
		var g *cleanupGuard

		assert.True(t, g.claim("proj", "C-001"), "an unset guard must not suppress cleanup")
		assert.True(t, g.claim("proj", "C-001"))
	})

	t.Run("expired entries are pruned rather than accumulating", func(t *testing.T) {
		now := time.Now()
		g := newCleanupGuard(5 * time.Second)
		g.now = func() time.Time { return now }

		for i := range 100 {
			require.True(t, g.claim("proj", "C-"+strconv.Itoa(i)))
		}

		require.Len(t, g.seen, 100)

		now = now.Add(time.Minute)

		require.True(t, g.claim("proj", "fresh"))

		assert.Len(t, g.seen, 1, "the expired entries are dropped on the next claim; seen=%v", g.seen)
	})

	t.Run("concurrent claims on one card elect exactly one winner", func(t *testing.T) {
		// This is the shape of the real bug: two subscriber goroutines handling
		// CardStateChanged and CardReleased for the same completion.
		g := newCleanupGuard(time.Minute)

		const racers = 16

		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			wins int
		)

		start := make(chan struct{})

		for range racers {
			wg.Go(func() {
				<-start

				if g.claim("proj", "C-001") {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			})
		}

		close(start)
		wg.Wait()

		assert.Equal(t, 1, wins, "exactly one of %d concurrent rounds may proceed", racers)
	})
}
