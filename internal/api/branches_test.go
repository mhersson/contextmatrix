package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// testSetupWithGitHubRepo creates a test environment with a project that has a GitHub repo URL.
func testSetupWithGitHubRepo(t *testing.T, repoURL string) (*service.CardService, *events.Bus, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "gh-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	boardConfig := `name: gh-project
prefix: GH
next_id: 1
repo: ` + repoURL + `
states: [todo, in_progress, done, stalled, not_planned]
types: [task]
priorities: [medium]
transitions:
  todo: [in_progress]
  in_progress: [done]
  done: [todo]
  stalled: [todo]
  not_planned: [todo]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return svc, bus, func() {}
}

// mockBranchFetcher is a test double for BranchFetcher.
type mockBranchFetcher struct {
	branches []string
	err      error
}

func (m *mockBranchFetcher) FetchBranches(_ context.Context, _, _ string) ([]string, error) {
	return m.branches, m.err
}

func TestListBranches_NoToken(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// No GitHubToken set — expect 503
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, "NO_GITHUB_TOKEN", apiErr.Code)
}

func TestListBranches_NoGitHubRepo(t *testing.T) {
	// testSetup creates a project with no repo URL
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, GitHubToken: "test-token"})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/test-project/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, "NO_GITHUB_REPO", apiErr.Code)
}

func TestListBranches_NonGitHubRepo(t *testing.T) {
	// Project has a repo URL that is not GitHub
	svc, bus, cleanup := testSetupWithGitHubRepo(t, "https://gitlab.com/owner/repo")
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, GitHubToken: "test-token"})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/gh-project/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, "NO_GITHUB_REPO", apiErr.Code)
}

func TestListBranches_ProjectNotFound(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, GitHubToken: "test-token"})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/nonexistent/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeProjectNotFound, apiErr.Code)
}

func TestListBranches_Success(t *testing.T) {
	svc, _, cleanup := testSetupWithGitHubRepo(t, "https://github.com/owner/repo")
	defer cleanup()

	mock := &mockBranchFetcher{branches: []string{"develop", "feature/abc", "main"}}

	bh := &branchHandlers{
		svc:             svc,
		githubToken:     "test-token",
		allowedHosts:    []string{"github.com"},
		newBranchClient: func(_, _ string) BranchFetcher { return mock },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects/{project}/branches", bh.listBranches)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/gh-project/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var branches []string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&branches))
	assert.Equal(t, []string{"develop", "feature/abc", "main"}, branches)
}

func TestListBranches_FetchError(t *testing.T) {
	svc, _, cleanup := testSetupWithGitHubRepo(t, "https://github.com/owner/repo")
	defer cleanup()

	mock := &mockBranchFetcher{err: errors.New("network error")}

	bh := &branchHandlers{
		svc:             svc,
		githubToken:     "test-token",
		allowedHosts:    []string{"github.com"},
		newBranchClient: func(_, _ string) BranchFetcher { return mock },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects/{project}/branches", bh.listBranches)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects/gh-project/branches")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInternalError, apiErr.Code)
}

