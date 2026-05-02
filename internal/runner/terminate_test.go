package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/runner"
)

func TestTerminalKillSubscriber_TerminalDoneReleased_Fires(t *testing.T) {
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

	runner.StartTerminalKillSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-001"})

	waitForKillCalls(t, fc, 1)

	calls := fc.KillCalls()
	assert.Equal(t, "proj", calls[0].Project)
	assert.Equal(t, "C-001", calls[0].CardID)
}

func TestTerminalKillSubscriber_NotPlannedReleased_Fires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-002": {
			ID:            "C-002",
			State:         "not_planned",
			AssignedAgent: "",
		},
	}}
	fc := &fakeClient{}

	runner.StartTerminalKillSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-002"})

	waitForKillCalls(t, fc, 1)
}

func TestTerminalKillSubscriber_StillClaimed_NoFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-003": {
			ID:            "C-003",
			State:         "done",
			AssignedAgent: "human:alice",
		},
	}}
	fc := &fakeClient{}

	runner.StartTerminalKillSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-003"})

	assertNoKillCall(t, fc)
}

func TestTerminalKillSubscriber_NonTerminalState_NoFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-004": {
			ID:            "C-004",
			State:         "in_progress",
			AssignedAgent: "",
		},
	}}
	fc := &fakeClient{}

	runner.StartTerminalKillSubscriber(ctx, bus, cg, fc, discardLogger())

	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-004"})

	assertNoKillCall(t, fc)
}

func TestTerminalKillSubscriber_AfterReleaseTransition_Fires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewBus()
	cg := &fakeCardGetter{cards: map[string]*board.Card{
		"proj/C-005": {
			ID:            "C-005",
			State:         "done",
			AssignedAgent: "agent:foo",
		},
	}}
	fc := &fakeClient{}

	runner.StartTerminalKillSubscriber(ctx, bus, cg, fc, discardLogger())

	// Initial state: claim still held — no fire.
	bus.Publish(events.Event{Type: events.CardStateChanged, Project: "proj", CardID: "C-005"})
	assertNoKillCall(t, fc)

	// Now release the claim and re-publish — the subscriber must fire.
	cg.setAgent("proj", "C-005", "")
	bus.Publish(events.Event{Type: events.CardReleased, Project: "proj", CardID: "C-005"})
	waitForKillCalls(t, fc, 1)
}

func TestTerminalKillSubscriber_NilDeps_NoCrash(t *testing.T) {
	// Missing dependency must not panic — the subscriber logs a warning
	// and returns. This test exercises every nil-dep combination.
	runner.StartTerminalKillSubscriber(context.Background(), nil, nil, nil, discardLogger())
	runner.StartTerminalKillSubscriber(context.Background(), events.NewBus(), nil, nil, discardLogger())
	runner.StartTerminalKillSubscriber(context.Background(), events.NewBus(), &fakeCardGetter{}, nil, discardLogger())
}
