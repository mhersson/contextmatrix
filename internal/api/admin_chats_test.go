package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
)

// newAdminChatFixture wires the admin chat routes onto a bare mux sharing
// the chat manager with the regular fixture. requireAdmin reads the user
// from the request context, injected via asUser.
func newAdminChatFixture(t *testing.T) (*http.ServeMux, *chat.Manager) {
	t.Helper()

	_, mgr := newChatFixture(t, defaultFixtureOpts())

	mux := http.NewServeMux()
	h := &adminChatHandlers{mgr: mgr}
	mux.HandleFunc("GET /api/admin/chats", h.listChats)
	mux.HandleFunc("POST /api/admin/chats/{id}/end", h.endChat)
	mux.HandleFunc("DELETE /api/admin/chats/{id}", h.deleteChat)

	return mux, mgr
}

func TestAdminChats_NonAdminForbidden(t *testing.T) {
	mux, mgr := newAdminChatFixture(t)
	sess := seedSession(t, mgr, "human:alice")
	bob := asUser(mux, "bob", false)

	reqs := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/chats"},
		{http.MethodPost, "/api/admin/chats/" + sess.ID + "/end"},
		{http.MethodDelete, "/api/admin/chats/" + sess.ID},
	}
	for _, tc := range reqs {
		w := httptest.NewRecorder()
		bob.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestAdminChats_ListReturnsAllOwners(t *testing.T) {
	mux, mgr := newAdminChatFixture(t)
	seedSession(t, mgr, "human:alice")
	seedSession(t, mgr, "human:bob")
	seedSession(t, mgr, "human:web-1a2b3c4d") // legacy row

	admin := asUser(mux, "root", true)

	w := httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/chats", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var sessions []chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
	assert.Len(t, sessions, 3)
}

func TestAdminChats_EndAndDeleteForeign(t *testing.T) {
	mux, mgr := newAdminChatFixture(t)
	sess := seedSession(t, mgr, "human:alice")

	_, err := mgr.OpenSession(t.Context(), sess.ID)
	require.NoError(t, err)

	admin := asUser(mux, "root", true)

	w := httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/chats/"+sess.ID+"/end", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	w = httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/admin/chats/"+sess.ID, nil))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestAdminChats_MissingID(t *testing.T) {
	mux, _ := newAdminChatFixture(t)
	admin := asUser(mux, "root", true)

	w := httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/chats/"+unknownChatID+"/end", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/admin/chats/"+unknownChatID, nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
