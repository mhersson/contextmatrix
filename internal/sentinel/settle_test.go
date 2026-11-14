package sentinel

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func usageHeal(h *state.Heal) { h.Cause, h.Response = protocol.StopCauseUsageLimit, RespUsageRerun }

// relaunchedAt puts the heal at stage rerun, relaunched at at with cost base base.
func relaunchedAt(at time.Time, base float64) func(*state.Heal) {
	return func(h *state.Heal) { h.Stage, h.RelaunchedAt, h.CostBaseUSD = state.StageRerun, &at, base }
}

// rerunEnded replaces card id's run row with its rerun's, as the launch does,
// started a minute from now and stopped with cause, the counters carried over;
// the card reads in progress with its worker at worker. The clock moves past
// the stop.
func (e *dispatchEnv) rerunEnded(t *testing.T, id, cause, worker string) {
	t.Helper()

	card := storedCard(t, e, id)
	card.State, card.WorkerStatus = board.StateInProgress, worker

	prev := e.store.run("alpha", id)
	started := e.clk.Now().Add(time.Minute)
	stopped := started.Add(5 * time.Minute)

	e.store.putRun(state.Run{
		Project: "alpha", CardID: id, Mode: prev.Mode, StartedAt: started, StoppedAt: &stopped,
		Launch:   state.LaunchFacts{Snapshot: board.TakeEditSnapshot(card), Autonomous: true, Origin: state.OriginSentinel},
		Attempts: prev.Attempts, UsageReruns: prev.UsageReruns, RunID: "run-rerun", StopCause: cause,
	})
	e.clk.Advance(10 * time.Minute)
}

func TestRerunCharge(t *testing.T) {
	tests := []struct {
		name     string
		base     float64
		usage    *board.TokenUsage
		subtasks float64
		want     float64
	}{
		{"the card and its subtasks cost more", 1.00, &board.TokenUsage{EstimatedCostUSD: 1.25}, 0.5, 0.75},
		{"the cost now is below the base", 1.00, &board.TokenUsage{EstimatedCostUSD: 0.4}, 0, 0},
		{"no usage at all", 0, nil, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card := &board.Card{TokenUsage: tc.usage, SubtaskCostUSD: tc.subtasks}

			assert.InDelta(t, tc.want, rerunCharge(state.Heal{CostBaseUSD: tc.base}, card), 1e-9)
		})
	}
}

func TestSettleHealedChargesOnce(t *testing.T) {
	tests := []struct {
		mode  string  // the rerun's channel
		today float64 // the endpoint spend the daily cap reads afterwards
	}{
		{"endpoint", 0.75},
		{"claude_code", 0},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			e := newPassEnv(t)
			ctx := context.Background()
			relaunched := epoch.Add(-30 * time.Minute)

			e.stopped("ALPHA-001", protocol.StopCauseDone, func(c *board.Card, r *state.Run) {
				c.State, c.WorkerStatus = board.StateReview, "completed"
				c.TokenUsage, c.SubtaskCostUSD = &board.TokenUsage{EstimatedCostUSD: 1.25}, 0.5
				r.StartedAt, r.Mode, r.Launch.Origin = relaunched.Add(time.Minute), tc.mode, state.OriginSentinel
			})
			e.openHeal(t, "ALPHA-001", state.StageRerun, relaunchedAt(relaunched, 1.00))

			// A callback writes the heal after the pass read it.
			e.cards.onGet = func() {
				h := e.store.heal(1)
				h.Version++
				e.store.putHeal(h)
				e.cards.onGet = nil
			}

			require.NoError(t, e.ctl.pass(ctx))

			assert.Equal(t, state.StageRerun, e.store.heal(1).Stage)
			assert.Empty(t, e.store.charges(), "a refused save charges nothing")
			assert.True(t, e.ctl.TakeNudgeForTest(), "the next pass decides again")

			require.NoError(t, e.ctl.pass(ctx))
			require.NoError(t, e.ctl.pass(ctx))

			assert.Equal(t, state.StageHealed, e.store.heal(1).Stage)
			assert.Equal(t, []ledgerRow{{HealID: 1, Mode: tc.mode, USD: 0.75, At: epoch}}, e.store.charges(),
				"charged once, on the rerun's channel")
			assert.Empty(t, e.cards.logged, "settling writes no activity entry")
			assert.True(t, e.store.run("alpha", "ALPHA-001").Decided, "the rerun's own run is decided")

			st := e.ctl.Status()
			assert.InDelta(t, tc.today, st.SpentTodayUSD, 1e-9)
			assert.Equal(t, 1, st.HealedToday)
		})
	}
}

