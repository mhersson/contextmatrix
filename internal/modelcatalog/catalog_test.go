package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// TestBuilderExcludesUnmatchedServedModel exercises the endpoint leg through
// refresh: a served slug with no AA family surfaces as a WARN exclusion while
// the matched sibling still becomes a candidate.
func TestBuilderExcludesUnmatchedServedModel(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor-x-1","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"capabilities":{"features":["tools"]}},
			{"id":"unmatched-model","context_length":128000,"pricing":{"prompt":"0.000001","completion":"0.000005"},"capabilities":{"features":["tools"]}}
		]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"vendor-x-1","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL // package-accessible field; set directly (no existing helper)

	// The matched model becomes a candidate; the one with no AA family does not.
	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.Equal(t, "vendor-x-1", cands[0].Slug)
}

func TestBuilderUsesEndpointLegWhenConfigured(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor-x-1","context_length":200000,
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
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL // package-accessible field; set directly (no existing helper)

	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.Equal(t, "vendor-x-1", cands[0].Slug)
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
// (no AA family / below floor / picker-only).
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
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	// picker-only is NOT a selection candidate (no AA family), but it is served and priced.
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
		WithEndpoint(endpointSrv.URL, "secret", nil))

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

	// OpenRouter echoes the served slug in every completion, so the
	// echoed-name fallback is endpoint-leg only: a name that is not a served
	// slug stays unpriced here, exactly as before.
	_, ok = b.Rate(context.Background(), "claude-sonnet-4.5")
	assert.False(t, ok)
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
		WithEndpoint(endpointSrv.URL, "secret", nil))

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
		WithEndpoint(endpointSrv.URL, "secret", nil))

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
		WithEndpoint(endpointSrv.URL, "secret", nil))

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

func TestBuilderLastRefreshed(t *testing.T) {
	var nilB *Builder

	assert.True(t, nilB.LastRefreshed(context.Background()).IsZero(), "nil receiver has no snapshot")

	// Seeded with a fresh stamp so no network refresh runs, then moved back
	// an hour: still inside the 6h TTL, so the stamp must come back as is.
	b := NewBuilder("", 0.65, nil, 0)
	seedServed(t, b, map[string]orEntry{"z-ai/glm-5.2": {ContextWindow: 131072}})

	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	b.cachedAt = stamp

	assert.Equal(t, stamp, b.LastRefreshed(context.Background()))
}

// TestCandidatePricePrecedence pins the selector-facing price order: the
// gateway's own price, else the AA list price, else the token_costs fill,
// else nothing. Rate() never sees the AA price; only candidates do.
func TestCandidatePricePrecedence(t *testing.T) {
	aaPriced := &aaModel{Slug: "row", PromptPrice: 2e-6, CompletionPrice: 8e-6}
	aaUnpriced := &aaModel{Slug: "row"}

	cases := []struct {
		name               string
		entry              orEntry
		row                *aaModel
		prompt, completion float64
		source             priceSource
	}{
		{"gateway beats aa", orEntry{PromptPrice: 1e-6, CompletionPrice: 5e-6, PriceSource: priceSourceGateway}, aaPriced, 1e-6, 5e-6, priceSourceGateway},
		{"an untagged priced entry is the gateway's", orEntry{PromptPrice: 1e-6, CompletionPrice: 5e-6}, aaPriced, 1e-6, 5e-6, priceSourceGateway},
		{"aa beats token_costs", orEntry{PromptPrice: 3e-6, CompletionPrice: 15e-6, PriceSource: priceSourceTokenCosts}, aaPriced, 2e-6, 8e-6, priceSourceAA},
		{"aa on an unpriced entry", orEntry{}, aaPriced, 2e-6, 8e-6, priceSourceAA},
		{"token_costs when aa is unpriced", orEntry{PromptPrice: 3e-6, CompletionPrice: 15e-6, PriceSource: priceSourceTokenCosts}, aaUnpriced, 3e-6, 15e-6, priceSourceTokenCosts},
		{"token_costs on the priors path", orEntry{PromptPrice: 3e-6, CompletionPrice: 15e-6, PriceSource: priceSourceTokenCosts}, nil, 3e-6, 15e-6, priceSourceTokenCosts},
		{"nothing", orEntry{}, aaUnpriced, 0, 0, priceSourceNone},
		{"nothing on the priors path", orEntry{}, nil, 0, 0, priceSourceNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, completion, source := candidatePrice(tc.entry, tc.row)
			assert.InDelta(t, tc.prompt, prompt, 1e-15)
			assert.InDelta(t, tc.completion, completion, 1e-15)
			assert.Equal(t, tc.source, source)
		})
	}
}

