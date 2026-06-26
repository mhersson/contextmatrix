package runner_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// TestReconciliationSweep_TerminalCardKillsContainer is the core fix: the
// runner reports a live container whose CM card is already `done`. The sweep
// must kill it, regardless of the card's runner_status field (which we no
// longer consult — that was the source of the old silent-skip bug).
func TestReconciliationSweep_TerminalCardKillsContainer(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:    "C-001",
			State: "done",
			// runner_status is deliberately set to "completed" — a value
			// the OLD sweep would have silently skipped. The new sweep
			// does not read this field.
			RunnerStatus:  "completed",
			AssignedAgent: "",
		},
	}}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "C-001",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

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
		listResult: []runner.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "C-010",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-2 * time.Minute),
			},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(), "sweep must not kill in-progress card's container")
}

// TestReconciliationSweep_MissingCardKillsContainer catches the "card was
// deleted but container still runs" case — e.g. a project-wide delete that
// bypassed the normal cleanup path. Without this rule, such a container
// would leak to the runner's 2h timeout.
func TestReconciliationSweep_MissingCardKillsContainer(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{
				ContainerID: "abc123",
				CardID:      "ORPHAN-001",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

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

	origMax := runner.ContainerMaxAge
	runner.ContainerMaxAge = 10 * time.Millisecond

	t.Cleanup(func() { runner.ContainerMaxAge = origMax })

	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-002": {ID: "C-002", State: "in_progress"},
	}}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
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

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

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
		listResult: []runner.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 0, discardLogger())

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
		listResult: []runner.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	// Interval well above the assertion deadline — if the first sweep waits
	// for the ticker, waitForKillCalls will time out.
	runner.StartReconciliationSweep(ctx, cg, nil, fc, 10*time.Second, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_RunnerListFailureSkipsTick is the transient-error
// contract: if the runner is briefly unreachable, the sweep must NOT treat
// an empty list as "kill nothing this tick and move on" — the actual
// failure is a skip, not a false-negative kill.
func TestReconciliationSweep_RunnerListFailureSkipsTick(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{listErr: errors.New("runner unreachable")}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	// Not kill and not panic — the ListContainers error just skips the tick.
	assert.Empty(t, fc.KillCalls(), "no kills on runner list failure")
}

// TestReconciliationSweep_MissingClient_NoPanic guards the nil-dependency
// path: main.go wiring must not crash the process if the runner client was
// not constructed (runner disabled in config).
func TestReconciliationSweep_MissingClient_NoPanic(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{}

	runner.StartReconciliationSweep(ctx, cg, nil, nil, 30*time.Millisecond, discardLogger())
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
		listResult: []runner.ContainerInfo{
			{ContainerID: "abc", CardID: "C-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

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
		listResult: []runner.ContainerInfo{
			{ContainerID: "abc", CardID: "GONE-001", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

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
		listResult: []runner.ContainerInfo{
			{ContainerID: "abc", CardID: "GONE-002", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

	waitForKillCalls(t, fc, 1)
}

// TestReconciliationSweep_SkipsChatContainers guards the boundary between the
// card-mode sweep and the chat-mode sweep: after Wave 2.2, /containers also
// reports chat containers (LabelSessionID, no LabelCardID). The card sweep
// must skip those rows — calling decideKill on a chat container with an empty
// CardID would route a malformed /end-session against the runner.
func TestReconciliationSweep_SkipsChatContainers(t *testing.T) {
	ctx := t.Context()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{
				ContainerID: "chat-ctr-1",
				SessionID:   "S-active",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}

	runner.StartReconciliationSweep(ctx, cg, nil, fc, 30*time.Millisecond, discardLogger())

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, fc.KillCalls(),
		"card sweep must skip chat containers (those carry SessionID, not CardID)")
	assert.Empty(t, fc.Calls(),
		"card sweep must not call /end-session for chat containers either")
}

// fakeChatReconciler implements runner.ChatReconciler for tests.
type fakeChatReconciler struct {
	mu       sync.Mutex
	active   []chatSessionStub
	warm     []chatSessionStub
	ended    []string
	endError error
}

// chatSessionStub mirrors the subset of chat.Session that the reconcile
// sweep reads — using a local stub keeps the test file independent of the
// chat package's full type surface.
type chatSessionStub struct {
	ID     string
	Status string
}

func (f *fakeChatReconciler) ListActiveChatSessions(_ context.Context) ([]runner.ChatSessionRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]runner.ChatSessionRef, 0, len(f.active)+len(f.warm))
	for _, s := range f.active {
		out = append(out, runner.ChatSessionRef{ID: s.ID, Status: s.Status})
	}

	for _, s := range f.warm {
		out = append(out, runner.ChatSessionRef{ID: s.ID, Status: s.Status})
	}

	return out, nil
}

func (f *fakeChatReconciler) EndChatSession(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ended = append(f.ended, id)

	return f.endError
}

func (f *fakeChatReconciler) endedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.ended))
	copy(out, f.ended)

	return out
}

// TestChatReconcileSweep_FlipsOrphanToCold is the central guarantee of Wave 2.3:
// if CM thinks a chat session is active or warm-idle but the runner has no
// container for it, the sweep flips that session to cold. Without this,
// stranded sessions persist forever after a runner restart or crash.
func TestChatReconcileSweep_FlipsOrphanToCold(t *testing.T) {
	ctx := t.Context()

	fcr := &fakeChatReconciler{
		active: []chatSessionStub{
			{ID: "S-live", Status: "active"},
			{ID: "S-orphan", Status: "active"},
		},
		warm: []chatSessionStub{
			{ID: "S-warm-orphan", Status: "warm-idle"},
		},
	}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{
				ContainerID: "card-ctr",
				CardID:      "C-001",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now(),
			},
			{
				ContainerID: "chat-ctr",
				SessionID:   "S-live",
				Project:     "proj",
				State:       "running",
				StartedAt:   time.Now(),
			},
		},
	}

	runner.RunChatReconcileSweep(ctx, fcr, fc, discardLogger())

	ended := fcr.endedCalls()
	require.ElementsMatch(t, []string{"S-orphan", "S-warm-orphan"}, ended,
		"both orphan sessions must be ended; the one with a live runner container must be left alone")
}

