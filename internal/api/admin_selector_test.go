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

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
)

// stubSelectorAdminStore is a minimal selectorAdminStore double. It records
// the last PutSelectorLadders argument so handler tests can prove the gate
// and the validation run before the store is touched.
type stubSelectorAdminStore struct {
	ladders     map[string]map[string]float64
	updatedAt   time.Time
	headroom    float64
	headroomAt  time.Time
	getErr      error
	putErr      error
	put         map[string]map[string]float64
	putHeadroom float64
}

func (s *stubSelectorAdminStore) SelectorLadders(context.Context) (map[string]map[string]float64, time.Time, error) {
	return s.ladders, s.updatedAt, s.getErr
}

func (s *stubSelectorAdminStore) SelectorHeadroom(context.Context) (float64, time.Time, error) {
	return s.headroom, s.headroomAt, s.getErr
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

func (s *stubSelectorAdminStore) PutSelectorHeadroom(_ context.Context, h float64) error {
	s.putHeadroom = h
	if s.putErr != nil {
		return s.putErr
	}

	s.headroom = h
	s.headroomAt = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

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

type stubSelectorCatalog struct {
	candidates  []protocol.CandidateModel
	floor       float64
	refreshedAt time.Time
}

func (s *stubSelectorCatalog) Candidates(context.Context) []protocol.CandidateModel {
	return s.candidates
}

func (s *stubSelectorCatalog) Floor() float64 { return s.floor }

func (s *stubSelectorCatalog) LastRefreshed(context.Context) time.Time { return s.refreshedAt }

// previewCatalog is the fixture behind the preview tests. Blended prices
// per token: a/cheap 2e-6, a/mid 4e-6, b/pricey 2e-5, c/weak 1e-6. Under
// the built-in ladder the complex reviewer pool is a/cheap, a/mid, b/pricey
// and the complex coder pool is a/cheap, b/pricey.
func previewCatalog() *stubSelectorCatalog {
	return &stubSelectorCatalog{
		floor:       0.65,
		refreshedAt: time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC),
		candidates: []protocol.CandidateModel{
			{Slug: "b/pricey", Creator: "b", CoderPrior: 0.83, ReviewerPrior: 0.84, PromptPricePerTok: 1e-5, CompletionPricePerTok: 1e-5, ContextWindow: 200000},
			{Slug: "a/cheap", Creator: "a", CoderPrior: 0.90, ReviewerPrior: 0.85, PromptPricePerTok: 1e-6, CompletionPricePerTok: 1e-6, ContextWindow: 200000},
			{Slug: "c/weak", Creator: "c", CoderPrior: 0.70, ReviewerPrior: 0.70, PromptPricePerTok: 5e-7, CompletionPricePerTok: 5e-7, ContextWindow: 100000},
			{Slug: "a/mid", Creator: "a", CoderPrior: 0.80, ReviewerPrior: 0.86, PromptPricePerTok: 2e-6, CompletionPricePerTok: 2e-6, ContextWindow: 200000},
		},
	}
}

func defaultLadderBody() map[string]any {
	return map[string]any{"ladders": map[string]map[string]float64{
		"coder":    {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.90},
		"reviewer": {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.90},
	}}
}

func TestAdminSelectorCandidates_ReportsInputsSorted(t *testing.T) {
	h := &selectorAdminHandlers{
		store:     &stubSelectorAdminStore{},
		catalog:   previewCatalog(),
		blacklist: &stubBlacklist{slugs: []string{"c/weak"}},
		favorites: map[string]board.TierFavorites{"critical": {ByRole: map[string][]string{"reviewer": {"b/pricey"}}}},
	}

	w := httptest.NewRecorder()
	h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorCandidatesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Candidates, 4)
	assert.Equal(t, "a/cheap", got.Candidates[0].Slug, "sorted by slug")
	assert.Equal(t, "a", got.Candidates[0].Creator)
	assert.InDelta(t, 0.85, got.Candidates[0].ReviewerPrior, 1e-9)
	assert.InDelta(t, 1e-6, got.Candidates[0].PromptPricePerTok, 1e-15)
	assert.Equal(t, 200000, got.Candidates[0].ContextWindow)
	assert.Equal(t, []string{"c/weak"}, got.Blacklist)
	require.Len(t, got.Favorites, 1)
	assert.Equal(t, "reviewer", got.Favorites[0].Role)
	assert.Equal(t, "critical", got.Favorites[0].Tier)
	assert.InDelta(t, 0.65, got.QualityFloor, 1e-9)
	assert.Equal(t, "2026-09-10T06:00:00Z", got.CatalogRefreshedAt)
}

