package runner_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// fakeCardLister is the CardLister implementation used by sweep tests. It
// also satisfies CardGetter so the same fixture can drive both the sweep and
// the event subscriber in tests that exercise the dropped-event scenario.
type fakeCardLister struct {
	mu       sync.RWMutex
	projects []board.ProjectConfig
	cards    map[string][]*board.Card
	listErr  map[string]error
}

func (f *fakeCardLister) ListProjects(_ context.Context) ([]board.ProjectConfig, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make([]board.ProjectConfig, len(f.projects))
	copy(out, f.projects)

	return out, nil
}

func (f *fakeCardLister) ListCards(_ context.Context, project string, _ storage.CardFilter) ([]*board.Card, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if err, ok := f.listErr[project]; ok {
		return nil, err
	}

	src := f.cards[project]
	out := make([]*board.Card, 0, len(src))

	for _, c := range src {
		cp := *c
		out = append(out, &cp)
	}

	return out, nil
}

// GetCard lets the same fake back the CardGetter used by the event
// subscriber, so the dropped-event test can run both paths against one
// fixture.
func (f *fakeCardLister) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, c := range f.cards[project] {
		if c.ID == id {
			cp := *c

			return &cp, nil
		}
	}

	return nil, errors.New("not found")
}

func TestReconciliationSweep_FindsLeakedContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{{Name: "proj"}},
		cards: map[string][]*board.Card{
			"proj": {
				{
					ID:            "C-001",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "running",
				},
			},
		},
	}
	fc := &fakeClient{}

	runner.StartReconciliationSweep(ctx, fl, fc, 30*time.Millisecond, discardLogger())

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)

	calls := fc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "C-001", calls[0].CardID)
	assert.Equal(t, "proj", calls[0].Project)

	killCalls := fc.KillCalls()
	require.Len(t, killCalls, 1)
	assert.Equal(t, "C-001", killCalls[0].CardID)
	assert.Equal(t, "proj", killCalls[0].Project)
}

func TestReconciliationSweep_SkipsActiveCards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{{Name: "proj"}},
		cards: map[string][]*board.Card{
			"proj": {
				{
					ID:            "C-010",
					State:         "in_progress",
					AssignedAgent: "agent-x",
					RunnerStatus:  "running",
				},
				{
					ID:            "C-011",
					State:         "done",
					AssignedAgent: "agent-y",
					RunnerStatus:  "running",
				},
				{
					ID:            "C-012",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "",
				},
			},
		},
	}
	fc := &fakeClient{}

	runner.StartReconciliationSweep(ctx, fl, fc, 30*time.Millisecond, discardLogger())

	// Let the sweep run a few ticks.
	time.Sleep(150 * time.Millisecond)

	assert.Empty(t, fc.Calls(), "no end-session calls expected for non-matching cards")
	assert.Empty(t, fc.KillCalls(), "no kill calls expected for non-matching cards")
}

func TestReconciliationSweep_ZeroIntervalDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{{Name: "proj"}},
		cards: map[string][]*board.Card{
			"proj": {
				{
					ID:            "C-001",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "running",
				},
			},
		},
	}
	fc := &fakeClient{}

	runner.StartReconciliationSweep(ctx, fl, fc, 0, discardLogger())

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, fc.Calls(), "sweep should be disabled at interval=0")
	assert.Empty(t, fc.KillCalls(), "sweep should be disabled at interval=0")
}

// TestReconciliationSweep_DroppedEventStillCleansUp is the scenario that
// motivates the sweep in the first place: a CardReleased event is published
// while CM is under load and the event-subscriber's buffer is already full,
// so the event is dropped. The sweep must still catch the leaked container
// on its next tick.
//
// We simulate "subscriber buffer full" by simply never starting the
// subscriber — the bus publishes with no receivers, so the subscriber path
// cannot possibly fire. The sweep is the only mechanism that can clean up.
func TestReconciliationSweep_DroppedEventStillCleansUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{{Name: "proj"}},
		cards: map[string][]*board.Card{
			"proj": {
				{
					ID:            "C-001",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "running",
				},
			},
		},
	}
	fc := &fakeClient{}

	// No subscriber is ever started; publish is a no-op for the terminate
	// path. The sweep must still kill the container.
	bus := events.NewBus()
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	runner.StartReconciliationSweep(ctx, fl, fc, 30*time.Millisecond, discardLogger())

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}

func TestReconciliationSweep_ListProjectsErrorContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{
			{Name: "broken"},
			{Name: "healthy"},
		},
		cards: map[string][]*board.Card{
			"healthy": {
				{
					ID:            "C-001",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "running",
				},
			},
		},
		listErr: map[string]error{
			"broken": errors.New("disk gone"),
		},
	}
	fc := &fakeClient{}

	runner.StartReconciliationSweep(ctx, fl, fc, 30*time.Millisecond, discardLogger())

	// A single broken project must not prevent cleanup in healthy projects.
	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)

	killCalls := fc.KillCalls()
	assert.Equal(t, "healthy", killCalls[0].Project)
	assert.Equal(t, "C-001", killCalls[0].CardID)
}

// TestReconciliationSweep_RunsImmediatelyOnStart validates that the sweep
// does not wait a full interval before its first scan — the restart-recovery
// scenario is the main reason the sweep exists, so containers orphaned
// between CM shutdown and startup must be cleaned up at startup, not a
// minute later.
func TestReconciliationSweep_RunsImmediatelyOnStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{
		projects: []board.ProjectConfig{{Name: "proj"}},
		cards: map[string][]*board.Card{
			"proj": {
				{
					ID:            "C-001",
					State:         "done",
					AssignedAgent: "",
					RunnerStatus:  "running",
				},
			},
		},
	}
	fc := &fakeClient{}

	// Interval well above the assertion deadline — if the first sweep waits
	// for the ticker, waitForCalls will time out.
	runner.StartReconciliationSweep(ctx, fl, fc, 10*time.Second, discardLogger())

	waitForCalls(t, fc, 1)
	waitForKillCalls(t, fc, 1)
}

func TestReconciliationSweep_MissingClient_NoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeCardLister{}

	// Must not start a goroutine, must not panic. Just an observable no-op.
	runner.StartReconciliationSweep(ctx, fl, nil, 30*time.Millisecond, discardLogger())
	time.Sleep(100 * time.Millisecond)
}
