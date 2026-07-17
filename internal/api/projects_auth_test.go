package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projectsAuthTestServer builds a multi-mode router with both a card Service
// (the "test-project" fixture from testSetup) and an AuthService wired the
// same way main.go wires RouterConfig.CredentialExists - so these tests
// exercise the real multi-mode validation path, not a stand-in. Seeds one
// credential-pool entry ("acme-pat") and returns a session cookie for the
// admin user that created it.
func projectsAuthTestServer(t *testing.T) (*httptest.Server, *http.Cookie) {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	authSvc := auth.NewService(store, time.Hour)

	u, err := store.CreateUser(t.Context(), "root", "Root", true, time.Now())
	require.NoError(t, err)

	hash, err := auth.HashPassword("root password1")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), u.ID, hash, time.Now()))

	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)
	authSvc.SetCredentialKey(credKey)
	authSvc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })

	require.NoError(t, authSvc.CreateCredential(t.Context(), auth.CredentialInput{
		Name: "acme-pat", Kind: authstore.CredentialKindPAT, Secret: "ghp_test",
	}, "human:root"))

	router := NewRouter(RouterConfig{
		Service:          svc,
		Bus:              bus,
		AuthService:      authSvc,
		AuthMode:         "multi",
		CredentialExists: authSvc.CredentialExists,
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	cookie := login(t, server, "root", "root password1")

	return server, cookie
}

// validUpdateProjectBody returns a request body that satisfies UpdateProject's
// non-credential validation (states/types/priorities/transitions), so tests
// only exercise the github_credential path. cred is nil-able.
func validUpdateProjectBody(cred *string) updateProjectRequest {
	return updateProjectRequest{
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
		GitHubCredential: cred,
	}
}

func putProject(t *testing.T, serverURL string, cookie *http.Cookie, body updateProjectRequest) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPut, serverURL+"/api/projects/test-project", bytes.NewReader(raw))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func TestUpdateProject_GitHubCredential_UnknownName_MultiMode(t *testing.T) {
	server, cookie := projectsAuthTestServer(t)

	unknown := "does-not-exist"

	resp := putProject(t, server.URL, cookie, validUpdateProjectBody(&unknown))
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Error, "unknown credential")
}

func TestUpdateProject_GitHubCredential_NonEmptyRejected_NoneMode(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	name := "acme-pat"

	resp := putProject(t, server.URL, nil, validUpdateProjectBody(&name))
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Error, "credential bindings require multi-user mode")
}

func TestUpdateProject_GitHubCredential_ValidName_MultiMode(t *testing.T) {
	server, cookie := projectsAuthTestServer(t)

	name := "acme-pat"

	resp := putProject(t, server.URL, cookie, validUpdateProjectBody(&name))
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Equal(t, "acme-pat", cfg.GitHubCredential)
}

// projectsAuthTestServerWithNonAdmin duplicates projectsAuthTestServer's
// construction (multi-mode router, "test-project" fixture from testSetup) but
// also seeds a non-admin user ("bob") so the admin-gating matrix tests have
// both an admin and a non-admin session to probe with. projectsAuthTestServer
// itself is left untouched - it doesn't expose the underlying authstore.Store,
// so a second user can't be layered on afterward.
func projectsAuthTestServerWithNonAdmin(t *testing.T) (server *httptest.Server, adminCookie, nonAdminCookie *http.Cookie) {
	t.Helper()

	svc, bus, cleanup := testSetup(t)
	t.Cleanup(cleanup)

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	authSvc := auth.NewService(store, time.Hour)

	root, err := store.CreateUser(t.Context(), "root", "Root", true, time.Now())
	require.NoError(t, err)

	rootHash, err := auth.HashPassword("root password1")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), root.ID, rootHash, time.Now()))

	bob, err := store.CreateUser(t.Context(), "bob", "Bob", false, time.Now())
	require.NoError(t, err)

	bobHash, err := auth.HashPassword("bob password1")
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(t.Context(), bob.ID, bobHash, time.Now()))

	router := NewRouter(RouterConfig{
		Service:     svc,
		Bus:         bus,
		AuthService: authSvc,
		AuthMode:    "multi",
	})

	testServer := httptest.NewServer(router)
	t.Cleanup(testServer.Close)

	adminCookie = login(t, testServer, "root", "root password1")
	nonAdminCookie = login(t, testServer, "bob", "bob password1")

	return testServer, adminCookie, nonAdminCookie
}

