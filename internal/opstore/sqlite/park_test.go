package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParkReset_RoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	got, err := st.ParkResets(ctx, "proj")
	require.NoError(t, err)
	assert.Empty(t, got)

	first := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Hour)

	require.NoError(t, st.PutParkReset(ctx, "proj", "P-001", first))
	require.NoError(t, st.PutParkReset(ctx, "proj", "P-002", first))
	require.NoError(t, st.PutParkReset(ctx, "other", "P-001", first))
	require.NoError(t, st.PutParkReset(ctx, "proj", "P-001", second))

	got, err = st.ParkResets(ctx, "proj")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, second.Equal(got["P-001"]))
	assert.True(t, first.Equal(got["P-002"]))

	require.NoError(t, st.ClearParkReset(ctx, "proj", "P-001"))
	require.NoError(t, st.ClearParkReset(ctx, "proj", "P-001"))

	got, err = st.ParkResets(ctx, "proj")
	require.NoError(t, err)
	assert.Len(t, got, 1)

	other, err := st.ParkResets(ctx, "other")
	require.NoError(t, err)
	assert.Len(t, other, 1)
}
