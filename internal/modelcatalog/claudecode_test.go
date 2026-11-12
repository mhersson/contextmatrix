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

func TestClaudeCodeID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in     string
		wantID string
		wantOK bool
	}{
		{"anthropic/claude-opus-5.5", "claude-opus-5-5", true},
		{"claude-sonnet-5-5", "claude-sonnet-5-5", true},
		{"anthropic/claude-sonnet-5.5:thinking", "claude-sonnet-5-5", true},
		{"anthropic/claude-fable-5.2", "", false},
		{"anthropic/claude-opus-5", "", false},
		{"openai/gpt-5.4", "", false},
		{"Anthropic/Claude-Opus-5.5", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			id, ok := ClaudeCodeID(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

const aaClaudeCodePage = `{"data":[
	{"slug":"claude-opus-5-5","model_creator":{"name":"Anthropic"},
	 "evaluations":{"artificial_analysis_intelligence_index":60,"artificial_analysis_coding_index":null},
	 "pricing":{"price_1m_input_tokens":5,"price_1m_output_tokens":25,"price_1m_cache_hit_tokens":0.5,"price_1m_cache_write_tokens":6.25}},
	{"slug":"claude-sonnet-5-5","model_creator":{"name":"Anthropic"},
	 "evaluations":{"artificial_analysis_intelligence_index":48,"artificial_analysis_coding_index":null},
	 "pricing":{"price_1m_input_tokens":3,"price_1m_output_tokens":15,"price_1m_cache_hit_tokens":0.3,"price_1m_cache_write_tokens":3.75}},
	{"slug":"claude-haiku-5-5","model_creator":{"name":"Anthropic"},
	 "evaluations":{"artificial_analysis_intelligence_index":36,"artificial_analysis_coding_index":null},
	 "pricing":{"price_1m_input_tokens":1,"price_1m_output_tokens":5,"price_1m_cache_hit_tokens":0.1,"price_1m_cache_write_tokens":1.25}}
],"tier":"free","intelligence_index_version":5,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false}}`

func claudeCodeBuilder(t *testing.T, aaKey string, opts ...BuilderOption) (*Builder, *atomic.Int32) {
	t.Helper()

	var aaHits atomic.Int32

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aaHits.Add(1)

		_, _ = w.Write([]byte(aaClaudeCodePage))
	}))
	t.Cleanup(aaSrv.Close)

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-opus-5.5","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000025"},"supported_parameters":["tools"]}
		]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := NewBuilder(aaKey, 0.5, nil, time.Hour, opts...)
	b.aaEndpoint = aaSrv.URL
	b.orEndpoint = orSrv.URL

	return b, &aaHits
}

func TestBuilder_ClaudeCodeCandidates(t *testing.T) {
	t.Parallel()

	b, _ := claudeCodeBuilder(t, "aa-key", WithClaudeCode())

	got := map[string]float64{}

	for _, c := range b.ClaudeCodeCandidates(context.Background()) {
		got[c.Slug] = c.CoderPrior

		assert.Empty(t, c.Creator)
		// The served opus entry (200k) sizes its ID; the rest default to 1M.
		want := 1_000_000
		if c.Slug == "claude-opus-5-5" {
			want = 200_000
		}

		assert.Equal(t, want, c.ContextWindow)
		assert.InDelta(t, c.CoderPrior, c.ReviewerPrior, 1e-12)
	}

	// Haiku (36/60 = 0.6) clears the 0.5 floor.
	assert.InDelta(t, 1.0, got["claude-opus-5-5"], 1e-12)
	assert.InDelta(t, 48.0/60, got["claude-sonnet-5-5"], 1e-12)
	assert.InDelta(t, 36.0/60, got["claude-haiku-5-5"], 1e-12)
	assert.Len(t, got, 3)

	for _, c := range b.ClaudeCodeCandidates(context.Background()) {
		if c.Slug == "claude-sonnet-5-5" {
			assert.InDelta(t, 3.0/1e6, c.PromptPricePerTok, 1e-15)
			assert.InDelta(t, 15.0/1e6, c.CompletionPricePerTok, 1e-15)
		}
	}
}

func TestBuilder_ClaudeCodeCandidates_BelowFloorExcluded(t *testing.T) {
	t.Parallel()

	b, _ := claudeCodeBuilder(t, "aa-key", WithClaudeCode())
	b.floor = 0.7

	var slugs []string
	for _, c := range b.ClaudeCodeCandidates(context.Background()) {
		slugs = append(slugs, c.Slug)
	}

	assert.ElementsMatch(t, []string{"claude-opus-5-5", "claude-sonnet-5-5"}, slugs)
}

func TestBuilder_ClaudeCodeSharesAAFetch(t *testing.T) {
	t.Parallel()

	b, hits := claudeCodeBuilder(t, "aa-key", WithClaudeCode())
	ctx := context.Background()

	assert.NotEmpty(t, b.Candidates(ctx))
	assert.NotEmpty(t, b.ClaudeCodeCandidates(ctx))
	_, ok := b.ClaudeCodeRate(ctx, "claude-opus-5-5")
	assert.True(t, ok)
	assert.EqualValues(t, 1, hits.Load())
}

