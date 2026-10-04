package modelcatalog

import (
	"bytes"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legsFixture is coderFixture's 24 measured rows plus two estimable ones:
// legs-new (a nil coding index, an ordinary intelligence index) and
// legs-frontier (an intelligence index beyond every measured row, so the
// raw fit overshoots the cap). 24 qualifying rows clear the size guard; the
// alternating noise keeps the residual standard deviation nonzero while the
// correlation stays high.
func legsFixture(noise float64) []aaModel {
	rows := coderFixture(noise)

	return append(rows,
		aaModel{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)},
		aaModel{Slug: "legs-frontier", Creator: "openai", IntelIndex: new(100.0)},
	)
}

// wantEstimate recomputes, from the fixture alone, the coder prior the
// fallback must produce for a row with the given intelligence index:
// fit(intel) - residualSD, capped at the best measured coder prior in the
// fit set minus 0.01, clamped to [0, 1].
func wantEstimate(t *testing.T, aa []aaModel, intel float64, allow []string, floor float64) float64 {
	t.Helper()

	maxCoding, maxIntel := maxIndices(aa)
	slope, intercept, sd, _, maxCoder, _ := expectedFit(t, aa, allow, floor, maxCoding, maxIntel)

	p := slope*norm(&intel, maxIntel) + intercept - sd
	p = math.Min(p, maxCoder-0.01)

	return math.Min(1, math.Max(0, p))
}

// findCandidate returns the candidate with the given slug.
func findCandidate(cands []protocol.CandidateModel, slug string) (protocol.CandidateModel, bool) {
	for _, c := range cands {
		if c.Slug == slug {
			return c, true
		}
	}

	return protocol.CandidateModel{}, false
}

// orCatalog is an OpenRouter catalog covering every slug the leg fixtures
// map to, all tool-capable.
func orCatalog() map[string]orEntry {
	or := make(map[string]orEntry, 28)

	for i := range 24 {
		or[fmt.Sprintf("openai/fit-row-%d", i)] = orEntry{ContextWindow: 1000, Tools: true}
	}

	or["openai/legs-new"] = orEntry{ContextWindow: 1000, Tools: true}
	or["openai/legs-frontier"] = orEntry{ContextWindow: 1000, Tools: true}
	or["z-ai/glm-5.2"] = orEntry{ContextWindow: 1000, Tools: true}

	return or
}

