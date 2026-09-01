package api

import (
	"context"
	"encoding/json"
	"errors"
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

// stubBlacklistErr is a blacklistReader whose read always fails - the
// best-effort contract must serve the picker list without flags.
type stubBlacklistErr struct {
	err error
}

func (s *stubBlacklistErr) BlacklistedSlugs(_ context.Context) ([]string, error) {
	return nil, s.err
}

func TestModelCatalogEndpointBlacklist(t *testing.T) {
	tests := []struct {
		name          string
		blacklist     blacklistReader
		wantFlaggedID string // "" when no entry should carry the flag
	}{
		{
			name:          "nil reader serves list without flags",
			blacklist:     nil,
			wantFlaggedID: "",
		},
		{
			name:          "blacklisted slug is flagged, others are not",
			blacklist:     &stubBlacklist{slugs: []string{"bad/model"}},
			wantFlaggedID: "bad/model",
		},
		{
			name:          "blacklist read error still serves the full list unflagged",
			blacklist:     &stubBlacklistErr{err: errors.New("ops.db unavailable")},
			wantFlaggedID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &modelCatalogHandlers{
				source:    "openrouter",
				blacklist: tt.blacklist,
				served: func(_ context.Context) []ServedModelView {
					return []ServedModelView{
						{ID: "bad/model", ContextWindow: 100000},
						{ID: "good/model", ContextWindow: 200000},
					}
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
			w := httptest.NewRecorder()
			h.listModels(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			var resp struct {
				Source string `json:"source"`
				Models []struct {
					ID          string          `json:"id"`
					Blacklisted json.RawMessage `json:"blacklisted"`
				} `json:"models"`
			}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			require.Len(t, resp.Models, 2)

			for _, m := range resp.Models {
				if m.ID == tt.wantFlaggedID {
					assert.JSONEq(t, "true", string(m.Blacklisted),
						"model %s must carry blacklisted: true", m.ID)
				} else {
					assert.Empty(t, string(m.Blacklisted),
						"model %s must omit the blacklisted key entirely (omitempty)", m.ID)
				}
			}
		})
	}
}

func TestChatListModelsBlacklist(t *testing.T) {
	tests := []struct {
		name          string
		blacklist     blacklistReader
		wantFlaggedID string // "" when no entry should carry the flag
	}{
		{
			name:          "nil reader serves list without flags",
			blacklist:     nil,
			wantFlaggedID: "",
		},
		{
			name:          "blacklisted slug is flagged, others are not",
			blacklist:     &stubBlacklist{slugs: []string{"bad/model"}},
			wantFlaggedID: "bad/model",
		},
		{
			name:          "blacklist read error still serves the full list unflagged",
			blacklist:     &stubBlacklistErr{err: errors.New("ops.db unavailable")},
			wantFlaggedID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &chatHandlers{
				openRouter: true,
				blacklist:  tt.blacklist,
				servedModels: func(_ context.Context) []chatModelEntry {
					return []chatModelEntry{
						{ID: "bad/model", Label: "Bad", MaxTokens: 100000},
						{ID: "good/model", Label: "Good", MaxTokens: 200000},
					}
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/chats/models", nil)
			w := httptest.NewRecorder()
			h.listModels(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			var resp struct {
				Source string `json:"source"`
				Models []struct {
					ID          string          `json:"id"`
					Blacklisted json.RawMessage `json:"blacklisted"`
				} `json:"models"`
			}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			require.Len(t, resp.Models, 2)

			for _, m := range resp.Models {
				if m.ID == tt.wantFlaggedID {
					assert.JSONEq(t, "true", string(m.Blacklisted),
						"model %s must carry blacklisted: true", m.ID)
				} else {
					assert.Empty(t, string(m.Blacklisted),
						"model %s must omit the blacklisted key entirely (omitempty)", m.ID)
				}
			}
		})
	}
}