// TestProjectMutations_NonAdminForbidden_MultiMode covers the gating matrix:
// every project-mutation route must reject a logged-in non-admin with 403 in
// multi mode. The admin gate is the first statement in each handler, so a
// near-empty JSON body never gets far enough to trigger a 400/422 instead.
func TestProjectMutations_NonAdminForbidden_MultiMode(t *testing.T) {
	server, _, bob := projectsAuthTestServerWithNonAdmin(t)

	probes := []struct{ method, path string }{
		{http.MethodPost, "/api/projects"},
		{http.MethodPut, "/api/projects/test-project"},
		{http.MethodDelete, "/api/projects/test-project"},
		{http.MethodPost, "/api/projects/test-project/recalculate-costs"},
	}

	for _, probe := range probes {
		req, err := http.NewRequest(probe.method, server.URL+probe.path, jsonBody(t, map[string]any{}))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "contextmatrix")
		req.AddCookie(bob)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", probe.method, probe.path)
	}
}

// TestProjectMutations_AllowedForAdmin_MultiMode is the positive side of the
// gating matrix: an admin session reaches the real handler logic (not a 403)
// on every gated route.
func TestProjectMutations_AllowedForAdmin_MultiMode(t *testing.T) {
	server, admin, _ := projectsAuthTestServerWithNonAdmin(t)

	// recalculate-costs
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/recalculate-costs",
		jsonBody(t, map[string]string{"default_model": "claude-sonnet-4-6"}))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(admin)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// update
	updateResp := putProject(t, server.URL, admin, validUpdateProjectBody(nil))
	defer closeBody(t, updateResp.Body)

	assert.Equal(t, http.StatusOK, updateResp.StatusCode)

	// create
	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/projects", jsonBody(t, validProjectBody()))
	require.NoError(t, err)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Requested-With", "contextmatrix")
	createReq.AddCookie(admin)

	createResp, err := http.DefaultClient.Do(createReq)
	require.NoError(t, err)

	_ = createResp.Body.Close()
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	// delete
	deleteReq, err := http.NewRequest(http.MethodDelete, server.URL+"/api/projects/new-project", nil)
	require.NoError(t, err)
	deleteReq.Header.Set("X-Requested-With", "contextmatrix")
	deleteReq.AddCookie(admin)

	deleteResp, err := http.DefaultClient.Do(deleteReq)
	require.NoError(t, err)

	_ = deleteResp.Body.Close()
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode)
}

// TestProjectReads_OpenToNonAdmin_MultiMode confirms the gate is scoped to
// mutations: every read route (and the card list, which is not part of
// projectHandlers at all) stays reachable for a logged-in non-admin.
func TestProjectReads_OpenToNonAdmin_MultiMode(t *testing.T) {
	server, _, bob := projectsAuthTestServerWithNonAdmin(t)

	paths := []string{
		"/api/projects",
		"/api/projects/test-project",
		"/api/projects/test-project/usage",
		"/api/projects/test-project/dashboard",
		"/api/projects/test-project/activity",
		"/api/projects/test-project/cards",
	}

	for _, path := range paths {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		req.AddCookie(bob)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "GET %s", path)
	}
}

// deleteCredentialAsAdmin issues DELETE /api/admin/credentials/{name} with
// the given cookie and returns the raw response for status/body assertions.
func deleteCredentialAsAdmin(t *testing.T, serverURL string, cookie *http.Cookie, name string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, serverURL+"/api/admin/credentials/"+name, nil)
	require.NoError(t, err)

	req.Header.Set("X-Requested-With", "contextmatrix")
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

// TestDeleteCredential_BoundToProject_Conflict409 covers the bound-delete
// guard end to end: bind "acme-pat" to test-project, confirm DELETE refuses
// with 409 naming the project, unbind, then confirm DELETE succeeds.
func TestDeleteCredential_BoundToProject_Conflict409(t *testing.T) {
	server, cookie := projectsAuthTestServer(t)

	name := "acme-pat"

	bindResp := putProject(t, server.URL, cookie, validUpdateProjectBody(&name))
	require.Equal(t, http.StatusOK, bindResp.StatusCode)
	closeBody(t, bindResp.Body)

	resp := deleteCredentialAsAdmin(t, server.URL, cookie, "acme-pat")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Details, "test-project")

	// Unbind, then delete succeeds.
	empty := ""

	unbindResp := putProject(t, server.URL, cookie, validUpdateProjectBody(&empty))
	require.Equal(t, http.StatusOK, unbindResp.StatusCode)
	closeBody(t, unbindResp.Body)

	deleteResp := deleteCredentialAsAdmin(t, server.URL, cookie, "acme-pat")
	defer closeBody(t, deleteResp.Body)

	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode)
}