// TestChatReconcileSweep_NoOpWhenAllSessionsHaveContainers confirms that when
// every active/warm session matches a runner container, no EndChatSession
// calls fire. Reconcile must be silent on the happy path.
func TestChatReconcileSweep_NoOpWhenAllSessionsHaveContainers(t *testing.T) {
	ctx := t.Context()

	fcr := &fakeChatReconciler{
		active: []chatSessionStub{
			{ID: "S-1", Status: "active"},
			{ID: "S-2", Status: "active"},
		},
	}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{ContainerID: "c1", SessionID: "S-1", Project: "proj", State: "running", StartedAt: time.Now()},
			{ContainerID: "c2", SessionID: "S-2", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	runner.RunChatReconcileSweep(ctx, fcr, fc, discardLogger())

	assert.Empty(t, fcr.endedCalls(),
		"happy path: every CM session has a runner container, no end calls expected")
}

// TestChatReconcileSweep_RunnerListErrorSkipsTick guards the safety property:
// a transient /containers failure must NOT cause CM to flip every chat
// session to cold. Better to skip a tick than to nuke live sessions.
func TestChatReconcileSweep_RunnerListErrorSkipsTick(t *testing.T) {
	ctx := t.Context()

	fcr := &fakeChatReconciler{
		active: []chatSessionStub{{ID: "S-1", Status: "active"}},
	}
	fc := &fakeClient{
		listErr: errors.New("runner unreachable"),
	}

	runner.RunChatReconcileSweep(ctx, fcr, fc, discardLogger())

	assert.Empty(t, fcr.endedCalls(),
		"runner-list error must skip the tick, not flip every session to cold")
}

// TestReconciliationSweep_SingleContainersFetchPerTick is the regression guard
// for the HMAC replay-cache 409 we hit in dev: card and chat sweeps were
// firing simultaneously on two tickers, each calling /containers with the
// same signed payload — the runner's replay cache rejected the second one
// as "duplicate request". Both reconcilers must now share a single
// ListContainers round-trip per tick.
func TestReconciliationSweep_SingleContainersFetchPerTick(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cg := &fakeCardGetter{cards: map[string]*board.Card{}}
	fcr := &fakeChatReconciler{
		active: []chatSessionStub{{ID: "S-1", Status: "active"}},
	}
	fc := &fakeClient{
		listResult: []runner.ContainerInfo{
			{ContainerID: "chat-ctr", SessionID: "S-1", Project: "proj", State: "running", StartedAt: time.Now()},
		},
	}

	// Long interval: only the initial tick fires within the test window,
	// so a stable assertion on ListCount==1 is possible. If both sweeps
	// fetched separately the count would be 2.
	runner.StartReconciliationSweep(ctx, cg, fcr, fc, time.Hour, discardLogger())

	require.Eventually(t, func() bool {
		return fc.ListCount() >= 1
	}, time.Second, 5*time.Millisecond, "expected initial sweep tick")

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, fc.ListCount(),
		"single tick must fetch /containers exactly once; separate fetches hit the runner's HMAC replay cache as duplicates")
}
