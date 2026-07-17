package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
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

		// "fast" and "balanced" have All slugs - they appear in the response.
		require.Contains(t, got.Favorites, "fast")
		assert.Equal(t, []string{"openrouter/auto", "anthropic/claude-haiku-4"}, got.Favorites["fast"])
		require.Contains(t, got.Favorites, "balanced")
		assert.Equal(t, []string{"anthropic/claude-sonnet-4-5"}, got.Favorites["balanced"])

		// "frontier" only has ByRole slugs, no All - excluded from the response.
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

func TestGetAppConfig_Mob(t *testing.T) {
	t.Run("full payload includes mob bounds and guest names only", func(t *testing.T) {
		h := &appConfigHandlers{
			theme:                  "everforest",
			mobMaxParticipants:     5,
			mobDefaultParticipants: 3,
			mobGuestNames:          []string{"laptop"},
			mobExecuteCheckpoints:  true,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		require.Equal(t, http.StatusOK, res.StatusCode)

		var got map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.EqualValues(t, 5, got["mob_max_participants"])
		assert.EqualValues(t, 3, got["mob_default_participants"])
		assert.Equal(t, []any{"laptop"}, got["mob_guest_names"])
		assert.True(t, got["mob_execute_checkpoints"].(bool))
		// Names ONLY - URLs and tokens must never reach the browser.
		assert.NotContains(t, w.Body.String(), "192.0.2.1")
		assert.NotContains(t, w.Body.String(), "guest-secret")
	})

	t.Run("guest names omitted when registry empty", func(t *testing.T) {
		h := &appConfigHandlers{theme: "everforest", mobMaxParticipants: 5, mobDefaultParticipants: 3}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		var got map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.NotContains(t, got, "mob_guest_names")
	})

	t.Run("slim payload omits mob fields", func(t *testing.T) {
		// authMode "multi" + no session -> appConfigSlimResponse, which has
		// no mob fields at all - same posture as best_of_n.
		h := &appConfigHandlers{
			theme:                  "everforest",
			authMode:               "multi",
			mobMaxParticipants:     5,
			mobDefaultParticipants: 3,
			mobGuestNames:          []string{"laptop"},
		}

		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		w := httptest.NewRecorder()
		h.getAppConfig(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		require.Equal(t, http.StatusOK, res.StatusCode)

		var got map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		assert.NotContains(t, got, "mob_max_participants")
		assert.NotContains(t, got, "mob_default_participants")
		assert.NotContains(t, got, "mob_guest_names")
	})
}

func TestAppConfig_ChatEnabledFollowsChatWiring(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	// Without chat wiring: chat_enabled is false.
	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/app/config")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg struct {
		ChatEnabled bool `json:"chat_enabled"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.False(t, cfg.ChatEnabled)
}

// TestAppConfig_ChatEnabledTrue exercises the true case at the router level:
// chat_enabled now means "a chat backend is configured" (an enabled
// backends.chat entry with url and api_key set), the same condition the
// images route uses to build its chat probe client (mirrors
// TestBackendImages_Chat in backend_images_test.go). No ChatManager/ChatHub
// wiring is needed for this to be true.
func TestAppConfig_ChatEnabledTrue(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	server := httptest.NewServer(NewRouter(RouterConfig{
		Service: svc, Bus: bus,
		ChatBackendCfg: &config.ChatBackendConfig{
			URL:    "http://chat-backend.invalid",
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
	}))
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/api/app/config")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got appConfigResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.True(t, got.ChatEnabled)
}

func TestMobGuestNames(t *testing.T) {
	assert.Nil(t, mobGuestNames(nil))
	assert.Equal(t, []string{"laptop", "desk"}, mobGuestNames([]config.MobGuest{
		{Name: "laptop", URL: "http://a", Token: "x"},
		{Name: "desk", URL: "http://b", Token: "y"},
	}))
}