func TestAdminSelectorCandidates_FavoritesAreOrdered(t *testing.T) {
	h := &selectorAdminHandlers{
		store:   &stubSelectorAdminStore{},
		catalog: previewCatalog(),
		favorites: map[string]board.TierFavorites{
			"critical": {All: []string{"a/cheap"}, ByRole: map[string][]string{"reviewer": {"b/pricey"}}},
			"complex":  {ByRole: map[string][]string{"coder": {"a/mid"}}},
		},
	}

	// Favorites come out of a map, so one call can pass by luck; repeat to
	// catch an unsorted result.
	for range 10 {
		w := httptest.NewRecorder()
		h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))

		require.Equal(t, http.StatusOK, w.Code)

		var got selectorCandidatesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

		require.Len(t, got.Favorites, 3)
		assert.Equal(t, []string{"complex/coder", "critical/", "critical/reviewer"},
			[]string{
				got.Favorites[0].Tier + "/" + got.Favorites[0].Role,
				got.Favorites[1].Tier + "/" + got.Favorites[1].Role,
				got.Favorites[2].Tier + "/" + got.Favorites[2].Role,
			})
	}
}

func TestAdminSelectorCandidates_EmptyInputsAreArrays(t *testing.T) {
	cat := previewCatalog()
	cat.candidates = nil
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: cat}

	w := httptest.NewRecorder()
	h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"candidates":[]`)
	assert.Contains(t, w.Body.String(), `"favorites":[]`)
	assert.Contains(t, w.Body.String(), `"blacklist":[]`)
}

func TestAdminSelectorCandidates_BlacklistReadFailureIs500(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog(), blacklist: &failingBlacklist{}}

	w := httptest.NewRecorder()
	h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type failingBlacklist struct{}

func (failingBlacklist) BlacklistedSlugs(context.Context) ([]string, error) {
	return nil, assert.AnError
}

func TestAdminSelectorCatalogUnavailable(t *testing.T) {
	never := previewCatalog()
	never.refreshedAt = time.Time{}

	for name, cat := range map[string]selectorCatalog{"no catalog": nil, "never refreshed": never} {
		t.Run(name, func(t *testing.T) {
			h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: cat}

			w := httptest.NewRecorder()
			h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)

			w = httptest.NewRecorder()
			h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, validLadderBody())))
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)

			var apiErr APIError
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apiErr))
			assert.Equal(t, ErrCodeCatalogUnavailable, apiErr.Code)
			assert.Equal(t, "catalog not available yet", apiErr.Error)
		})
	}
}

func TestAdminSelectorPreview_PicksAndPanel(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog(), blacklist: &stubBlacklist{}}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, defaultLadderBody())))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Tiers, 4)

	complexTier := got.Tiers["complex"]

	// Coder at complex: a/cheap (0.90) and b/pricey (0.83) clear 0.82; the
	// band from 2e-6 is 3e-6, so a/cheap is the pick.
	assert.Equal(t, "a/cheap", complexTier.Coder.Pick.Model)
	assert.True(t, complexTier.Coder.Pick.OK)
	assert.Equal(t, "coder", complexTier.Coder.Pick.Role)
	assert.Equal(t, "complex", complexTier.Coder.Pick.MetTier)
	assert.Equal(t, "auto", complexTier.Coder.Pick.Source)
	assert.InDelta(t, 2e-6, complexTier.Coder.Pick.PricePerTok, 1e-15)
	assert.Equal(t, "complex", complexTier.Coder.Report.Rung)
	assert.InDelta(t, 0.82, complexTier.Coder.Report.Bar, 1e-9)
	assert.Len(t, complexTier.Coder.Report.Pool, 2)
	assert.NotEmpty(t, complexTier.Coder.Report.FilteredOut)

	// Reviewer panel at complex: seat 1 a/cheap; seat 2 prefers an unseated
	// vendor, so b/pricey re-anchors the band and is walked; seat 3 has only
	// a/mid left at the rung, anchored above seat 1.
	require.Len(t, complexTier.Panel, 3)
	assert.Equal(t, "a/cheap", complexTier.Panel[0].Pick.Model)
	assert.False(t, complexTier.Panel[0].Walked)
	assert.Equal(t, "b/pricey", complexTier.Panel[1].Pick.Model)
	assert.True(t, complexTier.Panel[1].Walked)
	assert.Equal(t, "a/mid", complexTier.Panel[2].Pick.Model)
	assert.True(t, complexTier.Panel[2].Walked)
	assert.False(t, complexTier.Panel[2].Pick.Duplicate)

	// Critical: no reviewer clears 0.90, so the pick descends to complex;
	// a/cheap clears the coder critical bar exactly.
	critical := got.Tiers["critical"]
	assert.Equal(t, "a/cheap", critical.Reviewer.Pick.Model)
	assert.Equal(t, "complex", critical.Reviewer.Pick.MetTier)
	assert.Equal(t, "critical", critical.Reviewer.Pick.RequestedTier)
	assert.Equal(t, "a/cheap", critical.Coder.Pick.Model)
	assert.Equal(t, "critical", critical.Coder.Pick.MetTier)
}

func TestAdminSelectorPreview_AppliesBlacklistAndFavorites(t *testing.T) {
	h := &selectorAdminHandlers{
		store:     &stubSelectorAdminStore{},
		catalog:   previewCatalog(),
		blacklist: &stubBlacklist{slugs: []string{"a/cheap"}},
		favorites: map[string]board.TierFavorites{"complex": {ByRole: map[string][]string{"reviewer": {"b/pricey"}}}},
	}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, defaultLadderBody())))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	complexTier := got.Tiers["complex"]
	assert.Equal(t, "b/pricey", complexTier.Coder.Pick.Model, "a/cheap is blacklisted")
	assert.Equal(t, "auto", complexTier.Coder.Pick.Source)
	assert.Equal(t, "b/pricey", complexTier.Reviewer.Pick.Model)
	assert.Equal(t, "favorite", complexTier.Reviewer.Pick.Source)
}

func TestAdminSelectorPreview_NothingSelectableIsReported(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog()}

	// Every rung at 0.99: the walk bottoms out for both roles.
	body := map[string]any{"ladders": map[string]map[string]float64{
		"coder":    {"simple": 0.99, "moderate": 0.99, "complex": 0.99, "critical": 0.99},
		"reviewer": {"simple": 0.99, "moderate": 0.99, "complex": 0.99, "critical": 0.99},
	}}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, body)))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.False(t, got.Tiers["complex"].Coder.Pick.OK)
	assert.Empty(t, got.Tiers["complex"].Coder.Pick.Model)
	assert.Empty(t, got.Tiers["complex"].Panel)
	assert.Contains(t, w.Body.String(), `"panel":[]`)
}

func TestAdminSelectorPreview_HeadroomFromRequest(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog()}

	// Headroom 3 admits a/mid (4e-6 <= 2e-6 x 3) into the complex reviewer
	// band; its prior 0.86 beats a/cheap's 0.85.
	body := defaultLadderBody()
	body["headroom"] = 3.0

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, body)))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "a/mid", got.Tiers["complex"].Reviewer.Pick.Model)
}

func TestAdminSelectorPreview_InvalidLadderIs422(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog()}
	body := map[string]any{"ladders": map[string]map[string]float64{"coder": {"complex": 0.5}, "reviewer": {"complex": 0.9}}}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, body)))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestAdminSelectorCatalogRoutes_MultiMode(t *testing.T) {
	server := newSelectorAdminServer(t, RouterConfig{SelectorAdmin: &stubSelectorAdminStore{}, SelectorCatalog: previewCatalog()}, true)

	bob := login(t, server, "bob", "bob password1")

	resp, err := http.DefaultClient.Do(selectorRequest(t, http.MethodGet, server.URL+"/api/admin/selector/candidates", nil, bob))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp, err = http.DefaultClient.Do(selectorRequest(t, http.MethodPost, server.URL+"/api/admin/selector/preview", defaultLadderBody(), bob))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	root := login(t, server, "root", "root password1")

	resp, err = http.DefaultClient.Do(selectorRequest(t, http.MethodPost, server.URL+"/api/admin/selector/preview", defaultLadderBody(), root))
	require.NoError(t, err)
	closeBody(t, resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAdminSelectorLadders_EmptyStoreReportsTheBuiltInHeadroom(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}}

	w := httptest.NewRecorder()
	h.getLadders(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/ladders", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.InDelta(t, 1.5, got.Headroom, 1e-9)
	assert.InDelta(t, 1.5, got.HeadroomDefault, 1e-9)
	assert.True(t, got.IsDefault)
	assert.Empty(t, got.UpdatedAt)
}

func TestAdminSelectorLadders_StoredHeadroomIsReported(t *testing.T) {
	// Only the headroom is stored: the ladders read as the built-in ladder,
	// but the response is not the default and carries the headroom's write time.
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{
		headroom:   2,
		headroomAt: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC),
	}}

	w := httptest.NewRecorder()
	h.getLadders(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/ladders", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.InDelta(t, 2, got.Headroom, 1e-9)
	assert.InDelta(t, 1.5, got.HeadroomDefault, 1e-9)
	assert.False(t, got.IsDefault)
	assert.Equal(t, "2026-09-11T09:00:00Z", got.UpdatedAt)
	assert.InDelta(t, 0.82, got.Ladders["coder"]["complex"], 1e-9, "no stored ladder reads as the built-in one")
}

func TestAdminSelectorLadders_UpdatedAtIsTheLaterWrite(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{
		ladders: map[string]map[string]float64{
			"coder":    {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.90},
			"reviewer": {"simple": 0.65, "moderate": 0.76, "complex": 0.82, "critical": 0.90},
		},
		updatedAt:  time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC),
		headroom:   2,
		headroomAt: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC),
	}}

	w := httptest.NewRecorder()
	h.getLadders(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/ladders", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "2026-09-11T10:00:00Z", got.UpdatedAt)
}

func TestAdminSelectorLadders_PutStoresTheHeadroom(t *testing.T) {
	store := &stubSelectorAdminStore{}
	h := &selectorAdminHandlers{store: store}

	body := defaultLadderBody()
	body["headroom"] = 2.0

	w := httptest.NewRecorder()
	h.putLadders(w, httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", jsonBody(t, body)))

	require.Equal(t, http.StatusOK, w.Code)
	assert.InDelta(t, 2, store.putHeadroom, 1e-9)

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.InDelta(t, 2, got.Headroom, 1e-9)
	assert.False(t, got.IsDefault)
}

func TestAdminSelectorLadders_PutWithoutHeadroomKeepsTheStoredOne(t *testing.T) {
	// A client that only knows the ladders must never reset the headroom.
	store := &stubSelectorAdminStore{headroom: 2, headroomAt: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)}
	h := &selectorAdminHandlers{store: store}

	w := httptest.NewRecorder()
	h.putLadders(w, httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", jsonBody(t, defaultLadderBody())))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Zero(t, store.putHeadroom, "no headroom write happened")

	var got selectorLaddersResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.InDelta(t, 2, got.Headroom, 1e-9)
}

func TestAdminSelectorLadders_PutRejectsAHeadroomBelowOne(t *testing.T) {
	store := &stubSelectorAdminStore{}
	h := &selectorAdminHandlers{store: store}

	body := defaultLadderBody()
	body["headroom"] = 0.5

	w := httptest.NewRecorder()
	h.putLadders(w, httptest.NewRequest(http.MethodPut, "/api/admin/selector/ladders", jsonBody(t, body)))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), "must be a number of at least 1")
	assert.Nil(t, store.put, "nothing is written when either half fails validation")
	assert.Zero(t, store.putHeadroom)
}

func TestAdminSelectorPreview_HeadroomFallsBackToTheStoredOne(t *testing.T) {
	// Stored headroom 3 admits a/mid (4e-6 <= 2e-6 x 3) into the complex
	// reviewer band; its prior 0.86 beats a/cheap's 0.85. The request names
	// no headroom.
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{headroom: 3}, catalog: previewCatalog()}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, defaultLadderBody())))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "a/mid", got.Tiers["complex"].Reviewer.Pick.Model)
}

func TestAdminSelectorPreview_NoStoredHeadroomIsTheBuiltIn(t *testing.T) {
	// Built-in 1.5 keeps a/mid out of the band (4e-6 > 2e-6 x 1.5): a/cheap wins.
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog()}

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, defaultLadderBody())))

	require.Equal(t, http.StatusOK, w.Code)

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "a/cheap", got.Tiers["complex"].Reviewer.Pick.Model)
}

func TestAdminSelectorPreview_HeadroomBelowOneIs422(t *testing.T) {
	h := &selectorAdminHandlers{store: &stubSelectorAdminStore{}, catalog: previewCatalog()}

	body := defaultLadderBody()
	body["headroom"] = 0.5

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, body)))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), "must be a number of at least 1")
}
