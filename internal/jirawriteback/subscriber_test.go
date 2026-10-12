package jirawriteback

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
)

func stateEvent(project, id, oldState, newState string, synced bool) events.Event {
	data := map[string]any{"old_state": oldState, "new_state": newState}
	if synced {
		data["source"] = "sync"
	}

	return events.Event{Type: events.CardStateChanged, Project: project, CardID: id, Data: data}
}

func TestSubscriber_Enqueue(t *testing.T) {
	foreign := jiraCard("API-003", "PROJ-3", board.StateInProgress, testStart)
	foreign.Source.ExternalURL = "https://evil.example/browse/PROJ-3"

	tests := []struct {
		name     string
		card     *board.Card
		event    events.Event
		wantKind string // "" = nothing enqueued
	}{
		{
			name: "first in_progress", card: jiraCard("API-001", "PROJ-1", board.StateInProgress, testStart),
			event: stateEvent("api", "API-001", "todo", "in_progress", false), wantKind: KindStarted,
		},
		{
			name: "synced in_progress counts like local", card: jiraCard("API-001", "PROJ-1", board.StateInProgress, testStart),
			event: stateEvent("api", "API-001", "todo", "in_progress", true), wantKind: KindStarted,
		},
		{
			name: "done", card: jiraCard("API-001", "PROJ-1", board.StateDone, testStart),
			event: stateEvent("api", "API-001", "review", "done", false), wantKind: KindDone,
		},
		{
			name: "review is not a trigger", card: jiraCard("API-001", "PROJ-1", board.StateReview, testStart),
			event: stateEvent("api", "API-001", "in_progress", "review", false),
		},
		{
			name: "card without source", card: &board.Card{ID: "API-001", State: board.StateInProgress},
			event: stateEvent("api", "API-001", "todo", "in_progress", false),
		},
		{
			name: "foreign host", card: foreign,
			event: stateEvent("api", "API-003", "todo", "in_progress", false),
		},
		{
			name: "not a state change", card: jiraCard("API-001", "PROJ-1", board.StateInProgress, testStart),
			event: events.Event{Type: events.CardUpdated, Project: "api", CardID: "API-001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cards := newFakeCards(map[string]*board.Card{"api/" + tt.card.ID: tt.card})
			w, store, _ := newTestWriteback(t, cards, newFakeJira(st("To Do", "new"), simpleWorkflow()))

			w.handleEvent(t.Context(), tt.event)

			for _, kind := range []string{KindStarted, KindDone} {
				row, ok := rowState(t, store, "api", tt.card.ID, kind)
				if kind == tt.wantKind {
					require.True(t, ok)
					assert.Equal(t, tt.card.Source.ExternalID, row.IssueKey)
					assert.Equal(t, testStart.Unix(), row.NextAttemptAt)
				} else {
					assert.False(t, ok, "kind %s", kind)
				}
			}
		})
	}
}

func TestSubscriber_ReentryIsNoOp(t *testing.T) {
	cards := newFakeCards(map[string]*board.Card{"api/API-001": jiraCard("API-001", "PROJ-1", board.StateInProgress, testStart)})
	w, store, clk := newTestWriteback(t, cards, newFakeJira(st("To Do", "new"), simpleWorkflow()))

	w.handleEvent(t.Context(), stateEvent("api", "API-001", "todo", "in_progress", false))

	first, _ := rowState(t, store, "api", "API-001", KindStarted)

	clk.Advance(time.Hour)

	for _, from := range []string{"review", "stalled", "blocked"} {
		w.handleEvent(t.Context(), stateEvent("api", "API-001", from, "in_progress", false))
	}

	again, _ := rowState(t, store, "api", "API-001", KindStarted)
	assert.Equal(t, first, again, "re-entering in_progress neither adds nor touches the row")
}

func TestStart_EndToEndAndShutdown(t *testing.T) {
	t.Skip("worker lands in the next task")

	cards := newFakeCards(map[string]*board.Card{"api/API-001": jiraCard("API-001", "PROJ-1", board.StateInProgress, testStart)})
	fj := newFakeJira(st("To Do", "new"), simpleWorkflow())
	w, store, _ := newTestWriteback(t, cards, fj)

	ctx, cancel := context.WithCancel(t.Context())
	w.Start(ctx)

	w.bus.Publish(stateEvent("api", "API-001", "todo", "in_progress", false))

	require.Eventually(t, func() bool {
		row, ok, err := store.GetJiraWriteback(context.Background(), "api", "API-001", KindStarted)

		return err == nil && ok && row.State == "done"
	}, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, "In Progress", fj.status().Name)

	cancel()

	done := make(chan struct{})

	go func() { w.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after ctx cancel")
	}
}
