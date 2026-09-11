package api

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// stubBlacklistAdminStore is a minimal blacklistAdminStore double. Distinct
// from backend_test.go's blacklistReader doubles (BlacklistedSlugs only) -
// the admin endpoints need full rows and deletion.
type stubBlacklistAdminStore struct {
	entries    []sqlite.BlacklistEntry
	entriesErr error
	deleted    bool
	deleteErr  error
	// deletedSlug records what DeleteBlacklistEntry was called with, so the
	// wildcard-route test can prove slashes survive path parsing.
	deletedSlug string
	recordErr   error
	// recorded captures the last RecordIncapableModel call; nil until one
	// lands, so gate tests can prove the store was never touched.
	recorded *recordedBlacklist
}

type recordedBlacklist struct {
	slug, reason, sampleCard, reportedBy string
}

func (s *stubBlacklistAdminStore) BlacklistEntries(context.Context) ([]sqlite.BlacklistEntry, error) {
	return s.entries, s.entriesErr
}

func (s *stubBlacklistAdminStore) DeleteBlacklistEntry(_ context.Context, slug string) (bool, error) {
	s.deletedSlug = slug

	return s.deleted, s.deleteErr
}

func (s *stubBlacklistAdminStore) RecordIncapableModel(_ context.Context, slug, reason, sampleCard, reportedBy string) error {
	s.recorded = &recordedBlacklist{slug: slug, reason: reason, sampleCard: sampleCard, reportedBy: reportedBy}

	return s.recordErr
}

// postBlacklistWithCSRF builds the POST /api/admin/model-blacklist request
// with the CSRF header csrfGuard demands on every non-safe method.
func postBlacklistWithCSRF(t *testing.T, url, body string, cookie *http.Cookie) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("Content-Type", "application/json")

	if cookie != nil {
		req.AddCookie(cookie)
	}

	return req
}

// newBlacklistAdminServer mirrors newOutcomeAdminServer for the
// model-blacklist endpoints.
func newBlacklistAdminServer(t *testing.T, store blacklistAdminStore, multiMode bool) *httptest.Server {
	t.Helper()

	cfg := RouterConfig{BlacklistAdmin: store}

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

func TestAdminModelBlacklist_NoneMode(t *testing.T) {
	store := &stubBlacklistAdminStore{
		entries: []sqlite.BlacklistEntry{{
			Slug: "bad/model", Reason: "parse failures", SampleCard: "CM-7",
			ReportedBy: "agent:x", FirstSeen: 1700000000, LastSeen: 1700000100,
		}},
		deleted: true,
	}
	server := newBlacklistAdminServer(t, store, false)

	resp, err := http.Get(server.URL + "/api/admin/model-blacklist")
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got modelBlacklistResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Models, 1)

	e := got.Models[0]
	assert.Equal(t, "bad/model", e.Slug)
	assert.Equal(t, "parse failures", e.Reason)
	assert.Equal(t, "CM-7", e.SampleCard)
	assert.Equal(t, "agent:x", e.ReportedBy)
	assert.Equal(t, int64(1700000000), e.FirstSeen)
	assert.Equal(t, int64(1700000100), e.LastSeen)

	// DELETE with a slash in the slug - the wildcard route must hand the
	// full remainder to the store.
	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-blacklist/bad/model", nil))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	require.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, "bad/model", store.deletedSlug)
}

