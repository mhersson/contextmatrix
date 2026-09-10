package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
)

// stubSelectorAdminStore is a minimal selectorAdminStore double. It records
// the last PutSelectorLadders argument so handler tests can prove the gate
// and the validation run before the store is touched.
type stubSelectorAdminStore struct {
	ladders   map[string]map[string]float64
	updatedAt time.Time
	getErr    error
	putErr    error
	put       map[string]map[string]float64
}

func (s *stubSelectorAdminStore) SelectorLadders(context.Context) (map[string]map[string]float64, time.Time, error) {
	return s.ladders, s.updatedAt, s.getErr
}

func (s *stubSelectorAdminStore) PutSelectorLadders(_ context.Context, ladders map[string]map[string]float64) error {
	s.put = ladders
	if s.putErr != nil {
		return s.putErr
	}

	s.ladders = ladders
	s.updatedAt = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	return nil
}

// newSelectorAdminServer mirrors newBlacklistAdminServer: cfg is used as
// given in none mode; multi mode adds an auth service seeded with an admin
// ("root") and a non-admin ("bob").
func newSelectorAdminServer(t *testing.T, cfg RouterConfig, multiMode bool) *httptest.Server {
	t.Helper()

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

	server := httptest.NewServer(NewRouter(cfg))
	t.Cleanup(server.Close)

	return server
}

// selectorRequest builds a request carrying the CSRF header the router
// requires on every non-safe method, a JSON body when given, and an
// optional session cookie.
func selectorRequest(t *testing.T, method, url string, body any, cookie *http.Cookie) *http.Request {
	t.Helper()

	var (
		req *http.Request
		err error
	)

	if body == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, jsonBody(t, body))
	}

	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	if cookie != nil {
		req.AddCookie(cookie)
	}

	return req
}

func validLadderBody() map[string]any {
	return map[string]any{"ladders": map[string]map[string]float64{
		"coder":    {"complex": 0.9},
		"reviewer": {"complex": 0.9},
	}}
}

func TestAdminSelectorLadders_EmptyStoreIsDefault(t *testing.T) {
	store := &stubSelectorAdminStore{}
	server := newSelectorAdminServer(t, RouterConfig{SelectorAdmin: store}, false)

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodGet, server.URL+"/api/admin/selector/ladders", nil, nil))
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got selectorLaddersResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.True(t, got.IsDefault)
	assert.Empty(t, got.UpdatedAt)
	assert.Len(t, got.Ladders["coder"], 4)
	assert.InDelta(t, 0.82, got.Ladders["coder"]["complex"], 1e-9)
	assert.InDelta(t, 0.90, got.Ladders["reviewer"]["critical"], 1e-9)
	assert.InDelta(t, 0.76, got.Defaults["moderate"], 1e-9)
}

func TestAdminSelectorLadders_StoredLaddersAreReported(t *testing.T) {
	store := &stubSelectorAdminStore{
		ladders: map[string]map[string]float64{
			"coder":    {"simple": 0.65, "moderate": 0.80, "complex": 0.90, "critical": 0.95},
			"reviewer": {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.93},
		},
		updatedAt: time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC),
	}
	h := &selectorAdminHandlers{store: store}

	w := httptest.NewRecorder()
	h.getLadders(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/ladders", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.False(t, got.IsDefault)
	assert.Equal(t, "2026-09-10T08:30:00Z", got.UpdatedAt)
	assert.InDelta(t, 0.90, got.Ladders["coder"]["complex"], 1e-9)
	assert.InDelta(t, 0.93, got.Ladders["reviewer"]["critical"], 1e-9)
}

func TestAdminSelectorLadders_PutStoresAndEchoes(t *testing.T) {
	store := &stubSelectorAdminStore{}
	server := newSelectorAdminServer(t, RouterConfig{SelectorAdmin: store}, false)

	body := map[string]any{"ladders": map[string]map[string]float64{
		"coder":    {"critical": 0.95},
		"reviewer": {"complex": 0.85},
	}}

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodPut, server.URL+"/api/admin/selector/ladders", body, nil))
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, store.put)
	assert.InDelta(t, 0.95, store.put["coder"]["critical"], 1e-9)

	var got selectorLaddersResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.False(t, got.IsDefault)
	assert.Equal(t, "2026-09-10T12:00:00Z", got.UpdatedAt)
	assert.InDelta(t, 0.95, got.Ladders["coder"]["critical"], 1e-9)
	assert.InDelta(t, 0.82, got.Ladders["coder"]["complex"], 1e-9, "an unnamed tier reads as its default")
	assert.InDelta(t, 0.85, got.Ladders["reviewer"]["complex"], 1e-9)
}

func TestAdminSelectorLadders_PutRejects(t *testing.T) {
	cases := map[string]struct {
		body   any
		status int
		code   string
	}{
		"missing role": {
			body:   map[string]any{"ladders": map[string]map[string]float64{"coder": {"complex": 0.9}}},
			status: http.StatusUnprocessableEntity,
			code:   ErrCodeValidationError,
		},
		"non-monotone": {
			body:   map[string]any{"ladders": map[string]map[string]float64{"coder": {"complex": 0.7}, "reviewer": {"complex": 0.9}}},
			status: http.StatusUnprocessableEntity,
			code:   ErrCodeValidationError,
		},
		"unknown role": {
			body:   map[string]any{"ladders": map[string]map[string]float64{"coder": {"complex": 0.9}, "reviewer": {"complex": 0.9}, "judge": {"complex": 0.9}}},
			status: http.StatusUnprocessableEntity,
			code:   ErrCodeValidationError,
		},
		"bad json": {
			body:   "{",
			status: http.StatusBadRequest,
			code:   ErrCodeBadRequest,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := &stubSelectorAdminStore{}
			h := &selectorAdminHandlers{store: store}

			var req *http.Request
			if raw, ok := tc.body.(string); ok {
				req = httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", strings.NewReader(raw))
			} else {
				req = httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", jsonBody(t, tc.body))
			}

			w := httptest.NewRecorder()
			h.putLadders(w, req)

			assert.Equal(t, tc.status, w.Code)
			assert.Nil(t, store.put, "validation must run before the store is touched")

			var apiErr APIError
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apiErr))
			assert.Equal(t, tc.code, apiErr.Code)
		})
	}
}

func TestAdminSelectorLadders_StoreErrors(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		h := &selectorAdminHandlers{store: &stubSelectorAdminStore{getErr: assert.AnError}}

		w := httptest.NewRecorder()
		h.getLadders(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/ladders", nil))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("put", func(t *testing.T) {
		h := &selectorAdminHandlers{store: &stubSelectorAdminStore{putErr: assert.AnError}}

		w := httptest.NewRecorder()
		h.putLadders(w, httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", jsonBody(t, validLadderBody())))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestAdminSelectorLadders_MultiMode(t *testing.T) {
	store := &stubSelectorAdminStore{}
	server := newSelectorAdminServer(t, RouterConfig{SelectorAdmin: store}, true)

	bob := login(t, server, "bob", "bob password1")

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodGet, server.URL+"/api/admin/selector/ladders", nil, bob))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp, err = http.DefaultClient.Do(selectorRequest(t, http.MethodPut, server.URL+"/api/admin/selector/ladders", validLadderBody(), bob))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Nil(t, store.put, "gate must run before the store is touched")

	root := login(t, server, "root", "root password1")

	resp, err = http.DefaultClient.Do(selectorRequest(t, http.MethodPut, server.URL+"/api/admin/selector/ladders", validLadderBody(), root))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotNil(t, store.put)
}
