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

	"github.com/mhersson/contextmatrix/internal/config"
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

func TestClaudeCodeModels_MatchConfigList(t *testing.T) {
	t.Parallel()

	var ids []string

	for _, m := range ClaudeCodeModels() {
		ids = append(ids, m.ID)
		assert.Equal(t, 1_000_000, m.ContextWindow)
	}

	assert.ElementsMatch(t, config.ClaudeCodeModels, ids)
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
		assert.Equal(t, 1_000_000, c.ContextWindow)
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
