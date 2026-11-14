package sentinel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

func rolloutLock(status string) *board.CardPlaybookLock {
	return &board.CardPlaybookLock{ID: "rollout", RunStatus: status}
}

// rollout is playbook rollout with its run in status on instance, on entry e1,
// ALPHA-001; e2 is ALPHA-002.
func rollout(status, instance string) *service.PlaybookDetail {
	return &service.PlaybookDetail{
		ID: "rollout", Runnable: true,
		Run: &board.PlaybookRun{Status: status, Instance: instance, Entry: "e1"},
		Entries: []service.PlaybookEntryDetail{
			{PlaybookEntry: board.PlaybookEntry{ID: "e1", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-001"}},
			{PlaybookEntry: board.PlaybookEntry{ID: "e2", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-002"}},
		},
	}
}

// relaunchOn is rollout's run on entry, waiting on this instance.
func relaunchOn(entry string) *service.PlaybookDetail {
	pb := rollout(board.RunStatusWaiting, "lap-a")
	pb.Run.Entry = entry

	return pb
}

func TestRelaunchRoute(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		lock   *board.CardPlaybookLock
		pb     *service.PlaybookDetail
		want   route
	}{
		{"playbook origin, its run here waits on the card", state.OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusWaiting, "lap-a"), routePlaybook},
		{"playbook origin, its run was stopped", state.OriginPlaybook, rolloutLock(""), nil, routeNone},
		{"playbook origin, a stopped run read", state.OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusStopped, "lap-a"), routeNone},
		{"playbook origin, another instance's run", state.OriginPlaybook, rolloutLock(board.RunStatusWaiting), rollout(board.RunStatusWaiting, "lap-b"), routeNone},
		{"playbook origin, the run moved on to another card", state.OriginPlaybook, rolloutLock(board.RunStatusRunning), relaunchOn("e2"), routeNone},
		{"playbook origin, the run is on no entry", state.OriginPlaybook, rolloutLock(board.RunStatusWaiting), relaunchOn(""), routeNone},
		{"playbook origin, no playbook", state.OriginPlaybook, nil, nil, routeNone},
		{"rest origin, no lock", state.OriginREST, nil, nil, routeLaunch},
		{"mcp origin, an inactive lock", state.OriginMCP, rolloutLock(""), nil, routeLaunch},
		{"sentinel origin, no lock", state.OriginSentinel, nil, nil, routeLaunch},
		{"rest origin, an active playbook run holds the card", state.OriginREST, rolloutLock(board.RunStatusRunning), rollout(board.RunStatusRunning, "lap-a"), routeNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := state.Run{Launch: state.LaunchFacts{Origin: tc.origin}}
			card := &board.Card{ID: "ALPHA-001", Project: "alpha", PlaybookLock: tc.lock}

			got, reason := relaunchRoute(run, card, tc.pb, "lap-a")

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.want == routeNone, reason != "", "a route none names why")
		})
	}
}

// relaunch plans h's rerun and relaunches, as apply does once the fix is written.
func (e *dispatchEnv) relaunch(t *testing.T, h *state.Heal, card *board.Card, run state.Run) error {
	t.Helper()

	plan, err := e.ctl.planRelaunch(context.Background(), card, run)
	require.NoError(t, err)

	return e.ctl.relaunch(context.Background(), h, card, run, plan)
}

func TestRelaunchSavesRerunBeforeLaunching(t *testing.T) {
	e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
	card.TokenUsage = &board.TokenUsage{EstimatedCostUSD: 1.25}
	card.SubtaskCostUSD = 0.5

	var (
		atLaunch        state.Heal
		attempts        int
		atLaunchEntries int
	)

	e.onRelaunch = func() {
		atLaunch = e.store.heal(h.ID)
		attempts = e.store.run("alpha", "ALPHA-001").Attempts
		atLaunchEntries = len(e.cards.logged["ALPHA-001"])
	}

	require.NoError(t, e.relaunch(t, h, card, run))

	assert.Equal(t, state.StageRerun, atLaunch.Stage)
	require.NotNil(t, atLaunch.RelaunchedAt)
	assert.Equal(t, epoch, *atLaunch.RelaunchedAt)
	assert.InDelta(t, 1.75, atLaunch.CostBaseUSD, 1e-9)
	assert.Equal(t, run.Attempts+1, attempts)
	assert.Zero(t, atLaunchEntries, "the rerun's entry waits until the rerun is launched")

	assert.Equal(t, []string{"ALPHA-001 todo"}, e.cards.transitions, "launch runs a todo card")
	assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
	assert.Empty(t, e.runner.calls)

	entries := e.cards.logged["ALPHA-001"]
	require.Len(t, entries, 1)
	assert.Equal(t, Agent, entries[0].Agent)
	assert.Equal(t, activityAction, entries[0].Action)
}

