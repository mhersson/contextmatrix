package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openLadderStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

func TestSelectorLaddersEmptyStoreIsUnset(t *testing.T) {
	st := openLadderStore(t)

	got, at, err := st.SelectorLadders(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.True(t, at.IsZero(), "an empty store has no write time")
}

func TestPutSelectorLaddersRoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	in := map[string]map[string]float64{
		"coder":    {"simple": 0.65, "moderate": 0.80, "complex": 0.90, "critical": 0.95},
		"reviewer": {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.93},
	}
	require.NoError(t, st.PutSelectorLadders(ctx, in))

	got, at, err := st.SelectorLadders(ctx)
	require.NoError(t, err)
	assert.False(t, at.IsZero())
	require.Len(t, got, 2)
	assert.InDelta(t, 0.90, got["coder"]["complex"], 1e-9)
	assert.InDelta(t, 0.95, got["coder"]["critical"], 1e-9)
	assert.InDelta(t, 0.82, got["reviewer"]["complex"], 1e-9)
	assert.InDelta(t, 0.93, got["reviewer"]["critical"], 1e-9)
}

func TestPutSelectorLaddersPartialTiersMergeOverDefaults(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.PutSelectorLadders(ctx, map[string]map[string]float64{
		"coder":    {"critical": 0.95},
		"reviewer": {"complex": 0.85},
	}))

	got, _, err := st.SelectorLadders(ctx)
	require.NoError(t, err)
	require.Len(t, got["coder"], 4, "every tier is stored, the unnamed ones at their default")
	assert.InDelta(t, 0.65, got["coder"]["simple"], 1e-9)
	assert.InDelta(t, 0.82, got["coder"]["complex"], 1e-9)
	assert.InDelta(t, 0.95, got["coder"]["critical"], 1e-9)
	assert.InDelta(t, 0.85, got["reviewer"]["complex"], 1e-9)
	assert.InDelta(t, 0.90, got["reviewer"]["critical"], 1e-9)
}

func TestPutSelectorLaddersReplacesAllRows(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.PutSelectorLadders(ctx, map[string]map[string]float64{
		"coder": {"critical": 0.95}, "reviewer": {"critical": 0.95},
	}))
	require.NoError(t, st.PutSelectorLadders(ctx, map[string]map[string]float64{
		"coder": {"complex": 0.85}, "reviewer": {"complex": 0.85},
	}))

	got, _, err := st.SelectorLadders(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 0.90, got["coder"]["critical"], 1e-9, "the earlier critical override is gone")
	assert.InDelta(t, 0.85, got["coder"]["complex"], 1e-9)

	var n int

	require.NoError(t, st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM selector_ladder`).Scan(&n))
	assert.Equal(t, 8, n)
}

func TestPutSelectorLaddersRejects(t *testing.T) {
	cases := map[string]map[string]map[string]float64{
		"missing reviewer": {"coder": {"complex": 0.9}},
		"empty reviewer":   {"coder": {"complex": 0.9}, "reviewer": {}},
		"unknown role":     {"coder": {"complex": 0.9}, "reviewer": {"complex": 0.9}, "judge": {"complex": 0.9}},
		"non-monotone":     {"coder": {"complex": 0.70}, "reviewer": {"complex": 0.9}},
		"above one":        {"coder": {"critical": 1.5}, "reviewer": {"complex": 0.9}},
		"unknown tier":     {"coder": {"epic": 0.9}, "reviewer": {"complex": 0.9}},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			st := openLadderStore(t)
			ctx := context.Background()

			err := st.PutSelectorLadders(ctx, in)
			require.ErrorIs(t, err, ErrInvalidLadder)

			got, _, err := st.SelectorLadders(ctx)
			require.NoError(t, err)
			assert.Empty(t, got, "a rejected write leaves the store untouched")
		})
	}
}
