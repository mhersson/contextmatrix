package modelcatalog

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAppliesFloorAllowlistAndMapping(t *testing.T) {
	aa := []aaModel{
		{Slug: "glm-5-2", Creator: "z-ai", CodingIndex: new(76.5), IntelIndex: new(59.9)},  // max => norm 1.0
		{Slug: "weak-1", Creator: "openai", CodingIndex: new(30.0), IntelIndex: new(20.0)}, // norm .39 < floor .65
		{Slug: "untrusted-x", Creator: "longcat", CodingIndex: new(float64(70)), IntelIndex: new(float64(50))},
	}
	or := map[string]orEntry{
		"z-ai/glm-5.2":  {PromptPrice: 1.2e-6, CompletionPrice: 4.1e-6, ContextWindow: 1048576, Tools: true},
		"openai/weak-1": {PromptPrice: 1e-7, CompletionPrice: 2e-7, ContextWindow: 8192, Tools: true},
	}

	got := build(aa, or, 0.65, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (glm only), got %d: %+v", len(got), got)
	}

	c := got[0]
	if c.Slug != "z-ai/glm-5.2" || c.CoderPrior != 1.0 || c.ReviewerPrior != 1.0 || c.ContextWindow != 1048576 {
		t.Errorf("bad candidate: %+v", c)
	}

	if c.Creator != "z-ai" {
		t.Errorf("candidate must carry the creator prefix, got %q", c.Creator)
	}
}

func TestBuildCollapsesEffortVariants(t *testing.T) {
	// Two AA slugs that map to the SAME OR slug (z-ai/glm-5.2); the
	// higher-prior variant must win the collapse. Weaker is listed first
	// so the replacement branch in build() is exercised.
	aa := []aaModel{
		{Slug: "glm-5.2", Creator: "z-ai", CodingIndex: new(50.0), IntelIndex: new(40.0)}, // weaker
		{Slug: "glm-5-2", Creator: "z-ai", CodingIndex: new(76.5), IntelIndex: new(59.9)}, // stronger (index max)
	}
	or := map[string]orEntry{
		"z-ai/glm-5.2": {PromptPrice: 1.2e-6, CompletionPrice: 4.1e-6, ContextWindow: 1048576, Tools: true},
	}

	got := build(aa, or, 0.65, nil)
	if len(got) != 1 {
		t.Fatalf("effort variants must collapse to 1 candidate, got %d: %+v", len(got), got)
	}

	if got[0].CoderPrior != 1.0 || got[0].ReviewerPrior != 1.0 {
		t.Errorf("collapse must keep the highest-prior variant, got %+v", got[0])
	}

	if got[0].Creator != "z-ai" {
		t.Errorf("creator must survive the collapse, got %q", got[0].Creator)
	}
}

