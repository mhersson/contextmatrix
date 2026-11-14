package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaybookPins(t *testing.T) {
	st := openTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-1", true))
	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-1", true), "pinning twice is idempotent")
	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-2", true))
	require.NoError(t, st.SetPlaybookPin(ctx, "human:bob", "PB-1", true))

	got, err := st.PlaybookPins(ctx, "human:alice")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"PB-1", "PB-2"}, got)

	got, err = st.PlaybookPins(ctx, "human:bob")
	require.NoError(t, err)
	assert.Equal(t, []string{"PB-1"}, got)

	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-1", false))
	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-1", false), "unpinning twice is idempotent")

	got, err = st.PlaybookPins(ctx, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, []string{"PB-2"}, got)

	require.NoError(t, st.SetPlaybookPin(ctx, "human:alice", "PB-1", true))
	require.NoError(t, st.DeletePlaybookPins(ctx, "PB-1"))

	for _, owner := range []string{"human:alice", "human:bob"} {
		got, err = st.PlaybookPins(ctx, owner)
		require.NoError(t, err)
		assert.NotContains(t, got, "PB-1", owner)
	}

	got, err = st.PlaybookPins(ctx, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, []string{"PB-2"}, got)
}