func TestAdminModelBlacklist_MultiMode_NonAdmin403(t *testing.T) {
	store := &stubBlacklistAdminStore{}
	server := newBlacklistAdminServer(t, store, true)
	cookie := login(t, server, "bob", "bob password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/model-blacklist", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-blacklist/bad/model", cookie))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusForbidden, resp2.StatusCode)
	assert.Empty(t, store.deletedSlug, "gate must run before the store is touched")
}

func TestAdminModelBlacklist_MultiMode_Admin200(t *testing.T) {
	store := &stubBlacklistAdminStore{
		entries: []sqlite.BlacklistEntry{{Slug: "bad/model", Reason: "r", ReportedBy: "agent:x"}},
		deleted: true,
	}
	server := newBlacklistAdminServer(t, store, true)
	cookie := login(t, server, "root", "root password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/model-blacklist", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp2, err := http.DefaultClient.Do(deleteWithCSRF(t, server.URL+"/api/admin/model-blacklist/bad/model", cookie))
	require.NoError(t, err)

	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestAdminModelBlacklist_DeleteUnknownSlug404(t *testing.T) {
	store := &stubBlacklistAdminStore{deleted: false}
	h := &blacklistAdminHandlers{store: store}

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/model-blacklist/gone/model", nil)
	req.SetPathValue("slug", "gone/model")

	w := httptest.NewRecorder()
	h.delist(w, req)

	res := w.Result()
	defer closeBody(t, res.Body)

	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(res.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeModelNotBlacklisted, apiErr.Code)
}

func TestAdminModelBlacklist_EmptyListIsEmptyArray(t *testing.T) {
	store := &stubBlacklistAdminStore{}
	h := &blacklistAdminHandlers{store: store}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-blacklist", nil)
	w := httptest.NewRecorder()
	h.list(w, req)

	res := w.Result()
	defer closeBody(t, res.Body)

	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"models":[]}`, w.Body.String())
}

func TestAdminModelBlacklist_StoreErrors(t *testing.T) {
	t.Run("list store error", func(t *testing.T) {
		store := &stubBlacklistAdminStore{entriesErr: assert.AnError}
		h := &blacklistAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodGet, "/api/admin/model-blacklist", nil)
		w := httptest.NewRecorder()
		h.list(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})

	t.Run("delist store error", func(t *testing.T) {
		store := &stubBlacklistAdminStore{deleteErr: assert.AnError}
		h := &blacklistAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodDelete, "/api/admin/model-blacklist/bad/model", nil)
		req.SetPathValue("slug", "bad/model")

		w := httptest.NewRecorder()
		h.delist(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})
}

func TestAdminModelBlacklist_Add_NoneMode(t *testing.T) {
	store := &stubBlacklistAdminStore{}
	server := newBlacklistAdminServer(t, store, false)

	resp, err := http.DefaultClient.Do(postBlacklistWithCSRF(t, server.URL+"/api/admin/model-blacklist", `{"slug":"bad/model","reason":"loops on tool calls"}`, nil))
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"slug":"bad/model"}`, string(body))

	require.NotNil(t, store.recorded)
	assert.Equal(t, "bad/model", store.recorded.slug)
	assert.Equal(t, "loops on tool calls", store.recorded.reason)
	assert.Empty(t, store.recorded.sampleCard)
	assert.Equal(t, "operator", store.recorded.reportedBy, "none mode has no session: attributed to the operator")
}

func TestAdminModelBlacklist_Add_DefaultReason(t *testing.T) {
	store := &stubBlacklistAdminStore{}
	h := &blacklistAdminHandlers{store: store}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/model-blacklist", strings.NewReader(`{"slug":"bad/model"}`))
	w := httptest.NewRecorder()
	h.add(w, req)

	res := w.Result()
	defer closeBody(t, res.Body)

	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotNil(t, store.recorded)
	assert.Equal(t, "blacklisted by operator", store.recorded.reason)
}

func TestAdminModelBlacklist_Add_MultiMode(t *testing.T) {
	t.Run("non-admin 403 before the store is touched", func(t *testing.T) {
		store := &stubBlacklistAdminStore{}
		server := newBlacklistAdminServer(t, store, true)
		cookie := login(t, server, "bob", "bob password1")

		resp, err := http.DefaultClient.Do(postBlacklistWithCSRF(t, server.URL+"/api/admin/model-blacklist", `{"slug":"bad/model"}`, cookie))
		require.NoError(t, err)

		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Nil(t, store.recorded)
	})

	t.Run("admin 200 attributed to the session user", func(t *testing.T) {
		store := &stubBlacklistAdminStore{}
		server := newBlacklistAdminServer(t, store, true)
		cookie := login(t, server, "root", "root password1")

		resp, err := http.DefaultClient.Do(postBlacklistWithCSRF(t, server.URL+"/api/admin/model-blacklist", `{"slug":"bad/model"}`, cookie))
		require.NoError(t, err)

		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, store.recorded)
		assert.Equal(t, "root", store.recorded.reportedBy)
	})
}

func TestAdminModelBlacklist_Add_Rejects(t *testing.T) {
	t.Run("empty slug 422", func(t *testing.T) {
		store := &stubBlacklistAdminStore{}
		h := &blacklistAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodPost, "/api/admin/model-blacklist", strings.NewReader(`{"slug":"  ","reason":"r"}`))
		w := httptest.NewRecorder()
		h.add(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(res.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Nil(t, store.recorded)
	})

	t.Run("invalid JSON 400", func(t *testing.T) {
		store := &stubBlacklistAdminStore{}
		h := &blacklistAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodPost, "/api/admin/model-blacklist", strings.NewReader(`{`))
		w := httptest.NewRecorder()
		h.add(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Nil(t, store.recorded)
	})

	t.Run("store error 500", func(t *testing.T) {
		store := &stubBlacklistAdminStore{recordErr: assert.AnError}
		h := &blacklistAdminHandlers{store: store}

		req := httptest.NewRequest(http.MethodPost, "/api/admin/model-blacklist", strings.NewReader(`{"slug":"bad/model"}`))
		w := httptest.NewRecorder()
		h.add(w, req)

		res := w.Result()
		defer closeBody(t, res.Body)

		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})
}
