package sentinel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestCauseKey(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	stopped := now.Add(-time.Minute)
	started := now.Add(-time.Hour)
	forward := func(stage string, created time.Time) *state.Heal {
		return &state.Heal{Kind: state.HealKindForward, Stage: stage, CreatedAt: created}
	}
	card := func(state, worker string, age time.Duration, reviews int) *board.Card {
		return &board.Card{State: state, WorkerStatus: worker, Updated: now.Add(-age), ReviewAttempts: reviews}
	}
	run := func(cause string, stoppedAt *time.Time) state.Run {
		return state.Run{StartedAt: started, StoppedAt: stoppedAt, StopCause: cause}
	}

	tests := []struct {
		name    string
		run     state.Run
		card    *board.Card
		forward *state.Heal
		key     string
		settled bool
	}{
		{"running, no stop", run("", nil), card("in_progress", "running", time.Minute, 0), nil, "", false},
		{"queued, no stop", run("", nil), card("in_progress", "queued", time.Hour, 0), nil, "", false},
		{"parked before the callback", run("", nil), card("review", "parked", time.Minute, 0), nil, "", false},
		{"parked, callback never came", run("", nil), card("review", "parked", 11*time.Minute, 0), nil, "unknown", true},
		{"failed, callback never came", run("", nil), card("in_progress", "failed", 11*time.Minute, 0), nil, "unknown", true},
		{"completed before the callback", run("", nil), card("review", "", 9*time.Minute, 0), nil, "", false},
		{"completed, callback never came", run("", nil), card("review", "", 10*time.Minute, 0), nil, "unknown", true},
		{"ended past the grace, card written since", state.Run{StartedAt: started, EndedAt: new(now.Add(-10 * time.Minute))}, card("review", "", time.Minute, 0), nil, "unknown", true},
		{"ended inside the grace", state.Run{StartedAt: started, EndedAt: new(now.Add(-9 * time.Minute))}, card("review", "", time.Minute, 0), nil, "", false},
		{"stopped with cause", run("verify_parked", &stopped), card("in_progress", "failed", time.Minute, 0), nil, "verify_parked", true},
		{"stopped, no cause", run("", &stopped), card("in_progress", "failed", time.Minute, 0), nil, "unknown", true},
		{"stalled while running", run("", nil), card(board.StateStalled, "running", time.Minute, 0), nil, "stalled", true},
		{"human stop", state.Run{StartedAt: started, StoppedAt: &stopped, StopCause: "killed", HumanStop: true}, card("todo", "killed", time.Minute, 0), nil, "human_stop", true},
		{"review cap", run("review_parked", &stopped), card("review", "parked", time.Minute, 7), nil, "review_cap", true},
		{"review short of the cap", run("review_parked", &stopped), card("review", "parked", time.Minute, 6), nil, "review_parked", true},
		{"forwarded fix failed", run("error", &stopped), card("in_progress", "failed", time.Minute, 0), forward(state.StageForwarded, started.Add(-time.Minute)), "forward_fix_failed", true},
		{"forwarded fix, usage limit", run("usage_limit", &stopped), card("in_progress", "failed", time.Minute, 0), forward(state.StageForwarded, started.Add(-time.Minute)), "usage_limit", true},
		{"forwarded fix, run done", run("done", &stopped), card("done", "", time.Minute, 0), forward(state.StageForwarded, started.Add(-time.Minute)), "done", true},
		{"forward after run start", run("error", &stopped), card("in_progress", "failed", time.Minute, 0), forward(state.StageForwarded, started.Add(time.Minute)), "error", true},
		{"forward already judged", run("error", &stopped), card("in_progress", "failed", time.Minute, 0), forward(state.StageForwardFailed, started.Add(-time.Minute)), "error", true},
		{"heal row is no forward", run("error", &stopped), card("in_progress", "failed", time.Minute, 0), &state.Heal{Kind: state.HealKindHeal, Stage: state.StageForwarded, CreatedAt: started.Add(-time.Minute)}, "error", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, settled := causeKey(tt.run, tt.card, tt.forward, now)
			assert.Equal(t, tt.settled, settled)
			assert.Equal(t, tt.key, key)
		})
	}
}

func TestEligible(t *testing.T) {
	src := &board.Source{}

	tests := []struct {
		name      string
		run       state.Run
		card      board.Card
		elsewhere bool
		ok        bool
	}{
		{"plain", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: "in_progress"}, false, true},
		{"HITL launch", state.Run{}, board.Card{State: "in_progress"}, false, false},
		{"done", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: board.StateDone}, false, false},
		{"not planned", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: board.StateNotPlanned}, false, false},
		{"unvetted", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: "todo", Source: src}, false, false},
		{"claimed", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: "todo", AssignedAgent: "human:a"}, false, false},
		{"claimed elsewhere", state.Run{Launch: state.LaunchFacts{Autonomous: true}}, board.Card{State: "todo"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, why := eligible(tt.run, &tt.card, tt.elsewhere)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, !tt.ok, why != "")
		})
	}
}

func TestOpenStage(t *testing.T) {
	withLog := state.Run{RunID: "run-1"}
	attempts := func(n int) state.Run { return state.Run{RunID: "run-1", Attempts: n} }

	tests := []struct {
		name      string
		key       string
		run       state.Run
		stage     string
		reasonHas string // interpolated data the park reason must carry
	}{
		{"investigate_fix", protocol.StopCauseVerifyParked, withLog, state.StageInvestigate, ""},
		{"investigate_fix, one attempt used", protocol.StopCauseVerifyParked, attempts(1), state.StageInvestigate, ""},
		{"investigate_fix, attempts used", protocol.StopCauseVerifyParked, attempts(state.MaxHealAttempts), state.StageParked, "2"},
		{"investigate_fix, attempts used, no log", protocol.StopCauseMaxTurns, state.Run{Attempts: state.MaxHealAttempts}, state.StageParked, "2"},
		{"investigate_fix without a log", protocol.StopCauseVerifyParked, state.Run{}, state.StageParked, ""},
		{"investigate_park", protocol.StopCauseError, attempts(state.MaxHealAttempts), state.StageInvestigate, ""},
		{"investigate_park without a log", CauseStalled, state.Run{}, state.StageParked, ""},
		{"usage_rerun", protocol.StopCauseUsageLimit, state.Run{Attempts: state.MaxHealAttempts}, state.StageUsageWait, ""},
		{"park", protocol.StopCauseBudgetExceeded, withLog, state.StageParked, responseFor(protocol.StopCauseBudgetExceeded).Reason},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, reason := openStage(responseFor(tt.key), tt.run)

			assert.Equal(t, tt.stage, stage)
			assert.Equal(t, tt.stage == state.StageParked, reason != "", "a reason exactly when it parks")
			assert.Contains(t, reason, tt.reasonHas)
		})
	}
}
