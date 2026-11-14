package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

var runT0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func TestOpenCardRunRecordsLaunchFacts(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	facts := state.LaunchFacts{
		Snapshot:   board.EditSnapshot{"title": "aa", "## Plan": "bb"},
		Autonomous: true,
		Origin:     state.OriginMCP,
	}
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, facts))

	got, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, facts, got.Launch)
	assert.Equal(t, "endpoint", got.Mode)
	assert.True(t, got.StartedAt.Equal(runT0))
	assert.Nil(t, got.EndedAt)
	assert.Nil(t, got.StoppedAt)

	none, err := st.CardRunRow(ctx, "p", "P-404")
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestOpenCardRunAttemptsResetOnlyByREST(t *testing.T) {
	for origin, want := range map[string]int{
		state.OriginMCP:      2,
		state.OriginPlaybook: 2,
		state.OriginSentinel: 2,
		state.OriginREST:     0,
	} {
		t.Run(origin, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{}))
			_, err := st.db.ExecContext(ctx, `UPDATE card_run_modes SET attempts = 2`)
			require.NoError(t, err)

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour),
				state.LaunchFacts{Origin: origin}))

			got, err := st.CardRunRow(ctx, "p", "P-001")
			require.NoError(t, err)
			assert.Equal(t, want, got.Attempts)
		})
	}
}

func TestOpenCardRunResetsPerRunColumns(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0,
		state.LaunchFacts{Autonomous: true, Origin: state.OriginREST, Snapshot: board.EditSnapshot{"title": "x"}}))
	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", "r1", "running", "", runT0))
	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", "r1", "failed", "error", runT0))
	require.NoError(t, st.MarkHumanStop(ctx, "p", "P-001"))

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour), state.LaunchFacts{}))

	got, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.Empty(t, got.RunID)
	assert.Empty(t, got.StopCause)
	assert.Nil(t, got.StoppedAt)
	assert.False(t, got.HumanStop)
	assert.False(t, got.Launch.Autonomous)
	assert.Empty(t, got.Launch.Origin)
	assert.Empty(t, got.Launch.Snapshot)
}

func TestRecordRunStatus(t *testing.T) {
	type call struct{ runID, status, cause string }

	tests := []struct {
		name        string
		calls       []call
		wantRunID   string
		wantCause   string
		wantStopped bool
	}{
		{"running adopts run id", []call{{"r1", "running", ""}}, "r1", "", false},
		{"second running keeps the first", []call{{"r1", "running", ""}, {"r2", "running", ""}}, "r1", "", false},
		{"terminal for the stored run", []call{{"r1", "running", ""}, {"r1", "failed", "verify_parked"}}, "r1", "verify_parked", true},
		{"stale run id ignored", []call{{"r1", "running", ""}, {"r0", "failed", "verify_parked"}}, "r1", "", false},
		{"first terminal wins", []call{{"r1", "running", ""}, {"r1", "failed", "verify_parked"}, {"r1", "failed", "error"}}, "r1", "verify_parked", true},
		{"empty run id ignored", []call{{"r1", "running", ""}, {"", "failed", "error"}}, "r1", "", false},
		{"terminal on a row without run id", []call{{"r1", "failed", "error"}}, "", "", false},
		{"running after stop is ignored", []call{{"r1", "running", ""}, {"r1", "completed", ""}, {"r2", "running", ""}}, "r1", "", true},
		{"overlong cause stored as unknown", []call{{"r1", "running", ""}, {"r1", "failed", strings.Repeat("x", 65)}}, "r1", "", true},
		{"cause at the limit is kept", []call{{"r1", "running", ""}, {"r1", "failed", strings.Repeat("x", 64)}}, "r1", strings.Repeat("x", 64), true},
		{"unknown status ignored", []call{{"r1", "running", ""}, {"r1", "blocked", "x"}}, "r1", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{}))

			for _, c := range tt.calls {
				require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", c.runID, c.status, c.cause, runT0.Add(time.Minute)))
			}

			got, err := st.CardRunRow(ctx, "p", "P-001")
			require.NoError(t, err)
			assert.Equal(t, tt.wantRunID, got.RunID)
			assert.Equal(t, tt.wantCause, got.StopCause)
			assert.Equal(t, tt.wantStopped, got.StoppedAt != nil)
		})
	}
}

func TestMarkHumanStopAndRefreshSnapshot(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0,
		state.LaunchFacts{Snapshot: board.EditSnapshot{"title": "old"}}))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", runT0, state.LaunchFacts{}))

	require.NoError(t, st.MarkHumanStop(ctx, "p", "P-001"))
	require.NoError(t, st.RefreshEditSnapshot(ctx, "p", "P-001", board.EditSnapshot{"title": "new"}))
	// A card with no row is a no-op, not an error.
	require.NoError(t, st.MarkHumanStop(ctx, "p", "P-404"))
	require.NoError(t, st.RefreshEditSnapshot(ctx, "p", "P-404", board.EditSnapshot{"title": "x"}))

	got, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.True(t, got.HumanStop)
	assert.Equal(t, board.EditSnapshot{"title": "new"}, got.Launch.Snapshot)

	other, err := st.CardRunRow(ctx, "p", "P-002")
	require.NoError(t, err)
	assert.False(t, other.HumanStop)
}

