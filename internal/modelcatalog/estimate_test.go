package modelcatalog

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coderFixture builds 24 measured AA rows on a noisy linear coder-on-intel
// relation: coder = 40 + 1.6*i +- noise (alternating by row), intel = 20 +
// 2*i. The alternating noise keeps the residual standard deviation nonzero
// while the correlation stays high.
func coderFixture(noise float64) []aaModel {
	rows := make([]aaModel, 0, 24)

	for i := range 24 {
		j := noise
		if i%2 == 1 {
			j = -noise
		}

		rows = append(rows, aaModel{
			Slug:        fmt.Sprintf("fit-row-%d", i),
			Creator:     "openai",
			CodingIndex: new(40 + 1.6*float64(i) + j),
			IntelIndex:  new(20 + 2*float64(i)),
		})
	}

	return rows
}

// expectedFit recomputes, independently of fitCoderEstimator, the OLS fit of
// normalized coder prior on normalized intelligence prior over the fit-set
// screen: both indices present, trusted creator, clears the floor on either
// prior.
func expectedFit(t *testing.T, rows []aaModel, allow []string, floor, maxCoding, maxIntel float64) (slope, intercept, sd, r, maxCoder float64, n int) {
	t.Helper()

	type pair struct{ x, y float64 }

	pairs := make([]pair, 0, len(rows))
	for _, m := range rows {
		if m.CodingIndex == nil || m.IntelIndex == nil || !isTrusted(m.Creator, allow) {
			continue
		}

		x, y := norm(m.IntelIndex, maxIntel), norm(m.CodingIndex, maxCoding)
		if x < floor && y < floor {
			continue
		}

		pairs = append(pairs, pair{x, y})
	}

	n = len(pairs)
	require.NotZero(t, n)

	var sumX, sumY, sumXY, sumXX, sumYY float64

	for _, p := range pairs {
		sumX += p.x
		sumY += p.y
		sumXY += p.x * p.y
		sumXX += p.x * p.x
		sumYY += p.y * p.y
	}

	fn := float64(n)
	cov := fn*sumXY - sumX*sumY
	varX := fn*sumXX - sumX*sumX
	varY := fn*sumYY - sumY*sumY

	slope = cov / varX
	intercept = (sumY - slope*sumX) / fn
	r = cov / math.Sqrt(varX*varY)

	var ssr float64

	for _, p := range pairs {
		resid := p.y - (slope*p.x + intercept)
		ssr += resid * resid
		maxCoder = max(maxCoder, p.y)
	}

	sd = math.Sqrt(ssr / (fn - 2))

	return slope, intercept, sd, r, maxCoder, n
}

