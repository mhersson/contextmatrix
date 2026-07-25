package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListUsers_NonAdminSessionSortedNoPasswordLeak(t *testing.T) {
	server, _, store := newAuthTestServer(t) // seeds admin root/root password1

	// A second, non-admin user so we can log in without admin rights and
	// confirm the roster is not admin-gated.
	u, err := store.CreateUser(t.Context(), "bob", "Bob", false, timeNow())
	require.NoError(t, err)

	hash, err := authHashForTest(t, "password12345")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, timeNow()))

	bobCookie := login(t, server, "bob", "password12345")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/users", nil)
	require.NoError(t, err)
	req.AddCookie(bobCookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "has_password")
	assert.NotContains(t, string(raw), "last_login_at")
	assert.NotContains(t, string(raw), "is_admin")

	var got []userSummary

	require.NoError(t, json.Unmarshal(raw, &got))

	require.Len(t, got, 2)
	assert.Equal(t, "bob", got[0].Username)
	assert.Equal(t, "Bob", got[0].DisplayName)
	assert.Equal(t, "root", got[1].Username)
	assert.Equal(t, "Root", got[1].DisplayName)
}

func TestListUsers_DisabledUserExcluded(t *testing.T) {
	server, svc, store := newAuthTestServer(t)

	u, err := store.CreateUser(t.Context(), "carol", "Carol", false, timeNow())
	require.NoError(t, err)

	hash, err := authHashForTest(t, "password12345")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, timeNow()))

	require.NoError(t, svc.SetUserDisabled(t.Context(), "carol", true))

	adminCookie := login(t, server, "root", "root password1")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/users", nil)
	require.NoError(t, err)
	req.AddCookie(adminCookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	var got []userSummary

	require.NoError(t, jsonDecode(resp, &got))

	for _, u := range got {
		assert.NotEqual(t, "carol", u.Username, "disabled user must not appear in the roster")
	}

	require.Len(t, got, 1)
	assert.Equal(t, "root", got[0].Username)
}

func TestListUsers_NoSessionUnauthorized(t *testing.T) {
	server, _, _ := newAuthTestServer(t)

	resp, err := http.Get(server.URL + "/api/users")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListUsers_NoneModeNotFound(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/api/users")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
