package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// stubOutcomeAdminStore is a minimal outcomeAdminStore double. Distinct from
// backend_test.go's stubOutcomeStats (read-only: ModelOutcomeStats only) -
// the admin endpoints also need ResetModelOutcomes.
type stubOutcomeAdminStore struct {
	stats     []sqlite.OutcomeStats
	statsErr  error
	deleted   int64
	deleteErr error
}

func (s *stubOutcomeAdminStore) ModelOutcomeStats(context.Context) ([]sqlite.OutcomeStats, error) {
	return s.stats, s.statsErr
}

func (s *stubOutcomeAdminStore) ResetModelOutcomes(context.Context) (int64, error) {
	return s.deleted, s.deleteErr
}

// newOutcomeAdminServer builds a router exposing GET/DELETE
// /api/admin/model-outcomes. Mirrors newAuthTestServer (auth_test.go) but
// wires OutcomesAdmin and, in multi mode, seeds both an admin
// ("root") and a non-admin ("bob") account so both sides of the role gate
// are reachable via a real login.
func newOutcomeAdminServer(t *testing.T, store outcomeAdminStore, multiMode bool) *httptest.Server {
	t.Helper()

	cfg := RouterConfig{OutcomesAdmin: store}

	if multiMode {
		st, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = st.Close() })

		svc := auth.NewService(st, time.Hour)

		seed := func(username, password string, isAdmin bool) {
			u, err := st.CreateUser(t.Context(), username, username, isAdmin, time.Now())
			require.NoError(t, err)

			hash, err := auth.HashPassword(password)
			require.NoError(t, err)
			require.NoError(t, st.SetPasswordHash(t.Context(), u.ID, hash, time.Now()))
		}

		seed("root", "root password1", true)
		seed("bob", "bob password1", false)

		cfg.AuthService = svc
		cfg.AuthMode = "multi"
	}

	router := NewRouter(cfg)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server
}

// deleteWithCSRF builds a DELETE request carrying the X-Requested-With
// header the CSRF guard requires on every non-safe method, in every auth
// mode - without it, csrfGuard 403s before the request ever reaches the
// admin handler.
func deleteWithCSRF(t *testing.T, url string, cookie *http.Cookie) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")

	if cookie != nil {
		req.AddCookie(cookie)
	}

	return req
}

func TestAdminModelOutcomes_NoneMode(t *testing.T) {
	store := &stubOutcomeAdminStore{
		stats:   []sqlite.OutcomeStats{{Model: "m", RaceSamples: 3, RaceWins: 2, SoloSamples: 2}},
		deleted: 7,
	}
	server := newOutcomeAdminServer(t, store, false)

	// GET with no session at all - none mode is open, like project management.
	resp, err := http.Get(server.URL + "/api/admin/model-outcomes")
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var stats modelOutcomeStatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&stats))
	assert.Equal(t, 5, stats.TotalSamples)
	require.Len(t, stats.Models, 1)
	assert.Equal(t, 3, stats.Models[0].RaceSamples)
	assert.Equal(t, 2, stats.Models[0].SoloSamples)

	// DELETE - needs the CSRF header (unconditional in every mode), still no session.
	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-outcomes", nil))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var got map[string]int64
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&got))
	assert.Equal(t, int64(7), got["deleted"])
}

func TestAdminModelOutcomes_MultiMode_NonAdmin403(t *testing.T) {
	store := &stubOutcomeAdminStore{}
	server := newOutcomeAdminServer(t, store, true)
	cookie := login(t, server, "bob", "bob password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/model-outcomes", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeForbidden, apiErr.Code)

	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-outcomes", cookie))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusForbidden, resp2.StatusCode)

	var apiErr2 APIError
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&apiErr2))
	assert.Equal(t, ErrCodeForbidden, apiErr2.Code)
}

func TestAdminModelOutcomes_MultiMode_Admin200(t *testing.T) {
	store := &stubOutcomeAdminStore{
		stats: []sqlite.OutcomeStats{
			{Model: "a", RaceSamples: 21, RaceWins: 9, TotalCostUSD: 1.23},
			{Model: "b", SoloSamples: 3, SoloFailures: 1},
		},
		deleted: 24,
	}
	server := newOutcomeAdminServer(t, store, true)
	cookie := login(t, server, "root", "root password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/model-outcomes", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got modelOutcomeStatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Models, 2)
	assert.Equal(t, 24, got.TotalSamples, "race and solo samples both count toward the total")

	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-outcomes", cookie))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var deletedResp map[string]int64
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&deletedResp))
	assert.Equal(t, int64(24), deletedResp["deleted"])
}

// TestAdminModelOutcomes_StatsShape exercises the handler directly (no auth
// scaffolding needed - shape/arithmetic only): race stats and solo stats stay
// separate, the win rate is computed over race rows only, and the total sums
// both kinds.
func TestAdminModelOutcomes_StatsShape(t *testing.T) {
	t.Run("race and solo split", func(t *testing.T) {
		store := &stubOutcomeAdminStore{
			stats: []sqlite.OutcomeStats{
				{Model: "a", RaceSamples: 8, RaceWins: 5, SoloSamples: 12, SoloFailures: 2, TotalCostUSD: 3.5},
				{Model: "b", SoloSamples: 3, SoloFailures: 1},
			},
		}
		h := &outcomeAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodGet, "/api/admin/model-outcomes", nil)
		w := httptest.NewRecorder()
		h.getStats(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		require.Equal(t, http.StatusOK, res.StatusCode)

		var got modelOutcomeStatsResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))

		assert.Equal(t, 23, got.TotalSamples)
		require.Len(t, got.Models, 2)

		a := got.Models[0]
		assert.Equal(t, "a", a.Model)
		assert.Equal(t, 8, a.RaceSamples)
		assert.Equal(t, 5, a.RaceWins)
		assert.InDelta(t, 0.625, a.RaceWinRate, 1e-9)
		assert.Equal(t, 12, a.SoloSamples)
		assert.Equal(t, 2, a.SoloFailures)
		assert.InDelta(t, 3.5, a.TotalCostUSD, 1e-9)
	})

	t.Run("race_win_rate guards divide-by-zero when a model never raced", func(t *testing.T) {
		store := &stubOutcomeAdminStore{
			stats: []sqlite.OutcomeStats{{Model: "z", SoloSamples: 4}},
		}
		h := &outcomeAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodGet, "/api/admin/model-outcomes", nil)
		w := httptest.NewRecorder()
		h.getStats(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		var got modelOutcomeStatsResponse
		require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
		require.Len(t, got.Models, 1)
		assert.Zero(t, got.Models[0].RaceWinRate)
	})
}

// TestAdminModelOutcomes_StoreErrors covers the 500 path on both handlers -
// not enumerated in the brief's 4 named cases, but new production code with
// zero coverage on its only error branch is worth the few extra lines.
func TestAdminModelOutcomes_StoreErrors(t *testing.T) {
	t.Run("getStats store error", func(t *testing.T) {
		store := &stubOutcomeAdminStore{statsErr: assert.AnError}
		h := &outcomeAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodGet, "/api/admin/model-outcomes", nil)
		w := httptest.NewRecorder()
		h.getStats(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(res.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeInternalError, apiErr.Code)
	})

	t.Run("reset store error", func(t *testing.T) {
		store := &stubOutcomeAdminStore{deleteErr: assert.AnError}
		h := &outcomeAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodDelete, "/api/admin/model-outcomes", nil)
		w := httptest.NewRecorder()
		h.reset(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(res.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeInternalError, apiErr.Code)
	})
}
