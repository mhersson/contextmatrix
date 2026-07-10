package backend_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// TestReconciliationSweep_TerminalCardKillsContainer covers the case where the
// backend reports a live container whose CM card is already `done`. The sweep
// must kill it, regardless of the card's runner_status field, which the sweep
// does not consult — consulting it is the source of silent-skip bugs.
func TestReconciliationSweep_TerminalCardKillsContainer(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:    "C-001",
			State: "done",
			// runner_status is deliberately set to "completed" — a value
			// the sweep does not read. Gating on it would silently skip
			// this container.
			RunnerStatus:  "completed",
			AssignedAgent: "",
		},
	}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "C-001",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)

	killCalls := fc.KillCalls()
	require.Len(t, killCalls, 1)
	assert.Equal(t, "C-001", killCalls[0].CardID)
	assert.Equal(t, "proj", killCalls[0].Project)
}

// TestReconciliationSweep_SkipsNonTerminalCard confirms the sweep does NOT
// kill a container whose card is still in a working state. "in_progress"
// means the agent is legitimately running; the sweep has no business
// interrupting it.
func TestReconciliationSweep_SkipsNonTerminalCard(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-010": {ID: "C-010", State: "in_progress"},
	}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "C-010",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-2 * time.Minute),
			},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(), "sweep must not kill in-progress card's container")
}

// TestReconciliationSweep_MissingCardKillsContainer catches the "card was
// deleted but container still runs" case — e.g. a project-wide delete that
// bypassed the normal cleanup path. Without this rule, such a container
// would leak to the backend's 2h timeout.
func TestReconciliationSweep_MissingCardKillsContainer(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "ORPHAN-001",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)

	killCalls := fc.KillCalls()
	require.Len(t, killCalls, 1)
	assert.Equal(t, "ORPHAN-001", killCalls[0].CardID)
}

// TestReconciliationSweep_AgeCapKillsRunawayContainer is the last-resort
// safety net: a container whose card lookup keeps succeeding but whose card
// never transitions to terminal (pathological case — stuck state machine,
// UI bug, whatever) still gets killed once it exceeds ContainerMaxAge.
func TestReconciliationSweep_AgeCapKillsRunawayContainer(t *testing.T) {
	ctx := t.Context()

	origMax := backend.ContainerMaxAge
	backend.ContainerMaxAge = 10 * time.Millisecond

	t.Cleanup(func() { backend.ContainerMaxAge = origMax })

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-002": {ID: "C-002", State: "in_progress"},
	}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "C-002",
				Project:     "proj",
				State:       "running",
				// Well past the 10ms cap.
				StartedAt: time.Now().Add(-1 * time.Second),
			},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_ZeroIntervalDisabled keeps the opt-out contract:
// operators who don't want the sweep running (local dev, tight-loop tests)
// can set reconcile_interval=0 and get a guaranteed no-op.
func TestReconciliationSweep_ZeroIntervalDisabled(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {ID: "C-001", State: "done"},
	}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 0, discardLogger())

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(), "sweep must be a no-op at interval=0")
}

// TestReconciliationSweep_RunsImmediatelyOnStart validates that the sweep
// does not wait a full interval before its first scan. The restart-recovery
// scenario is the main reason the sweep exists — containers orphaned
// between CM shutdown and startup must be cleaned up at startup, not a
// minute later.
func TestReconciliationSweep_RunsImmediatelyOnStart(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {ID: "C-001", State: "done"},
	}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	// Interval well above the assertion deadline — if the first sweep waits
	// for the ticker, waitForKillCalls will time out.
	backend.StartReconciliationSweep(ctx, cg, fc, 10*time.Second, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_BackendListFailureSkipsTick is the transient-error
// contract: if the backend is briefly unreachable, the sweep must NOT treat
// an empty list as "kill nothing this tick and move on" — the actual
// failure is a skip, not a false-negative kill.
func TestReconciliationSweep_BackendListFailureSkipsTick(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{listErr: errors.New("backend unreachable")}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	// Not kill and not panic — the ListContainers error just skips the tick.
	assert.Empty(t, fc.KillCalls(), "no kills on backend list failure")
}

// TestReconciliationSweep_MissingClient_NoPanic guards the nil-dependency
// path: main.go wiring must not crash the process if the backend client was
// not constructed (no task backend in config).
func TestReconciliationSweep_MissingClient_NoPanic(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{}

	backend.StartReconciliationSweep(ctx, cg, nil, 30*time.Millisecond, discardLogger())
	time.Sleep(50 * time.Millisecond)
}

// TestReconciliationSweep_TransientCardErrorLeavesContainerAlone guards the
// store-outage path: an arbitrary GetCard error is NOT a positive
// "card not found" signal. If the backing store is briefly unreachable, the
// sweep must not panic-kill every container it sees.
func TestReconciliationSweep_TransientCardErrorLeavesContainerAlone(t *testing.T) {
	ctx := t.Context()

	// fakeCardGetterWithErr returns a non-"not found" error so the sweep's
	// isCardNotFound classifier leaves the container alone.
	cg := &erroringCardGetter{err: errors.New("disk gone")}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(), "transient card-store error must not trigger a kill")
}

type erroringCardGetter struct{ err error }

func (e *erroringCardGetter) GetCard(_ context.Context, _, _ string) (*board.Card, error) {
	return nil, e.err
}

// TestReconciliationSweep_StorageNotFoundErrorIsKill regression-guards the
// sentinel bug where isCardNotFound compared a local errors.New object
// against the store's own storage.ErrCardNotFound — two different instances,
// so errors.Is would return false and the "missing card" rule would never
// fire for real missing cards from the service layer.
func TestReconciliationSweep_StorageNotFoundErrorIsKill(t *testing.T) {
	ctx := t.Context()

	cg := &erroringCardGetter{err: storage.ErrCardNotFound}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{ContainerID: "abc", CardID: "GONE-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_WrappedStorageNotFoundErrorIsKill confirms the
// sentinel check survives `fmt.Errorf("get card: %w", err)` wrapping, which
// the service layer uses in adjacent paths. Without this guard, a future
// wrap of storage.ErrCardNotFound in service.GetCard would silently break
// the missing-card rule.
func TestReconciliationSweep_WrappedStorageNotFoundErrorIsKill(t *testing.T) {
	ctx := t.Context()

	cg := &erroringCardGetter{err: fmt.Errorf("get card: %w", storage.ErrCardNotFound)}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{ContainerID: "abc", CardID: "GONE-002", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_SkipsChatContainers guards the boundary between the
// card-mode sweep and the chat-mode sweep: after Wave 2.2, /containers also
// reports chat containers (LabelSessionID, no LabelCardID). The card sweep
// must skip those rows — calling decideKill on a chat container with an empty
// CardID would route a malformed /end-session against the backend.
func TestReconciliationSweep_SkipsChatContainers(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{
		listResult: []backend.ContainerInfo{
			{
				ContainerID: "chat-ctr-1",
				SessionID:   "S-active",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	backend.StartReconciliationSweep(ctx, cg, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(),
		"card sweep must skip chat containers (those carry SessionID, not CardID)")
	assert.Empty(t, fc.Calls(),
		"card sweep must not call /end-session for chat containers either")
}
