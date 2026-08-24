package chat_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhersson/contextmatrix/internal/chat"
)

// failingStore wraps a real chat.Store and can inject a one-shot error on
// SetRehydrationActive. All other methods delegate to the inner store via
// embedding - only methods that inject faults need explicit overrides.
// Used only in tests.
type failingStore struct {
	chat.Store
	failNextSetRehydration atomic.Bool
}

// FailNextSetRehydration arms the one-shot fault: the next call to
// SetRehydrationActive returns an error and disarms the fault.
func (f *failingStore) FailNextSetRehydration() {
	f.failNextSetRehydration.Store(true)
}

func (f *failingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool, startedAt time.Time) error {
	if f.failNextSetRehydration.CompareAndSwap(true, false) {
		return errors.New("injected: SetRehydrationActive failure")
	}

	return f.Store.SetRehydrationActive(ctx, sessionID, active, startedAt)
}

// trackingStore wraps a real chat.Store and records the Status of every
// UpdateSession call so tests can assert on intermediate writes.
type trackingStore struct {
	chat.Store
	mu       sync.Mutex
	statuses []chat.Status
}

func (ts *trackingStore) UpdateSession(ctx context.Context, s chat.Session) error {
	ts.mu.Lock()
	ts.statuses = append(ts.statuses, s.Status)
	ts.mu.Unlock()

	return ts.Store.UpdateSession(ctx, s)
}

func (ts *trackingStore) writtenStatuses() []chat.Status {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	out := make([]chat.Status, len(ts.statuses))
	copy(out, ts.statuses)

	return out
}

// yieldingStore wraps a real chat.Store and inserts a randomised sleep
// after every SetRehydrationActive. SQLite's UPDATE is heavyweight
// relative to the trivial cache write that follows in setRehydrationActive,
// so the natural race window between the two writes is tight enough to
// hide a store/cache ordering bug under -race -count. The variable
// sleeps scatter cache writes out of the order their preceding store
// writes committed in - the schedule that exposes the regression.
type yieldingStore struct {
	chat.Store
	rng atomic.Int64
}

func (y *yieldingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool, startedAt time.Time) error {
	err := y.Store.SetRehydrationActive(ctx, sessionID, active, startedAt)
	// xorshift-style scramble of an atomic counter gives each call a
	// different sleep duration without pulling in math/rand and without
	// introducing a shared mutex. Range is [0, ~2ms). Single-millisecond
	// jitter is what reliably surfaces the bug - narrower windows are
	// hidden by SQLite serialisation and the race detector's coarser
	// scheduling.
	n := y.rng.Add(1)
	n ^= n << 13
	n ^= n >> 7
	n ^= n << 17

	if n < 0 {
		n = -n
	}

	time.Sleep(time.Duration(n%2000) * time.Microsecond)

	return err
}

// clearAtomicFailingStore wraps a real chat.Store and can inject a one-shot
// failure into ClearTranscriptAtomic. Used by
// TestClearContext_DividerFailureLeavesTranscriptClean.
type clearAtomicFailingStore struct {
	chat.Store
	failNext atomic.Bool
}

func (c *clearAtomicFailingStore) FailNext() { c.failNext.Store(true) }

func (c *clearAtomicFailingStore) ClearTranscriptAtomic(ctx context.Context, sessionID string, divider chat.Message) (int64, chat.Message, error) {
	if c.failNext.CompareAndSwap(true, false) {
		return 0, chat.Message{}, errors.New("injected: ClearTranscriptAtomic failure")
	}

	return c.Store.ClearTranscriptAtomic(ctx, sessionID, divider)
}

// sessionGate is a per-session-id blocking gate used by gatingStore. block(id)
// arms the gate so the next AppendMessage call with that id parks on a
// channel; waiting(id) reports whether a goroutine is currently parked there;
// release(id) unblocks the parked call. Designed for tests that need to assert
// two AppendMessage calls reach the store concurrently.
type sessionGate struct {
	mu     sync.Mutex
	armed  map[string]chan struct{}
	parked map[string]bool
}

func newSessionGate() *sessionGate {
	return &sessionGate{
		armed:  map[string]chan struct{}{},
		parked: map[string]bool{},
	}
}

// block arms the gate for sessionID so the next AppendMessage call for that
// id will park until release(sessionID) runs.
func (g *sessionGate) block(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.armed[sessionID] = make(chan struct{})
}

// waiting reports whether a goroutine is currently parked on the gate for
// sessionID.
func (g *sessionGate) waiting(sessionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.parked[sessionID]
}

// release closes the gate channel for sessionID, unblocking the parked call.
// Safe to call before any goroutine has reached the gate - the close happens
// before the channel receive.
func (g *sessionGate) release(sessionID string) {
	g.mu.Lock()

	ch, ok := g.armed[sessionID]

	g.mu.Unlock()

	if !ok {
		return
	}

	close(ch)
}

