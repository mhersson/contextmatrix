package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/chat"
)

// asUser wraps a handler the way sessionGuard wraps the router in multi
// mode: every request carries the session-derived identity and user record.
func asUser(h http.Handler, username string, isAdmin bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := &authstore.User{Username: username, IsAdmin: isAdmin}
		h.ServeHTTP(w, r.WithContext(withSessionIdentity(r.Context(), u)))
	})
}

// seedSession creates a session owned by the given identity, bypassing the
// HTTP layer so tests control created_by directly.
func seedSession(t *testing.T, mgr *chat.Manager, owner string) chat.Session {
	t.Helper()

	sess, err := mgr.CreateSession(t.Context(), chat.CreateInput{
		Title:     "seeded",
		CreatedBy: owner,
	})
	require.NoError(t, err)

	return sess
}

const unknownChatID = "UNKNOWN0000000000000000000"

// TestChatOwnership_ForeignIndistinguishableFromMissing is the core matrix:
// for every per-ID endpoint, a foreign owner's request must produce a 404
// with a body identical to the unknown-ID 404 — no existence oracle.
func TestChatOwnership_ForeignIndistinguishableFromMissing(t *testing.T) {
	endpoints := []struct {
		name   string
		method string
		path   func(id string) string
		body   string
	}{
		{"get", http.MethodGet, func(id string) string { return "/api/chats/" + id }, ""},
		{"patch", http.MethodPatch, func(id string) string { return "/api/chats/" + id }, `{"title":"x"}`},
		{"delete", http.MethodDelete, func(id string) string { return "/api/chats/" + id }, ""},
		{"open", http.MethodPost, func(id string) string { return "/api/chats/" + id + "/open" }, ""},
		{"end", http.MethodPost, func(id string) string { return "/api/chats/" + id + "/end" }, ""},
		{"clear", http.MethodPost, func(id string) string { return "/api/chats/" + id + "/clear" }, ""},
		{"send message", http.MethodPost, func(id string) string { return "/api/chats/" + id + "/messages" }, `{"content":"hi"}`},
		{"list messages", http.MethodGet, func(id string) string { return "/api/chats/" + id + "/messages" }, ""},
		{"stream", http.MethodGet, func(id string) string { return "/api/chats/" + id + "/stream" }, ""},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			mux, mgr := newChatFixture(t, defaultFixtureOpts())
			alicesChat := seedSession(t, mgr, "human:alice")
			bob := asUser(mux, "bob", false)

			foreign := httptest.NewRecorder()
			bob.ServeHTTP(foreign, jsonReq(t, ep.method, ep.path(alicesChat.ID), ep.body))

			missing := httptest.NewRecorder()
			bob.ServeHTTP(missing, jsonReq(t, ep.method, ep.path(unknownChatID), ep.body))

			assert.Equal(t, http.StatusNotFound, foreign.Code, "foreign body: %s", foreign.Body.String())
			assert.Equal(t, http.StatusNotFound, missing.Code, "missing body: %s", missing.Body.String())
			assert.Equal(t, missing.Body.String(), foreign.Body.String(),
				"foreign 404 must be indistinguishable from missing 404")
		})
	}
}

// TestChatOwnership_OwnerFullLifecycle proves the owner path still works
// end-to-end with identity injected. clear is exercised by its own existing
// tests (it needs a primer fixture) and by the foreign matrix above.
func TestChatOwnership_OwnerFullLifecycle(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	alice := asUser(mux, "alice", false)

	sess := seedSession(t, mgr, "human:alice")

	steps := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
	}{
		{"get", http.MethodGet, "/api/chats/" + sess.ID, "", http.StatusOK},
		{"rename", http.MethodPatch, "/api/chats/" + sess.ID, `{"title":"mine"}`, http.StatusOK},
		{"open", http.MethodPost, "/api/chats/" + sess.ID + "/open", "", http.StatusOK},
		{"send", http.MethodPost, "/api/chats/" + sess.ID + "/messages", `{"content":"hi"}`, http.StatusAccepted},
		{"list messages", http.MethodGet, "/api/chats/" + sess.ID + "/messages", "", http.StatusOK},
		{"end", http.MethodPost, "/api/chats/" + sess.ID + "/end", "", http.StatusOK},
		{"delete", http.MethodDelete, "/api/chats/" + sess.ID, "", http.StatusNoContent},
	}

	for _, st := range steps {
		w := httptest.NewRecorder()
		alice.ServeHTTP(w, jsonReq(t, st.method, st.path, st.body))
		require.Equal(t, st.wantCode, w.Code, "step %s: body=%s", st.name, w.Body.String())
	}
}

