package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-protocol/selection"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
)

// TestNewCatalogBuilder_QualityFloor verifies the floor value on the Builder
// returned by newCatalogBuilder across all switch cases. The helper is tested
// directly: no network, no AA fetch, no HTTP.
func TestNewCatalogBuilder_QualityFloor(t *testing.T) {
	tests := []struct {
		name           string
		llmType        string
		agentCfg       *config.AgentBackendConfig
		hasAgent       bool
		chatOpenRouter bool
		expectedFloor  float64
	}{
		{
			name:    "agent AA with configured floor 0.6",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0.6,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.6,
		},
		{
			name:    "agent AA with unset floor defaults to 0.50",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0, // unset; NewBuilder's <=0 fallback
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.50,
		},
		{
			name:    "agent AA with endpoint type openai and configured floor 0.6",
			llmType: config.LLMEndpointTypeOpenAI,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0.6,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.6,
		},
		{
			name:    "no agent, endpoint openai, unset floor defaults to 0.50",
			llmType: config.LLMEndpointTypeOpenAI,
			agentCfg: &config.AgentBackendConfig{
				CatalogQualityFloor: 0,
			},
			hasAgent:       false,
			chatOpenRouter: false,
			expectedFloor:  0.50,
		},
		{
			name:           "chat only, openrouter catalog, unset floor defaults to 0.50",
			llmType:        config.LLMEndpointTypeOpenRouter,
			agentCfg:       nil,
			hasAgent:       false,
			chatOpenRouter: true,
			expectedFloor:  0.50,
		},
		{
			name:    "has agent without AA, openrouter, configured floor 0.6",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "",
				CatalogQualityFloor: 0.6,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.LLMEndpoint.Type = tt.llmType

			b := newCatalogBuilder(cfg, tt.agentCfg, tt.hasAgent, tt.chatOpenRouter)
			require.NotNil(t, b, "newCatalogBuilder should always return a Builder for this case")

			assert.InDelta(t, tt.expectedFloor, b.Floor(), 1e-9,
				"Floor() should match the expected value")
		})
	}
}

// TestNewCatalogBuilder_NilNoSource verifies that when there is no rate
// source at all the helper returns nil.
func TestNewCatalogBuilder_NilNoSource(t *testing.T) {
	cfg := &config.Config{}

	b := newCatalogBuilder(cfg, nil, false, false)
	assert.Nil(t, b, "no agent, no chat, no endpoint -> nil")
}

// TestNewCatalogBuilder_AgentAAClosesOverOpenRouter verifies the agentAA case
// on a default (OpenRouter) LLMEndpoint carries the configured floor.
func TestNewCatalogBuilder_AgentAAClosesOverOpenRouter(t *testing.T) {
	cfg := &config.Config{} // default LLMEndpoint type is openrouter
	agentCfg := &config.AgentBackendConfig{
		AAAPIKey:            "some-key",
		CatalogQualityFloor: 0.42,
	}

	b := newCatalogBuilder(cfg, agentCfg, true, false)
	require.NotNil(t, b)
	assert.InDelta(t, 0.42, b.Floor(), 1e-9)
}

// TestFloorAccessor verifies the Floor() accessor on modelcatalog.Builder
// directly, independently of the helper.
func TestFloorAccessor(t *testing.T) {
	tests := []struct {
		name     string
		floor    float64
		expected float64
	}{
		{"positive value passes through", 0.5, 0.5},
		{"zero is replaced by default", 0, 0.50},
		{"negative is replaced by default", -1, 0.50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := modelcatalog.NewBuilder("", tt.floor, nil, 0)
			assert.InDelta(t, tt.expected, b.Floor(), 1e-9)
		})
	}
}

func TestCatalogRateLookup_ClaudeCode(t *testing.T) {
	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"slug":"claude-sonnet-5-5","model_creator":{"name":"Anthropic"},
			 "evaluations":{"artificial_analysis_intelligence_index":60,"artificial_analysis_coding_index":null},
			 "pricing":{"price_1m_input_tokens":3,"price_1m_output_tokens":15,"price_1m_cache_hit_tokens":0.3,"price_1m_cache_write_tokens":3.75}}
		],"tier":"free","intelligence_index_version":5,"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false}}`))
	}))
	t.Cleanup(aaSrv.Close)

	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(orSrv.Close)

	b := modelcatalog.NewBuilder("aa-key", 0.5, nil, time.Hour, modelcatalog.WithClaudeCode(),
		modelcatalog.WithCatalogURLs(aaSrv.URL, orSrv.URL))

	rate, ok := catalogRateLookup(t.Context(), b)("claude-sonnet-5-5")
	require.True(t, ok)
	assert.InDelta(t, 3.0/1e6, rate.Prompt, 1e-15)
	assert.InDelta(t, 15.0/1e6, rate.Completion, 1e-15)

	_, ok = catalogRateLookup(t.Context(), b)("claude-unknown-5-5")
	assert.False(t, ok)
}

func TestUnpricedClaudeCodeIDs(t *testing.T) {
	configured := config.ClaudeCodeConfig{OAuthToken: "tok"}

	var all []string
	for _, m := range selection.ClaudeCodeModels() {
		all = append(all, m.ID)
	}

	rates := func(ids ...string) map[string]config.ModelRate {
		m := make(map[string]config.ModelRate, len(ids))
		for _, id := range ids {
			m[id] = config.ModelRate{}
		}

		return m
	}

	tests := []struct {
		name    string
		cc      config.ClaudeCodeConfig
		costs   map[string]config.ModelRate
		agentAA bool
		want    []string
	}{
		{"not configured", config.ClaudeCodeConfig{}, nil, false, nil},
		{"AA catalog built", configured, nil, true, nil},
		{"no token_costs rows", configured, nil, false, all},
		{"some rows", configured, rates(all[0]), false, all[1:]},
		{"all rows", configured, rates(all...), false, nil},
		{"unrelated rows only", configured, rates("other/model"), false, all},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{ClaudeCode: tt.cc, TokenCosts: tt.costs}
			assert.Equal(t, tt.want, unpricedClaudeCodeIDs(cfg, tt.agentAA))
		})
	}
}