func TestBuildEstimatesCoderPrior(t *testing.T) {
	t.Parallel()

	const floor = 0.25

	t.Run("nil-coding row gets fit minus sd, flagged estimated", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		maxCoding, maxIntel := maxIndices(aa)

		est := fitCoderEstimator(aa, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled, "the fixture must enable the fallback so the nil-coding row is estimated")

		want := wantEstimate(t, aa, 60.0, nil, floor)
		require.Greater(t, want, 0.0, "fixture must put the estimate above zero so the fit path is exercised")

		res := build(aa, orCatalog(), floor, nil)

		got, ok := findCandidate(res.candidates, "openai/legs-new")
		require.True(t, ok, "the nil-coding row clears the floor on its reviewer prior and must be a candidate")
		assert.InDelta(t, want, got.CoderPrior, 1e-12)
		assert.True(t, res.estimated["openai/legs-new"])
	})

	t.Run("estimate capped below the highest measured coder", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		maxCoding, maxIntel := maxIndices(aa)

		est := fitCoderEstimator(aa, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled)

		slope, intercept, sd, _, maxCoder, _ := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		require.Greater(t, slope*norm(new(100.0), maxIntel)+intercept-sd, maxCoder-0.01,
			"fixture must overshoot the cap so the capped path is exercised")

		res := build(aa, orCatalog(), floor, nil)

		got, ok := findCandidate(res.candidates, "openai/legs-frontier")
		require.True(t, ok)
		assert.InDelta(t, maxCoder-0.01, got.CoderPrior, 1e-12, "the raw fit must be clipped to the cap")
		assert.Less(t, got.CoderPrior, maxCoder, "an estimate never ties the best measured coder")
		assert.True(t, res.estimated["openai/legs-frontier"])
	})

	t.Run("measured row is untouched and unflagged", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		maxCoding, _ := maxIndices(aa)
		res := build(aa, orCatalog(), floor, nil)

		got, ok := findCandidate(res.candidates, "openai/fit-row-23")
		require.True(t, ok)
		assert.InDelta(t, norm(aa[23].CodingIndex, maxCoding), got.CoderPrior, 1e-12)
		assert.False(t, res.estimated["openai/fit-row-23"])
	})

	t.Run("estimated set is exactly the estimated slugs", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		res := build(aa, orCatalog(), floor, nil)

		var flagged []string

		for _, c := range res.candidates {
			if res.estimated[c.Slug] {
				flagged = append(flagged, c.Slug)
			}
		}

		slices.Sort(flagged)

		want := []string{"openai/legs-frontier", "openai/legs-new"}
		assert.Equal(t, want, flagged,
			"exactly the two nil-coding fixture rows may carry the estimate flag")
	})

	t.Run("fewer than 20 fit rows leaves the prior at 0 and flags nothing", func(t *testing.T) {
		t.Parallel()

		aa := slices.Concat(coderFixture(1)[:19], []aaModel{{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)}})
		maxCoding, maxIntel := maxIndices(aa)

		_, _, _, _, _, n := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		require.Equal(t, 19, n, "the size guard must be what disables the fit")

		res := build(aa, orCatalog(), floor, nil)

		// legs-new still clears the floor on its reviewer prior; without an
		// enabled estimator its coder prior stays 0.
		got, ok := findCandidate(res.candidates, "openai/legs-new")
		require.True(t, ok)
		assert.Zero(t, got.CoderPrior)
		assert.Empty(t, res.estimated)
	})

	t.Run("weak correlation leaves the prior at 0 and flags nothing", func(t *testing.T) {
		t.Parallel()

		aa := append(coderFixture(20), aaModel{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)})
		maxCoding, maxIntel := maxIndices(aa)

		_, _, _, r, _, n := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		require.Equal(t, 24, n, "the size guard must not be what disables the fit")
		require.Less(t, r, 0.8, "the correlation guard must be what disables the fit")

		res := build(aa, orCatalog(), floor, nil)

		got, ok := findCandidate(res.candidates, "openai/legs-new")
		require.True(t, ok)
		assert.Zero(t, got.CoderPrior)
		assert.Empty(t, res.estimated)
	})

	t.Run("collapse prefers a measured row over an estimated one for the same slug", func(t *testing.T) {
		t.Parallel()

		// glm-5.2 and glm-5-2 both map to the OR slug z-ai/glm-5.2: the
		// first carries a real coding index, the second only an intelligence
		// index whose estimate wins on combined priors. The measured row
		// must win the collapse anyway.
		aa := append(coderFixture(1),
			aaModel{Slug: "glm-5.2", Creator: "z-ai", CodingIndex: new(50.0), IntelIndex: new(40.0)},
			aaModel{Slug: "glm-5-2", Creator: "z-ai", IntelIndex: new(60.0)},
		)
		maxCoding, maxIntel := maxIndices(aa)

		est := fitCoderEstimator(aa, nil, floor, maxCoding, maxIntel)
		require.True(t, est.enabled)

		estimatedPrior, ok := est.estimate(aaModel{Slug: "glm-5-2", Creator: "z-ai", IntelIndex: new(60.0)})
		require.True(t, ok)

		measuredCombined := norm(new(50.0), maxCoding) + norm(new(40.0), maxIntel)
		require.Greater(t, estimatedPrior+norm(new(60.0), maxIntel), measuredCombined,
			"fixture must make the estimated row win on priors so the preference rule is exercised")

		res := build(aa, orCatalog(), floor, nil)

		got, ok := findCandidate(res.candidates, "z-ai/glm-5.2")
		require.True(t, ok)
		assert.InDelta(t, norm(new(50.0), maxCoding), got.CoderPrior, 1e-12,
			"the measured row's coding index must survive the collapse")
		assert.False(t, res.estimated["z-ai/glm-5.2"],
			"the winning row is measured, so the slug is not flagged estimated")
	})
}