func TestRelaunchPlaybookStopsThenPlays(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		transitions []string
	}{
		// A completed run leaves no worker status, which the walker relaunches only from todo.
		{"a completed run in review moves to todo first", board.StateReview, []string{"ALPHA-001 todo"}},
		{"a card in todo stays put", board.StateTodo, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
			card.State, card.WorkerStatus = tc.state, "" // what the completed callback stores
			card.PlaybookLock = rolloutLock(board.RunStatusWaiting)
			run.Launch.Origin = state.OriginPlaybook
			e.pbs["rollout"] = rollout(board.RunStatusWaiting, "lap-a")

			stateAt := map[string]string{}
			e.runner.onCall = func(call string) { stateAt[call] = storedCard(t, e, "ALPHA-001").State }

			require.NoError(t, e.relaunch(t, h, card, run))

			stop, play := "stop rollout "+Agent, "play rollout "+Agent
			assert.Equal(t, []string{stop, play}, e.runner.calls)
			assert.Equal(t, tc.transitions, e.cards.transitions)
			assert.Equal(t, board.StateTodo, stateAt[stop], "the card is in todo before Stop")
			assert.Empty(t, e.relaunched)
			assert.Equal(t, state.StageRerun, e.store.heal(h.ID).Stage)
			assert.Equal(t, 1, e.store.run("alpha", "ALPHA-001").Attempts)
		})
	}
}

func TestRelaunchErrorParks(t *testing.T) {
	// Raw service errors carry host paths; none may reach the committed reason.
	const rawPath = "/srv/boards/.git"

	noPath := fmt.Errorf("find transition path in %s: %w", rawPath, &board.ValidationError{Err: board.ErrNoPath})

	tests := []struct {
		name  string
		setup func(e *dispatchEnv, card *board.Card, run *state.Run)
		keeps []string // what the reason must name
		calls []string // the runner's calls
	}{
		{"the launch is refused", func(e *dispatchEnv, _ *board.Card, _ *state.Run) {
			e.relaunchErr = errors.New("card is already being executed by a worker")
		}, []string{"card is already being executed by a worker"}, nil},
		{"the move to todo is refused", func(e *dispatchEnv, _ *board.Card, _ *state.Run) {
			e.cards.refuse = map[string]error{"ALPHA-001": noPath}
		}, []string{"ALPHA-001", refusedByProject}, nil},
		{"the move to todo before a playbook rerun is refused", func(e *dispatchEnv, card *board.Card, run *state.Run) {
			card.PlaybookLock = rolloutLock(board.RunStatusWaiting)
			run.Launch.Origin = state.OriginPlaybook
			e.pbs["rollout"] = rollout(board.RunStatusWaiting, "lap-a")
			e.cards.refuse = map[string]error{"ALPHA-001": noPath}
		}, []string{"ALPHA-001", refusedByProject}, nil},
		{"the playbook run stopped meanwhile", func(e *dispatchEnv, card *board.Card, run *state.Run) {
			card.PlaybookLock = rolloutLock(board.RunStatusWaiting)
			run.Launch.Origin = state.OriginPlaybook
			e.pbs["rollout"] = rollout(board.RunStatusWaiting, "lap-a")
			e.runner.err = fmt.Errorf("%w: rollout in %s", playbookrun.ErrRunInactive, rawPath)
		}, []string{"rollout", refusedByChange}, []string{"stop rollout " + Agent}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
			tc.setup(e, card, &run)

			require.NoError(t, e.relaunch(t, h, card, run))

			stored := e.store.heal(h.ID)
			assert.Equal(t, state.StageParked, stored.Stage)

			for _, k := range tc.keeps {
				assert.Contains(t, stored.Reason, k)
			}

			assert.NotContains(t, stored.Reason, rawPath)
			assert.Equal(t, []board.ActivityEntry{{
				Agent: Agent, Timestamp: epoch, Action: activityAction, Message: activityText(stored),
			}}, e.cards.logged["ALPHA-001"], "a refused rerun writes only its park entry")
			assert.Equal(t, tc.calls, e.runner.calls, "a refused Stop plays nothing")
			assert.Equal(t, 1, e.store.run("alpha", "ALPHA-001").Attempts, "the attempt was counted before the launch")
		})
	}
}

func TestRelaunchRouteNoneParksUncounted(t *testing.T) {
	e, h, card, run := applyEnv(t, protocol.DiagnosisModel, nil)
	card.PlaybookLock = rolloutLock(board.RunStatusRunning)
	e.pbs["rollout"] = rollout(board.RunStatusRunning, "lap-a")

	require.NoError(t, e.relaunch(t, h, card, run))

	stored := e.store.heal(h.ID)
	assert.Equal(t, state.StageParked, stored.Stage)
	assert.NotEmpty(t, stored.Reason)
	assert.Zero(t, e.store.run("alpha", "ALPHA-001").Attempts)
	assert.Empty(t, e.relaunched)
	assert.Empty(t, e.runner.calls)
}

func TestRelaunchText(t *testing.T) {
	retired := &state.Change{Retired: []string{"ALPHA-011", "ALPHA-012"}, Fresh: true}

	text := relaunchText(retired, 1)
	assert.Contains(t, text, "ALPHA-011")
	assert.Contains(t, text, "ALPHA-012")

	resumed := &state.Change{VerifyAfter: &board.VerifyConfig{Command: "make check"}}

	assert.NotEqual(t, relaunchText(resumed, 1), relaunchText(&state.Change{VerifyAfter: resumed.VerifyAfter, Fresh: true}, 1),
		"fresh and resume read apart")
	assert.NotEqual(t, relaunchText(resumed, 1), relaunchText(resumed, 2), "the attempt is named")
	assert.NotEqual(t, relaunchText(nil, 1), relaunchText(resumed, 1), "a changed verify is named")
}
