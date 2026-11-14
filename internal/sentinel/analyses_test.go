package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func analysing() state.Settings {
	s := enabledSettings()
	s.Friction, s.AnalyseEveryRun = true, true

	return s
}

func analysisRows(e *dispatchEnv) []state.Heal {
	var rows []state.Heal

	for _, h := range e.store.allHeals() {
		if h.Kind == state.HealKindAnalysis {
			rows = append(rows, h)
		}
	}

	return rows
}

func TestQueueAnalysesQueuesASettledRun(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(analysing())
	_, run := e.stopped("ALPHA-001", protocol.StopCauseDone, func(c *board.Card, _ *state.Run) {
		c.State, c.WorkerStatus = board.StateReview, "completed"
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	rows := analysisRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, state.StageAnalyse, rows[0].Stage)
	assert.Equal(t, run.RunID, rows[0].RunID)
	assert.Equal(t, protocol.StopCauseDone, rows[0].Cause)
	assert.True(t, rows[0].RunStarted.Equal(run.StartedAt))
	assert.True(t, rows[0].CreatedAt.Equal(epoch))
	assert.True(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued)

	require.NoError(t, e.ctl.pass(context.Background()))
	assert.Len(t, analysisRows(e), 1, "a queued run is not queued again")
}

func TestQueueAnalysesSkipsHumanStopsAndGoneCards(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(analysing())
	e.stopped("ALPHA-001", protocol.StopCauseKilled, func(_ *board.Card, r *state.Run) { r.HumanStop = true })
	e.store.putRun(state.Run{
		Project: "alpha", CardID: "ALPHA-404", StartedAt: runStarted, RunID: "run-gone", Launch: state.LaunchFacts{Autonomous: true},
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, analysisRows(e))
	assert.True(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued)
	assert.True(t, e.store.run("alpha", "ALPHA-404").AnalysisQueued)
}

func TestQueueAnalysesWaitsForARunStillGoing(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(analysing())
	e.stopped("ALPHA-001", "", func(c *board.Card, r *state.Run) {
		c.WorkerStatus, c.Updated, r.StoppedAt = "running", epoch, nil
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, analysisRows(e))
	assert.False(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued)
}

func TestAnAnalysisSurvivesTheNextLaunch(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(analysing())
	e.stopped("ALPHA-001", protocol.StopCauseDone)

	require.NoError(t, e.ctl.pass(context.Background()))
	require.Len(t, analysisRows(e), 1)

	e.store.putRun(state.Run{
		Project: "alpha", CardID: "ALPHA-001", StartedAt: runStarted.Add(time.Hour), Launch: state.LaunchFacts{Autonomous: true},
	})

	rows := analysisRows(e)
	require.Len(t, rows, 1)
	assert.Equal(t, "run-ALPHA-001", rows[0].RunID)
	assert.Equal(t, state.StageAnalyse, rows[0].Stage)
	assert.False(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued, "the new run starts unqueued")
}

func TestQueueAnalysesIsOffWithoutTheSwitch(t *testing.T) {
	tests := []struct {
		name string
		edit func(*state.Settings)
	}{
		{"analyse every run off", func(s *state.Settings) { s.AnalyseEveryRun = false }},
		{"friction off", func(s *state.Settings) { s.Friction = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newPassEnv(t)
			s := analysing()
			tt.edit(&s)
			e.store.set(s)
			e.stopped("ALPHA-001", protocol.StopCauseDone)

			require.NoError(t, e.ctl.pass(context.Background()))

			assert.Empty(t, analysisRows(e))
			assert.False(t, e.store.run("alpha", "ALPHA-001").AnalysisQueued, "a run settled while off stays unqueued")
		})
	}
}

func reviewed(c *board.Card, _ *state.Run) { c.State, c.WorkerStatus = board.StateReview, "completed" }

// queued stores an analysis row for card id's run, edited by edits, and marks
// the run queued as the queue does.
func (e *dispatchEnv) queued(t *testing.T, id string, edits ...func(*state.Heal)) state.Heal {
	t.Helper()

	h := state.Heal{
		ID: int64(len(e.store.allHeals()) + 1), Kind: state.HealKindAnalysis, Project: "alpha", CardID: id,
		RunStarted: runStarted, RunID: "run-" + id, Cause: protocol.StopCauseDone, Stage: state.StageAnalyse,
		CreatedAt: epoch.Add(-time.Minute), UpdatedAt: epoch.Add(-time.Minute),
	}

	for _, edit := range edits {
		edit(&h)
	}

	e.store.putHeal(h)
	require.NoError(t, e.store.MarkAnalysisQueued(context.Background(), "alpha", id, h.RunStarted))

	return h
}

func TestAnAnalysisEndsAnalysedOnItsResult(t *testing.T) {
	ctx := context.Background()
	e := newPassEnv(t)
	e.store.set(analysing())
	e.baseIn(llmchannel.ModeEndpoint)
	e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed)
	row := e.queued(t, "ALPHA-001")

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, protocol.RunKindAnalyse, e.backend.sent[0].Kind)
	assert.Equal(t, "run-ALPHA-001", e.backend.sent[0].Analyse.RunID)
	assert.InDelta(t, 1.0, e.backend.sent[0].MaxCostUSD, 0)

	require.NoError(t, e.ctl.RunKindStatus(ctx, protocol.StatusCallbackPayload{
		Project: "alpha", CardID: "ALPHA-001", Kind: protocol.RunKindAnalyse, RunID: "rk-1",
		WorkerStatus: "completed", StopCause: protocol.StopCauseDone,
		Result: &protocol.RunKindResult{Analyse: &protocol.AnalyseResult{Findings: []protocol.FrictionFinding{}}},
		Usage:  []protocol.RunUsage{{Model: "z-ai/glm-5.2", CostUSD: 0.3}},
	}))
	require.NoError(t, e.ctl.pass(ctx))

	stored := e.store.heal(row.ID)
	assert.Equal(t, state.StageAnalysed, stored.Stage)
	assert.Len(t, e.backend.sent, 1, "an analysis runs once")
	assert.Equal(t, []ledgerRow{{HealID: row.ID, Mode: "endpoint", USD: 0.3, At: epoch, RunID: "rk-1"}}, e.store.charges())
	assert.Empty(t, e.cards.logged, "an analysis never writes the card")
}

func TestAnAnalysisThatNeverReportedIsChargedItsCeiling(t *testing.T) {
	e := newPassEnv(t)
	s := analysing()
	s.AnalyseEveryRun = false
	e.store.set(s)
	e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed)

	at := epoch.Add(-settleGrace)
	row := e.queued(t, "ALPHA-001", func(h *state.Heal) {
		h.Dispatch = state.Dispatch{Kind: protocol.RunKindAnalyse, Mode: "endpoint", CeilingUSD: 1, At: &at}
	})

	require.NoError(t, e.ctl.pass(context.Background()))

	stored := e.store.heal(row.ID)
	assert.Equal(t, state.StageAnalyseFailed, stored.Stage, "an unreported analysis ends while analyses are off")
	assert.False(t, inFlight(stored.Dispatch))
	assert.Equal(t, []ledgerRow{{HealID: row.ID, Mode: "endpoint", USD: 1, At: epoch}}, e.store.charges())
	assert.Empty(t, e.cards.logged)
}

