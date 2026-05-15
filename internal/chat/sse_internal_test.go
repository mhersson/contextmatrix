package chat

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSEHub_Drop_RemovesSessionEntry exercises the explicit
// lifecycle method: once Drop fires for a sessionID, the per-session
// ring buffer + subscriber map is freed (perSess no longer holds it).
func TestSSEHub_Drop_RemovesSessionEntry(t *testing.T) {
	hub := NewSSEHub(8)
	hub.Publish("S1", SSEEvent{Seq: 1, Content: "x"})

	hub.mu.Lock()
	_, present := hub.perSess["S1"]
	hub.mu.Unlock()
	require.True(t, present, "publish must create the per-session entry")

	hub.Drop("S1")

	hub.mu.Lock()
	_, present = hub.perSess["S1"]
	hub.mu.Unlock()
	assert.False(t, present, "Drop must remove the per-session entry")
}

// TestSSEHub_Drop_ClosesLiveSubscribers ensures that a Drop while a subscriber
// is still attached closes the subscriber's channel so the SSE handler can
// exit promptly (it shouldn't be left blocked on a channel that nothing will
// ever publish to again).
func TestSSEHub_Drop_ClosesLiveSubscribers(t *testing.T) {
	hub := NewSSEHub(8)
	ch, _, _ := hub.Subscribe("S1", 0)

	hub.Drop("S1")

	// Channel must be closed (a closed channel yields the zero value with ok=false).
	_, ok := <-ch
	assert.False(t, ok, "Drop must close subscriber channels")
}

// TestSSEHub_Drop_Idempotent ensures Drop on a session that was never seen
// or has already been dropped is a no-op.
func TestSSEHub_Drop_Idempotent(t *testing.T) {
	hub := NewSSEHub(8)

	hub.Drop("never-existed") // must not panic
	hub.Publish("S1", SSEEvent{Seq: 1})
	hub.Drop("S1")
	hub.Drop("S1") // second drop is a no-op
}

// TestSSEHub_PerSessNoLeak_ManyDrops verifies that after a flurry of
// Subscribe + Publish + Drop + Unsubscribe cycles the perSess map size returns
// to zero. The earlier version of this test never exercised Subscribe before
// Drop, which made it vacuous: Unsubscribe used to lazy-create a fresh perSess
// entry, defeating Drop. The Subscribe + deferred-Unsubscribe pattern is what
// the streamChat HTTP handler actually does.
func TestSSEHub_PerSessNoLeak_ManyDrops(t *testing.T) {
	hub := NewSSEHub(8)

	for i := range 100 {
		id := "session-" + itoa(i)
		ch, _, _ := hub.Subscribe(id, 0)
		hub.Publish(id, SSEEvent{Seq: 1, Content: "x"})
		hub.Drop(id)
		// Simulate the handler's deferred Unsubscribe firing AFTER Drop closed
		// the channel. With a lazy-create Unsubscribe this resurrects perSess[id].
		hub.Unsubscribe(id, ch)
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	assert.Empty(t, hub.perSess, "perSess must be empty after every session dropped")
}

// TestSSEHub_UnsubscribeAfterDropIsNoOp verifies the specific defer-after-Drop
// pattern from the streamChat handler: Drop closes the subscriber channel and
// removes the perSess entry; the handler's deferred Unsubscribe must NOT
// resurrect the entry.
func TestSSEHub_UnsubscribeAfterDropIsNoOp(t *testing.T) {
	hub := NewSSEHub(8)
	ch, _, _ := hub.Subscribe("S-drop", 0)

	hub.Drop("S-drop")

	// Drop should have closed ch already.
	_, ok := <-ch
	require.False(t, ok, "Drop must close subscriber channel")

	hub.Unsubscribe("S-drop", ch)

	hub.mu.Lock()
	_, present := hub.perSess["S-drop"]
	hub.mu.Unlock()
	assert.False(t, present, "Unsubscribe after Drop must not resurrect the perSess entry")
}

// TestSSEHub_PublishUnsubscribeRace_NoPanic stresses Publish against concurrent
// Subscribe/Unsubscribe to catch the send-on-closed-channel race. Without the
// fix, Publish copies subscribers under the lock then sends after releasing it;
// an interleaved Unsubscribe closes the channel between those two steps and
// the subsequent send panics. With the fan-out held under the per-session lock,
// the panic is impossible.
func TestSSEHub_PublishUnsubscribeRace_NoPanic(t *testing.T) {
	hub := NewSSEHub(64)

	stop := make(chan struct{})

	var wg sync.WaitGroup

	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					hub.Publish("S-race", SSEEvent{Seq: 1, Content: "x"})
				}
			}
		}()
	}

	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					ch, _, _ := hub.Subscribe("S-race", 0)
					hub.Unsubscribe("S-race", ch)
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestSSEHub_OnLastUnsubscribeHoldsLockAgainstSubscribe is the regression
// test for the grace-timer race: a concurrent Subscribe must be blocked while
// OnLastUnsubscribe is running. Without the fix the callback fired outside
// the per-session mutex, so a fast resubscribe could fire OnSubscribe BEFORE
// the OnLastUnsubscribe that was supposed to seed the grace timer to be
// cancelled by that OnSubscribe — leaving a stale 30s timer.
func TestSSEHub_OnLastUnsubscribeHoldsLockAgainstSubscribe(t *testing.T) {
	hub := NewSSEHub(8)

	gate := make(chan struct{})
	resumed := make(chan struct{})
	subscribeFired := make(chan struct{}, 1)

	hub.OnLastUnsubscribe = func(string) {
		// Hold the per-session lock long enough for a racing Subscribe to
		// arrive. If callbacks fire outside the lock, the racing Subscribe
		// proceeds immediately and the assertion below fails.
		<-gate
		close(resumed)
	}

	var watching bool

	hub.OnSubscribe = func(string) {
		if !watching {
			return
		}

		select {
		case subscribeFired <- struct{}{}:
		default:
		}
	}

	ch, _, _ := hub.Subscribe("S1", 0)
	watching = true

	done := make(chan struct{})

	go func() {
		hub.Unsubscribe("S1", ch)
		close(done)
	}()

	// Wait until Unsubscribe is well inside OnLastUnsubscribe (a brief sleep
	// is enough; the goroutine will be parked at <-gate).
	deadline := time.NewTimer(50 * time.Millisecond)
	defer deadline.Stop()

	go func() {
		<-deadline.C
		// Racing Subscribe — it MUST block until OnLastUnsubscribe returns.
		_, _, _ = hub.Subscribe("S1", 0)
	}()

	// Give the racing Subscribe a chance to register if it weren't blocked.
	select {
	case <-subscribeFired:
		t.Fatal("Subscribe callback fired while OnLastUnsubscribe was still running — callbacks are not under the per-session lock")
	case <-time.After(100 * time.Millisecond):
		// Good — racing Subscribe is properly blocked.
	}

	close(gate)
	<-resumed
	<-done
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var (
		buf [20]byte
		pos = len(buf)
	)

	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	return string(buf[pos:])
}