func TestFitCoderEstimator(t *testing.T) {
	t.Parallel()

	const floor = 0.25

	t.Run("fit coefficients match an independent OLS over the fixture", func(t *testing.T) {
		t.Parallel()

		rows := coderFixture(1)
		maxCoding, maxIntel := maxIndices(rows)
		est := fitCoderEstimator(rows, nil, floor, maxCoding, maxIntel)

		slope, intercept, sd, r, maxCoder, n := expectedFit(t, rows, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled)
		require.Equal(t, 24, n, "every fixture row must qualify for the fit set")
		require.GreaterOrEqual(t, r, 0.8, "fixture must clear the correlation guard")

		assert.InDelta(t, slope, est.slope, 1e-12)
		assert.InDelta(t, intercept, est.intercept, 1e-12)
		assert.InDelta(t, sd, est.residualSD, 1e-12)
		assert.InDelta(t, maxCoder, est.maxMeasured, 1e-12)
		assert.InDelta(t, maxIntel, est.maxIntel, 1e-12)
	})

	t.Run("nil-coding row gets fit minus residual sd", func(t *testing.T) {
		t.Parallel()

		estimated := aaModel{Slug: "new-1", Creator: "openai", IntelIndex: new(60.0)}
		aa := append(slices.Clone(coderFixture(1)), estimated)
		maxCoding, maxIntel := maxIndices(aa)
		est := fitCoderEstimator(aa, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled)

		slope, intercept, sd, _, maxCoder, _ := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		want := slope*norm(estimated.IntelIndex, maxIntel) + intercept - sd
		require.Less(t, want, maxCoder-0.01, "fixture must sit below the cap so the uncapped path is exercised")

		prior, ok := est.estimate(estimated)
		require.True(t, ok)
		assert.InDelta(t, want, prior, 1e-12)
	})

	t.Run("estimate capped below the best measured coder", func(t *testing.T) {
		t.Parallel()

		// An intelligence index beyond every measured row: the raw fit would
		// overshoot the cap, so the estimate must be clipped to it.
		frontier := aaModel{Slug: "frontier-1", Creator: "openai", IntelIndex: new(100.0)}
		aa := append(slices.Clone(coderFixture(1)), frontier)
		maxCoding, maxIntel := maxIndices(aa)
		est := fitCoderEstimator(aa, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled)

		slope, intercept, sd, _, maxCoder, _ := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		lim := maxCoder - 0.01
		require.Greater(t, slope*norm(frontier.IntelIndex, maxIntel)+intercept-sd, lim,
			"fixture must overshoot the cap so the capped path is exercised")

		prior, ok := est.estimate(frontier)
		require.True(t, ok)
		assert.InDelta(t, lim, prior, 1e-12)
		assert.Less(t, prior, maxCoder, "estimate must stay strictly below the best measured coder")
	})

	t.Run("measured and unscored rows are not estimated", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			m    aaModel
		}{
			{"real coding index", aaModel{Creator: "openai", CodingIndex: new(50.0), IntelIndex: new(60.0)}},
			{"no intelligence index", aaModel{Creator: "openai"}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				rows := coderFixture(1)
				maxCoding, maxIntel := maxIndices(rows)
				est := fitCoderEstimator(rows, nil, floor, maxCoding, maxIntel)
				require.True(t, est.enabled)

				prior, ok := est.estimate(tc.m)
				assert.False(t, ok)
				assert.Zero(t, prior)
			})
		}
	})

	t.Run("disabled estimator estimates nothing", func(t *testing.T) {
		t.Parallel()

		var est coderEstimator

		prior, ok := est.estimate(aaModel{Creator: "openai", IntelIndex: new(60.0)})
		assert.False(t, ok)
		assert.Zero(t, prior)
	})

	t.Run("fewer than 20 fit rows disables", func(t *testing.T) {
		t.Parallel()

		rows := coderFixture(1)[:19]
		maxCoding, maxIntel := maxIndices(rows)
		est := fitCoderEstimator(rows, nil, floor, maxCoding, maxIntel)

		assert.False(t, est.enabled)

		prior, ok := est.estimate(aaModel{Creator: "openai", IntelIndex: new(60.0)})
		assert.False(t, ok)
		assert.Zero(t, prior)
	})

	t.Run("weak correlation disables", func(t *testing.T) {
		t.Parallel()

		rows := coderFixture(20) // noise dominates the trend
		maxCoding, maxIntel := maxIndices(rows)

		_, _, _, r, _, _ := expectedFit(t, rows, nil, floor, maxCoding, maxIntel)
		require.Less(t, r, 0.8, "fixture must trip the correlation guard")

		est := fitCoderEstimator(rows, nil, floor, maxCoding, maxIntel)
		assert.False(t, est.enabled)
	})

	t.Run("untrusted creators are excluded from the fit", func(t *testing.T) {
		t.Parallel()

		trusted := coderFixture(1)
		maxCoding, maxIntel := maxIndices(trusted)
		est := fitCoderEstimator(trusted, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled, "24 trusted rows clear the size guard")

		// Marking 6 rows untrusted drops the fit set to 18, which trips the
		// size guard - so the excluded rows really left the fit.
		mixed := slices.Clone(trusted)
		for i := 18; i < len(mixed); i++ {
			mixed[i].Creator = "longcat"
		}

		est = fitCoderEstimator(mixed, nil, floor, maxCoding, maxIntel)
		assert.False(t, est.enabled)

		// An explicit allowlist that trusts the creator puts the rows back.
		est = fitCoderEstimator(mixed, []string{"openai", "longcat"}, floor, maxCoding, maxIntel)
		assert.True(t, est.enabled)
	})

	t.Run("zero intel variance disables without dividing by zero", func(t *testing.T) {
		t.Parallel()

		rows := coderFixture(1)
		for i := range rows {
			rows[i].IntelIndex = new(50.0)
		}

		maxCoding, maxIntel := maxIndices(rows)
		est := fitCoderEstimator(rows, nil, floor, maxCoding, maxIntel)

		assert.False(t, est.enabled)
	})

	t.Run("below-floor rows are excluded from the fit", func(t *testing.T) {
		t.Parallel()

		rows := coderFixture(1)
		// floor 0.63 drops the 6 weakest rows, leaving 18: below the size
		// guard. expectedFit applies the same screen, so a clean disable is
		// only explicable by the floor.
		const high = 0.63

		maxCoding, maxIntel := maxIndices(rows)

		_, _, _, _, _, n := expectedFit(t, rows, nil, high, maxCoding, maxIntel)
		require.Equal(t, 18, n)

		est := fitCoderEstimator(rows, nil, high, maxCoding, maxIntel)
		assert.False(t, est.enabled)
	})
}