func TestBuildEndpointCandidatesEstimatesCoderPrior(t *testing.T) {
	t.Parallel()

	const floor = 0.25

	t.Run("nil-coding row gets fit minus sd, flagged estimated", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		endpoint := map[string]orEntry{"legs-new": {ContextWindow: 1000, Tools: true}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, nil, floor, nil, "")
		require.Len(t, scored, 1)

		want := wantEstimate(t, aa, 60.0, nil, floor)
		require.Greater(t, want, 0.0)
		assert.InDelta(t, want, scored[0].Candidate.CoderPrior, 1e-12)
		assert.True(t, scored[0].Estimated)
		assert.Equal(t, joinAutomatic, scored[0].Join)
	})

	t.Run("estimate capped below the highest measured coder", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		maxCoding, maxIntel := maxIndices(aa)

		_, _, _, _, maxCoder, _ := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		lim := maxCoder - 0.01

		endpoint := map[string]orEntry{"legs-frontier": {ContextWindow: 1000, Tools: true}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, nil, floor, nil, "")
		require.Len(t, scored, 1)

		assert.InDelta(t, lim, scored[0].Candidate.CoderPrior, 1e-12)
		assert.Less(t, scored[0].Candidate.CoderPrior, maxCoder)
		assert.True(t, scored[0].Estimated)
	})

	t.Run("measured row is untouched and unflagged", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		maxCoding, _ := maxIndices(aa)
		endpoint := map[string]orEntry{"fit-row-23": {ContextWindow: 1000, Tools: true}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, nil, floor, nil, "")
		require.Len(t, scored, 1)

		assert.InDelta(t, norm(aa[23].CodingIndex, maxCoding), scored[0].Candidate.CoderPrior, 1e-12)
		assert.False(t, scored[0].Estimated)
	})

	t.Run("fewer than 20 fit rows leaves the prior at 0 and flags nothing", func(t *testing.T) {
		t.Parallel()

		aa := slices.Concat(coderFixture(1)[:19], []aaModel{{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)}})
		endpoint := map[string]orEntry{"legs-new": {ContextWindow: 1000, Tools: true}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, nil, floor, nil, "")
		require.Len(t, scored, 1)
		assert.Zero(t, scored[0].Candidate.CoderPrior)
		assert.False(t, scored[0].Estimated)
	})

	t.Run("weak correlation leaves the prior at 0 and flags nothing", func(t *testing.T) {
		t.Parallel()

		aa := slices.Concat(coderFixture(20), []aaModel{{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)}})
		maxCoding, maxIntel := maxIndices(aa)

		_, _, _, r, _, n := expectedFit(t, aa, nil, floor, maxCoding, maxIntel)
		require.Equal(t, 24, n)
		require.Less(t, r, 0.8)

		endpoint := map[string]orEntry{"legs-new": {ContextWindow: 1000, Tools: true}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, nil, floor, nil, "")
		require.Len(t, scored, 1)
		assert.Zero(t, scored[0].Candidate.CoderPrior)
		assert.False(t, scored[0].Estimated)
	})

	t.Run("model_priors entries are never estimated", func(t *testing.T) {
		t.Parallel()

		aa := legsFixture(1)
		endpoint := map[string]orEntry{"private-1": {ContextWindow: 1000, Tools: true}}
		priors := map[string]PriorOverride{"private-1": {Coder: 0.9, Reviewer: 0.8}}

		scored, _, _ := buildEndpointCandidates(aa, endpoint, priors, floor, nil, "")
		require.Len(t, scored, 1)
		assert.Equal(t, joinModelPriors, scored[0].Join)
		assert.False(t, scored[0].Estimated)
		assert.InDelta(t, 0.9, scored[0].Candidate.CoderPrior, 1e-12)
	})
}