func TestUsageRerunWaitsForResetThenResumes(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	resets := epoch.Add(10 * time.Minute)

	e.stopped("ALPHA-001", protocol.StopCauseUsageLimit, func(c *board.Card, r *state.Run) {
		c.WorkerStatus, c.Phase, c.ParkResetsAt = "parked", "execute", &resets
		r.Attempts, r.UsageReruns = 1, 1
	})
	e.openHeal(t, "ALPHA-001", state.StageUsageWait, usageHeal)

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageUsageWait, e.store.heal(1).Stage, "the limit has not reset")
	assert.Empty(t, e.relaunched)

	e.clk.Advance(11 * time.Minute)

	shut := enabledSettings()
	shut.DailyCapUSD = 0
	e.store.set(shut)

	require.NoError(t, e.ctl.pass(ctx))

	assert.Equal(t, state.StageUsageWait, e.store.heal(1).Stage, "the budget gate is shut")
	assert.Empty(t, e.relaunched)

	e.store.set(enabledSettings())

	require.NoError(t, e.ctl.pass(ctx))

	h := e.store.heal(1)
	assert.Equal(t, state.StageRerun, h.Stage)
	require.NotNil(t, h.RelaunchedAt)
	assert.Equal(t, e.clk.Now(), *h.RelaunchedAt)
	assert.Nil(t, h.Change, "a usage-limit rerun writes no fix")
	assert.Equal(t, []string{"alpha/ALPHA-001"}, e.relaunched)
	assert.Equal(t, []string{"ALPHA-001 todo system:sentinel"}, e.cards.transitions)
	assert.Empty(t, e.cards.patches, "nothing is written to the card")
	assert.Equal(t, "execute", storedCard(t, e, "ALPHA-001").Phase, "the rerun resumes")

	run := e.store.run("alpha", "ALPHA-001")
	assert.Equal(t, 2, run.UsageReruns)
	assert.Equal(t, 1, run.Attempts, "a usage-limit rerun spends no heal attempt")

	entries := e.cards.logged["ALPHA-001"]
	require.Len(t, entries, 1)
	assert.Equal(t, usageRerunText(2), entries[0].Message)
	assert.NotEqual(t, usageRerunText(1), usageRerunText(2), "the rerun is counted")
}

func TestUsageRerunCap(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseUsageLimit, func(c *board.Card, r *state.Run) {
		c.WorkerStatus = "parked"
		r.UsageReruns = state.MaxUsageReruns - 1
	})
	e.openHeal(t, "ALPHA-001", state.StageUsageWait, usageHeal)

	require.NoError(t, e.ctl.pass(ctx))
	require.Equal(t, state.StageRerun, e.store.heal(1).Stage)

	// The rerun hits the usage limit again: its heal fails and a new one waits.
	e.rerunEnded(t, "ALPHA-001", protocol.StopCauseUsageLimit, "parked")

	require.NoError(t, e.ctl.pass(ctx))

	heals := e.store.allHeals()
	require.Len(t, heals, 2)
	assert.Equal(t, state.StageRerunFailed, heals[0].Stage)
	assert.Equal(t, state.StageUsageWait, heals[1].Stage)
	assert.True(t, e.ctl.TakeNudgeForTest())

	require.NoError(t, e.ctl.pass(ctx))

	h := e.store.heal(2)
	assert.Equal(t, state.StageParked, h.Stage)
	assert.Contains(t, h.Reason, strconv.Itoa(state.MaxUsageReruns))
	assert.Len(t, e.relaunched, 1, "no rerun past the cap")
	assert.Equal(t, state.MaxUsageReruns, e.store.run("alpha", "ALPHA-001").UsageReruns)
	assert.Len(t, e.cards.logged["ALPHA-001"], 2, "the rerun's entry and the park's")
}

func TestSecondFailureAfterTwoHealsParks(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()

	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(_ *board.Card, r *state.Run) { r.Attempts = state.MaxHealAttempts })
	e.openHeal(t, "ALPHA-001", state.StageRerun, relaunchedAt(epoch, 0))
	e.rerunEnded(t, "ALPHA-001", protocol.StopCauseVerifyParked, "failed")

	require.NoError(t, e.ctl.pass(ctx))
	require.NoError(t, e.ctl.pass(ctx))

	heals := e.store.allHeals()
	require.Len(t, heals, 2)
	assert.Equal(t, state.StageRerunFailed, heals[0].Stage)
	assert.Len(t, e.store.charges(), 1, "the failed rerun is charged")

	second := heals[1]
	assert.Equal(t, state.StageParked, second.Stage)
	assert.Equal(t, protocol.StopCauseVerifyParked, second.Cause)
	assert.Equal(t, reasonAttemptsUsed, second.Reason)
	assert.Equal(t, "run-rerun", second.RunID, "the heal follows the rerun's own stop")
	assert.Empty(t, e.backend.sent, "nothing is investigated")
	assert.Empty(t, e.relaunched)
	assert.Len(t, e.cards.logged["ALPHA-001"], 1, "the park's entry")
}
