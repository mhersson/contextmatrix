package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aaIntelligenceOnlyPage is an AA free-tier page in the shape AA serves since
// Intelligence Index v5: every coding and agentic index is null, leaving the
// intelligence index beside capability indexes CM does not read.
const aaIntelligenceOnlyPage = `{"data":[
	{"slug":"gemini-4-carbon","model_creator":{"name":"Google"},
	 "evaluations":{"artificial_analysis_intelligence_index":59.4,"artificial_analysis_coding_index":null,"artificial_analysis_agentic_index":null,
	  "artificial_analysis_engineering_index":61.2,"artificial_analysis_legal_index":48.0},
	 "pricing":{"price_1m_input_tokens":2,"price_1m_output_tokens":12,"price_1m_cache_hit_tokens":0.2,"price_1m_cache_write_tokens":null}},
	{"slug":"claude-opus-5-5","model_creator":{"name":"Anthropic"},
	 "evaluations":{"artificial_analysis_intelligence_index":58.4,"artificial_analysis_coding_index":null,"artificial_analysis_agentic_index":null},
	 "pricing":{"price_1m_input_tokens":5,"price_1m_output_tokens":25,"price_1m_cache_hit_tokens":0.5,"price_1m_cache_write_tokens":6.25}},
	{"slug":"gpt-6-astra","model_creator":{"name":"OpenAI"},
	 "evaluations":{"artificial_analysis_intelligence_index":56.9,"artificial_analysis_coding_index":null,"artificial_analysis_agentic_index":null},
	 "pricing":{"price_1m_input_tokens":1.25,"price_1m_output_tokens":10,"price_1m_cache_hit_tokens":null,"price_1m_cache_write_tokens":null}},
	{"slug":"gpt-6-nano","model_creator":{"name":"OpenAI"},
	 "evaluations":{"artificial_analysis_intelligence_index":20.1,"artificial_analysis_coding_index":null,"artificial_analysis_agentic_index":null},
	 "pricing":{"price_1m_input_tokens":0.05,"price_1m_output_tokens":0.4,"price_1m_cache_hit_tokens":null,"price_1m_cache_write_tokens":null}},
	{"slug":"glm-4-5v","model_creator":{"name":"Z AI"},
	 "evaluations":{"artificial_analysis_intelligence_index":null,"artificial_analysis_coding_index":null,"artificial_analysis_agentic_index":null},
	 "pricing":{"price_1m_input_tokens":0.6,"price_1m_output_tokens":1.8,"price_1m_cache_hit_tokens":0.11,"price_1m_cache_write_tokens":null}}
],"tier":"free","intelligence_index_version":5,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false}}`

// Each floor-clearing fixture row must carry its normalized intelligence
// index (index / 59.4, the response-wide max) as both priors.
const (
	carbonPrior = 1.0
	opusPrior   = 58.4 / 59.4
	astraPrior  = 56.9 / 59.4
)

func serveAAIntelligenceOnly(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(aaIntelligenceOnlyPage))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestBuilderScoresIntelligenceOnlyPayload(t *testing.T) {
	t.Parallel()

	t.Run("openrouter leg", func(t *testing.T) {
		t.Parallel()

		orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[
				{"id":"google/gemini-4-carbon","context_length":1048576,"pricing":{"prompt":"0.000002","completion":"0.000012"},"supported_parameters":["tools"]},
				{"id":"anthropic/claude-opus-5.5","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000025"},"supported_parameters":["tools"]},
				{"id":"openai/gpt-6-astra","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"supported_parameters":["tools"]},
				{"id":"openai/gpt-6-nano","context_length":400000,"pricing":{"prompt":"0.00000005","completion":"0.0000004"},"supported_parameters":["tools"]}
			]}`))
		}))
		t.Cleanup(orSrv.Close)

		b := NewBuilder("aa-key", 0.5, nil, time.Hour)
		b.orEndpoint = orSrv.URL
		b.aaEndpoint = serveAAIntelligenceOnly(t).URL

		got := map[string][2]float64{}
		for _, c := range b.Candidates(context.Background()) {
			got[c.Slug] = [2]float64{c.CoderPrior, c.ReviewerPrior}
		}

		assertPriors(t, map[string][2]float64{
			"google/gemini-4-carbon":    {carbonPrior, carbonPrior},
			"anthropic/claude-opus-5.5": {opusPrior, opusPrior},
			"openai/gpt-6-astra":        {astraPrior, astraPrior},
		}, got)
	})

	t.Run("endpoint leg", func(t *testing.T) {
		t.Parallel()

		endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[
				{"id":"gemini-4-carbon","context_length":1048576,"pricing":{"prompt":"0.000002","completion":"0.000012"},"capabilities":{"features":["tools"]}},
				{"id":"claude-opus-5.5","context_length":200000,"pricing":{"prompt":"0.000005","completion":"0.000025"},"capabilities":{"features":["tools"]}},
				{"id":"gpt-6-astra","context_length":400000,"pricing":{"prompt":"0.00000125","completion":"0.00001"},"capabilities":{"features":["tools"]}},
				{"id":"gpt-6-nano","context_length":400000,"pricing":{"prompt":"0.00000005","completion":"0.0000004"},"capabilities":{"features":["tools"]}}
			]}`))
		}))
		t.Cleanup(endpointSrv.Close)

		b := NewBuilder("aa-key", 0.5, nil, time.Hour, WithEndpoint(endpointSrv.URL, "secret", nil))
		b.aaEndpoint = serveAAIntelligenceOnly(t).URL

		got := map[string][2]float64{}
		for _, c := range b.Candidates(context.Background()) {
			got[c.Slug] = [2]float64{c.CoderPrior, c.ReviewerPrior}
		}

		assertPriors(t, map[string][2]float64{
			"gemini-4-carbon": {carbonPrior, carbonPrior},
			"claude-opus-5.5": {opusPrior, opusPrior},
			"gpt-6-astra":     {astraPrior, astraPrior},
		}, got)
	})
}

// assertPriors compares {coder, reviewer} prior pairs by slug within float
// tolerance; the candidate set must be exactly want's slugs.
func assertPriors(t *testing.T, want, got map[string][2]float64) {
	t.Helper()

	require.Len(t, got, len(want), "candidate set: %v", got)

	for slug, w := range want {
		g, ok := got[slug]
		if !assert.True(t, ok, "missing candidate %s in %v", slug, got) {
			continue
		}

		assert.InDelta(t, w[0], g[0], 1e-12, "%s coder prior", slug)
		assert.InDelta(t, w[1], g[1], 1e-12, "%s reviewer prior", slug)
	}
}
