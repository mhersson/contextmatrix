package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adminTestServer: multi-mode router with admin "root" and non-admin "bob",
// both with password "password12345". Reuses newAuthTestServer's store/svc
// plumbing — extract a shared helper if that reads better, but do not change
// newAuthTestServer's existing behavior (S2/S3 tests depend on it).
func adminTestServer(t *testing.T) (*httptest.Server, *http.Cookie, *http.Cookie) {
	t.Helper()

	server, svc, store := newAuthTestServer(t) // seeds admin root/root password1
	_ = svc

	u, err := store.CreateUser(t.Context(), "bob", "Bob", false, timeNow())
	require.NoError(t, err)

	hash, err := authHashForTest(t, "password12345")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, timeNow()))

	adminCookie := login(t, server, "root", "root password1")
	bobCookie := login(t, server, "bob", "password12345")

	return server, adminCookie, bobCookie
}

func TestAdminUsers_ForbiddenForNonAdmin(t *testing.T) {
	server, _, bob := adminTestServer(t)

	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodPatch, "/api/admin/users/root"},
		{http.MethodPost, "/api/admin/users/root/invite"},
	} {
		req, _ := http.NewRequest(probe.method, server.URL+probe.path, jsonBody(t, map[string]string{}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(bob)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", probe.method, probe.path)
	}
}

func TestAdminUsers_CreateListPatchInvite(t *testing.T) {
	server, admin, _ := adminTestServer(t)

	// Create → 201 with user + invite token.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/users",
		jsonBody(t, map[string]any{"username": "Carol", "display_name": "Carol C"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	var created struct {
		User struct {
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
		} `json:"user"`
		Invite struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expires_at"`
		} `json:"invite"`
	}

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NoError(t, jsonDecode(resp, &created))
	assert.Equal(t, "carol", created.User.Username)
	assert.False(t, created.User.HasPassword)
	assert.NotEmpty(t, created.Invite.Token)

	// The invite token actually redeems over HTTP.
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/token/"+created.Invite.Token,
		jsonBody(t, map[string]string{"password": "carols password1"}))
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// List includes all three users, ordered.
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/admin/users", nil)
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	var list []map[string]any

	require.NoError(t, jsonDecode(resp, &list))
	assert.Len(t, list, 3)

	// Patch: promote carol.
	req, _ = http.NewRequest(http.MethodPatch, server.URL+"/api/admin/users/carol",
		jsonBody(t, map[string]any{"is_admin": true}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Regenerate: carol has a password now → reset purpose.
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/admin/users/carol/invite", jsonBody(t, map[string]string{}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	var regen struct {
		Token   string `json:"token"`
		Purpose string `json:"purpose"`
	}

	require.NoError(t, jsonDecode(resp, &regen))
	assert.Equal(t, "reset", regen.Purpose)
	assert.NotEmpty(t, regen.Token)
}

func TestAdminUsers_LastAdmin409(t *testing.T) {
	server, admin, _ := adminTestServer(t)

	req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/admin/users/root",
		jsonBody(t, map[string]any{"is_admin": false}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAdminUsers_ErrorMappings(t *testing.T) {
	server, admin, _ := adminTestServer(t)

	// Test duplicate username on POST /api/admin/users
	t.Run("duplicate username → 422 VALIDATION_ERROR", func(t *testing.T) {
		// First create Dave
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/users",
			jsonBody(t, map[string]any{"username": "Dave", "display_name": "Dave D"}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(admin)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Try to create Dave again
		req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/admin/users",
			jsonBody(t, map[string]any{"username": "Dave", "display_name": "Dave D"}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(admin)

		resp, err = http.DefaultClient.Do(req)
		require.NoError(t, err)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr struct {
			Code string `json:"code"`
		}
		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})

	// Test invalid username on POST /api/admin/users
	t.Run("invalid username → 422 VALIDATION_ERROR", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/users",
			jsonBody(t, map[string]any{"username": "Bad Name!", "display_name": "Bad"}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(admin)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr struct {
			Code string `json:"code"`
		}
		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})

	// Test unknown user on PATCH /api/admin/users/{username}
	t.Run("unknown user on PATCH → 404 USER_NOT_FOUND", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/admin/users/unknownuser",
			jsonBody(t, map[string]any{"is_admin": true}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(admin)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr struct {
			Code string `json:"code"`
		}
		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeUserNotFound, apiErr.Code)
	})

	// Test unknown user on POST /api/admin/users/{username}/invite
	t.Run("unknown user on POST /invite → 404 USER_NOT_FOUND", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/users/unknownuser/invite",
			jsonBody(t, map[string]string{}))
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(admin)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr struct {
			Code string `json:"code"`
		}
		require.NoError(t, jsonDecode(resp, &apiErr))
		assert.Equal(t, ErrCodeUserNotFound, apiErr.Code)
	})
}

func TestAdminCredentials_Journey(t *testing.T) {
	server, admin, bob := adminTestServer(t)

	// Non-admin: 403.
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/admin/credentials", nil)
	req.AddCookie(bob)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Create (checker is stubbed to success in newAuthTestServer's service —
	// see Step 3 note below).
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/admin/credentials",
		jsonBody(t, map[string]any{"name": "acme-pat", "kind": "pat", "secret": "ghp_zzz"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	_ = resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
	assert.NotContains(t, string(body), "ghp_zzz", "no response ever carries a secret")

	// List: metadata only.
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/admin/credentials", nil)
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	_ = resp.Body.Close()

	assert.Contains(t, string(body), `"acme-pat"`)
	assert.NotContains(t, string(body), "ghp_zzz")
	assert.NotContains(t, string(body), "secret", "no secret-shaped field in list responses")

	// Rotate via PUT with secret.
	req, _ = http.NewRequest(http.MethodPut, server.URL+"/api/admin/credentials/acme-pat",
		jsonBody(t, map[string]any{"secret": "ghp_new"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Disable, then delete.
	req, _ = http.NewRequest(http.MethodPut, server.URL+"/api/admin/credentials/acme-pat",
		jsonBody(t, map[string]any{"disabled": true}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/admin/credentials/acme-pat", nil)
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestAdminCredentials_ValidationErrors(t *testing.T) {
	server, admin, _ := adminTestServer(t)

	// Shape error → 422.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/credentials",
		jsonBody(t, map[string]any{"name": "Bad Name", "kind": "pat", "secret": "x"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

// TestAdminCredentials_GitHubRejection overrides the auth.Service's checker
// (set up in newAuthTestServer) to exercise the 422 GitHub-rejection path
// over HTTP. adminTestServer wraps newAuthTestServer but doesn't return the
// svc handle, so this test drives newAuthTestServer directly and seeds its
// own admin session.
func TestAdminCredentials_GitHubRejection(t *testing.T) {
	server, svc, _ := newAuthTestServer(t)

	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error {
		return assert.AnError
	})

	adminCookie := login(t, server, "root", "root password1")

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/admin/credentials",
		jsonBody(t, map[string]any{"name": "rejected-pat", "kind": "pat", "secret": "ghp_bad"}))
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(adminCookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	_ = resp.Body.Close()

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))
	assert.NotContains(t, string(body), "ghp_bad", "no response ever carries a secret, even on rejection")
}
