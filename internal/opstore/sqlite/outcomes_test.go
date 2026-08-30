package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelOutcomesRecordStatsReset(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)

	defer st.Close()

	ctx := context.Background()
	rows := []ModelOutcome{
		{Project: "p", CardID: "CM-1", Model: "a/x", Role: "coder", Result: "win", VerifyPass: true, CostUSD: 1.5, NCandidates: 3, JudgeModel: "j/m"},
		{Project: "p", CardID: "CM-1", Model: "b/y", Role: "coder", Result: "loss", VerifyPass: true, CostUSD: 1.0, NCandidates: 3, JudgeModel: "j/m"},
		{Project: "p", CardID: "CM-1", Model: "c/z", Role: "coder", Result: "failed", VerifyPass: false, CostUSD: 0.2, NCandidates: 3, JudgeModel: "j/m"},
	}
	require.NoError(t, st.RecordModelOutcomes(ctx, rows))
	// Second game: a/x loses a 2-way.
	require.NoError(t, st.RecordModelOutcomes(ctx, []ModelOutcome{
		{Project: "p", CardID: "CM-2", Model: "a/x", Role: "coder", Result: "loss", VerifyPass: true, CostUSD: 0.5, NCandidates: 2},
		{Project: "p", CardID: "CM-2", Model: "b/y", Role: "coder", Result: "win", VerifyPass: true, CostUSD: 0.6, NCandidates: 2},
	}))

	stats, err := st.ModelOutcomeStats(ctx)
	require.NoError(t, err)

	byModel := map[string]OutcomeStats{}
	for _, s := range stats {
		byModel[s.Model] = s
	}

	ax := byModel["a/x"]
	assert.Equal(t, 2, ax.RaceSamples)
	assert.Equal(t, 1, ax.RaceWins)
	assert.Equal(t, 0, ax.SoloSamples)
	assert.InDelta(t, 2.0, ax.TotalCostUSD, 1e-9)
	assert.Equal(t, 1, byModel["c/z"].RaceSamples, "failed counts as a race sample")
	assert.Equal(t, 0, byModel["c/z"].RaceWins)

	n, err := st.ResetModelOutcomes(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	stats, err = st.ModelOutcomeStats(ctx)
	require.NoError(t, err)
	assert.Empty(t, stats)
}

func TestRecordModelOutcomesValidation(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)

	defer st.Close()

	err = st.RecordModelOutcomes(context.Background(), []ModelOutcome{{Model: "", Result: "win", NCandidates: 2}})
	assert.Error(t, err, "empty model rejected") //nolint:testifylint
	err = st.RecordModelOutcomes(context.Background(), []ModelOutcome{{Model: "a", Result: "meh", NCandidates: 2}})
	assert.Error(t, err, "unknown result rejected") //nolint:testifylint
	err = st.RecordModelOutcomes(context.Background(), []ModelOutcome{{Model: "a", Result: "win", NCandidates: 0}})
	assert.Error(t, err, "n_candidates < 1 rejected") //nolint:testifylint
	err = st.RecordModelOutcomes(context.Background(), []ModelOutcome{{Model: "a", Result: "loss", NCandidates: 1}})
	assert.Error(t, err, "solo loss rejected: only a judge produces a loss, and a solo run has no judge") //nolint:testifylint
	assert.NoError(t, st.RecordModelOutcomes(context.Background(), nil), "empty batch is a no-op")
}

func TestRecordModelOutcomesSoloAdmitted(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)

	defer st.Close()

	ctx := context.Background()

	require.NoError(t, st.RecordModelOutcomes(ctx, []ModelOutcome{
		{Project: "p", CardID: "CM-1", Model: "a/x", Role: "coder", Result: "win", NCandidates: 1},
	}))
	require.NoError(t, st.RecordModelOutcomes(ctx, []ModelOutcome{
		{Project: "p", CardID: "CM-2", Model: "b/y", Role: "coder", Result: "failed", NCandidates: 1},
	}))

	stats, err := st.ModelOutcomeStats(ctx)
	require.NoError(t, err)

	byModel := map[string]OutcomeStats{}
	for _, s := range stats {
		byModel[s.Model] = s
	}

	ax := byModel["a/x"]
	assert.Equal(t, 0, ax.RaceSamples, "a solo run is not a race sample")
	assert.Equal(t, 0, ax.RaceWins, "a solo completion is not a race win")
	assert.Equal(t, 1, ax.SoloSamples)
	assert.Equal(t, 0, ax.SoloFailures)

	by := byModel["b/y"]
	assert.Equal(t, 1, by.SoloSamples)
	assert.Equal(t, 1, by.SoloFailures)
	assert.Equal(t, 0, by.RaceSamples)
}

func TestSchemaUpgradeFromV1AddsOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")
	st, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, st.Close())

	st, err = Open(path) // reopen: idempotent, stamps current version
	require.NoError(t, err)

	defer st.Close()

	require.NoError(t, st.RecordModelOutcomes(context.Background(), []ModelOutcome{
		{Project: "p", CardID: "C-1", Model: "m", Result: "win", NCandidates: 2},
	}))
}