func TestCardRunsSchemaKeepsV6Statements(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	// The statements an older binary runs, verbatim.
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO card_run_modes (project, card_id, mode, started_at, ended_at) VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(project, card_id) DO UPDATE SET mode = excluded.mode,
		   started_at = excluded.started_at, ended_at = NULL`,
		"p", "P-001", "endpoint", runT0.UnixNano())
	require.NoError(t, err)

	_, err = st.db.ExecContext(ctx,
		`UPDATE card_run_modes SET ended_at = ?
		 WHERE project = ? AND card_id = ? AND ended_at IS NULL AND started_at <= ?`,
		runT0.UnixNano(), "p", "P-001", runT0.UnixNano())
	require.NoError(t, err)

	got, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	require.NotNil(t, got.EndedAt)
	assert.Equal(t, state.LaunchFacts{}, got.Launch)
	assert.Zero(t, got.Attempts)
	assert.Empty(t, got.RunID)
	assert.Empty(t, got.StopCause)
	assert.Nil(t, got.StoppedAt)
	assert.False(t, got.HumanStop)
}

func TestOpenCardRunClearsDecidedAndRESTResetsUsageReruns(t *testing.T) {
	for origin, want := range map[string]int{
		state.OriginMCP:      2,
		state.OriginPlaybook: 2,
		state.OriginSentinel: 2,
		state.OriginREST:     0,
	} {
		t.Run(origin, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, state.LaunchFacts{Autonomous: true}))

			for range 2 {
				require.NoError(t, st.IncrementAttempts(ctx, "p", "P-001"))
				require.NoError(t, st.IncrementUsageReruns(ctx, "p", "P-001"))
			}

			require.NoError(t, st.MarkRunDecided(ctx, "p", "P-001", runT0))

			require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour),
				state.LaunchFacts{Autonomous: true, Origin: origin}))

			got, err := st.CardRunRow(ctx, "p", "P-001")
			require.NoError(t, err)
			assert.False(t, got.Decided)
			assert.Equal(t, want, got.UsageReruns)
			assert.Equal(t, want, got.Attempts)
		})
	}
}

func TestUndecidedRunsSkipsDecidedAndNonAutonomous(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	auto := state.LaunchFacts{Autonomous: true, Origin: state.OriginMCP}
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, auto))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-002", "endpoint", runT0, auto))
	require.NoError(t, st.OpenCardRun(ctx, "p", "P-003", "endpoint", runT0, state.LaunchFacts{Origin: state.OriginMCP}))

	require.NoError(t, st.MarkRunDecided(ctx, "p", "P-002", runT0))
	// A decision about an earlier run leaves the current one undecided.
	require.NoError(t, st.MarkRunDecided(ctx, "p", "P-001", runT0.Add(-time.Hour)))

	got, err := st.UndecidedRuns(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "P-001", got[0].CardID)
	assert.Equal(t, auto, got[0].Launch)
	assert.True(t, got[0].StartedAt.Equal(runT0))

	decided, err := st.CardRunRow(ctx, "p", "P-002")
	require.NoError(t, err)
	assert.True(t, decided.Decided)
}

func TestSentinelTablesKeepV6Statements(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	// OpenCardRun's upsert as a schema v6 binary without the Sentinel tables runs it, verbatim.
	const v6Upsert = `INSERT INTO card_run_modes (project, card_id, mode, started_at, ended_at, edit_snapshot, autonomous, origin)
		 VALUES (?, ?, ?, ?, NULL, ?, ?, ?)
		 ON CONFLICT(project, card_id) DO UPDATE SET mode = excluded.mode,
		   started_at = excluded.started_at, ended_at = NULL,
		   edit_snapshot = excluded.edit_snapshot, autonomous = excluded.autonomous, origin = excluded.origin,
		   attempts = CASE WHEN excluded.origin = 'rest' THEN 0 ELSE attempts END,
		   run_id = '', stop_cause = '', stopped_at = NULL, human_stop = 0`

	for _, at := range []time.Time{runT0, runT0.Add(time.Hour)} {
		_, err := st.db.ExecContext(ctx, v6Upsert, "p", "P-001", "endpoint", at.UnixNano(), "{}", true, state.OriginMCP)
		require.NoError(t, err)
	}

	got, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.True(t, got.StartedAt.Equal(runT0.Add(time.Hour)))
	assert.True(t, got.Launch.Autonomous)
	assert.Zero(t, got.UsageReruns)
	assert.False(t, got.Decided)

	var version int
	require.NoError(t, st.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version))
	assert.Equal(t, 6, version)
}

func TestAnAnalysisSurvivesTheNextLaunch(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()
	facts := state.LaunchFacts{Autonomous: true, Origin: state.OriginREST}

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0, facts))
	require.NoError(t, st.RecordRunStatus(ctx, "p", "P-001", "r1", "running", "", runT0))
	require.NoError(t, st.QueueAnalysis(ctx, analysisRow("P-001", "r1", runT0)))

	require.NoError(t, st.OpenCardRun(ctx, "p", "P-001", "endpoint", runT0.Add(time.Hour), facts))

	rows, err := st.OpenAnalyses(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "r1", rows[0].RunID)

	run, err := st.CardRunRow(ctx, "p", "P-001")
	require.NoError(t, err)
	assert.False(t, run.AnalysisQueued, "the new run starts unqueued")
}