// TestChatOwnership_NoneModeRemainsFlat: without a session identity the
// surface stays unscoped — a spoofed X-Agent-ID (jsonReq sets human:web-x)
// must NOT scope access.
func TestChatOwnership_NoneModeRemainsFlat(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	sess := seedSession(t, mgr, "human:someone-else")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/chats/"+sess.ID, ""))

	assert.Equal(t, http.StatusOK, w.Code)

	var got chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, sess.ID, got.ID)
}

// TestDeleteChat_MissingID_ModeContract: DELETE on a missing ID 404s in
// every mode. DeleteSession loads the session before deleting, so its own
// not-found path fires whether or not there is an identity to scope on —
// there has never been a 204 no-op for an unknown ID in either mode. This
// guards against that ever silently changing, which would reopen the
// existence leak the ownership check closes for foreign IDs.
func TestDeleteChat_MissingID_ModeContract(t *testing.T) {
	t.Run("multi mode", func(t *testing.T) {
		mux, _ := newChatFixture(t, defaultFixtureOpts())
		alice := asUser(mux, "alice", false)

		w := httptest.NewRecorder()
		alice.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/chats/"+unknownChatID, ""))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("none mode", func(t *testing.T) {
		mux, _ := newChatFixture(t, defaultFixtureOpts())

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/chats/"+unknownChatID, ""))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestListChats_ScopedToCallerInMultiMode: the list is force-filtered to
// the caller; a client-supplied created_by is ignored; legacy rows
// (owner matching no account) appear for nobody on the regular list.
func TestListChats_ScopedToCallerInMultiMode(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	seedSession(t, mgr, "human:alice")
	seedSession(t, mgr, "human:alice")
	seedSession(t, mgr, "human:bob")
	seedSession(t, mgr, "human:web-1a2b3c4d") // legacy pre-multi-user row

	alice := asUser(mux, "alice", false)

	for _, path := range []string{"/api/chats", "/api/chats?created_by=human:bob"} {
		w := httptest.NewRecorder()
		alice.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)

		var sessions []chat.Session
		require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
		require.Len(t, sessions, 2, "path %s", path)

		for _, s := range sessions {
			assert.Equal(t, "human:alice", s.CreatedBy)
		}
	}
}

// TestListChats_NoneModeKeepsClientFilter: without identity the
// created_by query param keeps its existing client-filter behavior.
func TestListChats_NoneModeKeepsClientFilter(t *testing.T) {
	mux, mgr := newChatFixture(t, defaultFixtureOpts())
	seedSession(t, mgr, "human:alice")
	seedSession(t, mgr, "human:bob")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/chats?created_by=human:bob", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var sessions []chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
	require.Len(t, sessions, 1)
	assert.Equal(t, "human:bob", sessions[0].CreatedBy)
}

// TestCreateChat_StampsSessionIdentityOverHeader: created_by must be the
// session identity even when the browser sends a spoofed X-Agent-ID
// (jsonReq sets X-Agent-ID: human:web-x).
func TestCreateChat_StampsSessionIdentityOverHeader(t *testing.T) {
	mux, _ := newChatFixture(t, defaultFixtureOpts())
	alice := asUser(mux, "alice", false)

	w := httptest.NewRecorder()
	alice.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/chats", `{"title":"t"}`))
	require.Equal(t, http.StatusCreated, w.Code)

	var sess chat.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&sess))
	assert.Equal(t, "human:alice", sess.CreatedBy)
}