// TestBuildEndpointCandidatesPricesCandidates proves the build routes both
// paths through candidatePrice: the AA join takes the row's list price over
// the fill, and the model_priors path (no row) takes the fill.
func TestBuildEndpointCandidatesPricesCandidates(t *testing.T) {
	aa := []aaModel{
		{Slug: "vendor-x-1", Creator: "vendor", CodingIndex: new(80.0), IntelIndex: new(80.0), PromptPrice: 2e-6, CompletionPrice: 8e-6},
	}
	endpoint := map[string]orEntry{
		"vendor-x-1": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true, PriceSource: priceSourceTokenCosts},
		"model-c":    {PromptPrice: 5e-6, CompletionPrice: 25e-6, ContextWindow: 200000, Tools: true, PriceSource: priceSourceTokenCosts},
	}
	priors := map[string]PriorOverride{"model-c": {Coder: 0.9, Reviewer: 0.88}}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, priors, 0.65, []string{"vendor"}, "")
	require.Empty(t, exclusions)
	require.Len(t, scored, 2)

	bySlug := map[string]aaScored{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s
	}

	assert.Equal(t, priceSourceAA, bySlug["vendor-x-1"].PriceSource)
	assert.InDelta(t, 2e-6, bySlug["vendor-x-1"].Candidate.PromptPricePerTok, 1e-15)
	assert.InDelta(t, 8e-6, bySlug["vendor-x-1"].Candidate.CompletionPricePerTok, 1e-15)

	assert.Equal(t, priceSourceTokenCosts, bySlug["model-c"].PriceSource)
	assert.InDelta(t, 5e-6, bySlug["model-c"].Candidate.PromptPricePerTok, 1e-15)
}

// TestBuilderRateAdoptsAAListPriceWhenGatewayUnpriced: on the openai leg a
// served model the gateway left unpriced is priced for card costs from the
// AA row it was scored from, all four rates, ahead of the token_costs fill;
// the candidate and Rate() then agree.
func TestBuilderRateAdoptsAAListPriceWhenGatewayUnpriced(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":200000,
			"alias_names":["model-a"],"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"model-a","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80},
			"pricing":{"price_1m_input_tokens":2,"price_1m_output_tokens":8,"price_1m_cache_hit_tokens":0.2,"price_1m_cache_write_tokens":2.5}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil),
		WithTokenCosts(map[string]ModelPrice{"model-a": {Prompt: 3e-6, Completion: 15e-6}}))
	b.aaEndpoint = aaSrv.URL

	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.InDelta(t, 2e-6, cands[0].PromptPricePerTok, 1e-15, "the candidate carries the AA list price")
	assert.InDelta(t, 8e-6, cands[0].CompletionPricePerTok, 1e-15)

	price, ok := b.Rate(context.Background(), "vendor/model-a")
	require.True(t, ok)
	assert.InDelta(t, 2e-6, price.Prompt, 1e-15, "card costs adopt the AA list price over the token_costs fill")
	assert.InDelta(t, 8e-6, price.Completion, 1e-15)
	assert.InDelta(t, 0.2e-6, price.CacheRead, 1e-15)
	assert.InDelta(t, 2.5e-6, price.CacheWrite, 1e-15)

	assert.Equal(t, map[string]CandidateProvenance{"vendor/model-a": {PriceSource: "aa", ScoredFrom: "model-a"}},
		b.Provenance(context.Background()))
}

