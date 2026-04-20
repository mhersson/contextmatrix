package runner_test

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

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/runner"
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
		return nil, errors.New("not found")
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
	mu    sync.Mutex
	calls []runner.EndSessionPayload
	err   error
}

func (f *fakeClient) EndSession(_ context.Context, p runner.EndSessionPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, p)

	return f.err
}

func (f *fakeClient) Calls() []runner.EndSessionPayload {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]runner.EndSessionPayload, len(f.calls))
	copy(out, f.calls)

	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForCalls(t *testing.T, fc *fakeClient, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fc.Calls()) >= want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	require.GreaterOrEqualf(t, len(fc.Calls()), want, "expected %d calls, got %d", want, len(fc.Calls()))
}

func assertNoCall(t *testing.T, fc *fakeClient) {
	t.Helper()

	time.Sleep(100 * time.Millisecond)

	assert.Empty(t, fc.Calls(), "expected no end-session calls")
}

func TestEndSessionSubscriber_TerminalDoneReleasedRunning_Fires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)

	calls := fc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "C-001", calls[0].CardID)
	assert.Equal(t, "proj", calls[0].Project)
}

func TestEndSessionSubscriber_MidWorkflow_NoCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "in_progress",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)
}

func TestEndSessionSubscriber_SubtaskNoRunnerStatus_NoCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/SUB-001": {
			ID:            "SUB-001",
			State:         "done",
			AssignedAgent: "",
			RunnerStatus:  "",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "SUB-001"})

	assertNoCall(t, fc)
}

func TestEndSessionSubscriber_StateChangedWithAgentStillSet_NoCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "agent-123",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)

	// Now release the card and expect a call.
	cg.setAgent("proj", "C-001", "")

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})
	waitForCalls(t, fc, 1)
}

func TestEndSessionSubscriber_NotPlannedReleasedRunning_Fires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "not_planned",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
}

func TestEndSessionSubscriber_DoubleEvent_TwoCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-001"})
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 2)
}

func TestEndSessionSubscriber_UnrelatedEvent_NoCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardCreated, Project: "proj", CardID: "C-001"})

	assertNoCall(t, fc)
}

func TestEndSessionSubscriber_WebhookError_NoCrash(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-001": {
			ID:            "C-001",
			State:         "done",
			AssignedAgent: "",
			RunnerStatus:  "running",
		},
	}}
	fc := &fakeClient{err: errors.New("runner is down")}

	runner.StartEndSessionSubscriber(ctx, bus, cg, fc, discardLogger())
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForCalls(t, fc, 1)
	// If we got here without the subscriber deadlocking or panicking, we're good.
}