// TestBuildFromAAMapExactRowAndExclusions covers the openai-leg build: exact-row
// AA joins and the exclusion diagnostics for every non-candidate (unscored row
// with sibling suggestions, missing AA slug, unmapped slug, tool-incapable).
func TestBuildFromAAMapExactRowAndExclusions(t *testing.T) {
	aa := []aaModel{
		// base row of the model-a family: unscored (AA populates variants only)
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: nil, IntelIndex: nil},
		// higher-scoring sibling variants that exact-row scoring must ignore
		{Slug: "vendor-x-1-thinking", Creator: "vendor", CodingIndex: new(float64(80)), IntelIndex: new(float64(80))},
		{Slug: "vendor-x-1-high", Creator: "vendor", CodingIndex: new(float64(76.5)), IntelIndex: new(float64(59.9))},
		// an unrelated family, scored on both axes
		{Slug: "vendor-y-2", Creator: "vendor", CodingIndex: new(float64(70)), IntelIndex: new(float64(70))},
	}
	endpoint := map[string]orEntry{
		"model-a":  {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true},
		"model-b":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: false},
		"model-c":  {PromptPrice: 5e-6, CompletionPrice: 25e-6, ContextWindow: 200000, Tools: true},
		"model-d":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"model-e":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"model-g":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"model-h":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"no-tools": {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: false},
	}
	aaModelMap := map[string]string{
		"model-a": "vendor-x-1",           // unscored base row: excluded with siblings
		"model-b": "vendor-x-1",           // tool-incapable: dropped before scoring
		"model-c": "vendor-x-1",           // overridden: AA join skipped
		"model-d": "vendor-x-1",           // second unscored-row exclusion
		"model-e": "vendor-y-2",           // scored row, both axes
		"model-g": "vendor-ghost-missing", // mapped to a nonexistent AA slug
		// model-h: unmapped
		// no-tools: tool-incapable, dropped before any mapping lookup
	}
	priors := map[string]PriorOverride{"model-c": {Coder: 0.9, Reviewer: 0.88}}

	scored, exclusions := buildFromAAMap(aa, endpoint, aaModelMap, priors, 0.65)

	bySlug := map[string]protocol.CandidateModel{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s.Candidate
	}

	// model-e: exact scored row - only that row's normalized indices against
	// the response-wide maxima (80 coding / 80 intelligence).
	require.Contains(t, bySlug, "model-e")
	assert.InDelta(t, 70.0/80, bySlug["model-e"].CoderPrior, 1e-9)
	assert.InDelta(t, 70.0/80, bySlug["model-e"].ReviewerPrior, 1e-9)

	// model-c: override wins verbatim, AA join skipped, creator unknown.
	require.Contains(t, bySlug, "model-c")
	assert.InDelta(t, 0.9, bySlug["model-c"].CoderPrior, 1e-9)
	assert.InDelta(t, 0.88, bySlug["model-c"].ReviewerPrior, 1e-9)
	assert.Empty(t, bySlug["model-c"].Creator)

	// model-b and no-tools: endpoint marks them tool-incapable - never scored,
	// never excluded (the endpoint's capability flag is not an exclusion).
	assert.NotContains(t, bySlug, "model-b")
	assert.NotContains(t, bySlug, "no-tools")
	require.Len(t, scored, 2)

	byExcl := map[string]aaExclusion{}
	for _, x := range exclusions {
		byExcl[x.Slug] = x
	}

	// model-a: mapped to the unscored base row - excluded, and the scored
	// sibling variants are named (display-only re-pointing hint).
	require.Contains(t, byExcl, "model-a")
	assert.Equal(t, exclUnscored, byExcl["model-a"].Reason)

	siblings := map[string]aaSibling{}
	for _, sib := range byExcl["model-a"].Siblings {
		siblings[sib.Slug] = sib
	}

	require.Len(t, siblings, 2, "both scored sibling variants must be suggested")
	assert.InDelta(t, 1.0, siblings["vendor-x-1-thinking"].Coder, 1e-9)
	assert.InDelta(t, 1.0, siblings["vendor-x-1-thinking"].Reviewer, 1e-9)
	assert.InDelta(t, 76.5/80, siblings["vendor-x-1-high"].Coder, 1e-9)
	assert.InDelta(t, 59.9/80, siblings["vendor-x-1-high"].Reviewer, 1e-9)
	assert.NotContains(t, siblings, "vendor-y-2", "other families must not be suggested")
	assert.NotContains(t, siblings, "vendor-x-1", "the mapped row itself is not a sibling")

	// model-d: duplicate unscored mapping - same exclusion shape, no candidate.
	require.Contains(t, byExcl, "model-d")
	assert.Equal(t, exclUnscored, byExcl["model-d"].Reason)

	// model-g: mapped AA slug does not exist in the catalog.
	require.Contains(t, byExcl, "model-g")
	assert.Equal(t, exclAASlugMissing, byExcl["model-g"].Reason)
	assert.Empty(t, byExcl["model-g"].Siblings)

	// model-h: no aa_model_map entry and no override.
	require.Contains(t, byExcl, "model-h")
	assert.Equal(t, exclUnmapped, byExcl["model-h"].Reason)

	require.Len(t, exclusions, 4)
}