// TestBuilderProvenanceOpenRouterLeg: every OpenRouter candidate is priced by
// the served catalog and its AA row is not exposed, so provenance says
// gateway and nothing else.
func TestBuilderProvenanceOpenRouterLeg(t *testing.T) {
	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"z-ai/glm-5.2","context_length":1048576,
			"pricing":{"prompt":"0.0000012","completion":"0.0000041"},"supported_parameters":["tools"]}]}`))
	}))
	defer orSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"glm-5-2","model_creator":{"name":"Z AI"},
			"evaluations":{"artificial_analysis_coding_index":76.5,"artificial_analysis_intelligence_index":59.9}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	require.Len(t, b.Candidates(context.Background()), 1)
	assert.Equal(t, map[string]CandidateProvenance{"z-ai/glm-5.2": {PriceSource: "gateway"}}, b.Provenance(context.Background()))
}

func TestBuilderProvenanceNilReceiver(t *testing.T) {
	var b *Builder

	assert.Nil(t, b.Provenance(context.Background()))
}

// TestBuildEndpointCandidatesAutomaticJoin covers the openai-leg build end to
// end: the automatic family join by id and by alias, the closest-row choice,
// the price precedence per path, the allowlist screen, the model_priors
// override, and every exclusion reason.
func TestBuildEndpointCandidatesAutomaticJoin(t *testing.T) {
	aa := []aaModel{
		// gpt-5-2 family: a scored, priced base row and a stronger effort variant.
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(60.0), IntelIndex: new(60.0), PromptPrice: 1.75e-6, CompletionPrice: 14e-6},
		{Slug: "gpt-5-2-medium", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		// deepseek-v4-flash family: dated rows only, the dated base scored on one axis.
		{Slug: "deepseek-v4-flash-0420", Creator: "deepseek", CodingIndex: new(60.0), IntelIndex: nil},
		{Slug: "deepseek-v4-flash-0420-high", Creator: "deepseek", CodingIndex: new(70.0), IntelIndex: new(70.0)},
		// A family with no scored row.
		{Slug: "ghost-1", Creator: "openai"},
		// A scored family from a creator outside the built-in allowlist.
		{Slug: "outsider-1", Creator: "longcat", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		// A family below the floor on both axes.
		{Slug: "weak-1", Creator: "openai", CodingIndex: new(10.0), IntelIndex: new(10.0)},
		// The Anthropic ordering flip.
		{Slug: "claude-4-5-sonnet", Creator: "anthropic", CodingIndex: new(65.0), IntelIndex: new(65.0)},
	}
	endpoint := map[string]orEntry{
		"openai/gpt-5.2":             {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 400000, Tools: true, PriceSource: priceSourceTokenCosts},
		"deepseek-v4-flash":          {ContextWindow: 128000, Tools: true},
		"vendor-dated-alias":         {ContextWindow: 128000, Tools: true, Aliases: []string{"gpt-5.2-2026-01-15"}},
		"claude-sonnet-4-5-20250929": {PromptPrice: 3e-6, CompletionPrice: 15e-6, ContextWindow: 200000, Tools: true, PriceSource: priceSourceGateway},
		"ghost-1":                    {ContextWindow: 1000, Tools: true},
		"outsider-1":                 {ContextWindow: 1000, Tools: true},
		"weak-1":                     {ContextWindow: 1000, Tools: true},
		"nothing-like-it":            {ContextWindow: 1000, Tools: true, Aliases: []string{"still-nothing"}},
		"private-1":                  {PromptPrice: 5e-6, CompletionPrice: 25e-6, ContextWindow: 1000, Tools: true, PriceSource: priceSourceTokenCosts},
		"embed-1":                    {ContextWindow: 1000, Tools: false},
		"embed-prior-1":              {ContextWindow: 1000, Tools: false},
	}
	priors := map[string]PriorOverride{"private-1": {Coder: 0.9, Reviewer: 0.88}, "embed-prior-1": {Coder: 0.5, Reviewer: 0.5}}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, priors, 0.65, nil, "")

	bySlug := map[string]aaScored{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s
	}

	byExcl := map[string]aaExclusion{}
	for _, x := range exclusions {
		byExcl[x.Slug] = x
	}

	// openai/gpt-5.2: vendor prefix and dot handled; the scored base row wins
	// over the stronger medium variant; the AA list price beats the fill.
	require.Contains(t, bySlug, "openai/gpt-5.2")
	got := bySlug["openai/gpt-5.2"]
	assert.Equal(t, "gpt-5-2", got.Source)
	assert.Equal(t, joinAutomatic, got.Join)
	assert.Empty(t, got.Effort, "no effort named or configured")
	assert.InDelta(t, 60.0/80, got.Candidate.CoderPrior, 1e-9)
	assert.InDelta(t, 60.0/80, got.Candidate.ReviewerPrior, 1e-9)
	assert.Equal(t, "openai", got.Candidate.Creator)
	assert.Equal(t, priceSourceAA, got.PriceSource)
	assert.InDelta(t, 1.75e-6, got.Candidate.PromptPricePerTok, 1e-15)
	assert.InDelta(t, 14e-6, got.Candidate.CompletionPricePerTok, 1e-15)
	assert.Equal(t, 400000, got.Candidate.ContextWindow)

	// deepseek-v4-flash: no base row; the dated base beats the dated effort
	// variant, its nil intelligence index yields no reviewer prior, and
	// nothing prices it.
	require.Contains(t, bySlug, "deepseek-v4-flash")
	assert.Equal(t, "deepseek-v4-flash-0420", bySlug["deepseek-v4-flash"].Source)
	assert.InDelta(t, 60.0/80, bySlug["deepseek-v4-flash"].Candidate.CoderPrior, 1e-9)
	assert.Zero(t, bySlug["deepseek-v4-flash"].Candidate.ReviewerPrior)
	assert.Equal(t, priceSourceNone, bySlug["deepseek-v4-flash"].PriceSource)

	// vendor-dated-alias: the id matches nothing, the dated alias reduces to gpt-5-2.
	require.Contains(t, bySlug, "vendor-dated-alias")
	assert.Equal(t, "gpt-5-2", bySlug["vendor-dated-alias"].Source)

	// claude-sonnet-4-5-20250929: the dated vendor id reaches AA's
	// version-first slug through the built-in rewrite; the gateway price wins.
	require.Contains(t, bySlug, "claude-sonnet-4-5-20250929")
	assert.Equal(t, "claude-4-5-sonnet", bySlug["claude-sonnet-4-5-20250929"].Source)
	assert.Equal(t, priceSourceGateway, bySlug["claude-sonnet-4-5-20250929"].PriceSource)

	// private-1: model_priors verbatim, no join, no creator, the fill's price.
	require.Contains(t, bySlug, "private-1")
	assert.Equal(t, joinModelPriors, bySlug["private-1"].Join)
	assert.Equal(t, "model_priors override", bySlug["private-1"].Source)
	assert.InDelta(t, 0.9, bySlug["private-1"].Candidate.CoderPrior, 1e-9)
	assert.Empty(t, bySlug["private-1"].Candidate.Creator)
	assert.Equal(t, priceSourceTokenCosts, bySlug["private-1"].PriceSource)

	assert.NotContains(t, bySlug, "embed-1", "tool-incapable models are never scored")
	assert.NotContains(t, bySlug, "embed-prior-1", "tool-incapable models are never scored")
	require.Len(t, scored, 5)

	require.Contains(t, byExcl, "ghost-1")
	assert.Equal(t, exclUnscored, byExcl["ghost-1"].Reason)
	assert.Equal(t, []string{"ghost-1"}, byExcl["ghost-1"].Family)

	require.Contains(t, byExcl, "outsider-1")
	assert.Equal(t, exclNotAllowed, byExcl["outsider-1"].Reason)
	assert.Equal(t, "outsider-1", byExcl["outsider-1"].Source)

	require.Contains(t, byExcl, "weak-1")
	assert.Equal(t, exclBelowFloor, byExcl["weak-1"].Reason)
	assert.Equal(t, "weak-1", byExcl["weak-1"].Source)

	require.Contains(t, byExcl, "nothing-like-it")
	assert.Equal(t, exclNoFamily, byExcl["nothing-like-it"].Reason)
	assert.Equal(t, []string{"nothing-like-it", "still-nothing"}, byExcl["nothing-like-it"].Keys)

	assert.NotContains(t, byExcl, "embed-1", "the capability flag is not an exclusion when no priors entry exists")

	require.Contains(t, byExcl, "embed-prior-1")
	assert.Equal(t, exclNoTools, byExcl["embed-prior-1"].Reason)

	require.Len(t, exclusions, 5)
}

// TestBuildEndpointCandidatesPriorsBeatAutomatic pins override precedence: a
// model_priors entry wins over a family AA would have joined, verbatim, and
// skips the allowlist screen.
func TestBuildEndpointCandidatesPriorsBeatAutomatic(t *testing.T) {
	aa := []aaModel{{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)}}
	endpoint := map[string]orEntry{"gpt-5.2": {ContextWindow: 1000, Tools: true}}
	priors := map[string]PriorOverride{"gpt-5.2": {Coder: 0.42, Reviewer: 0.37}}

	// Allowlist without openai: the override must still pass.
	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, priors, 0.3, []string{"anthropic"}, "")
	require.Empty(t, exclusions)
	require.Len(t, scored, 1)
	assert.Equal(t, joinModelPriors, scored[0].Join)
	assert.InDelta(t, 0.42, scored[0].Candidate.CoderPrior, 1e-9)
	assert.InDelta(t, 0.37, scored[0].Candidate.ReviewerPrior, 1e-9)
	assert.Empty(t, scored[0].Candidate.Creator)
}

// TestBuildEndpointCandidatesAllowlistOverride: a configured allowlist
// replaces the built-in one for automatic matches, as on the OpenRouter leg.
func TestBuildEndpointCandidatesAllowlistOverride(t *testing.T) {
	aa := []aaModel{
		{Slug: "outsider-1", Creator: "longcat", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
	}
	endpoint := map[string]orEntry{
		"outsider-1": {ContextWindow: 1000, Tools: true},
		"gpt-5.2":    {ContextWindow: 1000, Tools: true},
	}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, nil, 0.65, []string{"longcat"}, "")
	require.Len(t, scored, 1)
	assert.Equal(t, "outsider-1", scored[0].Candidate.Slug)
	require.Len(t, exclusions, 1)
	assert.Equal(t, "gpt-5.2", exclusions[0].Slug)
	assert.Equal(t, exclNotAllowed, exclusions[0].Reason)
}

// TestBuildEndpointCandidatesReasoningEffort pins which AA row scores a
// served model when a reasoning effort is in play: the served id's own
// suffix first, then the gateway's configured effort, and the base row when
// the family has no row for the wanted effort.
func TestBuildEndpointCandidatesReasoningEffort(t *testing.T) {
	aa := []aaModel{
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(60.0), IntelIndex: new(60.0)},
		{Slug: "gpt-5-2-medium", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		{Slug: "gpt-5-2-high", Creator: "openai", CodingIndex: new(90.0), IntelIndex: new(90.0)},
	}
	endpoint := map[string]orEntry{
		"gpt-5.2":            {ContextWindow: 1000, Tools: true},
		"gpt-5.2-high":       {ContextWindow: 1000, Tools: true},
		"openai/gpt-5.2-low": {ContextWindow: 1000, Tools: true},
	}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, nil, 0.3, nil, "medium")
	require.Empty(t, exclusions)
	require.Len(t, scored, 3)

	bySlug := map[string]aaScored{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s
	}

	assert.Equal(t, "gpt-5-2-medium", bySlug["gpt-5.2"].Source, "a bare id takes the gateway's effort")
	assert.Equal(t, "medium", bySlug["gpt-5.2"].Effort)
	assert.InDelta(t, 80.0/90, bySlug["gpt-5.2"].Candidate.CoderPrior, 1e-9)

	assert.Equal(t, "gpt-5-2-high", bySlug["gpt-5.2-high"].Source, "the id's own suffix beats the gateway's effort")
	assert.Equal(t, "high", bySlug["gpt-5.2-high"].Effort)

	assert.Equal(t, "gpt-5-2", bySlug["openai/gpt-5.2-low"].Source, "no row for the wanted effort: the base row")
	assert.Equal(t, "low", bySlug["openai/gpt-5.2-low"].Effort)

	unset, _, _ := buildEndpointCandidates(aa, endpoint, nil, 0.3, nil, "")

	unsetBySlug := map[string]aaScored{}
	for _, s := range unset {
		unsetBySlug[s.Candidate.Slug] = s
	}

	require.Contains(t, unsetBySlug, "gpt-5.2")
	assert.Equal(t, "gpt-5-2", unsetBySlug["gpt-5.2"].Source, "no effort anywhere: the base-row rule")
	assert.Empty(t, unsetBySlug["gpt-5.2"].Effort)
}

// TestBuildEndpointCandidatesAliasEffort pins the seam where the served id
// matches no family and a gateway alias does: the alias's own effort suffix
// is the wanted effort, ahead of the gateway's configured one.
func TestBuildEndpointCandidatesAliasEffort(t *testing.T) {
	aa := []aaModel{
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(60.0), IntelIndex: new(60.0)},
		{Slug: "gpt-5-2-medium", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		{Slug: "gpt-5-2-high", Creator: "openai", CodingIndex: new(90.0), IntelIndex: new(90.0)},
	}
	endpoint := map[string]orEntry{
		"vendor-alias-only": {ContextWindow: 1000, Tools: true, Aliases: []string{"gpt-5.2-high"}},
	}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, nil, 0.3, nil, "medium")
	require.Empty(t, exclusions)
	require.Len(t, scored, 1)
	assert.Equal(t, "gpt-5-2-high", scored[0].Source, "the alias that found the family names the effort")
	assert.Equal(t, "high", scored[0].Effort)
}

// TestBuildEndpointCandidatesReasoningEffortOpenAIOnly pins that the
// gateway's configured effort is an OpenAI setting: a served model whose AA
// family belongs to another creator is scored by the base-row rule even
// when the gateway pins an effort, while an effort its own id names still
// picks that row.
func TestBuildEndpointCandidatesReasoningEffortOpenAIOnly(t *testing.T) {
	aa := []aaModel{
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(60.0), IntelIndex: new(60.0)},
		{Slug: "gpt-5-2-medium", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		{Slug: "claude-opus-5", Creator: "anthropic", CodingIndex: new(60.0), IntelIndex: new(60.0)},
		{Slug: "claude-opus-5-medium", Creator: "anthropic", CodingIndex: new(80.0), IntelIndex: new(80.0)},
	}
	endpoint := map[string]orEntry{
		"openai/gpt-5.2":                 {ContextWindow: 1000, Tools: true},
		"anthropic/claude-opus-5":        {ContextWindow: 1000, Tools: true},
		"anthropic/claude-opus-5-medium": {ContextWindow: 1000, Tools: true},
		"vendor-alias-only":              {ContextWindow: 1000, Tools: true, Aliases: []string{"claude-opus-5-medium"}},
	}

	scored, exclusions, _ := buildEndpointCandidates(aa, endpoint, nil, 0.3, nil, "medium")
	require.Empty(t, exclusions)
	require.Len(t, scored, 4)

	bySlug := map[string]aaScored{}
	for _, s := range scored {
		bySlug[s.Candidate.Slug] = s
	}

	assert.Equal(t, "gpt-5-2-medium", bySlug["openai/gpt-5.2"].Source, "an OpenAI family takes the gateway's effort")
	assert.Equal(t, "medium", bySlug["openai/gpt-5.2"].Effort)

	assert.Equal(t, "claude-opus-5", bySlug["anthropic/claude-opus-5"].Source, "the gateway's effort never reaches another creator's family")
	assert.Empty(t, bySlug["anthropic/claude-opus-5"].Effort)
	assert.InDelta(t, 60.0/80, bySlug["anthropic/claude-opus-5"].Candidate.CoderPrior, 1e-9)

	assert.Equal(t, "claude-opus-5-medium", bySlug["anthropic/claude-opus-5-medium"].Source, "the id's own suffix still names the row")
	assert.Equal(t, "medium", bySlug["anthropic/claude-opus-5-medium"].Effort)

	assert.Equal(t, "claude-opus-5-medium", bySlug["vendor-alias-only"].Source, "an alias's own suffix names the row for any creator")
	assert.Equal(t, "medium", bySlug["vendor-alias-only"].Effort)
}

// TestBuilderReasoningEffortReachesTheJoin: the configured gateway effort
// picks the family row of an OpenAI model through a full refresh, with the
// AA creator name resolved to its vendor prefix on the way.
func TestBuilderReasoningEffortReachesTheJoin(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/model-a","context_length":200000,"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"slug":"model-a","model_creator":{"name":"OpenAI"},
			 "evaluations":{"artificial_analysis_coding_index":60,"artificial_analysis_intelligence_index":60}},
			{"slug":"model-a-medium","model_creator":{"name":"OpenAI"},
			 "evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}
		]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"openai"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil),
		WithReasoningEffort("medium"))
	b.aaEndpoint = aaSrv.URL

	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.InDelta(t, 1.0, cands[0].CoderPrior, 1e-9, "scored from the medium row, the family leader")
	assert.Equal(t, "model-a-medium", b.Provenance(context.Background())["openai/model-a"].ScoredFrom)
}

// TestBuilderProvenanceModelPriorsNamesNoRow: a model_priors entry joins no
// AA row, so its provenance carries the price source only.
func TestBuilderProvenanceModelPriorsNamesNoRow(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/private-1","context_length":200000,"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	// fetchAAModels treats a wholly empty AA response as a fetch error (see
	// aa.go), so this fixture carries one unrelated AA row; the model_priors
	// path for vendor/private-1 never consults the family index, so it plays
	// no part in the join.
	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"unrelated-model","model_creator":{"name":"someone"},
			"evaluations":{"artificial_analysis_coding_index":50,"artificial_analysis_intelligence_index":50}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, nil, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]PriorOverride{"vendor/private-1": {Coder: 0.9, Reviewer: 0.8}}))
	b.aaEndpoint = aaSrv.URL

	require.Len(t, b.Candidates(context.Background()), 1)
	assert.Equal(t, map[string]CandidateProvenance{"vendor/private-1": {PriceSource: "none"}}, b.Provenance(context.Background()))
}

// TestBuilderAAFailureKeepsAAPricedCatalog: card prices must not depend on
// AA availability at every TTL expiry. When the gateway refresh succeeds but
// the AA fetch fails, Rate() keeps serving the last-good AA-priced catalog
// instead of the fresh unpriced one.
func TestBuilderAAFailureKeepsAAPricedCatalog(t *testing.T) {
	var aaFail atomic.Bool

	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":200000,
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if aaFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(`{"data":[{"slug":"model-a","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80},
			"pricing":{"price_1m_input_tokens":2,"price_1m_output_tokens":8}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	ctx := context.Background()

	price, ok := b.Rate(ctx, "vendor/model-a")
	require.True(t, ok)
	require.InDelta(t, 2e-6, price.Prompt, 1e-15)

	aaFail.Store(true)
	b.mu.Lock()
	b.cachedAt = time.Now().Add(-2 * time.Hour)
	b.lastRefreshAttempt = time.Time{}
	b.mu.Unlock()

	price, ok = b.Rate(ctx, "vendor/model-a")
	require.True(t, ok)
	assert.InDelta(t, 2e-6, price.Prompt, 1e-15, "an AA outage must not zero the AA-priced card rate")
	assert.InDelta(t, 8e-6, price.Completion, 1e-15)
	assert.Len(t, b.Candidates(ctx), 1, "candidates stay last-good too")

	price, ok = b.Rate(ctx, "model-a-2026-01-01")
	require.True(t, ok, "the name index stays with the last-good catalog")
	assert.InDelta(t, 2e-6, price.Prompt, 1e-15)
}

// TestBuilderFirstRefreshAAFailureStillServes: with no last-good catalog, an
// AA failure on the very first refresh still leaves the gateway's served set
// behind Rate() and Served(), unpriced, so pickers and validation work.
func TestBuilderFirstRefreshAAFailureStillServes(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":200000,
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	ctx := context.Background()

	price, ok := b.Rate(ctx, "vendor/model-a")
	require.True(t, ok, "the served set is known even though AA failed")
	assert.Zero(t, price.Prompt)
	assert.Len(t, b.Served(ctx), 1)
	assert.Empty(t, b.Candidates(ctx))
}

// TestBuilderRatePricesExcludedServedModelsFromAA: floor and allowlist gate
// selection, not billing. A served model the join found a priced AA row for
// is priced for card costs even when it is below the floor or its creator
// is outside the allowlist, so a pinned or chat-picked model still costs.
func TestBuilderRatePricesExcludedServedModelsFromAA(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/model-a","context_length":200000,"capabilities":{"features":["tools"]}},
			{"id":"vendor/model-low","context_length":200000,"capabilities":{"features":["tools"]}},
			{"id":"other/model-x","context_length":200000,"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"slug":"model-a","model_creator":{"name":"vendor"},
			 "evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80},
			 "pricing":{"price_1m_input_tokens":2,"price_1m_output_tokens":8}},
			{"slug":"model-low","model_creator":{"name":"vendor"},
			 "evaluations":{"artificial_analysis_coding_index":10,"artificial_analysis_intelligence_index":10},
			 "pricing":{"price_1m_input_tokens":0.5,"price_1m_output_tokens":1.5}},
			{"slug":"model-x","model_creator":{"name":"other"},
			 "evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80},
			 "pricing":{"price_1m_input_tokens":4,"price_1m_output_tokens":16}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.65, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	ctx := context.Background()

	cands := b.Candidates(ctx)
	require.Len(t, cands, 1)
	assert.Equal(t, "vendor/model-a", cands[0].Slug)

	low, ok := b.Rate(ctx, "vendor/model-low")
	require.True(t, ok)
	assert.InDelta(t, 0.5e-6, low.Prompt, 1e-15, "below the floor is still billed at the AA list price")
	assert.InDelta(t, 1.5e-6, low.Completion, 1e-15)

	x, ok := b.Rate(ctx, "other/model-x")
	require.True(t, ok)
	assert.InDelta(t, 4e-6, x.Prompt, 1e-15, "outside the allowlist is still billed at the AA list price")
	assert.InDelta(t, 16e-6, x.Completion, 1e-15)
}

// TestBuildEndpointCandidatesCapturesAAListPrices: the join hands back the AA
// list price for every served slug whose price resolved to AA, including the
// ones the allowlist or the floor then excluded from selection; a
// gateway-priced entry, a model_priors override and an unpriced row yield
// none.
func TestBuildEndpointCandidatesCapturesAAListPrices(t *testing.T) {
	aa := []aaModel{
		{Slug: "model-a", Creator: "vendor", CodingIndex: new(80.0), IntelIndex: new(80.0), PromptPrice: 2e-6, CompletionPrice: 8e-6, CacheReadPrice: 0.2e-6, CacheWritePrice: 2.5e-6},
		{Slug: "model-low", Creator: "vendor", CodingIndex: new(10.0), IntelIndex: new(10.0), PromptPrice: 0.5e-6, CompletionPrice: 1.5e-6},
		{Slug: "model-x", Creator: "other", CodingIndex: new(80.0), IntelIndex: new(80.0), PromptPrice: 4e-6, CompletionPrice: 16e-6},
		{Slug: "model-g", Creator: "vendor", CodingIndex: new(80.0), IntelIndex: new(80.0), PromptPrice: 9e-6, CompletionPrice: 9e-6},
		{Slug: "model-free", Creator: "vendor", CodingIndex: new(80.0), IntelIndex: new(80.0)},
	}
	endpoint := map[string]orEntry{
		"model-a":    {ContextWindow: 1000, Tools: true},
		"model-low":  {ContextWindow: 1000, Tools: true},
		"model-x":    {ContextWindow: 1000, Tools: true},
		"model-g":    {PromptPrice: 1e-6, CompletionPrice: 2e-6, ContextWindow: 1000, Tools: true, PriceSource: priceSourceGateway},
		"model-free": {ContextWindow: 1000, Tools: true},
		"model-p":    {ContextWindow: 1000, Tools: true},
	}
	priors := map[string]PriorOverride{"model-p": {Coder: 0.9, Reviewer: 0.9}}

	scored, exclusions, listPrices := buildEndpointCandidates(aa, endpoint, priors, 0.65, []string{"vendor"}, "")
	require.Len(t, scored, 4, "model-a, model-g, model-free and model-p select")
	require.Len(t, exclusions, 2, "model-low is below floor, model-x is not allowed")

	assert.Equal(t, map[string]ModelPrice{
		"model-a":   {Prompt: 2e-6, Completion: 8e-6, CacheRead: 0.2e-6, CacheWrite: 2.5e-6},
		"model-low": {Prompt: 0.5e-6, Completion: 1.5e-6},
		"model-x":   {Prompt: 4e-6, Completion: 16e-6},
	}, listPrices)
}

// TestBuilderRateResolvesGatewayEchoedName: a usage report carries the
// model name the gateway echoed in the completion, not the served id, so
// Rate() resolves a name by served id, then by vendor-stripped id or
// gateway alias, then by the name with its date token removed (a snapshot
// the gateway did not list as an alias). Effort words are never stripped,
// and a name two served ids could claim is not guessed.
func TestBuilderRateResolvesGatewayEchoedName(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/model-a","context_length":200000,"alias_names":["model-a","model-a-20260101"],"capabilities":{"features":["tools"]}},
			{"id":"vendor/model-b","context_length":200000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"capabilities":{"features":["tools"]}},
			{"id":"other/model-b","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000004"},"capabilities":{"features":["tools"]}},
			{"id":"model-d","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000006"},"capabilities":{"features":["tools"]}},
			{"id":"vendor/model-e-reasoning","context_length":200000,"pricing":{"prompt":"0.000007","completion":"0.000008"},"capabilities":{"features":["tools"]}},
			{"id":"vendor/model-f-20250929","context_length":200000,"pricing":{"prompt":"0.000009","completion":"0.000010"},"capabilities":{"features":["tools"]}}
		]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"model-a","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80},
			"pricing":{"price_1m_input_tokens":2,"price_1m_output_tokens":8}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	for _, tc := range []struct {
		name   string
		ok     bool
		prompt float64
	}{
		{name: "vendor/model-a", ok: true, prompt: 2e-6},             // served id
		{name: "model-a", ok: true, prompt: 2e-6},                    // vendor-stripped id, also an alias
		{name: "model-a-20260101", ok: true, prompt: 2e-6},           // gateway alias
		{name: "model-a-2026-03-05", ok: true, prompt: 2e-6},         // dated echo, same family, not an alias
		{name: "vendor/model-b", ok: true, prompt: 1e-6},             // served id, exact wins over the family
		{name: "model-b", ok: false},                                 // two served ids claim it
		{name: "model-d-20260301", ok: true, prompt: 5e-6},           // dated echo of a bare served id
		{name: "model-e-reasoning-20260301", ok: true, prompt: 7e-6}, // dated echo of an effort variant
		{name: "model-e", ok: false},                                 // a different product, not the served effort variant
		{name: "model-e-20260301", ok: false},                        // nor its snapshot
		{name: "model-f", ok: true, prompt: 9e-6},                    // undated echo of a dated served id
		{name: "model-c", ok: false},                                 // not served
	} {
		t.Run(tc.name, func(t *testing.T) {
			price, ok := b.Rate(context.Background(), tc.name)
			require.Equal(t, tc.ok, ok)

			if tc.ok {
				assert.InDelta(t, tc.prompt, price.Prompt, 1e-15)
			}
		})
	}
}
