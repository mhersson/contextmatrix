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
// SetRehydrationActive. All other methods delegate directly to the inner store.
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

func (f *failingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	if f.failNextSetRehydration.CompareAndSwap(true, false) {
		return errors.New("injected: SetRehydrationActive failure")
	}

	return f.Store.SetRehydrationActive(ctx, sessionID, active)
}

// Remaining Store methods delegate to the inner store via embedding, so we
// only need explicit overrides for methods where we inject faults.
// These thin wrappers exist to satisfy the interface with pointer receiver:

func (f *failingStore) CreateSession(ctx context.Context, s chat.Session) error {
	return f.Store.CreateSession(ctx, s)
}

func (f *failingStore) GetSession(ctx context.Context, id string) (chat.Session, error) {
	return f.Store.GetSession(ctx, id)
}

func (f *failingStore) ListSessions(ctx context.Context, filter chat.SessionFilter) ([]chat.Session, error) {
	return f.Store.ListSessions(ctx, filter)
}

func (f *failingStore) UpdateSession(ctx context.Context, s chat.Session) error {
	return f.Store.UpdateSession(ctx, s)
}

func (f *failingStore) DeleteSession(ctx context.Context, id string) error {
	return f.Store.DeleteSession(ctx, id)
}

func (f *failingStore) UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error {
	return f.Store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt)
}

func (f *failingStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	return f.Store.AppendMessage(ctx, m)
}

func (f *failingStore) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]chat.Message, error) {
	return f.Store.ListMessages(ctx, sessionID, sinceSeq, limit)
}

func (f *failingStore) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]chat.Message, error) {
	return f.Store.ListMessagesTail(ctx, sessionID, limit)
}

func (f *failingStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	return f.Store.MaxSeq(ctx, sessionID)
}

func (f *failingStore) MarkAllMessagesRehydrationPhase(ctx context.Context, sessionID string) (int64, error) {
	return f.Store.MarkAllMessagesRehydrationPhase(ctx, sessionID)
}

func (f *failingStore) Close() error {
	return f.Store.Close()
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

// Explicit interface delegation (needed because trackingStore has a pointer
// receiver on UpdateSession, which shadows the embedded interface).

func (ts *trackingStore) CreateSession(ctx context.Context, s chat.Session) error {
	return ts.Store.CreateSession(ctx, s)
}

func (ts *trackingStore) GetSession(ctx context.Context, id string) (chat.Session, error) {
	return ts.Store.GetSession(ctx, id)
}

func (ts *trackingStore) ListSessions(ctx context.Context, filter chat.SessionFilter) ([]chat.Session, error) {
	return ts.Store.ListSessions(ctx, filter)
}

func (ts *trackingStore) DeleteSession(ctx context.Context, id string) error {
	return ts.Store.DeleteSession(ctx, id)
}

func (ts *trackingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	return ts.Store.SetRehydrationActive(ctx, sessionID, active)
}

func (ts *trackingStore) UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error {
	return ts.Store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt)
}

func (ts *trackingStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	return ts.Store.AppendMessage(ctx, m)
}

func (ts *trackingStore) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]chat.Message, error) {
	return ts.Store.ListMessages(ctx, sessionID, sinceSeq, limit)
}

func (ts *trackingStore) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]chat.Message, error) {
	return ts.Store.ListMessagesTail(ctx, sessionID, limit)
}

func (ts *trackingStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	return ts.Store.MaxSeq(ctx, sessionID)
}

func (ts *trackingStore) MarkAllMessagesRehydrationPhase(ctx context.Context, sessionID string) (int64, error) {
	return ts.Store.MarkAllMessagesRehydrationPhase(ctx, sessionID)
}

func (ts *trackingStore) Close() error {
	return ts.Store.Close()
}

// yieldingStore wraps a real chat.Store and inserts a randomised sleep
// after every SetRehydrationActive. SQLite's UPDATE is heavyweight
// relative to the trivial cache write that follows in setRehydrationActive,
// so the natural race window between the two writes is tight enough to
// hide a store/cache ordering bug under -race -count. The variable
// sleeps scatter cache writes out of the order their preceding store
// writes committed in — the schedule that exposes the regression.
type yieldingStore struct {
	chat.Store
	rng atomic.Int64
}

