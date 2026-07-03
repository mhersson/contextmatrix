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
// same way main.go wires RouterConfig.CredentialExists — so these tests
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