// TestBuildFromAAMapExactRowHit pins the root cause this change fixes: a
// served slug mapped to a SCORED AA row gets exactly that row's normalized
// indices, even when a higher-scoring sibling variant exists in the family.
func TestBuildFromAAMapExactRowHit(t *testing.T) {
	aa := []aaModel{
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: new(float64(40)), IntelIndex: new(float64(40))},
		{Slug: "vendor-x-1-thinking", Creator: "vendor", CodingIndex: new(float64(80)), IntelIndex: new(float64(80))},
	}
	endpoint := map[string]orEntry{
		"model-a": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true},
	}
	aaModelMap := map[string]string{"model-a": "vendor-x-1"}

	scored, exclusions := buildFromAAMap(aa, endpoint, aaModelMap, nil, 0.4)
	require.Empty(t, exclusions)
	require.Len(t, scored, 1)

	got := scored[0]
	assert.Equal(t, "model-a", got.Candidate.Slug)
	assert.Equal(t, "vendor-x-1", got.Source, "source must name the exact AA row")
	assert.InDelta(t, 0.5, got.Candidate.CoderPrior, 1e-9, "must use the mapped row's coder index, not the sibling's")
	assert.InDelta(t, 0.5, got.Candidate.ReviewerPrior, 1e-9, "must use the mapped row's reviewer index, not the sibling's")
	assert.Equal(t, "vendor", got.Candidate.Creator)
	assert.Equal(t, 200000, got.Candidate.ContextWindow)
	assert.InDelta(t, 3e-6, got.Candidate.PromptPricePerTok, 1e-12)
}

// TestBuildFromAAMapBelowFloorExclusion covers the exclBelowFloor branch for
// the AA-join path: a scored mapped row whose normalized priors both fall
// below the floor is excluded rather than scored.
func TestBuildFromAAMapBelowFloorExclusion(t *testing.T) {
	aa := []aaModel{
		{Slug: "strong-1", Creator: "vendor", CodingIndex: new(float64(80)), IntelIndex: new(float64(80))},
		{Slug: "weak-1", Creator: "vendor", CodingIndex: new(float64(10)), IntelIndex: new(float64(10))},
	}
	endpoint := map[string]orEntry{
		"model-a": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true},
		"model-b": {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
	}
	aaModelMap := map[string]string{
		"model-a": "strong-1", // clears the floor: candidate
		"model-b": "weak-1",   // normalized 0.125/0.125, both below floor 0.65
	}

	scored, exclusions := buildFromAAMap(aa, endpoint, aaModelMap, nil, 0.65)

	require.Len(t, scored, 1)
	assert.Equal(t, "model-a", scored[0].Candidate.Slug)

	require.Len(t, exclusions, 1)
	assert.Equal(t, "model-b", exclusions[0].Slug)
	assert.Equal(t, exclBelowFloor, exclusions[0].Reason)
	assert.Empty(t, exclusions[0].Siblings)
}

// TestBuildFromAAMapOverrideBeatsAAMap pins override precedence: model_priors
// values pass through verbatim and the AA join is skipped entirely (a missing
// or poisoned AA catalog must not matter).
func TestBuildFromAAMapOverrideBeatsAAMap(t *testing.T) {
	aa := []aaModel{
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: new(float64(80)), IntelIndex: new(float64(80))},
	}
	endpoint := map[string]orEntry{
		"model-a": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true},
	}
	priors := map[string]PriorOverride{"model-a": {Coder: 0.42, Reviewer: 0.37}}

	// Floor 0.3: the override values clear it, so an exclusion here would mean
	// the AA join (or a floor re-check) was wrongly applied to the override.
	scored, exclusions := buildFromAAMap(aa, endpoint, map[string]string{"model-a": "vendor-x-1"}, priors, 0.3)
	require.Empty(t, exclusions)
	require.Len(t, scored, 1)

	assert.Equal(t, "model-a", scored[0].Candidate.Slug)
	assert.Equal(t, "model_priors override", scored[0].Source)
	assert.InDelta(t, 0.42, scored[0].Candidate.CoderPrior, 1e-9)
	assert.InDelta(t, 0.37, scored[0].Candidate.ReviewerPrior, 1e-9)
	assert.Empty(t, scored[0].Candidate.Creator, "the skipped join leaves the creator unknown")
}

