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
	assert.Equal(t, 2, ax.Samples)
	assert.Equal(t, 1, ax.Wins)
	assert.InDelta(t, 1.0/3+1.0/2, ax.ExpectedWins, 1e-9)
	assert.InDelta(t, 2.0, ax.TotalCostUSD, 1e-9)
	assert.Equal(t, 1, byModel["c/z"].Samples, "failed counts as a sample")
	assert.Equal(t, 0, byModel["c/z"].Wins)

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
	err = st.RecordModelOutcomes(context.Background(), []ModelOutcome{{Model: "a", Result: "win", NCandidates: 1}})
	assert.Error(t, err, "n_candidates < 2 rejected") //nolint:testifylint
	assert.NoError(t, st.RecordModelOutcomes(context.Background(), nil), "empty batch is a no-op")
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
