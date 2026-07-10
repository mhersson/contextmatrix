package backend_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type fakeCardGetter struct {
	mu    sync.RWMutex
	cards map[string]*board.Card
}

func (f *fakeCardGetter) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	c, ok := f.cards[project+"/"+id]
	if !ok {
		return nil, storage.ErrCardNotFound
	}
	// return a copy so the subscriber's read is isolated
	cp := *c

	return &cp, nil
}

// setAgent updates AssignedAgent under the fake's lock, so tests can safely
// mutate card state while the subscriber may be reading it concurrently.
func (f *fakeCardGetter) setAgent(project, id, agent string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if c, ok := f.cards[project+"/"+id]; ok {
		c.AssignedAgent = agent
	}
}

type fakeClient struct {
	mu         sync.Mutex
	calls      []backend.EndSessionPayload
	killCalls  []backend.KillPayload
	err        error
	killErr    error
	listResult []backend.ContainerInfo
	listErr    error
	listCount  int
}

func (f *fakeClient) EndSession(_ context.Context, p backend.EndSessionPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, p)

	return f.err
}

func (f *fakeClient) Kill(_ context.Context, p backend.KillPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.killCalls = append(f.killCalls, p)

	return f.killErr
}

// ListContainers is the authoritative backend-side input the sweep consults
// on every tick. Tests set listResult to configure the container snapshot
// the sweep sees; listErr covers the backend-unreachable path so the sweep
// can skip ticks without crashing.
func (f *fakeClient) ListContainers(_ context.Context) ([]backend.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.listCount++

	if f.listErr != nil {
		return nil, f.listErr
	}

	out := make([]backend.ContainerInfo, len(f.listResult))
	copy(out, f.listResult)

	return out, nil
}

// ListCount returns the number of ListContainers calls observed since the
// fake was created. Used by tests that need to assert a single sweep tick
// makes exactly one /containers round-trip to avoid hitting the backend's
// HMAC replay cache.
func (f *fakeClient) ListCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.listCount
}

func (f *fakeClient) Calls() []backend.EndSessionPayload {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.EndSessionPayload, len(f.calls))
	copy(out, f.calls)

	return out
}

func (f *fakeClient) KillCalls() []backend.KillPayload {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.KillPayload, len(f.killCalls))
	copy(out, f.killCalls)

	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStatusErr is an error that exposes an HTTP status code via
// HTTPStatusCode(), matching what backend.Client returns for 4xx responses.
// Used to drive the subscriber's expected-error classification path.
type fakeStatusErr struct {
	status int
	msg    string
}

func (e *fakeStatusErr) Error() string       { return e.msg }
func (e *fakeStatusErr) HTTPStatusCode() int { return e.status }

func waitForCalls(t *testing.T, fc *fakeClient, want int) {
	t.Helper()

	require.Eventually(t, func() bool {
		return len(fc.Calls()) >= want
	}, 2*time.Second, 5*time.Millisecond, "expected >= %d end-session calls", want)
}

func waitForKillCalls(t *testing.T, fc *fakeClient, want int) {
	t.Helper()

	require.Eventually(t, func() bool {
		return len(fc.KillCalls()) >= want
	}, 2*time.Second, 5*time.Millisecond, "expected >= %d kill calls", want)
}

func assertNoCall(t *testing.T, fc *fakeClient) {
	t.Helper()

	// Genuine wall-clock wait: the subscriber dispatches asynchronously via
	// the events bus, so we need a short window to confirm no call *would*
	// have fired. A fake clock cannot drive the bus's fan-out goroutine.
	time.Sleep(100 * time.Millisecond)

	assert.Empty(t, fc.Calls(), "expected no end-session calls")
	assert.Empty(t, fc.KillCalls(), "expected no kill calls")
}

func TestEndSessionSubscriber_TerminalDoneReleasedRunning_Fires(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)

	calls := fc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "C-001", calls[0].CardID)
	assert.Equal(t, "proj", calls[0].Project)

	killCalls := fc.KillCalls()
	require.Len(t, killCalls, 1, "kill must fire after end-session as a safety net")
	assert.Equal(t, "C-001", killCalls[0].CardID)
	assert.Equal(t, "proj", killCalls[0].Project)
}

func TestEndSessionSubscriber_MidWorkflow_NoCall(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "in_progress",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)
}

// TestEndSessionSubscriber_TerminalAndReleased_FiresRegardlessOfWorkerStatus
// verifies the subscriber fires on terminal + released, ignoring
// worker_status. A card whose worker_status has drifted to "" (or
// "completed", "failed") but still has a live container on the backend must
// still get a /kill — the backend's /kill is idempotent, so a spurious call
// against an already-dead container is a 200 no-op, and silent-skip bugs
// around worker_status drift are eliminated at the source.
func TestEndSessionSubscriber_TerminalAndReleased_FiresRegardlessOfWorkerStatus(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/SUB-001": {
			ID:            "SUB-001",
			State:         "done",
			AssignedAgent: "",
			// Empty worker_status — the subscriber fires anyway because
			// the card is terminal and released, which is all the truth
			// it needs.
			WorkerStatus: "",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "SUB-001"})

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}

func TestEndSessionSubscriber_StateChangedWithAgentStillSet_NoCall(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "agent-123",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)

	// Now release the card and expect a call.
	cg.setAgent("proj", "C-001", "")

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})
	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}

func TestEndSessionSubscriber_NotPlannedReleasedRunning_Fires(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "not_planned",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}

func TestEndSessionSubscriber_DoubleEvent_TwoCalls(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 2)
	waitForKillCalls(t, fc, 2)
}

func TestEndSessionSubscriber_UnrelatedEvent_NoCall(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardCreated, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)
}

func TestEndSessionSubscriber_WebhookError_NoCrash(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{err: errors.New("backend is down")}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
	// Kill is the safety net — even if end-session errors, kill must still
	// fire so a wedged container can't outlive the terminal state.
	waitForKillCalls(t, fc, 1)
}

// TestEndSessionSubscriber_EndSession409_KillStillFires verifies that a 409
// from /end-session (autonomous container — no stdin attached) doesn't
// prevent the follow-up /kill. This is the common path for pure-autonomous
// runs that reach a terminal state: there's no interactive stdin to close,
// but the container must still be terminated.
func TestEndSessionSubscriber_EndSession409_KillStillFires(t *testing.T) {
	ctx := t.Context()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			WorkerStatus:  "running",
		},
	}}
	fc := &fakeClient{err: &fakeStatusErr{status: 409, msg: "container is not in interactive mode"}}

	backend.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}