// TestBuildFromAAMapSingleAxisPriors proves per-axis nil handling: a mapped
// row scored on one axis only yields a candidate competing on that axis (the
// other prior is 0), while a row with both axes nil is excluded.
func TestBuildFromAAMapSingleAxisPriors(t *testing.T) {
	aa := []aaModel{
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: new(float64(80)), IntelIndex: nil},
		{Slug: "vendor-y-2", Creator: "vendor", CodingIndex: nil, IntelIndex: new(float64(59.9))},
		// an isolated unscored row with no scored family to suggest
		{Slug: "vendor-w-9", Creator: "vendor", CodingIndex: nil, IntelIndex: nil},
		// an unscored variant slug plus its scored base row and branch
		{Slug: "vendor-z-3", Creator: "vendor", CodingIndex: nil, IntelIndex: nil},
		{Slug: "vendor-z-3-thinking", Creator: "vendor", CodingIndex: nil, IntelIndex: nil},
		{Slug: "vendor-z-3-base", Creator: "vendor", CodingIndex: new(float64(70)), IntelIndex: new(float64(70))},
		{Slug: "vendor-z-3-branch", Creator: "vendor", CodingIndex: new(float64(76.5)), IntelIndex: new(float64(59.9))},
	}
	endpoint := map[string]orEntry{
		"coder-only":  {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"review-only": {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"unscored":    {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
		"variant-map": {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: true},
	}
	aaModelMap := map[string]string{
		"coder-only":  "vendor-x-1",
		"review-only": "vendor-y-2",
		"unscored":    "vendor-w-9",
		"variant-map": "vendor-z-3-thinking",
	}

	scored, exclusions := buildFromAAMap(aa, endpoint, aaModelMap, nil, 0.65)

	bySlug := map[string]aaScored{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s
	}

	require.Contains(t, bySlug, "coder-only")
	assert.InDelta(t, 1.0, bySlug["coder-only"].Candidate.CoderPrior, 1e-9)
	assert.InDelta(t, 0, bySlug["coder-only"].Candidate.ReviewerPrior, 1e-9)

	require.Contains(t, bySlug, "review-only")
	assert.InDelta(t, 0, bySlug["review-only"].Candidate.CoderPrior, 1e-9)
	// intel maximum is vendor-z-3-base's 70 (the highest intelligence index).
	assert.InDelta(t, 59.9/70, bySlug["review-only"].Candidate.ReviewerPrior, 1e-9)

	require.Len(t, exclusions, 2)

	byExcl := map[string]aaExclusion{}
	for _, x := range exclusions {
		byExcl[x.Slug] = x
	}

	// unscored: the mapped row has no usable scores and no scored siblings.
	require.Contains(t, byExcl, "unscored")
	assert.Equal(t, exclUnscored, byExcl["unscored"].Reason)
	assert.Empty(t, byExcl["unscored"].Siblings, "vendor-w-9 has no scored siblings to suggest")

	// variant-map: mapped to an unscored variant slug. The hint derives the
	// family base ("vendor-z-3-thinking" -> "vendor-z-3") so the operator
	// still sees the scored base row and branch. Only scored rows qualify;
	// the unscored base row itself is not suggested.
	require.Contains(t, byExcl, "variant-map")

	siblings := map[string]aaSibling{}
	for _, sib := range byExcl["variant-map"].Siblings {
		siblings[sib.Slug] = sib
	}

	require.Len(t, siblings, 2)
	assert.InDelta(t, 70.0/80, siblings["vendor-z-3-base"].Coder, 1e-9)
	assert.InDelta(t, 1.0, siblings["vendor-z-3-base"].Reviewer, 1e-9)
	assert.InDelta(t, 76.5/80, siblings["vendor-z-3-branch"].Coder, 1e-9)
	assert.InDelta(t, 59.9/70, siblings["vendor-z-3-branch"].Reviewer, 1e-9)
	assert.NotContains(t, siblings, "vendor-z-3", "the unscored base row must not be suggested")
	assert.NotContains(t, siblings, "vendor-z-3-thinking", "the mapped row itself is not a sibling")
	assert.NotContains(t, siblings, "vendor-x-1", "other families must not be suggested")
	assert.NotContains(t, siblings, "vendor-y-2", "other families must not be suggested")
}

// TestBuilderExcludesUnmappedServedModel exercises the endpoint leg through
// refresh: a served, tool-capable slug with no mapping surfaces as a WARN
// exclusion while the mapped sibling still becomes a candidate.
func TestBuilderExcludesUnmappedServedModel(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"capabilities":{"features":["tools"]}},
			{"id":"unmapped-model","context_length":128000,"pricing":{"prompt":"0.000001","completion":"0.000005"},"capabilities":{"features":["tools"]}}
		]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	prev := slog.Default()

	var buf bytes.Buffer

	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]string{"model-a": "vendor-x-1"}, nil))
	b.aaEndpoint = aaSrv.URL // package-accessible field; set directly (no existing helper)

	// The mapped model becomes a candidate; the unmapped one does not.
	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.Equal(t, "model-a", cands[0].Slug)

	// The exclusion is surfaced as a WARN naming the slug and reason.
	logs := buf.String()
	assert.Contains(t, logs, `msg="endpoint model not selectable"`)
	assert.Contains(t, logs, `slug=unmapped-model`)
	assert.Contains(t, logs, `reason="no aa_model_map or model_priors entry"`)
}

