package sqlite

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectorHeadroomEmptyStoreIsUnset(t *testing.T) {
	st := openLadderStore(t)

	got, at, err := st.SelectorHeadroom(context.Background())
	require.NoError(t, err)
	assert.Zero(t, got)
	assert.True(t, at.IsZero(), "an empty store has no write time")
}

func TestPutSelectorHeadroomRoundTripAndReplace(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	require.NoError(t, st.PutSelectorHeadroom(ctx, 2))

	got, at, err := st.SelectorHeadroom(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 2, got, 1e-9)
	assert.False(t, at.IsZero())

	// A second write replaces the single row rather than adding one.
	require.NoError(t, st.PutSelectorHeadroom(ctx, 1))

	got, _, err = st.SelectorHeadroom(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 1, got, 1e-9, "exactly 1 is legal: the band is the cheapest candidate alone")
}

func TestPutSelectorHeadroomRejects(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	for name, h := range map[string]float64{
		"below one": 0.5,
		"zero":      0,
		"negative":  -1,
		"nan":       math.NaN(),
		"inf":       math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			err := st.PutSelectorHeadroom(ctx, h)
			require.ErrorIs(t, err, ErrInvalidHeadroom)
		})
	}

	got, _, err := st.SelectorHeadroom(ctx)
	require.NoError(t, err)
	assert.Zero(t, got, "a rejected write leaves the store empty")
}

func TestValidateSelectorHeadroomDetails(t *testing.T) {
	err := ValidateSelectorHeadroom(0.5)
	require.ErrorIs(t, err, ErrInvalidHeadroom)
	assert.Equal(t, "invalid selector headroom: must be a number of at least 1, got 0.5", err.Error())
	require.NoError(t, ValidateSelectorHeadroom(1.5))
}
