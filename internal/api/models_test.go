package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogEndpoint(t *testing.T) {
	h := &modelCatalogHandlers{
		source: "openrouter",
		served: func(_ context.Context) []ServedModelView {
			return []ServedModelView{{ID: "anthropic/claude-sonnet-4.5", ContextWindow: 200000}}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	w := httptest.NewRecorder()
	h.listModels(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Source string `json:"source"`
		Models []struct {
			ID        string `json:"id"`
			MaxTokens int64  `json:"max_tokens"`
		} `json:"models"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "openrouter", resp.Source)
	require.Len(t, resp.Models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4.5", resp.Models[0].ID)
	assert.Equal(t, int64(200000), resp.Models[0].MaxTokens)
}

func TestModelCatalogEndpointNoBuilder(t *testing.T) {
	h := &modelCatalogHandlers{}

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	w := httptest.NewRecorder()
	h.listModels(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, `"models":[]`) // [] not null

	var resp struct {
		Source string            `json:"source"`
		Models []json.RawMessage `json:"models"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.Equal(t, "none", resp.Source)
	assert.Empty(t, resp.Models)
}