func TestBuilderUsesEndpointLegWhenConfigured(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]string{"model-a": "vendor-x-1"}, nil))
	b.aaEndpoint = aaSrv.URL // package-accessible field; set directly (no existing helper)

	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.Equal(t, "model-a", cands[0].Slug)
}

// TestBuilderCandidatesNilReceiver proves that calling Candidates on a nil
// *Builder returns nil without panicking - the nil-receiver guard protects
// against the typed-nil-interface footgun in main.go.
func TestBuilderCandidatesNilReceiver(t *testing.T) {
	var b *Builder

	// Without the nil-receiver guard this panics on b.mu.Lock() (nil receiver dereference).
	got := b.Candidates(context.Background())
	if got != nil {
		t.Errorf("nil Builder.Candidates must return nil, got %v", got)
	}
}

// TestBuilderRatePricesAnyServedModel verifies that Rate returns prices for every
// model in the raw catalog, including models that are not selection candidates
// (unmapped / below floor / picker-only).
func TestBuilderRatePricesAnyServedModel(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"capabilities":{"features":["tools"]}},
			{"id":"picker-only","context_length":128000,"pricing":{"prompt":"0.000001","completion":"0.000005"},"capabilities":{"features":["tools"]}}
		]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]string{"model-a": "vendor-x-1"}, nil))
	b.aaEndpoint = aaSrv.URL

	// picker-only is NOT a selection candidate (unmapped), but it is served and priced.
	price, ok := b.Rate(context.Background(), "picker-only")
	require.True(t, ok)
	assert.InDelta(t, 0.000001, price.Prompt, 1e-12)
	assert.InDelta(t, 0.000005, price.Completion, 1e-12)

	_, ok = b.Rate(context.Background(), "not-served")
	assert.False(t, ok)
}

// TestBuilderRatePricesEndpointWithoutAAKey verifies that Rate prices
// endpoint-served models even when no AA key is configured - the chat-only +
// openai-endpoint topology (no agent backend, no AA key).
func TestBuilderRatePricesEndpointWithoutAAKey(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000005","input_cache_write":"0.00000625"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	// No agent backend, no AA key - the chat-only + openai-endpoint topology.
	b := NewBuilder("", 0.65, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil, nil))

	price, ok := b.Rate(context.Background(), "model-a")
	require.True(t, ok, "endpoint pricing must resolve without an AA key")
	assert.InDelta(t, 0.000003, price.Prompt, 1e-12)
	assert.InDelta(t, 0.000015, price.Completion, 1e-12)
	assert.InDelta(t, 0.0000005, price.CacheRead, 1e-12)
	assert.InDelta(t, 0.00000625, price.CacheWrite, 1e-12)
}

