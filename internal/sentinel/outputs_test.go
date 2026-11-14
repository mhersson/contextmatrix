package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func routeEnv(t *testing.T, findings ...state.Finding) *dispatchEnv {
	t.Helper()

	e := newPassEnv(t)
	e.cards.projects["alpha"] = &board.ProjectConfig{Name: "alpha", Types: []string{"task"}, Priorities: []string{"medium"}}

	for _, f := range findings {
		e.store.putFinding(f)
	}

	return e
}

func queued(id int64, kind, title, text string) state.Finding {
	return state.Finding{
		ID: id, Project: "alpha", CardID: "ALPHA-001", Kind: kind, Title: title, Text: text, Evidence: "ev",
		State: state.FindingQueued,
	}
}

func TestRouteFindingsCreatesOneCardAndQueuesGuidance(t *testing.T) {
	e := routeEnv(t,
		queued(1, "code", "Cache the config loader", "the loader reads every file twice because the cache key ignores the path"),
		queued(2, "guidance", "", "run make fmt before every commit so lint stays quiet"),
	)

	ch, unsubscribe := e.bus.Subscribe()
	t.Cleanup(unsubscribe)

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Contains(t, cardEvents(ch), "alpha/ALPHA-001", "routing a finding announces its card")
	require.Len(t, e.cards.created, 1)
	assert.Equal(t, []string{frictionLabel}, e.cards.created[0].Labels)

	code := e.store.finding(1)
	assert.Equal(t, state.FindingCarded, code.State)
	assert.Equal(t, "ALPHA-100", code.Output)
	assert.Equal(t, state.FindingPending, e.store.finding(2).State)
}

func TestRouteFindingsDedupsWithinOnePass(t *testing.T) {
	text := "the loader reads every file twice because the cache key ignores the path"
	e := routeEnv(t,
		queued(1, "code", "Cache the config loader", text),
		queued(2, "code", "Cache the config loader", text),
		queued(3, "guidance", "", "run make fmt before every commit so lint stays quiet"),
		queued(4, "guidance", "", "run make fmt before every commit so lint stays quiet"),
	)

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Len(t, e.cards.created, 1)
	assert.Equal(t, state.FindingCarded, e.store.finding(1).State)
	assert.Equal(t, state.FindingDuplicate, e.store.finding(2).State)
	assert.Equal(t, "like card ALPHA-100", e.store.finding(2).Reason)
	assert.Equal(t, state.FindingPending, e.store.finding(3).State)
	assert.Equal(t, state.FindingDuplicate, e.store.finding(4).State)
	assert.Equal(t, "like finding 3", e.store.finding(4).Reason)
}

func TestRouteFindingsHoldsSecrets(t *testing.T) {
	e := routeEnv(t, queued(1, "code", "Rotate it", "the key is hunter2hunter2 in the log"))
	e.ctl.cfg.Secrets = func() []string { return []string{"hunter2hunter2"} }

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.cards.created)
	assert.Equal(t, state.FindingDropped, e.store.finding(1).State)
}

func TestRouteFindingsStopsAtTheDailyLimitAndWritesOnce(t *testing.T) {
	var fs []state.Finding

	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	for i, w := range words {
		fs = append(fs, queued(int64(i+1), "code", "Fix "+w, w+" "+w+" one two three four five "+w))
	}

	e := routeEnv(t, fs...)

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Len(t, e.cards.created, maxFrictionCardsPerDay)

	held := e.store.finding(6)
	assert.Equal(t, state.FindingQueued, held.State)
	assert.Equal(t, "today's friction-card limit reached", held.Reason)

	before := e.store.outcomeWrites
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Equal(t, before, e.store.outcomeWrites, "a limit-held finding is not rewritten")
	assert.Len(t, e.cards.created, maxFrictionCardsPerDay)

	e.clk.Advance(24 * time.Hour)
	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Len(t, e.cards.created, maxFrictionCardsPerDay+3, "the next day frees the held findings")
}

func TestRouteFindingsIsOffWithFriction(t *testing.T) {
	e := routeEnv(t, queued(1, "code", "Cache it", "the loader reads every file twice because the cache key ignores the path"))
	e.store.settings.Friction = false

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.cards.created)
	assert.Equal(t, state.FindingQueued, e.store.finding(1).State)
}
