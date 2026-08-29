package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			name:    "agent AA with configured floor 0.5",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0.5,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.5,
		},
		{
			name:    "agent AA with unset floor defaults to 0.65",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0, // unset; NewBuilder's <=0 fallback
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.65,
		},
		{
			name:    "agent AA with endpoint type openai and configured floor 0.5",
			llmType: config.LLMEndpointTypeOpenAI,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "some-key",
				CatalogQualityFloor: 0.5,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.5,
		},
		{
			name:    "no agent, endpoint openai, unset floor defaults to 0.65",
			llmType: config.LLMEndpointTypeOpenAI,
			agentCfg: &config.AgentBackendConfig{
				CatalogQualityFloor: 0,
			},
			hasAgent:       false,
			chatOpenRouter: false,
			expectedFloor:  0.65,
		},
		{
			name:           "chat only, openrouter catalog, unset floor defaults to 0.65",
			llmType:        config.LLMEndpointTypeOpenRouter,
			agentCfg:       nil,
			hasAgent:       false,
			chatOpenRouter: true,
			expectedFloor:  0.65,
		},
		{
			name:    "has agent without AA, openrouter, configured floor 0.5",
			llmType: config.LLMEndpointTypeOpenRouter,
			agentCfg: &config.AgentBackendConfig{
				AAAPIKey:            "",
				CatalogQualityFloor: 0.5,
			},
			hasAgent:       true,
			chatOpenRouter: false,
			expectedFloor:  0.5,
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
		{"zero is replaced by default", 0, 0.65},
		{"negative is replaced by default", -1, 0.65},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := modelcatalog.NewBuilder("", tt.floor, nil, 0)
			assert.InDelta(t, tt.expected, b.Floor(), 1e-9)
		})
	}
}