func TestAnalysesWaitWhileSwitchedOff(t *testing.T) {
	e := newPassEnv(t)
	s := analysing()
	s.AnalyseEveryRun = false
	e.store.set(s)
	e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed)
	row := e.queued(t, "ALPHA-001")

	require.NoError(t, e.ctl.pass(context.Background()))

	assert.Empty(t, e.backend.sent)
	assert.Zero(t, e.backend.healths, "nothing can dispatch, so the agent is not asked")
	assert.Equal(t, row, e.store.heal(row.ID), "the row waits untouched")
}

func TestAnInvestigatedRunIsNotAnalysedAgain(t *testing.T) {
	e := newPassEnv(t)
	e.store.set(analysing())
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageParked, investigated(protocol.DiagnosisEnvironment, nil))
	row := e.queued(t, "ALPHA-001")

	require.NoError(t, e.ctl.pass(context.Background()))

	stored := e.store.heal(row.ID)
	assert.Equal(t, state.StageAnalyseSkipped, stored.Stage)
	assert.Equal(t, reasonAnalysisInvestigated, stored.Reason)
	assert.Empty(t, e.backend.sent)
}

func TestAnAnalysisWaitsForTheRunsInvestigation(t *testing.T) {
	e := newPassEnv(t)
	e.backend.health.MaxConcurrent = 10
	e.store.set(analysing())
	e.stopped("ALPHA-001", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-001", state.StageInvestigate)
	row := e.queued(t, "ALPHA-001")

	require.NoError(t, e.ctl.pass(context.Background()))

	require.Len(t, e.backend.sent, 1)
	assert.Equal(t, protocol.RunKindInvestigate, e.backend.sent[0].Kind)
	assert.Equal(t, row, e.store.heal(row.ID), "the investigation may cover the run")
}

func TestOneAnalysisInFlightPerCard(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	e.backend.health.MaxConcurrent = 10
	e.store.set(analysing())
	e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed)
	older := e.queued(t, "ALPHA-001", func(h *state.Heal) {
		h.RunStarted, h.RunID = runStarted.Add(-2*time.Hour), "run-earlier"
	})
	newer := e.queued(t, "ALPHA-001")

	require.NoError(t, e.ctl.pass(ctx))
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1, "a second analyse of the card would answer the wrong row")
	assert.Equal(t, newer.RunID, e.backend.sent[0].Analyse.RunID, "newest first")
	assert.Nil(t, e.store.heal(older.ID).Dispatch.At)
}