func TestRefreshWithoutAAKeyPopulatesORCatalog(t *testing.T) {
	or := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-sonnet-4.5","context_length":200000,
			 "pricing":{"prompt":"0.000003","completion":"0.000015"},
			 "supported_parameters":["tools"]}]}`))
	}))
	defer or.Close()

	b := NewBuilder("", 0.65, nil, 0)
	b.orEndpoint = or.URL

	// No AA key: zero selection candidates, but the raw catalog is cached.
	assert.Empty(t, b.Candidates(context.Background()))

	price, ok := b.Rate(context.Background(), "anthropic/claude-sonnet-4.5")
	require.True(t, ok)
	assert.InDelta(t, 0.000003, price.Prompt, 1e-12)
	assert.InDelta(t, 0.000015, price.Completion, 1e-12)
}

// TestBuilderRateNilReceiver verifies that Rate on a nil *Builder returns false
// without panicking.
func TestBuilderRateNilReceiver(t *testing.T) {
	var b *Builder

	_, ok := b.Rate(context.Background(), "any-model")
	assert.False(t, ok)
}

// TestBuilderEndpointModelsProjectsCachedCatalog verifies that EndpointModels
// projects the Builder's cached catalog (the same /models fetch already shared
// by Candidates and Rate) to the picker's tool-capable model list, rather than
// requiring a second independent fetch.
func TestBuilderEndpointModelsProjectsCachedCatalog(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"capabilities":{"features":["tools"]}},
			{"id":"model-b","context_length":32000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"capabilities":{"features":[]}}
		]}`))
	}))
	defer endpointSrv.Close()

	b := NewBuilder("", 0.65, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil, nil))

	got := b.EndpointModels(context.Background())
	require.Len(t, got, 1)
	assert.Equal(t, "model-a", got[0].ID)
	assert.Equal(t, "model-a", got[0].Label)
	assert.Equal(t, 200000, got[0].MaxTokens)
}

// TestBuilderRefreshFailureBackoff pins the failure backoff: a failed catalog
// refresh must not be re-attempted on every call. During a provider outage
// callers get the last-good state (or nothing) without paying the fetch
// timeout, until the cooldown elapses.
func TestBuilderRefreshFailureBackoff(t *testing.T) {
	var hits atomic.Int32

	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer endpointSrv.Close()

	b := NewBuilder("", 0.65, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil, nil))

	ctx := context.Background()

	_, ok := b.Rate(ctx, "model-a")
	assert.False(t, ok)
	require.EqualValues(t, 1, hits.Load(), "first call must attempt a refresh")

	_, ok = b.Rate(ctx, "model-a")
	assert.False(t, ok)
	assert.EqualValues(t, 1, hits.Load(), "call within cooldown must not refetch")

	// Backdate the last attempt past the cooldown: the next call retries.
	b.mu.Lock()
	b.lastRefreshAttempt = time.Now().Add(-2 * refreshFailureCooldown)
	b.mu.Unlock()

	_, _ = b.Rate(ctx, "model-a")

	assert.EqualValues(t, 2, hits.Load(), "call after cooldown must retry")
}

// TestBuilderRefreshFailureServesLastGood verifies that a failed refresh after
// a successful one keeps serving the last-good catalog and still backs off.
func TestBuilderRefreshFailureServesLastGood(t *testing.T) {
	var (
		fail atomic.Bool
		hits atomic.Int32
	)

	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	b := NewBuilder("", 0.65, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil, nil))

	ctx := context.Background()

	_, ok := b.Rate(ctx, "model-a")
	require.True(t, ok)

	// Expire the TTL and make the endpoint fail: Rate must serve last-good.
	fail.Store(true)
	b.mu.Lock()
	b.cachedAt = time.Now().Add(-2 * time.Hour)
	b.lastRefreshAttempt = time.Time{}
	b.mu.Unlock()

	price, ok := b.Rate(ctx, "model-a")
	require.True(t, ok, "failed refresh must serve last-good")
	assert.InDelta(t, 0.000003, price.Prompt, 1e-12)
	require.EqualValues(t, 2, hits.Load())

	_, ok = b.Rate(ctx, "model-a")
	require.True(t, ok)
	assert.EqualValues(t, 2, hits.Load(), "failed refresh must back off, not retry per call")
}
