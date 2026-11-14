package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
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