// wait is invoked from gatingStore.AppendMessage. If the gate is armed for
// sessionID, the call parks until release runs. Marks parked[sessionID]=true
// before parking and clears it after release.
func (g *sessionGate) wait(sessionID string) {
	g.mu.Lock()

	ch, ok := g.armed[sessionID]
	if !ok {
		g.mu.Unlock()

		return
	}

	g.parked[sessionID] = true
	g.mu.Unlock()

	<-ch

	g.mu.Lock()
	g.parked[sessionID] = false
	g.mu.Unlock()
}

// gatingStore wraps a real chat.Store and routes AppendMessage through a
// sessionGate so tests can park individual append calls per-session-id. All
// other methods delegate to the embedded store via embedding.
type gatingStore struct {
	chat.Store
	gate *sessionGate
}

func (g *gatingStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	g.gate.wait(m.SessionID)

	return g.Store.AppendMessage(ctx, m)
}

// commitThenErrorStore wraps a real chat.Store and injects a one-shot error
// on AppendMessage AFTER delegating to the inner store (which commits the
// INSERT). Simulates the modernc.org/sqlite cancellation race where
// sqlite3_step returns SQLITE_DONE but the driver overwrites the result
// with ctx.Err(). Used by TestAppendMessage_CommittedButErroredDoesNotWedge.
type commitThenErrorStore struct {
	chat.Store
	failNext atomic.Bool
}

func (c *commitThenErrorStore) FailNext() { c.failNext.Store(true) }

func (c *commitThenErrorStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	seq, err := c.Store.AppendMessage(ctx, m)
	if err != nil {
		return seq, err
	}

	if c.failNext.CompareAndSwap(true, false) {
		return seq, errors.New("injected: AppendMessage error after commit (simulated cancellation race)")
	}

	return seq, nil
}

// noopAppendStore wraps a real chat.Store and injects a one-shot error on
// AppendMessage WITHOUT delegating to the inner store. Simulates a store
// failure where the row is never persisted. Used by
// TestAppendMessage_NotCommittedAppendReusesSeq.
type noopAppendStore struct {
	chat.Store
	failNext atomic.Bool
}

func (n *noopAppendStore) FailNext() { n.failNext.Store(true) }

func (n *noopAppendStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	if n.failNext.CompareAndSwap(true, false) {
		return 0, errors.New("injected: AppendMessage error without commit")
	}

	return n.Store.AppendMessage(ctx, m)
}

// clearCommitThenErrorStore wraps a real chat.Store and injects a one-shot
// error on ClearTranscriptAtomic AFTER delegating to the inner store (which
// commits the divider). Simulates the modernc.org/sqlite cancellation race
// for the divider write path. Used by
// TestClearContext_DividerCommittedButErrored.
type clearCommitThenErrorStore struct {
	chat.Store
	failNext atomic.Bool
}

func (c *clearCommitThenErrorStore) FailNext() { c.failNext.Store(true) }

func (c *clearCommitThenErrorStore) ClearTranscriptAtomic(ctx context.Context, sessionID string, divider chat.Message) (int64, chat.Message, error) {
	marked, msg, err := c.Store.ClearTranscriptAtomic(ctx, sessionID, divider)
	if err != nil {
		return marked, msg, err
	}

	if c.failNext.CompareAndSwap(true, false) {
		return marked, msg, errors.New("injected: ClearTranscriptAtomic error after commit (simulated cancellation race)")
	}

	return marked, msg, nil
}

// commitThenErrorNoReseedStore commits the INSERT, errors on AppendMessage,
// and fails the follow-up MaxSeq so the reseed cannot heal the counter from
// disk. Exercises the fallback branch where the counter must be left ahead of
// disk rather than decremented back onto the committed row. Used by
// TestAppendMessage_ReseedFailureLeavesCounterAheadOfDisk.
type commitThenErrorNoReseedStore struct {
	chat.Store
	failNext   atomic.Bool
	failMaxSeq atomic.Bool
}

// FailNext arms both faults together: the next AppendMessage commits then
// errors, and the reseed MaxSeq that follows it fails.
func (c *commitThenErrorNoReseedStore) FailNext() {
	c.failMaxSeq.Store(true)
	c.failNext.Store(true)
}

func (c *commitThenErrorNoReseedStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	seq, err := c.Store.AppendMessage(ctx, m)
	if err != nil {
		return seq, err
	}

	if c.failNext.CompareAndSwap(true, false) {
		return seq, errors.New("injected: AppendMessage error after commit")
	}

	return seq, nil
}

func (c *commitThenErrorNoReseedStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	if c.failMaxSeq.CompareAndSwap(true, false) {
		return 0, errors.New("injected: MaxSeq failure")
	}

	return c.Store.MaxSeq(ctx, sessionID)
}
