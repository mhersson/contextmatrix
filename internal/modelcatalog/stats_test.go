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

func TestFetchAAModelsReadsIndexVersion(t *testing.T) {
	t.Parallel()

	row := `{"slug":"m","model_creator":{"name":"OpenAI"},"evaluations":{"artificial_analysis_intelligence_index":50}}`

	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{"number", `"intelligence_index_version":5,`, "5"},
		{"string", `"intelligence_index_version":"5",`, "5"},
		{"decimal", `"intelligence_index_version":5.1,`, "5.1"},
		{"absent", ``, ""},
		{"null", `"intelligence_index_version":null,`, ""},
		{"object", `"intelligence_index_version":{"x":1},`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{` + tt.extra + `"data":[` + row + `],"pagination":{"has_more":false}}`))
			}))
			t.Cleanup(srv.Close)

			cat, err := fetchAAModels(context.Background(), srv.URL, "k")
			require.NoError(t, err)
			assert.Equal(t, tt.want, cat.IndexVersion)
			assert.NotEmpty(t, cat.Models)
		})
	}
}

func TestBuilderStatsOpenRouterLeg(t *testing.T) {
	t.Parallel()

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"google/gemini-4-carbon","context_length":1048576,"pricing":{"prompt":"0.000002","completion":"0.000012"},"supported_parameters":["tools"]},
			{"id":"anthropic/claude-opus-5.5","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000025"},"supported_parameters":["tools"]},
			{"id":"openai/gpt-6-astra","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"supported_parameters":["tools"]},
			{"id":"openai/gpt-6-nano","context_length":400000,"pricing":{"prompt":"0.00000005","completion":"0.0000004"}}
		]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := NewBuilder("aa-key", 0.5, []string{"anthropic", "openai"}, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = serveAAIntelligenceOnly(t).URL

	ctx := context.Background()

	slugs := map[string]bool{}
	for _, c := range b.Candidates(ctx) {
		slugs[c.Slug] = true
	}

	require.Equal(t, map[string]bool{"anthropic/claude-opus-5.5": true, "openai/gpt-6-astra": true}, slugs)

	assert.Equal(t, CatalogStats{IndexVersion: "5", AAScored: 4, ServedScored: 3, BestAAScore: 59.4}, b.Stats(ctx))

	prov := b.Provenance(ctx)
	for slug := range slugs {
		assert.Equal(t, PriorSourceAA, prov[slug].PriorSource, slug)
	}
}

func TestBuilderStatsEndpointLeg(t *testing.T) {
	t.Parallel()

	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"gemini-4-carbon","context_length":1048576,"pricing":{"prompt":"0.000002","completion":"0.000012"},"capabilities":{"features":["tools"]}},
			{"id":"claude-opus-5.5","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000025"},"capabilities":{"features":["tools"]}},
			{"id":"gpt-6-astra","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"capabilities":{"features":["tools"]}},
			{"id":"gpt-6-nano","context_length":400000,"pricing":{"prompt":"0.00000005","completion":"0.0000004"},"capabilities":{"features":["tools"]}},
			{"id":"gpt-6-astra-high","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"capabilities":{"features":[]}},
			{"id":"op/custom","context_length":100000,"pricing":{"prompt":"0.000001","completion":"0.000002"},"capabilities":{"features":["tools"]}}
		]}`))
	}))
	t.Cleanup(endpointSrv.Close)

	b := NewBuilder("aa-key", 0.5, []string{"anthropic", "openai"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", map[string]PriorOverride{"op/custom": {Coder: 0.9, Reviewer: 0.4}}))
	b.aaEndpoint = serveAAIntelligenceOnly(t).URL

	ctx := context.Background()
	require.NotEmpty(t, b.Candidates(ctx))

	stats := b.Stats(ctx)
	assert.Equal(t, 4, stats.ServedScored)
	assert.Equal(t, 4, stats.AAScored)
	assert.InDelta(t, 59.4, stats.BestAAScore, 1e-12)
	assert.Equal(t, "5", stats.IndexVersion)

	prov := b.Provenance(ctx)
	assert.Equal(t, PriorSourceModelPriors, prov["op/custom"].PriorSource)
	assert.Equal(t, PriorSourceAA, prov["claude-opus-5.5"].PriorSource)
}

func TestBuilderStatsKeepLastGoodOnFailedRefresh(t *testing.T) {
	t.Parallel()

	var (
		fail     atomic.Bool
		requests atomic.Int32
	)

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(aaIntelligenceOnlyPage))
	}))
	t.Cleanup(aaSrv.Close)

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"openai/gpt-6-astra","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"supported_parameters":["tools"]}
		]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := NewBuilder("aa-key", 0.5, nil, time.Hour)
	b.orEndpoint = orSrv.URL
	b.aaEndpoint = aaSrv.URL

	ctx := context.Background()
	want := b.Stats(ctx)
	require.Equal(t, 4, want.AAScored)

	before := requests.Load()

	fail.Store(true)
	b.mu.Lock()
	b.cachedAt = time.Now().Add(-2 * time.Hour)
	b.lastRefreshAttempt = time.Now().Add(-2 * refreshFailureCooldown)
	b.mu.Unlock()

	assert.Equal(t, want, b.Stats(ctx))
	assert.Greater(t, requests.Load(), before, "the expired TTL must have triggered a refresh attempt")

	var nilBuilder *Builder

	assert.Equal(t, CatalogStats{}, nilBuilder.Stats(ctx))
}