func TestBuilder_ClaudeCodeRate(t *testing.T) {
	t.Parallel()

	t.Run("list price", func(t *testing.T) {
		t.Parallel()

		b, _ := claudeCodeBuilder(t, "aa-key", WithClaudeCode())

		p, ok := b.ClaudeCodeRate(context.Background(), "claude-opus-5-5")
		require.True(t, ok)
		assert.InDelta(t, 5.0/1e6, p.Prompt, 1e-15)
		assert.InDelta(t, 25.0/1e6, p.Completion, 1e-15)
		assert.InDelta(t, 0.5/1e6, p.CacheRead, 1e-15)
		assert.InDelta(t, 6.25/1e6, p.CacheWrite, 1e-15)

		_, ok = b.ClaudeCodeRate(context.Background(), "claude-unknown-5-5")
		assert.False(t, ok)
	})

	t.Run("without WithClaudeCode", func(t *testing.T) {
		t.Parallel()

		b, _ := claudeCodeBuilder(t, "aa-key")

		_, ok := b.ClaudeCodeRate(context.Background(), "claude-opus-5-5")
		assert.False(t, ok)
		assert.Empty(t, b.ClaudeCodeCandidates(context.Background()))
	})

	t.Run("without AA key", func(t *testing.T) {
		t.Parallel()

		b, _ := claudeCodeBuilder(t, "", WithClaudeCode())

		_, ok := b.ClaudeCodeRate(context.Background(), "claude-opus-5-5")
		assert.False(t, ok)
		assert.Empty(t, b.ClaudeCodeCandidates(context.Background()))
	})
}

// A well-formed Claude Code ID the served catalog lacks takes the default
// window; a served entry wins, and other names resolve nothing.
func TestContextWindow_FallsBackToClaudeCodeList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var noCatalog *Builder

	served := NewBuilder("", 0.65, nil, 0)
	seedServed(t, served, map[string]orEntry{
		"z-ai/glm-5.2":     {ContextWindow: 131072},
		"claude-haiku-5-5": {ContextWindow: 200000},
	})

	tests := []struct {
		name  string
		b     *Builder
		model string
		want  int
	}{
		{"no catalog, catalog id", noCatalog, "claude-sonnet-5-5", 1_000_000},
		{"no catalog, anthropic slug", noCatalog, "anthropic/claude-opus-5.5", 0},
		{"no catalog, dotted bare name", noCatalog, "claude-sonnet-5.5", 0},
		{"no catalog, unknown model", noCatalog, "deepseek/deepseek-v4-flash", 0},
		{"served catalog lacks the id", served, "claude-sonnet-5-5", 1_000_000},
		{"served entry wins", served, "claude-haiku-5-5", 200000},
		{"served non-claude model", served, "z-ai/glm-5.2", 131072},
		{"well-formed id the catalog never lists", served, "claude-fable-5-5", 1_000_000},
		{"dotted anthropic slug is not an id", served, "anthropic/claude-opus-5.5", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.b.ContextWindow(ctx, tt.model))
		})
	}
}

// A refresh whose AA fetch fails keeps the Claude Code pair the last
// successful refresh built.
func TestBuilder_ClaudeCodeFailedRefreshKeepsLastGood(t *testing.T) {
	t.Parallel()

	var failAA atomic.Bool

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failAA.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(aaClaudeCodePage))
	}))
	t.Cleanup(aaSrv.Close)

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := NewBuilder("aa-key", 0.5, nil, time.Minute, WithClaudeCode())
	b.aaEndpoint = aaSrv.URL
	b.orEndpoint = orSrv.URL

	ctx := context.Background()

	require.Len(t, b.ClaudeCodeCandidates(ctx), 3)

	want, ok := b.ClaudeCodeRate(ctx, "claude-opus-5-5")
	require.True(t, ok)

	failAA.Store(true)
	b.mu.Lock()
	b.cachedAt = time.Now().Add(-2 * time.Hour)
	b.lastRefreshAttempt = time.Time{}
	b.mu.Unlock()

	assert.Len(t, b.ClaudeCodeCandidates(ctx), 3)

	got, ok := b.ClaudeCodeRate(ctx, "claude-opus-5-5")
	require.True(t, ok)
	assert.Equal(t, want, got)
}