// TestBuilderProvenanceFlagsEstimates drives refresh on the OpenRouter leg:
// the provenance flag lands exactly on the slugs build estimated, and a
// measured candidate is never flagged.
func TestBuilderProvenanceFlagsEstimates(t *testing.T) {
	t.Parallel()

	aa := legsFixture(1)
	aaSrv := httptestServer(t, aaJSON(aa))

	defer aaSrv.Close()

	orSrv := httptestServer(t, []byte(`{"data":[{"id":"openai/legs-new","context_length":1000,
		"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]},
		{"id":"openai/fit-row-0","context_length":1000,
		"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}]}`))
	defer orSrv.Close()

	b := NewBuilder("aa-key", 0.25, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	cands := b.Candidates(t.Context())
	require.Len(t, cands, 2)

	prov := b.Provenance(t.Context())
	require.Len(t, prov, 2)

	assert.True(t, prov["openai/legs-new"].CoderPriorEstimated,
		"the nil-coding row's fallback prior must be flagged in the provenance")
	assert.False(t, prov["openai/fit-row-0"].CoderPriorEstimated,
		"the measured row must not be flagged")

	// Cross-check against the pure build on the same fixture: the flagged
	// set is exactly the estimated set.
	var flagged []string

	for slug, p := range prov {
		if p.CoderPriorEstimated {
			flagged = append(flagged, slug)
		}
	}

	assert.Equal(t, []string{"openai/legs-new"}, flagged)
}

// TestBuilderProvenanceMeasuredRecovered drives two refreshes: the first
// response leaves legs-new without a coding index (estimated), the second
// publishes one, so the flag drops and the candidate carries the measured
// prior again.
func TestBuilderProvenanceMeasuredRecovered(t *testing.T) {
	t.Parallel()

	aaFirst := legsFixture(1)
	aaSecond := slices.Clone(aaFirst)

	for i := range aaSecond {
		if aaSecond[i].Slug == "legs-new" {
			aaSecond[i].CodingIndex = new(55.0)
		}
	}

	page := 0

	aaSrv := dynamicServer(t, func() []byte {
		page++
		if page == 1 {
			return aaJSON(aaFirst)
		}

		return aaJSON(aaSecond)
	})

	defer aaSrv.Close()

	orSrv := httptestServer(t, []byte(`{"data":[{"id":"openai/legs-new","context_length":1000,
		"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}]}`))
	defer orSrv.Close()

	b := NewBuilder("aa-key", 0.25, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	ctx := t.Context()

	require.Len(t, b.Candidates(ctx), 1)
	assert.True(t, b.Provenance(ctx)["openai/legs-new"].CoderPriorEstimated)

	b.lastRefreshAttempt = time.Time{} // force the second refresh past the cooldown
	b.cachedAt = time.Time{}           // and past the TTL
	cands := b.Candidates(ctx)
	require.Len(t, cands, 1)

	assert.False(t, b.Provenance(ctx)["openai/legs-new"].CoderPriorEstimated,
		"a measured coding index clears the estimate flag on the next refresh")

	maxCoding, _ := maxIndices(aaSecond)
	assert.InDelta(t, norm(new(55.0), maxCoding), cands[0].CoderPrior, 1e-12)
}

// --- log capture -----------------------------------------------------------

// captureLogs installs a slog handler over buf for the duration of the test
// and returns it, so assertions can check what refresh logged.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return &buf
}

// TestProvenanceMeasuredRecoveryLogging drives refresh on the OpenRouter leg
// and asserts the measured-recovery log line fires only when a previously
// estimated slug now carries a measured coding index - not when it stays
// estimated or drops out of the candidate set.
// Sequential: captureLogs installs a process-wide slog handler.
func TestProvenanceMeasuredRecoveryLogging(t *testing.T) {
	// First refresh: legs-new and legs-guard estimated (the 24-row fit set
	// enables the fallback); legs-removed is not in the OR catalog.
	aaFirst := append(legsFixture(1),
		aaModel{Slug: "legs-removed", Creator: "openai", IntelIndex: new(70.0)},
		aaModel{Slug: "legs-guard", Creator: "openai", IntelIndex: new(70.0)},
	)

	// Second refresh: legs-new now carries a measured coding index and
	// joins the fit set, so the fallback stays enabled; legs-guard stays
	// estimated (its coding index is still nil) and legs-removed is still
	// not in the OR catalog.
	aaSecond := slices.Clone(aaFirst)
	aaSecond = slices.DeleteFunc(aaSecond, func(m aaModel) bool { return m.Slug == "legs-new" })
	aaSecond = append(aaSecond, aaModel{Slug: "legs-new", Creator: "openai", CodingIndex: new(55.0), IntelIndex: new(60.0)})

	page := 0

	aaSrv := dynamicServer(t, func() []byte {
		page++
		if page == 1 {
			return aaJSON(aaFirst)
		}

		return aaJSON(aaSecond)
	})

	defer aaSrv.Close()

	orSrv := httptestServer(t, []byte(`{"data":[
		{"id":"openai/legs-new","context_length":1000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]},
		{"id":"openai/legs-guard","context_length":1000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]},
		{"id":"openai/fit-row-0","context_length":1000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}
	]}`))
	defer orSrv.Close()

	b := NewBuilder("aa-key", 0.25, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	ctx := t.Context()

	// First refresh: legs-new and legs-guard estimated (the 24-row fit set
	// enables the fallback); legs-removed is not in the OR catalog.
	buf := captureLogs(t)
	require.Len(t, b.Candidates(ctx), 3)

	assert.Contains(t, buf.String(), "coder prior estimated from the intelligence index",
		"the first refresh must log the estimated slugs")
	assert.NotContains(t, buf.String(), "coder prior now measured",
		"no slug can have recovered on the first refresh")

	// Second refresh: legs-new measured; legs-guard stays estimated (its
	// coding index is still nil and the fit set is still above the size
	// guard); legs-removed is still not in the OR catalog.
	buf.Reset()
	b.lastRefreshAttempt = time.Time{}
	b.cachedAt = time.Time{}

	require.Len(t, b.Candidates(ctx), 3)

	logs := buf.String()
	assert.Contains(t, logs, `slugs=openai/legs-new`,
		"the recovery line must name the slug whose coding index is now measured")

	// The recovery line must name legs-new and nothing else: legs-guard is
	// still estimated and legs-removed left the candidate set.
	assert.Regexp(t, `msg="coder prior now measured[^"]*" slugs=openai/legs-new\n`, logs)
}

// TestProvenanceMeasuredRecoveryGuardAndRemoval drive the two silent cases:
// a previously estimated slug removed from the candidate set, and a
// guard-disabled fit, must not log a measured recovery.
// Sequential: captureLogs installs a process-wide slog handler.
func TestProvenanceMeasuredRecoveryGuardAndRemoval(t *testing.T) {
	// First refresh: the full fixture, legs-new estimated.
	aaFirst := legsFixture(1)

	// Second refresh: the fit guard trips (fewer than 20 rows qualify) and
	// legs-new is removed from the OR catalog entirely - both silent cases
	// at once. The coding index is still nil: nothing was measured.
	aaSecond := slices.Concat(coderFixture(1)[:10], []aaModel{
		{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)},
	})

	page := 0

	aaSrv := dynamicServer(t, func() []byte {
		page++
		if page == 1 {
			return aaJSON(aaFirst)
		}

		return aaJSON(aaSecond)
	})

	defer aaSrv.Close()

	orFirst := `{"data":[{"id":"openai/legs-new","context_length":1000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}]}`
	orSecond := `{"data":[]}`

	page2 := 0

	orSrv := dynamicServer(t, func() []byte {
		page2++
		if page2 == 1 {
			return []byte(orFirst)
		}

		return []byte(orSecond)
	})

	defer orSrv.Close()

	b := NewBuilder("aa-key", 0.25, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	ctx := t.Context()

	buf := captureLogs(t)
	require.Len(t, b.Candidates(ctx), 1)
	assert.True(t, b.Provenance(ctx)["openai/legs-new"].CoderPriorEstimated)

	buf.Reset()
	b.lastRefreshAttempt = time.Time{}
	b.cachedAt = time.Time{}

	// The second build produces no candidates (empty OR catalog).
	require.Empty(t, b.Candidates(ctx))

	logs := buf.String()
	assert.NotContains(t, logs, "coder prior now measured",
		"a removed candidate and a guard-disabled fit must not log a measured recovery")
}

// TestProvenanceMeasuredRecoveryGuardOnlyDoesNotLog drives the guard case in
// isolation: the second AA response leaves fewer than 20 fit rows, so the
// fallback disables while the OR catalog keeps serving legs-new. Its prior
// drops to 0 and the estimate flag clears, but with no measured coding index
// that must stay silent - no measured-recovery line.
// Sequential: captureLogs installs a process-wide slog handler.
func TestProvenanceMeasuredRecoveryGuardOnlyDoesNotLog(t *testing.T) {
	aaFirst := legsFixture(1)

	// Second refresh: fewer than 20 rows carry both indices, so the size
	// guard trips; legs-new is still served and its coding index is still
	// nil.
	aaSecond := slices.Concat(coderFixture(1)[:19], []aaModel{
		{Slug: "legs-new", Creator: "openai", IntelIndex: new(60.0)},
	})

	page := 0

	aaSrv := dynamicServer(t, func() []byte {
		page++
		if page == 1 {
			return aaJSON(aaFirst)
		}

		return aaJSON(aaSecond)
	})

	defer aaSrv.Close()

	orSrv := httptestServer(t, []byte(`{"data":[{"id":"openai/legs-new","context_length":1000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]}]}`))
	defer orSrv.Close()

	b := NewBuilder("aa-key", 0.25, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	ctx := t.Context()

	buf := captureLogs(t)
	require.Len(t, b.Candidates(ctx), 1)
	assert.True(t, b.Provenance(ctx)["openai/legs-new"].CoderPriorEstimated)

	buf.Reset()

	b.lastRefreshAttempt = time.Time{}
	b.cachedAt = time.Time{}

	// legs-new stays served; only its prior (back to 0) and estimate flag
	// change.
	cands := b.Candidates(ctx)
	require.Len(t, cands, 1)
	assert.Zero(t, cands[0].CoderPrior)
	assert.False(t, b.Provenance(ctx)["openai/legs-new"].CoderPriorEstimated)

	logs := buf.String()
	assert.NotContains(t, logs, "coder prior now measured",
		"a guard-disabled fit must not log a measured recovery")
	assert.NotContains(t, logs, "coder prior estimated from the intelligence index",
		"with the guard tripped nothing is estimated, so the estimate line must stay silent too")
}

// --- helpers ---------------------------------------------------------------

// aaJSON renders aaModel rows as the AA page fixture fetchAAModels decodes.
func aaJSON(rows []aaModel) []byte {
	data := make([]string, 0, len(rows))

	for _, m := range rows {
		evals := make([]string, 0, 2)
		if m.CodingIndex != nil {
			evals = append(evals, fmt.Sprintf(`"artificial_analysis_coding_index":%g`, *m.CodingIndex))
		}

		if m.IntelIndex != nil {
			evals = append(evals, `"artificial_analysis_intelligence_index":`+fmt.Sprintf("%g", *m.IntelIndex))
		}

		data = append(data, fmt.Sprintf(`{"slug":%q,"model_creator":{"name":%q},"evaluations":{%s}}`,
			m.Slug, m.Creator, strings.Join(evals, ",")))
	}

	return []byte(`{"data":[` + strings.Join(data, ",") + `]}`)
}

// httptestServer serves body verbatim for the lifetime of the test.
func httptestServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return srv
}

// dynamicServer serves the bytes its body closure returns, so a test can
// change the fixture between requests.
func dynamicServer(t *testing.T, body func() []byte) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body())
	}))
	t.Cleanup(srv.Close)

	return srv
}
