package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestGetAppConfig(t *testing.T) {
	tests := []struct {
		name            string
		theme           string
		taskBackend     string
		wantTheme       string
		wantTaskBackend string
		wantStatus      int
		wantCTHeader    string
	}{
		{
			name:         "everforest theme",
			theme:        "everforest",
			wantTheme:    "everforest",
			wantStatus:   http.StatusOK,
			wantCTHeader: "application/json",
		},
		{
			name:         "radix theme",
			theme:        "radix",
			wantTheme:    "radix",
			wantStatus:   http.StatusOK,
			wantCTHeader: "application/json",
		},
		{
			name:            "agent backend reported",
			theme:           "everforest",
			taskBackend:     "agent",
			wantTheme:       "everforest",
			wantTaskBackend: "agent",
			wantStatus:      http.StatusOK,
			wantCTHeader:    "application/json",
		},
		{
			name:            "runner backend reported",
			theme:           "everforest",
			taskBackend:     "runner",
			wantTheme:       "everforest",
			wantTaskBackend: "runner",
			wantStatus:      http.StatusOK,
			wantCTHeader:    "application/json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &appConfigHandlers{theme: tc.theme, taskBackend: tc.taskBackend}

			req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
			w := httptest.NewRecorder()

			h.getAppConfig(w, req)

			res := w.Result()
			defer closeBody(t, res.Body)

			assert.Equal(t, tc.wantStatus, res.StatusCode)
			assert.Contains(t, res.Header.Get("Content-Type"), tc.wantCTHeader)

			var got appConfigResponse
			require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
			assert.Equal(t, tc.wantTheme, got.Theme)
			assert.Equal(t, tc.wantTaskBackend, got.TaskBackend)
		})
	}
}

func TestGetAppConfig_Favorites(t *testing.T) {
	t.Run("favorites serialized from All slugs per tier", func(t *testing.T) {
		h := &appConfigHandlers{
			theme:       "everforest",
			taskBackend: "agent",
			favorites: extractFavorites(map[string]board.TierFavorites{
				"fast":     {All: []string{"openrouter/auto", "anthropic/claude-haiku-4"}},
				"balanced": {All: []string{"anthropic/claude-sonnet-4-5"}},
				"frontier": {ByRole: map[string][]string{"coder": {"anthropic/claude-opus-4"}}},
			}),
		}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusOK, res.StatusCode)

		var got appConfigResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))

		// "fast" and "balanced" have All slugs — they appear in the response.
		require.Contains(t, got.Favorites, "fast")
		assert.Equal(t, []string{"openrouter/auto", "anthropic/claude-haiku-4"}, got.Favorites["fast"])
		require.Contains(t, got.Favorites, "balanced")
		assert.Equal(t, []string{"anthropic/claude-sonnet-4-5"}, got.Favorites["balanced"])

		// "frontier" only has ByRole slugs, no All — excluded from the response.
		assert.NotContains(t, got.Favorites, "frontier")
	})

	t.Run("favorites omitted when empty", func(t *testing.T) {
		h := &appConfigHandlers{theme: "everforest", taskBackend: "agent"}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		var got appConfigResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.Nil(t, got.Favorites)
	})

	t.Run("favorites omitted when all tiers have only ByRole slugs", func(t *testing.T) {
		h := &appConfigHandlers{
			theme:       "everforest",
			taskBackend: "agent",
			favorites: extractFavorites(map[string]board.TierFavorites{
				"frontier": {ByRole: map[string][]string{"reviewer": {"anthropic/claude-opus-4"}}},
			}),
		}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		var got appConfigResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.Nil(t, got.Favorites)
	})
}

func TestGetAppConfig_BestOfN(t *testing.T) {
	t.Run("full payload includes best_of_n bounds", func(t *testing.T) {
		h := &appConfigHandlers{theme: "everforest", bestOfNMax: 5, bestOfNDefault: 3}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		require.Equal(t, http.StatusOK, res.StatusCode)

		var got map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.EqualValues(t, 5, got["best_of_n_max"])
		assert.EqualValues(t, 3, got["best_of_n_default"])
	})

	t.Run("slim payload omits best_of_n bounds", func(t *testing.T) {
		// authMode "multi" + no session in context -> getAppConfig serves
		// appConfigSlimResponse, which has no best_of_n fields at all.
		h := &appConfigHandlers{theme: "everforest", authMode: "multi", bestOfNMax: 5, bestOfNDefault: 3}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		require.Equal(t, http.StatusOK, res.StatusCode)

		var got map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.NotContains(t, got, "best_of_n_max")
		assert.NotContains(t, got, "best_of_n_default")
	})
}
