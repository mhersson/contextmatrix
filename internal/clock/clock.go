// Package clock provides a minimal Clock abstraction that lets subsystems
// treat "wall time" as a dependency. Production code uses clock.Real(), which
// delegates to the stdlib time package. Tests can inject clock.Fake(...) and
// deterministically advance time via (*FakeClock).Advance.
//
// Only the subset of time operations actually needed by this codebase is
// exposed: Now, NewTicker, After, Sleep. Anything else should be added with
// care — every primitive must have a deterministic fake semantics.
package clock

import (
	"sync"
	"time"
)

// Clock abstracts wall time. Implementations must be safe for concurrent use.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
}

// Ticker abstracts *time.Ticker. The channel-accessor shape (C() rather than
// a struct field) is required so the fake clock can return a channel it owns.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Real returns a clock backed by the stdlib time package. All calls delegate
// directly; overhead is one interface dispatch.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (realClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (realClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }

// FakeClock is a deterministic clock for tests. Advance is the only way time
// moves forward. Timers and tickers registered via After/NewTicker fire
// synchronously from the goroutine that calls Advance.
//
// Channel design: every fake timer/ticker uses a buffered-1 channel so that
// Advance never blocks. If a ticker fires while the previous tick is still
// buffered, the new tick is dropped — this matches stdlib time.Ticker behaviour
// and means tests should drain the channel between advances if they need to
// observe each tick individually.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	tickers []*fakeTicker
}

type fakeTimer struct {
	wake time.Time
	ch   chan time.Time
	// fired is set once the timer has delivered its single tick so Advance can
	// safely skip it.
	fired bool
}

type fakeTicker struct {
	mu       sync.Mutex
	clock    *FakeClock
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  bool
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

func (t *fakeTicker) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	// Best-effort remove from the clock's registry so Advance doesn't keep
	// considering us. Not strictly required for correctness — stopped tickers
	// are also skipped at fire time — but prevents unbounded slice growth in
	// long-running tests that create/stop many tickers.
	t.clock.removeTicker(t)
}

// Fake returns a FakeClock starting at the given instant.
func Fake(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

// Now returns the fake clock's current time.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

// After returns a channel that will receive the clock's time at wake.
// The channel is buffered=1 so Advance never blocks.
func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)

	f.mu.Lock()
	wake := f.now.Add(d)

	if d <= 0 {
		// Fire immediately, matching stdlib time.After(0) behaviour.
		ch <- f.now
		f.mu.Unlock()

		return ch
	}

	f.timers = append(f.timers, &fakeTimer{wake: wake, ch: ch})
	f.mu.Unlock()

	return ch
}

// Sleep blocks until the fake clock has been advanced by at least d.
// Note: tests rarely want this — prefer structuring code so the subsystem
// under test uses After/NewTicker and is signalled via Advance. Sleep is
// provided only for completeness (Clock interface parity).
func (f *FakeClock) Sleep(d time.Duration) {
	<-f.After(d)
}

// NewTicker registers a ticker that fires every d. d must be > 0, matching
// stdlib time.NewTicker.
func (f *FakeClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock.FakeClock.NewTicker: non-positive interval")
	}

	f.mu.Lock()

	t := &fakeTicker{
		clock:    f,
		interval: d,
		next:     f.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	f.tickers = append(f.tickers, t)
	f.mu.Unlock()

	return t
}

// Advance moves the clock forward by d and fires any timers/tickers whose
// wake time is ≤ the new now. Timers fire once; tickers reschedule. If a
// ticker's interval has elapsed multiple times during d, it fires once per
// elapsed interval *up to the channel buffer capacity* (1) — extra ticks are
// coalesced, matching stdlib behaviour under slow consumers.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()

	f.now = f.now.Add(d)
	newNow := f.now

	// Fire any ready timers. Walk a copy to avoid holding the lock while
	// writing to channels (channels are buffered=1 so writes won't block,
	// but we still don't want to hold the lock across user-visible sends).
	var (
		readyTimers  []*fakeTimer
		kept         = f.timers[:0]
		readyTickers []*fakeTicker
	)

	for _, t := range f.timers {
		if t.fired {
			continue
		}

		if !t.wake.After(newNow) {
			t.fired = true

			readyTimers = append(readyTimers, t)
		} else {
			kept = append(kept, t)
		}
	}

	f.timers = kept

	for _, t := range f.tickers {
		t.mu.Lock()
		if t.stopped {
			t.mu.Unlock()

			continue
		}
		// A ticker may have missed multiple intervals during a large advance.
		// Catch up the schedule (so the next Advance uses the correct "next")
		// but only signal once because the channel is buffered=1.
		fire := false

		for !t.next.After(newNow) {
			t.next = t.next.Add(t.interval)
			fire = true
		}
		t.mu.Unlock()

		if fire {
			readyTickers = append(readyTickers, t)
		}
	}

	f.mu.Unlock()

	for _, t := range readyTimers {
		select {
		case t.ch <- newNow:
		default:
			// Buffer full — caller hasn't drained yet. Drop, matching stdlib.
		}
	}

	for _, t := range readyTickers {
		select {
		case t.ch <- newNow:
		default:
			// Tick coalesced.
		}
	}
}

// removeTicker drops t from the ticker registry. Called by fakeTicker.Stop.
// Safe to call on a ticker that is not registered.
func (f *FakeClock) removeTicker(target *fakeTicker) {
	f.mu.Lock()
	defer f.mu.Unlock()

	kept := f.tickers[:0]

	for _, t := range f.tickers {
		if t != target {
			kept = append(kept, t)
		}
	}

	f.tickers = kept
}

// PendingTimers returns the count of timers registered via After that have
// not yet fired. Tests use this to wait deterministically until a goroutine
// has reached its After call before calling Advance.
func (f *FakeClock) PendingTimers() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := 0

	for _, t := range f.timers {
		if !t.fired {
			n++
		}
	}

	return n
}
