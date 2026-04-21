package clock_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func TestReal_Now(t *testing.T) {
	t.Parallel()

	c := clock.Real()
	before := time.Now()
	got := c.Now()
	after := time.Now()

	assert.False(t, got.Before(before))
	assert.False(t, got.After(after))
}

func TestReal_AfterFires(t *testing.T) {
	t.Parallel()

	c := clock.Real()
	select {
	case <-c.After(5 * time.Millisecond):
	case <-time.After(1 * time.Second):
		t.Fatal("real After did not fire")
	}
}

func TestFake_Now(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	assert.Equal(t, epoch, f.Now())
	f.Advance(3 * time.Second)
	assert.Equal(t, epoch.Add(3*time.Second), f.Now())
}

func TestFake_AfterFiresExactly(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)
	ch := f.After(100 * time.Millisecond)

	// No firing yet.
	select {
	case <-ch:
		t.Fatal("After fired before its wake time")
	default:
	}

	// Below the threshold: still no fire.
	f.Advance(50 * time.Millisecond)

	select {
	case <-ch:
		t.Fatal("After fired before its wake time")
	default:
	}

	// At the threshold: fires.
	f.Advance(50 * time.Millisecond)

	select {
	case got := <-ch:
		assert.Equal(t, epoch.Add(100*time.Millisecond), got)
	default:
		t.Fatal("After did not fire at its wake time")
	}
}

func TestFake_AfterZeroFiresImmediately(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)
	ch := f.After(0)

	select {
	case <-ch:
	default:
		t.Fatal("After(0) did not fire immediately")
	}
}

func TestFake_TickerFiresAtInterval(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	tk := f.NewTicker(10 * time.Millisecond)
	defer tk.Stop()

	// Before first interval: no tick.
	f.Advance(5 * time.Millisecond)

	select {
	case <-tk.C():
		t.Fatal("ticker fired before first interval")
	default:
	}

	// Crossing the first interval: one tick.
	f.Advance(5 * time.Millisecond)

	select {
	case <-tk.C():
	default:
		t.Fatal("ticker did not fire at first interval")
	}

	// Crossing the second interval: another tick.
	f.Advance(10 * time.Millisecond)

	select {
	case <-tk.C():
	default:
		t.Fatal("ticker did not fire at second interval")
	}
}

func TestFake_TickerCoalescesMultipleIntervals(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	tk := f.NewTicker(10 * time.Millisecond)
	defer tk.Stop()

	// Skip past 5 intervals in one Advance. Because the channel buffer is 1
	// we get at most one tick queued (matches stdlib coalescing).
	f.Advance(55 * time.Millisecond)

	select {
	case <-tk.C():
	default:
		t.Fatal("ticker did not fire after multi-interval advance")
	}

	// Buffer drained; no residual tick.
	select {
	case <-tk.C():
		t.Fatal("ticker fired twice after multi-interval advance (should coalesce)")
	default:
	}

	// Ticker should still work after the big jump — advance to the next
	// scheduled tick and verify it fires.
	f.Advance(10 * time.Millisecond)

	select {
	case <-tk.C():
	default:
		t.Fatal("ticker did not fire after catching up schedule")
	}
}

func TestFake_TickerStop(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)
	tk := f.NewTicker(10 * time.Millisecond)

	tk.Stop()
	f.Advance(100 * time.Millisecond)

	select {
	case <-tk.C():
		t.Fatal("stopped ticker fired")
	default:
	}
}

func TestFake_MultipleAfterConcurrent(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	var wg sync.WaitGroup

	var fired atomic.Int32

	const n = 20
	for i := 0; i < n; i++ {
		wg.Add(1)

		d := time.Duration(i+1) * 10 * time.Millisecond

		go func() {
			defer wg.Done()

			<-f.After(d)
			fired.Add(1)
		}()
	}

	// Wait deterministically for all n goroutines to have registered their
	// timers with the fake clock. Uses PendingTimers(), the clock's own view
	// of registered work — no wall-clock wait.
	require.Eventually(t, func() bool {
		return f.PendingTimers() == n
	}, 2*time.Second, time.Millisecond, "goroutines did not register timers")

	f.Advance(5 * time.Second) // well past all timers

	require.Eventually(t, func() bool { return fired.Load() == int32(n) }, 2*time.Second, time.Millisecond)
	wg.Wait()
}

func TestFake_SleepBlocksUntilAdvance(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	done := make(chan struct{})

	go func() {
		f.Sleep(100 * time.Millisecond)
		close(done)
	}()

	// Wait deterministically for the goroutine to register its Sleep timer.
	require.Eventually(t, func() bool {
		return f.PendingTimers() == 1
	}, 2*time.Second, time.Millisecond, "Sleep did not register timer")

	select {
	case <-done:
		t.Fatal("Sleep returned before Advance")
	default:
	}

	f.Advance(100 * time.Millisecond)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Sleep did not unblock after Advance")
	}
}

func TestFake_TickerStopRemovesFromRegistry(t *testing.T) {
	t.Parallel()

	// Smoke test: create many tickers, stop them all, advance, and confirm
	// no ticks are delivered on any channel.
	f := clock.Fake(epoch)

	const n = 10

	ticks := make([]clock.Ticker, n)
	for i := range ticks {
		ticks[i] = f.NewTicker(10 * time.Millisecond)
	}

	for _, tk := range ticks {
		tk.Stop()
	}

	f.Advance(1 * time.Second)

	for _, tk := range ticks {
		select {
		case <-tk.C():
			t.Fatal("stopped ticker fired")
		default:
		}
	}
}

func TestFake_NewTickerPanicsOnNonPositive(t *testing.T) {
	t.Parallel()

	f := clock.Fake(epoch)

	assert.Panics(t, func() { f.NewTicker(0) })
	assert.Panics(t, func() { f.NewTicker(-1) })
}