func (y *yieldingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	err := y.Store.SetRehydrationActive(ctx, sessionID, active)
	// xorshift-style scramble of an atomic counter gives each call a
	// different sleep duration without pulling in math/rand and without
	// introducing a shared mutex. Range is [0, ~2ms). Single-millisecond
	// jitter is what reliably surfaces the bug — narrower windows are
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

// Explicit interface delegation — pointer-receiver SetRehydrationActive
// would otherwise shadow only that method while the rest fall through to
// the embedded chat.Store. Listing every method keeps go vet happy and
// mirrors the failingStore/trackingStore pattern.

func (y *yieldingStore) CreateSession(ctx context.Context, s chat.Session) error {
	return y.Store.CreateSession(ctx, s)
}

func (y *yieldingStore) GetSession(ctx context.Context, id string) (chat.Session, error) {
	return y.Store.GetSession(ctx, id)
}

func (y *yieldingStore) ListSessions(ctx context.Context, filter chat.SessionFilter) ([]chat.Session, error) {
	return y.Store.ListSessions(ctx, filter)
}

func (y *yieldingStore) UpdateSession(ctx context.Context, s chat.Session) error {
	return y.Store.UpdateSession(ctx, s)
}

func (y *yieldingStore) DeleteSession(ctx context.Context, id string) error {
	return y.Store.DeleteSession(ctx, id)
}

func (y *yieldingStore) UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error {
	return y.Store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt)
}

func (y *yieldingStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	return y.Store.AppendMessage(ctx, m)
}

func (y *yieldingStore) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]chat.Message, error) {
	return y.Store.ListMessages(ctx, sessionID, sinceSeq, limit)
}

func (y *yieldingStore) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]chat.Message, error) {
	return y.Store.ListMessagesTail(ctx, sessionID, limit)
}

func (y *yieldingStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	return y.Store.MaxSeq(ctx, sessionID)
}

func (y *yieldingStore) MarkAllMessagesRehydrationPhase(ctx context.Context, sessionID string) (int64, error) {
	return y.Store.MarkAllMessagesRehydrationPhase(ctx, sessionID)
}

func (y *yieldingStore) Close() error {
	return y.Store.Close()
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
// Safe to call before any goroutine has reached the gate — the close happens
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
// other methods delegate to the embedded store. Modelled on yieldingStore and
// failingStore — explicit method overrides for every Store method because the
// pointer-receiver AppendMessage would otherwise shadow only that one and
// leave the rest fall-through, which go vet flags as ambiguous.
type gatingStore struct {
	chat.Store
	gate *sessionGate
}

func (g *gatingStore) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	g.gate.wait(m.SessionID)

	return g.Store.AppendMessage(ctx, m)
}

func (g *gatingStore) CreateSession(ctx context.Context, s chat.Session) error {
	return g.Store.CreateSession(ctx, s)
}

func (g *gatingStore) GetSession(ctx context.Context, id string) (chat.Session, error) {
	return g.Store.GetSession(ctx, id)
}

func (g *gatingStore) ListSessions(ctx context.Context, filter chat.SessionFilter) ([]chat.Session, error) {
	return g.Store.ListSessions(ctx, filter)
}

func (g *gatingStore) UpdateSession(ctx context.Context, s chat.Session) error {
	return g.Store.UpdateSession(ctx, s)
}

func (g *gatingStore) DeleteSession(ctx context.Context, id string) error {
	return g.Store.DeleteSession(ctx, id)
}

func (g *gatingStore) SetRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	return g.Store.SetRehydrationActive(ctx, sessionID, active)
}

func (g *gatingStore) UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error {
	return g.Store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt)
}

func (g *gatingStore) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]chat.Message, error) {
	return g.Store.ListMessages(ctx, sessionID, sinceSeq, limit)
}

func (g *gatingStore) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]chat.Message, error) {
	return g.Store.ListMessagesTail(ctx, sessionID, limit)
}

func (g *gatingStore) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	return g.Store.MaxSeq(ctx, sessionID)
}

func (g *gatingStore) MarkAllMessagesRehydrationPhase(ctx context.Context, sessionID string) (int64, error) {
	return g.Store.MarkAllMessagesRehydrationPhase(ctx, sessionID)
}

func (g *gatingStore) Close() error {
	return g.Store.Close()
}
