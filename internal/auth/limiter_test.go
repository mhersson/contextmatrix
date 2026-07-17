package auth

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Note: internal (package auth) test - the fake clock field is unexported.

func newTestLimiter(start time.Time) (*Limiter, *time.Time) {
	clock := start
	l := NewLimiter()
	l.now = func() time.Time { return clock }

	return l, &clock
}

func TestLimiter_AllowsFirstFailures(t *testing.T) {
	l, _ := newTestLimiter(time.Unix(1000, 0))

	for range 3 {
		_, ok := l.Allow("alice|1.2.3.4")
		assert.True(t, ok)
		l.Failure("alice|1.2.3.4")
	}
}

func TestLimiter_BacksOffExponentially(t *testing.T) {
	l, clock := newTestLimiter(time.Unix(1000, 0))
	key := "alice|1.2.3.4"

	// 3 failures → blocked for 1s.
	for range 3 {
		l.Failure(key)
	}

	retry, ok := l.Allow(key)
	assert.False(t, ok)
	assert.Equal(t, time.Second, retry)

	// After the block expires, one more failure doubles the next block.
	*clock = clock.Add(time.Second)

	_, ok = l.Allow(key)
	assert.True(t, ok)

	l.Failure(key)

	retry, ok = l.Allow(key)
	assert.False(t, ok)
	assert.Equal(t, 2*time.Second, retry)
}

func TestLimiter_CapsAtFiveMinutes(t *testing.T) {
	l, _ := newTestLimiter(time.Unix(1000, 0))
	key := "alice|1.2.3.4"

	for range 30 {
		l.Failure(key)
	}

	retry, ok := l.Allow(key)
	assert.False(t, ok)
	assert.Equal(t, 5*time.Minute, retry)
}

func TestLimiter_ResetClears(t *testing.T) {
	l, _ := newTestLimiter(time.Unix(1000, 0))
	key := "alice|1.2.3.4"

	for range 5 {
		l.Failure(key)
	}

	l.Reset(key)

	_, ok := l.Allow(key)
	assert.True(t, ok)
}

func TestLimiter_KeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(time.Unix(1000, 0))

	for range 10 {
		l.Failure("alice|1.2.3.4")
	}

	_, ok := l.Allow("alice|5.6.7.8")
	assert.True(t, ok, "different IP, different key")

	_, ok = l.Allow("bob|1.2.3.4")
	assert.True(t, ok, "different account, different key")
}

func TestLimiter_PrunesIdleEntries(t *testing.T) {
	l, clock := newTestLimiter(time.Unix(1000, 0))

	for i := range 1100 {
		l.Failure(fmt.Sprintf("user%d|ip", i))
	}

	*clock = clock.Add(2 * time.Hour)

	l.Failure("trigger|prune")

	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()

	assert.Less(t, n, 100, "idle entries pruned once map exceeded the cap")
}

func TestLimiter_OverflowingBackoffClampsToCap(t *testing.T) {
	l, _ := newTestLimiter(time.Unix(1000, 0))
	key := "alice|1.2.3.4"

	// Sweep the failure count through every shift region: pre-cap, capped
	// without overflow (shift 9–33), wrapped (34–54, where the shifted value
	// overflows int64 into an arbitrary signed number), and shifted-to-zero
	// (≥ 55). Every region must yield a block in (0, limiterMaxBlock].
	for i := 1; i <= 70; i++ {
		l.Failure(key)

		if i < limiterFreeFailures {
			continue
		}

		retry, ok := l.Allow(key)
		assert.False(t, ok, "failures=%d must block", i)
		assert.Greater(t, retry, time.Duration(0), "failures=%d must never yield a non-positive block", i)
		assert.LessOrEqual(t, retry, limiterMaxBlock, "failures=%d must clamp to the cap", i)
	}
}