// The endpoint (openai-compatible) leg derives the Claude Code pair from the
// same AA fetch as the OpenRouter leg.
func TestBuilder_ClaudeCodeEndpointLeg(t *testing.T) {
	t.Parallel()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(aaClaudeCodePage))
	}))
	t.Cleanup(aaSrv.Close)

	epSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a","context_length":200000,
			"pricing":{"prompt":"0.000003","completion":"0.000015"},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	t.Cleanup(epSrv.Close)

	b := NewBuilder("aa-key", 0.5, nil, time.Hour,
		WithClaudeCode(), WithEndpoint(epSrv.URL, "secret", nil))
	b.aaEndpoint = aaSrv.URL

	ctx := context.Background()
	got := map[string]float64{}

	for _, c := range b.ClaudeCodeCandidates(ctx) {
		got[c.Slug] = c.CoderPrior
	}

	assert.InDelta(t, 1.0, got["claude-opus-5-5"], 1e-12)
	assert.InDelta(t, 48.0/60, got["claude-sonnet-5-5"], 1e-12)
	assert.InDelta(t, 36.0/60, got["claude-haiku-5-5"], 1e-12)
	assert.Len(t, got, 3)

	p, ok := b.ClaudeCodeRate(ctx, "claude-opus-5-5")
	require.True(t, ok)
	assert.InDelta(t, 5.0/1e6, p.Prompt, 1e-15)
	assert.InDelta(t, 25.0/1e6, p.Completion, 1e-15)
}

func TestBuilder_ClaudeCodeProvenanceAndStats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("with Claude Code", func(t *testing.T) {
		t.Parallel()

		b, hits := claudeCodeBuilder(t, "aa-key", WithClaudeCode())

		prov := b.ClaudeCodeProvenance(ctx)
		require.Len(t, prov, 3)

		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
			assert.Equal(t, CandidateProvenance{PriceSource: "aa", ScoredFrom: id, PriorSource: PriorSourceAA}, prov[id], id)
		}

		stats := b.ClaudeCodeStats(ctx)
		served := b.Stats(ctx)

		assert.Equal(t, 3, stats.ServedScored)
		assert.Equal(t, served.IndexVersion, stats.IndexVersion)
		assert.Equal(t, served.AAScored, stats.AAScored)
		assert.InDelta(t, served.BestAAScore, stats.BestAAScore, 1e-12)
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("below the floor still counts as scored", func(t *testing.T) {
		t.Parallel()

		b, _ := claudeCodeBuilder(t, "aa-key", WithClaudeCode())
		b.floor = 0.7

		assert.Len(t, b.ClaudeCodeProvenance(ctx), 2)
		assert.Equal(t, 3, b.ClaudeCodeStats(ctx).ServedScored)
	})

	t.Run("without WithClaudeCode", func(t *testing.T) {
		t.Parallel()

		b, _ := claudeCodeBuilder(t, "aa-key")

		assert.Empty(t, b.ClaudeCodeProvenance(ctx))
		assert.Equal(t, CatalogStats{}, b.ClaudeCodeStats(ctx))
	})
}

func aaRow(slug, creator string, intel float64, in, out float64) aaModel {
	return aaModel{Slug: slug, Creator: creator, IntelIndex: &intel, PromptPrice: in / 1e6, CompletionPrice: out / 1e6}
}

// The catalog is the distinct family keys of the AA rows that are well-formed
// Claude Code IDs; the floor drops old versions and the window comes from the
// served entry when it has one.
func TestBuildClaudeCode_DerivesIDsFromAA(t *testing.T) {
	t.Parallel()

	aa := []aaModel{
		aaRow("claude-opus-5-5", "anthropic", 60, 5, 25),
		aaRow("claude-opus-5-5-thinking", "anthropic", 60, 5, 25),
		aaRow("claude-sonnet-5-5-high", "anthropic", 48, 3, 15),
		aaRow("claude-sonnet-5-5-non-reasoning", "anthropic", 48, 3, 15),
		aaRow("claude-haiku-5-5-20260101", "anthropic", 36, 1, 5),
		aaRow("claude-fable-5-5", "anthropic", 55, 10, 50),
		aaRow("claude-3-opus", "anthropic", 10, 15, 75),
		aaRow("gpt-5-4", "openai", 58, 2, 10),
		aaRow("deepseek-v4-flash", "deepseek", 40, 1, 2),
	}

	served := map[string]orEntry{
		"anthropic/claude-sonnet-5.5": {ContextWindow: 200_000},
		"claude-fable-5-5":            {ContextWindow: 400_000},
	}

	cands, catalog, _, _ := buildClaudeCode(aa, served, 0.5)

	got := map[string]int{}
	for _, c := range cands {
		got[c.Slug] = c.ContextWindow

		assert.Empty(t, c.Creator)
	}

	assert.Equal(t, map[string]int{
		"claude-opus-5-5":   1_000_000,
		"claude-sonnet-5-5": 200_000,
		"claude-haiku-5-5":  1_000_000,
		"claude-fable-5-5":  400_000,
	}, got)

	assert.NotContains(t, catalog, "gpt-5-4")
	assert.NotContains(t, catalog, "claude-opus-5-5-thinking")
	assert.InDelta(t, 10.0/1e6, catalog["claude-fable-5-5"].PromptPrice, 1e-15)
}

func TestClaudeCodeRate_PricesDerivedID(t *testing.T) {
	t.Parallel()

	b, _ := claudeCodeBuilder(t, "aa-key", WithClaudeCode())

	p, ok := b.ClaudeCodeRate(context.Background(), "claude-haiku-5-5")
	require.True(t, ok)
	assert.InDelta(t, 1.0/1e6, p.Prompt, 1e-15)
}