func TestAnUnknownKindHoldsAnalysesOnly(t *testing.T) {
	e := newPassEnv(t)
	ctx := context.Background()
	e.backend.health.MaxConcurrent = 10
	e.store.set(analysing())
	e.baseIn(llmchannel.ModeEndpoint)
	e.stopped("ALPHA-001", protocol.StopCauseDone, reviewed)
	row := e.queued(t, "ALPHA-001")
	e.backend.err = backend.ErrRunKindUnknown

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 1)
	stored := e.store.heal(row.ID)
	assert.Equal(t, state.StageAnalyse, stored.Stage, "the analysis stays pending")
	assert.Equal(t, state.Dispatch{}, stored.Dispatch)
	assert.Empty(t, e.store.charges())
	assert.Empty(t, e.cards.logged)

	st := e.ctl.Status()
	assert.Zero(t, st.Analysing)
	assert.Equal(t, StateWatching, st.State, "heals are not held")

	e.backend.err = nil
	e.clk.Advance(noRunKindsHold - time.Second)
	e.stopped("ALPHA-002", protocol.StopCauseVerifyParked)
	e.openHeal(t, "ALPHA-002", state.StageInvestigate)

	built := len(e.models)

	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 2, "no analyse for 15 minutes")
	assert.Equal(t, protocol.RunKindInvestigate, e.backend.sent[1].Kind, "an investigate in the window is sent")
	assert.Len(t, e.models, built+1, "no payload, and so no token, is built for the held kind")

	e.clk.Advance(time.Second)
	require.NoError(t, e.ctl.pass(ctx))

	require.Len(t, e.backend.sent, 3)
	assert.Equal(t, protocol.RunKindAnalyse, e.backend.sent[2].Kind)
	assert.NotNil(t, e.store.heal(row.ID).Dispatch.At)
}
