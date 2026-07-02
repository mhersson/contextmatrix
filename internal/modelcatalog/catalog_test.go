package modelcatalog

import (
	"context"
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
		{Slug: "glm-5-2", Creator: "zai", CodingIndex: f(76.5), IntelIndex: f(59.9)},   // max => norm 1.0
		{Slug: "weak-1", Creator: "openai", CodingIndex: f(30.0), IntelIndex: f(20.0)}, // norm .39 < floor .65
		{Slug: "untrusted-x", Creator: "longcat", CodingIndex: f(70), IntelIndex: f(50)},
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
}

func TestBuildCollapsesEffortVariants(t *testing.T) {
	// Two AA slugs that map to the SAME OR slug (z-ai/glm-5.2); the
	// higher-prior variant must win the collapse. Weaker is listed first
	// so the replacement branch in build() is exercised.
	aa := []aaModel{
		{Slug: "glm-5.2", Creator: "zai", CodingIndex: f(50.0), IntelIndex: f(40.0)}, // weaker
		{Slug: "glm-5-2", Creator: "zai", CodingIndex: f(76.5), IntelIndex: f(59.9)}, // stronger (index max)
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
}

func f(v float64) *float64 { return &v }

func TestBuildFromStemMapAggregatesFamilyFiltersToolsAndHonorsOverride(t *testing.T) {
	aa := []aaModel{
		// base row often unscored; a variant carries the score
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: nil, IntelIndex: f(50)},
		{Slug: "vendor-x-1-thinking", Creator: "vendor", CodingIndex: f(80), IntelIndex: f(80)},
		{Slug: "vendor-y-2", Creator: "vendor", CodingIndex: f(40), IntelIndex: f(40)},
	}
	endpoint := map[string]orEntry{
		"model-a": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true},
		"model-b": {PromptPrice: 1e-6, CompletionPrice: 5e-6, ContextWindow: 128000, Tools: false},
		"model-c": {PromptPrice: 5e-6, CompletionPrice: 25e-6, ContextWindow: 200000, Tools: true},
	}
	stemMap := map[string]string{
		"model-a": "vendor-x-1", // family: vendor-x-1 + vendor-x-1-thinking
		"model-b": "vendor-y-2",
	}
	// model-c is not in the stem map (AA does not rate it) but has an override.
	priors := map[string]PriorOverride{"model-c": {Coder: 0.9, Reviewer: 0.88}}

	got := buildFromStemMap(aa, endpoint, stemMap, priors, 0.65)

	bySlug := map[string]protocol.CandidateModel{}
	for _, c := range got {
		bySlug[c.Slug] = c
	}

	// model-a: per-axis max picks coder 80/80=1.0 from the -thinking row.
	require.Contains(t, bySlug, "model-a")
	assert.InDelta(t, 1.0, bySlug["model-a"].CoderPrior, 1e-9)
	assert.InDelta(t, 1.0, bySlug["model-a"].ReviewerPrior, 1e-9)
	assert.Equal(t, 200000, bySlug["model-a"].ContextWindow)
	assert.InDelta(t, 3e-6, bySlug["model-a"].PromptPricePerTok, 1e-12)

	// model-c: override used verbatim, AA join skipped, kept (tool-capable, clears floor).
	require.Contains(t, bySlug, "model-c")
	assert.InDelta(t, 0.9, bySlug["model-c"].CoderPrior, 1e-9)
	assert.InDelta(t, 0.88, bySlug["model-c"].ReviewerPrior, 1e-9)

	// model-b dropped: endpoint marks it tool-incapable.
	assert.NotContains(t, bySlug, "model-b")
	require.Len(t, got, 2)
}

func TestBuilderUsesEndpointLegWhenConfigured(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"slug":"vendor"},
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
// *Builder returns nil without panicking — the nil-receiver guard protects
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
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"slug":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]string{"model-a": "vendor-x-1"}, nil))
	b.aaEndpoint = aaSrv.URL

	// picker-only is NOT a selection candidate (unmapped), but it is served and priced.
	p, c, ok := b.Rate(context.Background(), "picker-only")
	require.True(t, ok)
	assert.InDelta(t, 0.000001, p, 1e-12)
	assert.InDelta(t, 0.000005, c, 1e-12)

	_, _, ok = b.Rate(context.Background(), "not-served")
	assert.False(t, ok)
}

// TestBuilderRatePricesEndpointWithoutAAKey verifies that Rate prices
// endpoint-served models even when no AA key is configured — the chat-only +
// openai-endpoint topology (no agent backend, no AA key).
func TestBuilderRatePricesEndpointWithoutAAKey(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	// No agent backend, no AA key — the chat-only + openai-endpoint topology.
	b := NewBuilder("", 0.65, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil, nil))

	p, c, ok := b.Rate(context.Background(), "model-a")
	require.True(t, ok, "endpoint pricing must resolve without an AA key")
	assert.InDelta(t, 0.000003, p, 1e-12)
	assert.InDelta(t, 0.000015, c, 1e-12)
}

// TestBuilderRateNilReceiver verifies that Rate on a nil *Builder returns false
// without panicking.
func TestBuilderRateNilReceiver(t *testing.T) {
	var b *Builder

	_, _, ok := b.Rate(context.Background(), "any-model")
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

	_, _, ok := b.Rate(ctx, "model-a")
	assert.False(t, ok)
	require.EqualValues(t, 1, hits.Load(), "first call must attempt a refresh")

	_, _, ok = b.Rate(ctx, "model-a")
	assert.False(t, ok)
	assert.EqualValues(t, 1, hits.Load(), "call within cooldown must not refetch")

	// Backdate the last attempt past the cooldown: the next call retries.
	b.mu.Lock()
	b.lastRefreshAttempt = time.Now().Add(-2 * refreshFailureCooldown)
	b.mu.Unlock()

	_, _, _ = b.Rate(ctx, "model-a")
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

	_, _, ok := b.Rate(ctx, "model-a")
	require.True(t, ok)

	// Expire the TTL and make the endpoint fail: Rate must serve last-good.
	fail.Store(true)
	b.mu.Lock()
	b.cachedAt = time.Now().Add(-2 * time.Hour)
	b.lastRefreshAttempt = time.Time{}
	b.mu.Unlock()

	p, _, ok := b.Rate(ctx, "model-a")
	require.True(t, ok, "failed refresh must serve last-good")
	assert.InDelta(t, 0.000003, p, 1e-12)
	require.EqualValues(t, 2, hits.Load())

	_, _, ok = b.Rate(ctx, "model-a")
	require.True(t, ok)
	assert.EqualValues(t, 2, hits.Load(), "failed refresh must back off, not retry per call")
}
