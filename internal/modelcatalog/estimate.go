package modelcatalog

import (
	"log/slog"
	"math"
)

// coderEstimator is the per-build fallback that derives a model's coder
// prior from its intelligence index when AA publishes no coding index. The
// zero value is the disabled state: no estimate is offered and priors stay
// 0, the behaviour before the fallback existed.
type coderEstimator struct {
	enabled bool

	// OLS fit of the normalized coder prior on the normalized intelligence
	// prior, plus the residual standard deviation the estimate pays for the
	// scatter around that line.
	slope, intercept, residualSD float64

	// maxMeasured is the highest normalized coder prior in the fit set. An
	// estimate is capped just below it so an unproven model never ties or
	// outranks the best measured coder - on equal priors price decides, and
	// price would favour the unproven estimate.
	maxMeasured float64

	// maxIntel is the normalization denominator for the intelligence index,
	// needed to normalize the row being estimated.
	maxIntel float64
}

// fitCoderEstimator fits the coder-prior fallback over one AA response; run
// it once per catalog build and keep the result for the whole build. The fit
// set is the rows that carry both indices, pass the allowlist screen, and
// clear the floor on either prior. The returned estimator is disabled (zero
// value) when the fit is degenerate (no variance in the intelligence
// priors), too small (fewer than 20 rows) or too weak (r below 0.8) - in
// every disabled case priors stay 0.
func fitCoderEstimator(aa []aaModel, allow []string, floor, maxCoding, maxIntel float64) coderEstimator {
	type priorPair struct{ intel, coder float64 }

	pairs := make([]priorPair, 0, len(aa))
	for _, m := range aa {
		if m.CodingIndex == nil || m.IntelIndex == nil || !isTrusted(m.Creator, allow) {
			continue
		}

		intel, coder := norm(m.IntelIndex, maxIntel), norm(m.CodingIndex, maxCoding)
		if intel < floor && coder < floor {
			continue
		}

		pairs = append(pairs, priorPair{intel, coder})
	}

	if len(pairs) == 0 {
		slog.Warn("coder prior fallback disabled: fit too small or too weak", "n", 0)

		return coderEstimator{}
	}

	var sumX, sumY, sumXY, sumXX, sumYY float64
	for _, p := range pairs {
		sumX += p.intel
		sumY += p.coder
		sumXY += p.intel * p.coder
		sumXX += p.intel * p.intel
		sumYY += p.coder * p.coder
	}

	n := float64(len(pairs))
	cov := n*sumXY - sumX*sumY
	varX := n*sumXX - sumX*sumX
	varY := n*sumYY - sumY*sumY

	// With no spread on either axis the slope and r are 0/0; any fit would
	// be NaN-poisoned, so stop here rather than divide by zero.
	if varX <= 0 || varY <= 0 {
		slog.Warn("coder prior fallback disabled: no variance in the fit set", "n", len(pairs))

		return coderEstimator{}
	}

	r := cov / math.Sqrt(varX*varY)
	if len(pairs) < 20 || r < 0.8 {
		slog.Warn("coder prior fallback disabled: fit too small or too weak", "n", len(pairs), "r", r)

		return coderEstimator{}
	}

	slope := cov / varX
	intercept := (sumY - slope*sumX) / n

	var ssr, maxMeasured float64

	for _, p := range pairs {
		resid := p.coder - (slope*p.intel + intercept)
		ssr += resid * resid
		maxMeasured = max(maxMeasured, p.coder)
	}

	return coderEstimator{
		enabled:     true,
		slope:       slope,
		intercept:   intercept,
		residualSD:  math.Sqrt(ssr / (n - 2)),
		maxMeasured: maxMeasured,
		maxIntel:    maxIntel,
	}
}

// estimate returns the coder prior estimated from m's intelligence index,
// for a row whose coding index AA does not publish: fit(intel) - residualSD,
// capped just below the best measured coder and clamped to [0,1]. ok is
// false when the estimator is disabled or the row has a real coding index
// (or no intelligence index to estimate from); the caller then keeps the
// measured path unchanged.
func (e coderEstimator) estimate(m aaModel) (prior float64, ok bool) {
	if !e.enabled || m.CodingIndex != nil || m.IntelIndex == nil {
		return 0, false
	}

	p := e.slope*norm(m.IntelIndex, e.maxIntel) + e.intercept - e.residualSD
	p = min(p, e.maxMeasured-0.01)

	return min(1, max(0, p)), true
}
