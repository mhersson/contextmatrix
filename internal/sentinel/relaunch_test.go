package sentinel

import (
	"context"
	"errors"
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func rolloutLock(status string) *board.CardPlaybookLock {
	return &board.CardPlaybookLock{ID: "rollout", RunStatus: status}
}

// rollout is playbook rollout with its run in status on instance, holding
// ALPHA-001.
func rollout(status, instance string) *service.PlaybookDetail {
	return &service.PlaybookDetail{
		ID: "rollout", Runnable: true,
		Run: &board.PlaybookRun{Status: status, Instance: instance},
		Entries: []service.PlaybookEntryDetail{
			{PlaybookEntry: board.PlaybookEntry{ID: "e1", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-001"}},
		},
	}
}

func TestRelaunchRoute(t *testing.T) {
	other := rollout(board.RunStatusWaiting, "lap-a")
	other.Entries[0].Card = "ALPHA-002"

	tests := []struct {
		name   string
		origin string
		lock   *board.CardPlaybookLock
		pb     *service.PlaybookDetail
		want   route
	}{
		{"playbook origin, its run here waits on the card", OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusWaiting, "lap-a"), routePlaybook},
		{"playbook origin, its run was stopped", OriginPlaybook, rolloutLock(""), nil, routeNone},
		{"playbook origin, a stopped run read", OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusStopped, "lap-a"), routeNone},
		{"playbook origin, another instance's run", OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusWaiting, "lap-b"), routeNone},
		{"playbook origin, the run no longer holds the card", OriginPlaybook, rolloutLock(board.RunStatusWaiting), other, routeNone},
		{"playbook origin, no playbook", OriginPlaybook, nil, nil, routeNone},
		{"rest origin, no lock", OriginREST, nil, nil, routeLaunch},
		{"mcp origin, an inactive lock", OriginMCP, rolloutLock(""), nil, routeLaunch},
		{"sentinel origin, no lock", OriginSentinel, nil, nil, routeLaunch},
		{"rest origin, an active playbook run holds the card", OriginREST, rolloutLock(board.RunStatusRunning), rollout(board.RunStatusRunning, "lap-a"), routeNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := Run{Launch: LaunchFacts{Origin: tc.origin}}
			card := &board.Card{ID: "ALPHA-001", Project: "alpha", PlaybookLock: tc.lock}

			got, reason := relaunchRoute(run, card, tc.pb, "lap-a")

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.want == routeNone, reason != "", "a route none names why")
		})
	}
}

func TestRelaunchSavesRerunBeforeLaunching(t *testing.T) {
	e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
	card.TokenUsage = &board.TokenUsage{EstimatedCostUSD: 1.25}
	card.SubtaskCostUSD = 0.5

	var (
		atLaunch Heal
		attempts int
	)

	e.onRelaunch = func() {
		atLaunch = e.store.heal(h.ID)
		attempts = e.store.run("alpha", "ALPHA-001").Attempts
	}

	require.NoError(t, e.ctl.relaunch(context.Background(), h, card, run))

	assert.Equal(t, StageRerun, atLaunch.Stage)
	require.NotNil(t, atLaunch.RelaunchedAt)
	assert.Equal(t, epoch, *atLaunch.RelaunchedAt)
	assert.InDelta(t, 1.75, atLaunch.CostBaseUSD, 1e-9)
	assert.Equal(t, run.Attempts+1, attempts)

	assert.Equal(t, []string{"ALPHA-001 todo"}, e.cards.transitions, "launch runs a todo card")
	assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
	assert.Empty(t, e.runner.calls)

	entries := e.cards.logged["ALPHA-001"]
	require.Len(t, entries, 1)
	assert.Equal(t, Agent, entries[0].Agent)
	assert.Equal(t, activityAction, entries[0].Action)
}

func TestRelaunchPlaybookStopsThenPlays(t *testing.T) {
	e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
	card.PlaybookLock = rolloutLock(board.RunStatusWaiting)
	run.Launch.Origin = OriginPlaybook
	e.pbs["rollout"] = rollout(board.RunStatusWaiting, "lap-a")

	require.NoError(t, e.ctl.relaunch(context.Background(), h, card, run))

	assert.Equal(t, []string{"stop rollout " + Agent, "play rollout " + Agent}, e.runner.calls)
	assert.Empty(t, e.relaunched)
	assert.Empty(t, e.cards.transitions, "the playbook run moves its card to todo")
	assert.Equal(t, StageRerun, e.store.heal(h.ID).Stage)
	assert.Equal(t, 1, e.store.run("alpha", "ALPHA-001").Attempts)
}

func TestRelaunchErrorParks(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *dispatchEnv, card *board.Card, run *Run)
		cause string
	}{
		{"the launch is refused", func(e *dispatchEnv, _ *board.Card, _ *Run) {
			e.relaunchErr = errors.New("card is already being executed by a worker")
		}, "card is already being executed by a worker"},
		{"the playbook run is not resumed", func(e *dispatchEnv, card *board.Card, run *Run) {
			card.PlaybookLock = rolloutLock(board.RunStatusWaiting)
			run.Launch.Origin = OriginPlaybook
			e.pbs["rollout"] = rollout(board.RunStatusWaiting, "lap-a")
			e.runner.err = errors.New("playbook run inactive")
		}, "playbook run inactive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
			tc.setup(e, card, &run)

			require.NoError(t, e.ctl.relaunch(context.Background(), h, card, run))

			stored := e.store.heal(h.ID)
			assert.Equal(t, StageParked, stored.Stage)
			assert.Contains(t, stored.Reason, tc.cause)
			assert.Equal(t, 1, e.store.run("alpha", "ALPHA-001").Attempts, "the attempt was counted before the launch")
		})
	}
}

func TestRelaunchRouteNoneParksUncounted(t *testing.T) {
	e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
	card.PlaybookLock = rolloutLock(board.RunStatusRunning)
	e.pbs["rollout"] = rollout(board.RunStatusRunning, "lap-a")

	require.NoError(t, e.ctl.relaunch(context.Background(), h, card, run))

	stored := e.store.heal(h.ID)
	assert.Equal(t, StageParked, stored.Stage)
	assert.NotEmpty(t, stored.Reason)
	assert.Zero(t, e.store.run("alpha", "ALPHA-001").Attempts)
	assert.Empty(t, e.relaunched)
	assert.Empty(t, e.runner.calls)
}

func TestRelaunchText(t *testing.T) {
	retired := &Change{Retired: []string{"ALPHA-011", "ALPHA-012"}, Fresh: true}

	text := relaunchText(retired, 1)
	assert.Contains(t, text, "ALPHA-011")
	assert.Contains(t, text, "ALPHA-012")

	resumed := &Change{VerifyAfter: &board.VerifyConfig{Command: "make check"}}

	assert.NotEqual(t, relaunchText(resumed, 1), relaunchText(&Change{VerifyAfter: resumed.VerifyAfter, Fresh: true}, 1),
		"fresh and resume read apart")
	assert.NotEqual(t, relaunchText(resumed, 1), relaunchText(resumed, 2), "the attempt is named")
	assert.NotEqual(t, relaunchText(nil, 1), relaunchText(resumed, 1), "a changed verify is named")
}
