package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// closeBody is a helper to close response body and check error in tests.
func closeBody(t *testing.T, body io.Closer) {
	t.Helper()

	if err := body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}
}

// doGet issues a GET with the CSRF header the router requires on browser routes.
func doGet(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

// testSetup creates a test environment with all dependencies.
func testSetup(t *testing.T) (*service.CardService, *events.Bus, func()) {
	t.Helper()

	// Create temp directory
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Create a test project
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	// Write .board.yaml
	boardConfig := `name: test-project
prefix: TEST
next_id: 1
states: [todo, in_progress, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	// Initialize git (boards directory is the git repo)
	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	// Seed an initial commit so HEAD exists and CurrentBranch() works.
	require.NoError(t, git.CommitFile(context.Background(), "test-project/.board.yaml", "init: seed boards repo"))

	// Initialize store
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	// Initialize event bus
	bus := events.NewBus()

	// Initialize lock manager
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// Initialize service
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	cleanup := func() {
		// Temp directory is automatically cleaned up by t.TempDir()
	}

	return svc, bus, cleanup
}

func TestListProjects(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var projects []board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&projects))

	assert.Len(t, projects, 1)
	assert.Equal(t, "test-project", projects[0].Name)
}

func TestGetProject(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("existing project", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var project board.ProjectConfig
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&project))

		assert.Equal(t, "test-project", project.Name)
		assert.Equal(t, "TEST", project.Prefix)
	})

	t.Run("non-existent project", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/nonexistent")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeProjectNotFound, apiErr.Code)
	})
}

func TestCreateCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("valid card", func(t *testing.T) {
		body := createCardRequest{
			Title:    "Test Card",
			Type:     "task",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(jsonBody))

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))

		assert.Equal(t, "TEST-001", card.ID)
		assert.Equal(t, "Test Card", card.Title)
		assert.Equal(t, "task", card.Type)
		assert.Equal(t, "todo", card.State) // Default state
	})

	t.Run("missing title", func(t *testing.T) {
		body := createCardRequest{
			Type:     "task",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(jsonBody))

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid type", func(t *testing.T) {
		body := createCardRequest{
			Title:    "Test Card",
			Type:     "invalid",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(jsonBody))

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	})

	t.Run("non-existent parent returns 404 PARENT_NOT_FOUND", func(t *testing.T) {
		// Regression guard: a missing parent must surface as 404
		// PARENT_NOT_FOUND, not 422 VALIDATION_ERROR - parent is a
		// resource, so clients need 404.
		body := createCardRequest{
			Title:    "Subtask with bogus parent",
			Type:     "task",
			Priority: "medium",
			Parent:   "TEST-999",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(jsonBody))

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeParentNotFound, apiErr.Code)
	})

	t.Run("non-existent project", func(t *testing.T) {
		body := createCardRequest{
			Title:    "Test Card",
			Type:     "task",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(server.URL+"/api/projects/nonexistent/cards", "application/json", bytes.NewReader(jsonBody))

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestGetCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("existing card", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards/TEST-001")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "TEST-001", card.ID)
		assert.Equal(t, "Test Card", card.Title)
	})

	t.Run("non-existent card", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards/TEST-999")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeCardNotFound, apiErr.Code)
	})
}

func TestListCards(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create cards
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Task 1",
		Type:     "task",
		Priority: "high",
	})
	require.NoError(t, err)

	_, err = svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Bug 1",
		Type:     "bug",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("list all", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 2)
		assert.Empty(t, page.NextCursor, "last page should not emit next_cursor")
		require.NotNil(t, page.Total, "first page should include total")
		assert.Equal(t, 2, *page.Total)
	})

	t.Run("filter by type", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?type=task")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 1)
		assert.Equal(t, "task", page.Items[0].Type)
		// Total reflects the UN-filtered project size.
		require.NotNil(t, page.Total)
		assert.Equal(t, 2, *page.Total)
	})

	t.Run("filter by priority", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?priority=high")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 1)
		assert.Equal(t, "high", page.Items[0].Priority)
	})
}

func TestPatchCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("valid state transition", func(t *testing.T) {
		newState := "in_progress"
		body := patchCardRequest{
			State: &newState,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "in_progress", card.State)
	})

	t.Run("invalid state transition", func(t *testing.T) {
		newState := "done" // Can't go from in_progress to done with current state
		body := patchCardRequest{
			State: &newState,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode) // in_progress -> done is valid
	})

	t.Run("invalid phase returns 422", func(t *testing.T) {
		badPhase := "shipping"
		body := patchCardRequest{
			Phase: &badPhase,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Details, "invalid phase")
	})
}

func TestUpdateCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("full update", func(t *testing.T) {
		body := updateCardRequest{
			Title:    "Updated Title",
			Type:     "bug",
			State:    "in_progress",
			Priority: "high",
			Body:     "Updated body content",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "Updated Title", card.Title)
		assert.Equal(t, "bug", card.Type)
		assert.Equal(t, "in_progress", card.State)
		assert.Equal(t, "high", card.Priority)
	})

	t.Run("invalid phase returns 422", func(t *testing.T) {
		badPhase := "shipping"
		body := updateCardRequest{
			Title:    "Updated Title",
			Type:     "bug",
			State:    "in_progress",
			Priority: "high",
			Phase:    &badPhase,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Details, "invalid phase")
	})
}

func TestDeleteCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("delete existing card", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/projects/test-project/cards/TEST-001", nil)

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Verify it's gone
		getResp, err := http.Get(server.URL + "/api/projects/test-project/cards/TEST-001")

		require.NoError(t, err)
		defer closeBody(t, getResp.Body)

		assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
	})

	t.Run("delete non-existent card", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/projects/test-project/cards/TEST-999", nil)

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestCORSHeaders(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, CORSOrigin: "http://localhost:5173"})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, "http://localhost:5173", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORSDisabledWhenEmpty(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORSPreflight(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, CORSOrigin: "http://localhost:5173"})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/projects", nil)
	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "POST")
}

func TestRequestID(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("generates request ID", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		requestID := resp.Header.Get("X-Request-ID")
		assert.NotEmpty(t, requestID)
	})

	t.Run("uses provided request ID", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/projects", nil)
		req.Header.Set("X-Request-ID", "custom-id-123")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, "custom-id-123", resp.Header.Get("X-Request-ID"))
	})
}

func TestHealthz(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestHealthzNotLogged(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Capture log output by replacing the default slog handler.
	var buf bytes.Buffer

	orig := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	resp, err := http.Get(server.URL + "/healthz")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Empty(t, buf.String(), "expected no log output for GET /healthz")
}

func TestReadyzHappyPath(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/readyz")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result readyzResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result.Status)
	assert.NotEmpty(t, result.Checks)

	for _, c := range result.Checks {
		assert.True(t, c.OK, "check %q should be ok", c.Name)
	}
}

func TestReadyzDegradedOnStoreFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod 000 has no effect as root")
	}

	// Create a fresh environment with a boards dir we can chmod to trigger failure.
	boardsDir := t.TempDir()
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	boardConfig := `name: test-project
prefix: TEST
next_id: 1
states: [todo, in_progress, done, stalled, not_planned]
types: [task]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo]
  not_planned: [todo]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	// Seed an initial commit so HEAD exists and git check passes.
	require.NoError(t, git.CommitFile(context.Background(), "test-project/.board.yaml", "init: seed boards repo"))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus2 := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc2 := service.NewCardService(store, git, lockMgr, bus2, boardsDir, nil, true, false)

	router := NewRouter(RouterConfig{Service: svc2, Bus: bus2})

	server := httptest.NewServer(router)
	defer server.Close()

	// Revoke read access to the boards directory so ListProjects fails.
	require.NoError(t, os.Chmod(boardsDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(boardsDir, 0o755) })

	resp, err := http.Get(server.URL + "/readyz")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var result readyzResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "degraded", result.Status)

	// At least the store check should have failed.
	hasFailed := false

	for _, c := range result.Checks {
		if !c.OK {
			hasFailed = true
		}
	}

	assert.True(t, hasFailed, "expected at least one failed check")
}

func TestReadyzNotLogged(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	var buf bytes.Buffer

	orig := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	resp, err := http.Get(server.URL + "/readyz")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Empty(t, buf.String(), "expected no log output for GET /readyz")
}

// === Agent Endpoint Tests ===

func TestClaimCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("successful claim", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "claude-1", card.AssignedAgent)
		assert.NotNil(t, card.LastHeartbeat)
	})

	t.Run("claim already claimed card - 409", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-2")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeAlreadyClaimed, apiErr.Code)
	})

	t.Run("claim with header agent ID", func(t *testing.T) {
		// Release first
		releaseBody := agentRequest{}
		releaseJSON, _ := json.Marshal(releaseBody)
		releaseReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(releaseJSON))
		releaseReq.Header.Set("Content-Type", "application/json")
		releaseReq.Header.Set("X-Agent-ID", "claude-1")
		releaseResp, _ := http.DefaultClient.Do(releaseReq)
		closeBody(t, releaseResp.Body)

		// Claim using header
		body := agentRequest{} // Empty body
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-from-header")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "claude-from-header", card.AssignedAgent)
	})

	t.Run("missing agent_id - 400", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("non-existent card - 404", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-999/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("claim with empty body succeeds", func(t *testing.T) {
		// Ensure the card is unclaimed before this subtest
		_, _ = svc.ReleaseCard(context.Background(), "test-project", "TEST-001", "claude-from-header")

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", http.NoBody)
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestReleaseCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create and claim a card
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(context.Background(), "test-project", "TEST-001", "claude-1")
	require.NoError(t, err)

	t.Run("successful release", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Empty(t, card.AssignedAgent)
		assert.Nil(t, card.LastHeartbeat)
	})

	t.Run("release unclaimed card - 409", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeNotClaimed, apiErr.Code)
	})

	t.Run("release wrong agent - 403", func(t *testing.T) {
		// Claim first
		_, err := svc.ClaimCard(context.Background(), "test-project", "TEST-001", "claude-1")
		require.NoError(t, err)

		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-2")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeAgentMismatch, apiErr.Code)
	})

	t.Run("release with empty body and owning agent succeeds", func(t *testing.T) {
		// Ensure the card is claimed by claude-1
		_, err := svc.ClaimCard(context.Background(), "test-project", "TEST-001", "claude-1")
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", http.NoBody)
		req.Header.Set("X-Agent-ID", "claude-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// === Agent Auth on Mutation Tests ===

func TestAgentAuthOnMutations(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	t.Run("update unclaimed card - no auth required", func(t *testing.T) {
		body := updateCardRequest{
			Title:    "Updated Without Auth",
			Type:     "task",
			State:    "in_progress",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		// No X-Agent-ID header

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("patch unclaimed card - no auth required", func(t *testing.T) {
		newTitle := "Patched Without Auth"
		body := patchCardRequest{
			Title: &newTitle,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		// No X-Agent-ID header

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Claim the card for subsequent tests
	_, err = svc.ClaimCard(context.Background(), "test-project", "TEST-001", "owner-agent")
	require.NoError(t, err)

	t.Run("update claimed card with matching agent", func(t *testing.T) {
		// Internal cards (no Source) are always vetted; a non-human agent
		// must echo the existing Vetted=true on PUT to avoid the
		// human-only-fields guard.
		body := updateCardRequest{
			Title:    "Updated By Owner",
			Type:     "task",
			State:    "in_progress",
			Priority: "high",
			Vetted:   true,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "owner-agent")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("update claimed card with wrong agent - 403", func(t *testing.T) {
		body := updateCardRequest{
			Title:    "Attempted Update",
			Type:     "task",
			State:    "in_progress",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "wrong-agent")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeAgentMismatch, apiErr.Code)
	})

	t.Run("update claimed card without header - 403", func(t *testing.T) {
		body := updateCardRequest{
			Title:    "Attempted Update",
			Type:     "task",
			State:    "in_progress",
			Priority: "medium",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		// No X-Agent-ID header

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("patch claimed card with wrong agent - 403", func(t *testing.T) {
		newTitle := "Attempted Patch"
		body := patchCardRequest{
			Title: &newTitle,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-001", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "wrong-agent")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("delete claimed card with wrong agent - 403", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/projects/test-project/cards/TEST-001", nil)
		req.Header.Set("X-Agent-ID", "wrong-agent")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("delete claimed card with matching agent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/projects/test-project/cards/TEST-001", nil)
		req.Header.Set("X-Agent-ID", "owner-agent")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("human agent can mutate their claimed card", func(t *testing.T) {
		// Create and claim a new card with human agent
		_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
			Title:    "Human Card",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		_, err = svc.ClaimCard(context.Background(), "test-project", "TEST-002", "human:alice")
		require.NoError(t, err)

		newTitle := "Updated by Human"
		body := patchCardRequest{
			Title: &newTitle,
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/TEST-002", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Equal(t, "Updated by Human", card.Title)
	})
}

// === Integration Tests ===

// TestFullCardLifecycle walks through a complete agent workflow via HTTP:
// create → claim → log → transition → heartbeat → transition → release → verify.
func TestFullCardLifecycle(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	baseURL := server.URL + "/api/projects/test-project/cards"
	agentID := "agent-lifecycle"

	// Step 1: Create card
	createBody, _ := json.Marshal(createCardRequest{
		Title:    "Lifecycle Test Card",
		Type:     "task",
		Priority: "high",
		Labels:   []string{"integration"},
		Body:     "## Plan\nIntegration test card",
	})
	resp, err := http.Post(baseURL, "application/json", bytes.NewReader(createBody))

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	cardID := created.ID
	assert.Equal(t, "TEST-001", cardID)
	assert.Equal(t, "todo", created.State)
	assert.Empty(t, created.AssignedAgent)

	cardURL := baseURL + "/" + cardID

	// Step 2: Claim card
	claimBody, _ := json.Marshal(agentRequest{})
	req, _ := http.NewRequest(http.MethodPost, cardURL+"/claim", bytes.NewReader(claimBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", agentID)
	resp2, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var claimed board.Card
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&claimed))
	assert.Equal(t, agentID, claimed.AssignedAgent)
	assert.NotNil(t, claimed.LastHeartbeat)

	// Step 3: Add log entry. addLogEntry is now a service-layer-only
	// operation (the REST mirror was removed); MCP's add_log tool and the
	// backend's promote-webhook-failed path exercise the same svc call.
	logged, err := svc.AddLogEntry(context.Background(), "test-project", cardID, board.ActivityEntry{
		Agent:   agentID,
		Action:  "status_update",
		Message: "Starting implementation",
	})
	require.NoError(t, err)
	require.Len(t, logged.ActivityLog, 1)
	assert.Equal(t, "Starting implementation", logged.ActivityLog[0].Message)

	// Step 4: Transition todo → in_progress
	newState := "in_progress"
	patchBody, _ := json.Marshal(patchCardRequest{State: &newState})
	req, _ = http.NewRequest(http.MethodPatch, cardURL, bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", agentID)
	resp4, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp4.Body)

	assert.Equal(t, http.StatusOK, resp4.StatusCode)

	var inProgress board.Card
	require.NoError(t, json.NewDecoder(resp4.Body).Decode(&inProgress))
	assert.Equal(t, "in_progress", inProgress.State)

	// Step 5: Heartbeat. heartbeatCard is now a service-layer-only operation
	// (the REST mirror was removed); MCP's heartbeat tool exercises the same
	// svc call.
	require.NoError(t, svc.HeartbeatCard(context.Background(), "test-project", cardID, agentID))

	// Step 6: Transition in_progress → done
	doneState := "done"
	patchBody2, _ := json.Marshal(patchCardRequest{State: &doneState})
	req, _ = http.NewRequest(http.MethodPatch, cardURL, bytes.NewReader(patchBody2))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", agentID)
	resp6, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp6.Body)

	assert.Equal(t, http.StatusOK, resp6.StatusCode)

	// Step 7: Release card
	releaseBody, _ := json.Marshal(agentRequest{})
	req, _ = http.NewRequest(http.MethodPost, cardURL+"/release", bytes.NewReader(releaseBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", agentID)
	resp7, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp7.Body)

	assert.Equal(t, http.StatusOK, resp7.StatusCode)

	// Step 8: Verify final state via GET
	resp8, err := http.Get(cardURL)

	require.NoError(t, err)
	defer closeBody(t, resp8.Body)

	assert.Equal(t, http.StatusOK, resp8.StatusCode)

	var final board.Card
	require.NoError(t, json.NewDecoder(resp8.Body).Decode(&final))
	assert.Equal(t, "done", final.State)
	assert.Empty(t, final.AssignedAgent)
	assert.Nil(t, final.LastHeartbeat)
	// One log entry from AddLogEntry plus state_changed entries for
	// todo -> in_progress and in_progress -> done.
	require.Len(t, final.ActivityLog, 3)
	assert.Equal(t, "Starting implementation", final.ActivityLog[0].Message)
	assert.Equal(t, "state_changed", final.ActivityLog[1].Action)
	assert.Equal(t, "todo -> in_progress", final.ActivityLog[1].Message)
	assert.Equal(t, "state_changed", final.ActivityLog[2].Action)
	assert.Equal(t, "in_progress -> done", final.ActivityLog[2].Message)
	assert.Equal(t, []string{"integration"}, final.Labels)
	assert.Contains(t, final.Body, "## Plan")
}

// TestSSEEventStreamIntegration tests that SSE events are received via the
// HTTP SSE endpoint when card operations occur through the API.
func TestSSEEventStreamIntegration(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Connect to SSE endpoint
	sseResp, err := http.Get(server.URL + "/api/events")

	require.NoError(t, err)
	defer closeBody(t, sseResp.Body)

	assert.Equal(t, http.StatusOK, sseResp.StatusCode)
	assert.Equal(t, "text/event-stream", sseResp.Header.Get("Content-Type"))

	// Read SSE events in a background goroutine. The reader signals on
	// connectedCh as soon as it sees the ":connected" prelude so the test
	// can proceed without a blind time.Sleep.
	var (
		receivedEvents []events.Event
		mu             sync.Mutex
	)

	readDone := make(chan struct{})
	connectedCh := make(chan struct{})

	go func() {
		defer close(readDone)

		var connectedOnce sync.Once

		buf := make([]byte, 4096)
		for {
			n, readErr := sseResp.Body.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				if strings.Contains(chunk, ": connected") {
					connectedOnce.Do(func() { close(connectedCh) })
				}

				for line := range strings.SplitSeq(chunk, "\n") {
					if jsonData, ok := strings.CutPrefix(line, "data: "); ok {
						var ev events.Event
						if err := json.Unmarshal([]byte(jsonData), &ev); err == nil {
							mu.Lock()

							receivedEvents = append(receivedEvents, ev)
							mu.Unlock()
						}
					}
				}
			}

			if readErr != nil {
				return
			}
		}
	}()

	// Wait for the SSE prelude ":connected" to arrive - proves the handler
	// has subscribed and flushed headers. Deterministic replacement for the
	// previous 100 ms sleep.
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("SSE ':connected' prelude never arrived")
	}

	// Create a card via API (triggers CardCreated event)
	createBody, _ := json.Marshal(createCardRequest{
		Title:    "SSE Test Card",
		Type:     "task",
		Priority: "medium",
	})
	createResp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	closeBody(t, createResp.Body)
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	// Claim the card (triggers CardClaimed event)
	claimBody, _ := json.Marshal(agentRequest{})
	claimReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Content-Type", "application/json")
	claimReq.Header.Set("X-Agent-ID", "sse-agent")
	claimResp, err := http.DefaultClient.Do(claimReq)
	require.NoError(t, err)
	closeBody(t, claimResp.Body)
	require.Equal(t, http.StatusOK, claimResp.StatusCode)

	// Wait until both CardCreated and CardClaimed have landed on the reader
	// goroutine, or 5 s elapses. Polls the mutex-guarded receivedEvents.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		var hasCreated, hasClaimed bool

		for _, ev := range receivedEvents {
			if ev.Type == events.CardCreated && ev.CardID == "TEST-001" {
				hasCreated = true
			}

			if ev.Type == events.CardClaimed && ev.CardID == "TEST-001" {
				hasClaimed = true
			}
		}

		return hasCreated && hasClaimed
	}, 5*time.Second, 5*time.Millisecond, "did not receive both CardCreated and CardClaimed events")

	// Close the SSE connection to stop the reader
	_ = sseResp.Body.Close()

	<-readDone

	// Verify we received both events
	mu.Lock()
	defer mu.Unlock()

	require.GreaterOrEqual(t, len(receivedEvents), 2, "should receive at least CardCreated and CardClaimed events")

	var hasCreated, hasClaimed bool

	for _, ev := range receivedEvents {
		if ev.Type == events.CardCreated && ev.CardID == "TEST-001" {
			hasCreated = true
		}

		if ev.Type == events.CardClaimed && ev.CardID == "TEST-001" {
			hasClaimed = true
		}
	}

	assert.True(t, hasCreated, "should have received CardCreated event")
	assert.True(t, hasClaimed, "should have received CardClaimed event")
}

// TestConcurrentAgentClaims tests multiple agents claiming different cards simultaneously.
func TestConcurrentAgentClaims(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	cardCount := 5
	ctx := context.Background()

	// Create N cards
	cardIDs := make([]string, cardCount)
	for i := range cardCount {
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title:    fmt.Sprintf("Concurrent Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		cardIDs[i] = card.ID
	}

	// Spawn goroutines to claim different cards concurrently
	var wg sync.WaitGroup

	results := make([]int, cardCount) // HTTP status codes
	for i := range cardCount {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			agentID := fmt.Sprintf("agent-%d", idx)
			body, _ := json.Marshal(agentRequest{})
			req, _ := http.NewRequest(http.MethodPost,
				server.URL+"/api/projects/test-project/cards/"+cardIDs[idx]+"/claim",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Agent-ID", agentID)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}

			closeBody(t, resp.Body)
			results[idx] = resp.StatusCode
		}(i)
	}

	wg.Wait()

	// All should succeed since each targets a different card
	for i, status := range results {
		assert.Equal(t, http.StatusOK, status, "card %d claim should succeed", i)
	}

	// Verify each card has the correct agent
	for i, id := range cardIDs {
		card, err := svc.GetCard(ctx, "test-project", id)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("agent-%d", i), card.AssignedAgent)
	}
}

// TestConcurrentClaimSameCard tests multiple agents racing to claim the same card.
// Exactly one should succeed, the rest should get 409 Conflict.
func TestConcurrentClaimSameCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a single card
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Race Condition Card",
		Type:     "bug",
		Priority: "high",
	})
	require.NoError(t, err)

	agentCount := 10

	var wg sync.WaitGroup

	statuses := make([]int, agentCount)

	// All agents try to claim the same card
	for i := range agentCount {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			agentID := fmt.Sprintf("racer-%d", idx)
			body, _ := json.Marshal(agentRequest{})
			req, _ := http.NewRequest(http.MethodPost,
				server.URL+"/api/projects/test-project/cards/"+card.ID+"/claim",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Agent-ID", agentID)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}

			closeBody(t, resp.Body)
			statuses[idx] = resp.StatusCode
		}(i)
	}

	wg.Wait()

	// Count successes - exactly one agent should win
	successCount := 0

	for _, status := range statuses {
		if status == http.StatusOK {
			successCount++
		}
	}

	assert.Equal(t, 1, successCount, "exactly one agent should win the claim")

	// Verify the card is claimed by exactly one agent
	card, err = svc.GetCard(context.Background(), "test-project", card.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, card.AssignedAgent, "card should be claimed by one agent")
}

func TestGetProjectUsage(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	ctx := context.Background()

	// Create two cards and report usage via service
	card1, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Card 1", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Card 2", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card1.ID, service.ReportUsageInput{
		AgentID: "a1", PromptTokens: 1000, CompletionTokens: 500,
	})
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, service.ReportUsageInput{
		AgentID: "a2", PromptTokens: 2000, CompletionTokens: 1000,
	})
	require.NoError(t, err)

	// Hit the usage endpoint
	resp, err := http.Get(server.URL + "/api/projects/test-project/usage")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var usage service.ProjectUsage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&usage))
	assert.Equal(t, int64(3000), usage.PromptTokens)
	assert.Equal(t, int64(1500), usage.CompletionTokens)
	assert.Equal(t, 2, usage.CardCount)
}

func TestGetProjectDashboard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	ctx := context.Background()

	// Create cards in different states.
	_, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Todo card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Active card", Type: "task", Priority: "high",
	})
	require.NoError(t, err)

	// Move card2 to in_progress and claim it.
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card2.ID, service.PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card2.ID, "agent-1")
	require.NoError(t, err)

	// Hit dashboard endpoint.
	resp, err := http.Get(server.URL + "/api/projects/test-project/dashboard")

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var dashboard service.DashboardData
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&dashboard))

	assert.Equal(t, 1, dashboard.StateCounts["todo"])
	assert.Equal(t, 1, dashboard.StateCounts["in_progress"])
	require.Len(t, dashboard.ActiveAgents, 1)
	assert.Equal(t, "agent-1", dashboard.ActiveAgents[0].AgentID)

	// Nonexistent project returns 404.
	resp2, err := http.Get(server.URL + "/api/projects/nonexistent/dashboard")

	require.NoError(t, err)
	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

// emptyTestSetup creates a test environment with no pre-existing projects.
func emptyTestSetup(t *testing.T) (*service.CardService, *events.Bus) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return svc, bus
}

func validProjectBody() createProjectRequest {
	return createProjectRequest{
		Name:       "new-project",
		Prefix:     "NEW",
		Repo:       "https://github.com/org/new-project",
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
	}
}

func TestCreateProject_API(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body, _ := json.Marshal(validProjectBody())
	resp, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Equal(t, "new-project", cfg.Name)
	assert.Equal(t, "NEW", cfg.Prefix)
	assert.Equal(t, 1, cfg.NextID)
}

func TestCreateProject_API_Conflict(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body, _ := json.Marshal(validProjectBody())
	resp1, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	closeBody(t, resp1.Body)
	assert.Equal(t, http.StatusCreated, resp1.StatusCode)

	resp2, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))

	require.NoError(t, err)
	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusConflict, resp2.StatusCode)
}

func TestCreateProject_API_BadRequest(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	req := validProjectBody()
	req.Name = ""
	body, _ := json.Marshal(req)
	resp, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUpdateProject_API(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	req := updateProjectRequest{
		Repo:       "https://github.com/org/test",
		States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg board.ProjectConfig
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Contains(t, cfg.States, "review")
	assert.Equal(t, "https://github.com/org/test", cfg.Repo)
}

func TestUpdateProject_API_NotFound(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body, _ := json.Marshal(updateProjectRequest{
		States:     []string{"todo", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low"},
		Transitions: map[string][]string{
			"todo":        {"done"},
			"done":        {"todo"},
			"stalled":     {"todo"},
			"not_planned": {"todo"},
		},
	})

	httpReq, _ := http.NewRequest("PUT", server.URL+"/api/projects/nonexistent", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteProject_API(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a project first
	body, _ := json.Marshal(validProjectBody())
	resp1, err := http.Post(server.URL+"/api/projects", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	closeBody(t, resp1.Body)

	httpReq, _ := http.NewRequest("DELETE", server.URL+"/api/projects/new-project", nil)
	resp, err := http.DefaultClient.Do(httpReq)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify gone
	resp2, err := http.Get(server.URL + "/api/projects/new-project")

	require.NoError(t, err)
	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

func TestDeleteProject_API_HasCards(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	cardBody, _ := json.Marshal(map[string]any{
		"title": "Test", "type": "task", "priority": "medium",
	})
	resp1, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(cardBody))
	require.NoError(t, err)
	closeBody(t, resp1.Body)

	httpReq, _ := http.NewRequest("DELETE", server.URL+"/api/projects/test-project", nil)
	resp, err := http.DefaultClient.Do(httpReq)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestDeleteProject_API_NotFound(t *testing.T) {
	svc, bus := emptyTestSetup(t)
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	httpReq, _ := http.NewRequest("DELETE", server.URL+"/api/projects/nonexistent", nil)
	resp, err := http.DefaultClient.Do(httpReq)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Autonomous mode security tests ---

func TestHumanOnlyFields_PatchCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card first
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	patchBody := `{"autonomous": true}`

	t.Run("agent rejected", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human agent allowed", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("no agent header allowed", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestHumanOnlyFields_CreateCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"title":"Test","type":"task","priority":"medium","autonomous":true}`

	t.Run("agent rejected on create", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "claude-7a3f")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("no agent header allowed on create", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var respCard board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
		assert.True(t, respCard.Autonomous)
		assert.NotEmpty(t, respCard.BranchName)
	})
}

func TestCreateCard_CreatePRDefaultAndBaseBranch(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	post := func(t *testing.T, body, agent string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agent != "" {
			req.Header.Set("X-Agent-ID", agent)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	decode := func(t *testing.T, resp *http.Response) board.Card {
		t.Helper()

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))

		return card
	}

	t.Run("create_pr defaults true when omitted", func(t *testing.T) {
		resp := post(t, `{"title":"Defaults","type":"task","priority":"medium"}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.True(t, decode(t, resp).CreatePR)
	})

	t.Run("explicit create_pr false respected", func(t *testing.T) {
		resp := post(t, `{"title":"No PR","type":"task","priority":"medium","create_pr":false}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.False(t, decode(t, resp).CreatePR)
	})

	t.Run("base_branch accepted on create", func(t *testing.T) {
		resp := post(t, `{"title":"Based","type":"task","priority":"medium","base_branch":"develop"}`, "")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "develop", decode(t, resp).BaseBranch)
	})

	t.Run("agent explicit create_pr rejected", func(t *testing.T) {
		resp := post(t, `{"title":"Agent PR","type":"task","priority":"medium","create_pr":false}`, "claude-7a3f")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("agent base_branch rejected", func(t *testing.T) {
		resp := post(t, `{"title":"Agent base","type":"task","priority":"medium","base_branch":"develop"}`, "claude-7a3f")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestHumanOnlyFields_PutClear(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card with autonomous mode enabled (via service, simulating human)
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Auto card", Type: "task", Priority: "medium",
		Autonomous: true,
	})
	require.NoError(t, err)
	assert.True(t, card.Autonomous)

	// Agent tries to PUT with autonomous=false (clearing it) - must be rejected
	putBody := fmt.Sprintf(`{"title":"%s","type":"task","state":"todo","priority":"medium","autonomous":false}`, card.Title)
	req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
		strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)

	// Verify the card was NOT modified
	reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, reloaded.Autonomous, "autonomous should still be true")
}

func TestHumanOnlyFields_PutPassthrough(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card with autonomous mode enabled
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Auto card", Type: "task", Priority: "medium",
		Autonomous: true,
	})
	require.NoError(t, err)

	// Agent sends PUT with same autonomous+vetted values - should pass through.
	// create_pr must echo the card's value (defaulted true at create) or the
	// human-only guard rejects the request.
	putBody := `{"title":"Updated title","type":"task","state":"todo","priority":"medium","autonomous":true,"create_pr":true,"vetted":true}`
	req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
		strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHumanOnlyFields_PutSet(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card with autonomous=false (default)
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Plain card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	assert.False(t, card.Autonomous)

	// Agent tries to SET autonomous to true via PUT - must be rejected
	putBody := fmt.Sprintf(`{"title":"%s","type":"task","state":"todo","priority":"medium","autonomous":true}`, card.Title)
	req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
		strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)

	// Verify the card was NOT modified
	reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
	require.NoError(t, err)
	assert.False(t, reloaded.Autonomous, "autonomous should still be false")
}

// testSetupWithRemoteExecution creates a test environment with a project that has
// remote_execution configured. The boardConfig parameter overrides the default board config.
func testSetupWithRemoteExecution(t *testing.T, boardConfigYAML string) (*service.CardService, *events.Bus, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfigYAML), 0o644))

	git, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return svc, bus, func() {}
}

// TestProjectGETReturnsStoredRemoteExecution pins that GET /api/projects and
// GET /api/projects/{project} return remote_execution exactly as stored in
// .board.yaml - no fabricated fields. Runnability is instance-global (a
// configured task backend), surfaced to clients via GET /api/app/config
// task_backend, never via project config. Decodes into a raw map so the
// assertion is about the wire shape, not the Go struct.
func TestProjectGETReturnsStoredRemoteExecution(t *testing.T) {
	boardConfigWithImage := `name: test-project
prefix: TEST
next_id: 1
states: [todo, in_progress, done, stalled, not_planned]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
  not_planned: [todo]
remote_execution:
  worker_image: my-worker:latest
`

	assertStored := func(t *testing.T, raw map[string]any) {
		t.Helper()

		re, ok := raw["remote_execution"].(map[string]any)
		require.True(t, ok, "stored remote_execution must be present")
		assert.Equal(t, "my-worker:latest", re["worker_image"])

		_, hasEnabled := re["enabled"]
		assert.False(t, hasEnabled, "GET must not fabricate an enabled field")
	}

	t.Run("single project", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithImage)
		defer cleanup()

		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects/test-project")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var raw map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
		assertStored(t, raw)
	})

	t.Run("project list", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithImage)
		defer cleanup()

		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var raws []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raws))
		require.Len(t, raws, 1)
		assertStored(t, raws[0])
	})
}

func TestHumanOnlyFields_Vetted_PatchCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Vetted patch test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	patchBody := `{"vetted": true}`

	t.Run("agent PATCH with vetted=true returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human PATCH with vetted=true returns 200", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.True(t, updated.Vetted)
	})
}

func TestHumanOnlyFields_Vetted_CreateCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"title":"Vetted import","type":"task","priority":"medium","vetted":true}`

	t.Run("agent create with vetted=true returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-importer")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})
}

func TestHumanOnlyFields_Vetted_PutCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card with vetted=false (default for card without source)
	// But we need a card that is NOT vetted, so create it with an explicit false.
	// Cards created without a source are auto-vetted, so let's use the service to
	// create a card and then manually set vetted=false by patching via service.
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Vetted PUT test", Type: "task", Priority: "medium",
		Source: &board.Source{System: "jira", ExternalID: "JIRA-42", ExternalURL: "https://example.com/JIRA-42"},
	})
	require.NoError(t, err)
	assert.False(t, card.Vetted)

	t.Run("agent PUT changing vetted returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		// Card has vetted=false; agent PUTs with vetted=true - must be rejected
		putBody := fmt.Sprintf(`{"title":"%s","type":"task","state":"todo","priority":"medium","vetted":true}`, card.Title)
		req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(putBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)

		// Verify the card was NOT modified
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.False(t, reloaded.Vetted, "vetted should still be false")
	})
}

func TestHumanOnlyFields_ModelPins_PatchCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Model pin patch test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	patchBody := `{"model_orchestrator": "anthropic/claude-opus-4"}`

	t.Run("agent PATCH with model_orchestrator returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human PATCH with model_orchestrator returns 200", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, "anthropic/claude-opus-4", updated.ModelOrchestrator)
	})
}

func TestHumanOnlyFields_ModelPins_PutCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Created without pins - all three model pin fields default to "".
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Model pin PUT test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	assert.Empty(t, card.ModelOrchestrator)

	t.Run("agent PUT changing model_orchestrator returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		// Card has model_orchestrator=""; agent PUTs a slug - must be rejected.
		// vetted:true echoes the card's auto-vetted state (no source) so the
		// pin is the only field the guard sees changing.
		putBody := fmt.Sprintf(
			`{"title":"%s","type":"task","state":"todo","priority":"medium","vetted":true,"model_orchestrator":"anthropic/claude-opus-4"}`,
			card.Title)
		req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(putBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)

		// Verify the card was NOT modified
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Empty(t, reloaded.ModelOrchestrator, "model_orchestrator should still be empty")
	})

	t.Run("agent PUT with pin values equal to disk passes through", func(t *testing.T) {
		// Pin the card as a human first.
		patchBody := `{"model_orchestrator": "anthropic/claude-opus-4"}`
		patchReq, _ := http.NewRequest("PATCH", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(patchBody))
		patchReq.Header.Set("Content-Type", "application/json")
		patchReq.Header.Set("X-Agent-ID", "human:alice")

		patchResp, err := http.DefaultClient.Do(patchReq)
		require.NoError(t, err)
		closeBody(t, patchResp.Body)
		require.Equal(t, http.StatusOK, patchResp.StatusCode)

		// Agent PUT echoing the same pin value - should pass through.
		// vetted:true echoes the card's auto-vetted state (no source);
		// create_pr:true echoes the value defaulted at create.
		putBody := `{"title":"Updated title","type":"task","state":"todo","priority":"medium","vetted":true,"create_pr":true,"model_orchestrator":"anthropic/claude-opus-4"}`
		req, _ := http.NewRequest("PUT", server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(putBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, "anthropic/claude-opus-4", updated.ModelOrchestrator)
		assert.Equal(t, "Updated title", updated.Title)
	})
}

// TestPatchCardBestOfN covers the five PATCH cases from the best_of_n brief:
// valid set, reject 1 (below the 2..max range), reject above max_candidates,
// 0 clears the field, and the human-only gate rejects a non-human agent.
func TestPatchCardBestOfN(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, BestOfN: config.BestOfNConfig{MaxCandidates: 5}})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Best of N patch test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	patchAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	t.Run("PATCH best_of_n=3 as human sets the field", func(t *testing.T) {
		resp := patchAs(t, `{"best_of_n": 3}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, 3, updated.BestOfN)
	})

	t.Run("PATCH best_of_n=1 returns 400", func(t *testing.T) {
		resp := patchAs(t, `{"best_of_n": 1}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)

		// Verify the card was NOT modified - still 3 from the prior subtest.
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, 3, reloaded.BestOfN)
	})

	t.Run("PATCH best_of_n=9 over max_candidates=5 returns 400", func(t *testing.T) {
		resp := patchAs(t, `{"best_of_n": 9}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)

		// Verify the card was NOT modified - still 3 from the first subtest.
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, 3, reloaded.BestOfN)
	})

	t.Run("PATCH best_of_n=0 clears the field", func(t *testing.T) {
		resp := patchAs(t, `{"best_of_n": 0}`, "")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Zero(t, updated.BestOfN)
	})

	t.Run("PATCH best_of_n=3 as non-human agent returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		resp := patchAs(t, `{"best_of_n": 3}`, "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "best_of_n")

		// Verify the card was NOT modified - still cleared from the prior subtest.
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Zero(t, reloaded.BestOfN)
	})

	t.Run("PATCH best_of_n=9 as non-human agent returns 403 not 400", func(t *testing.T) {
		// 9 is out of range (max_candidates=5) AND the caller is non-human -
		// authorization must be checked before value validation, so this is
		// 403 HUMAN_ONLY_FIELD, not 400 invalid best_of_n.
		resp := patchAs(t, `{"best_of_n": 9}`, "agent:x")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "best_of_n")

		// Verify the card was NOT modified - still cleared from the prior subtest.
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Zero(t, reloaded.BestOfN)
	})
}

// TestUpdateCardBestOfN covers the PUT path: setting via a human caller,
// range validation, the zero-value-clears semantics (mirrors autonomous),
// and the human-only compare-to-existing gate for agents.
func TestUpdateCardBestOfN(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, BestOfN: config.BestOfNConfig{MaxCandidates: 5}})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Best of N PUT test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	assert.Zero(t, card.BestOfN)

	// vetted:true echoes the card's auto-vetted state (no source) so best_of_n
	// is the only field under test each time.
	putBody := func(bestOfN int) string {
		return fmt.Sprintf(
			`{"title":"%s","type":"task","state":"todo","priority":"medium","vetted":true,"best_of_n":%d}`,
			card.Title, bestOfN)
	}

	putAs := func(t *testing.T, body, agentID string) *http.Response {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/projects/test-project/cards/"+card.ID,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", agentID)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		return resp
	}

	t.Run("human PUT sets best_of_n", func(t *testing.T) {
		resp := putAs(t, putBody(3), "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Equal(t, 3, updated.BestOfN)
	})

	t.Run("human PUT with best_of_n=1 returns 400", func(t *testing.T) {
		resp := putAs(t, putBody(1), "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)

		// Verify the card was NOT modified - still 3 from the prior subtest.
		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, 3, reloaded.BestOfN)
	})

	t.Run("human PUT with best_of_n absent clears the field like autonomous:false", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"title":"%s","type":"task","state":"todo","priority":"medium","vetted":true}`,
			card.Title)

		resp := putAs(t, body, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Zero(t, updated.BestOfN, "best_of_n absent on PUT clears the field, matching autonomous:false semantics")
	})

	t.Run("agent PUT changing best_of_n returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		// Card currently has best_of_n=0 (cleared above); agent PUTs a nonzero value.
		resp := putAs(t, putBody(4), "agent-1")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "best_of_n")

		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Zero(t, reloaded.BestOfN, "best_of_n should still be cleared from the prior subtest")
	})

	t.Run("agent PUT echoing existing best_of_n passes through", func(t *testing.T) {
		// Card is at best_of_n=0; agent PUT echoes 0 - no change, so the
		// compare-to-existing gate must allow it through.
		resp := putAs(t, putBody(0), "agent-1")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		assert.Zero(t, updated.BestOfN)
	})

	t.Run("agent PUT with best_of_n=9 over max returns 403 not 400", func(t *testing.T) {
		// Card is at best_of_n=0; 9 is both out of range (max_candidates=5)
		// and differs from the existing value, and the caller is non-human -
		// authorization must be checked before value validation, so this is
		// 403 HUMAN_ONLY_FIELD, not 400 invalid best_of_n.
		resp := putAs(t, putBody(9), "agent-1")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
		assert.Contains(t, apiErr.Details, "best_of_n")

		reloaded, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Zero(t, reloaded.BestOfN, "best_of_n should still be cleared from the prior subtest")
	})
}

func TestHumanOnlyFields_ModelPins_CreateCard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"title":"Pinned","type":"task","priority":"medium",` +
		`"model_orchestrator":"anthropic/claude-opus-4",` +
		`"model_coder":"anthropic/claude-sonnet-4-5",` +
		`"model_reviewer":"openai/gpt-4o"}`

	t.Run("agent create with pins returns 403 HUMAN_ONLY_FIELD", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human create with pins persists them on the card", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/projects/test-project/cards",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var created board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
		assert.Equal(t, "anthropic/claude-opus-4", created.ModelOrchestrator)
		assert.Equal(t, "anthropic/claude-sonnet-4-5", created.ModelCoder)
		assert.Equal(t, "openai/gpt-4o", created.ModelReviewer)
	})
}

func TestClaimCard_VettedGuard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create an imported card (with source) - not vetted by default.
	unvetted, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Imported task",
		Type:     "task",
		Priority: "medium",
		Source:   &board.Source{System: "jira", ExternalID: "JIRA-99", ExternalURL: "https://example.com/JIRA-99"},
	})
	require.NoError(t, err)
	assert.False(t, unvetted.Vetted)

	t.Run("agent claim unvetted card returns 403 CARD_NOT_VETTED", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost,
			server.URL+"/api/projects/test-project/cards/"+unvetted.ID+"/claim",
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeCardNotVetted, apiErr.Code)
	})

	t.Run("human claim unvetted card returns 200", func(t *testing.T) {
		body := agentRequest{}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost,
			server.URL+"/api/projects/test-project/cards/"+unvetted.ID+"/claim",
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestListCards_Pagination(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create 5 cards; limit=2 should yield 3 pages (2 + 2 + 1).
	for i := range 5 {
		_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
			Title:    fmt.Sprintf("Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	t.Run("first page includes total, last page omits next_cursor", func(t *testing.T) {
		seen := map[string]bool{}

		var (
			cursor   string
			pageNum  int
			gotTotal bool
		)

		for {
			pageNum++

			url := server.URL + "/api/projects/test-project/cards?limit=2"
			if cursor != "" {
				url += "&cursor=" + cursor
			}

			resp, err := http.Get(url)
			require.NoError(t, err)

			var page listCardsResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
			closeBody(t, resp.Body)

			if pageNum == 1 {
				require.NotNil(t, page.Total, "first page must include total")
				assert.Equal(t, 5, *page.Total)

				gotTotal = true
			} else {
				assert.Nil(t, page.Total, "subsequent pages must omit total")
			}

			for _, c := range page.Items {
				assert.False(t, seen[c.ID], "duplicate card across pages: %s", c.ID)
				seen[c.ID] = true
			}

			if page.NextCursor == "" {
				break
			}

			cursor = page.NextCursor

			require.LessOrEqual(t, pageNum, 10, "too many pages; pagination likely looped")
		}

		assert.True(t, gotTotal)
		assert.Len(t, seen, 5, "walking cursors should yield every card exactly once")
	})

	t.Run("limit=1 returns one item plus cursor", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?limit=1")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 1)
		assert.NotEmpty(t, page.NextCursor, "not on last page, cursor required")
	})

	t.Run("invalid cursor returns 400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?cursor=!!!not-base64url")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
	})

	t.Run("negative limit returns 400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?limit=-1")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("zero limit returns 400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?limit=0")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("limit above max returns 400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?limit=2001")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("non-numeric limit returns 400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?limit=abc")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("default limit respected when not specified", func(t *testing.T) {
		// With 5 cards and default limit of 500, one page should fit everything.
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 5)
		assert.Empty(t, page.NextCursor)
	})
}

func TestListCards_VettedFilter(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card without source - auto-vetted.
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Auto-vetted task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Create an imported card - not vetted.
	_, err = svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Imported task",
		Type:     "task",
		Priority: "medium",
		Source:   &board.Source{System: "jira", ExternalID: "JIRA-1", ExternalURL: "https://example.com/1"},
	})
	require.NoError(t, err)

	t.Run("?vetted=true returns only vetted cards", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?vetted=true")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 1)
		assert.True(t, page.Items[0].Vetted)
	})

	t.Run("?vetted=false returns only unvetted cards", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?vetted=false")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 1)
		assert.False(t, page.Items[0].Vetted)
	})

	t.Run("no vetted param returns all cards", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var page listCardsResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Len(t, page.Items, 2)
	})
}

// TestMCPPanicRecovery verifies that a panicking MCP handler routed through
// the shared router middleware chain returns HTTP 500 and does not crash.
func TestMCPPanicRecovery(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	svc, _, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, MCPHandler: panicHandler})

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{}`))

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInternalError, apiErr.Code)
}

// TestMCPBodyLimit verifies that an oversized POST to /mcp is rejected with
// HTTP 413 via the shared body-limit middleware.
func TestMCPBodyLimit(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	svc, _, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, MCPHandler: okHandler})

	server := httptest.NewServer(router)
	defer server.Close()

	const twentyMB = 20 * 1024 * 1024

	bigBody := bytes.Repeat([]byte("x"), twentyMB)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", bytes.NewReader(bigBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

// TestRequestIDLogging verifies that an API request produces a log line
// containing the request_id from the X-Request-ID header (or auto-generated)
// so that log output is correlated with the HTTP request.
func TestRequestIDLogging(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	// Capture log output via a custom slog handler.
	var buf bytes.Buffer

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origDefault := slog.Default()

	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(origDefault) })

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	const wantID = "test-correlation-id-xyz"

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/projects", nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-ID", wantID)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, wantID, resp.Header.Get("X-Request-ID"),
		"response should echo the X-Request-ID header")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "request_id="+wantID,
		"log output should contain the request_id from the header")
}

func TestCardAPI_SkillsRoundTrip(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("create with skills", func(t *testing.T) {
		body := createCardRequest{
			Title:    "skills-test",
			Type:     "task",
			Priority: "low",
			Skills:   &[]string{"go-development", "documentation"},
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(
			server.URL+"/api/projects/test-project/cards",
			"application/json",
			bytes.NewReader(jsonBody),
		)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))

		require.NotNil(t, card.Skills)
		assert.Equal(t, []string{"go-development", "documentation"}, *card.Skills)
	})

	t.Run("create without skills omits field in JSON output", func(t *testing.T) {
		body := createCardRequest{
			Title:    "no-skills-test",
			Type:     "task",
			Priority: "low",
		}
		jsonBody, _ := json.Marshal(body)

		resp, err := http.Post(
			server.URL+"/api/projects/test-project/cards",
			"application/json",
			bytes.NewReader(jsonBody),
		)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Decode into map to check raw JSON keys
		var respMap map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&respMap))

		// When skills is nil, the field should be omitted from JSON
		_, hasSkills := respMap["skills"]
		assert.False(t, hasSkills, "skills key should be absent when not set")
	})

	t.Run("patch with explicit empty clears", func(t *testing.T) {
		// Create card with skills
		createBody := createCardRequest{
			Title:    "patch-test",
			Type:     "task",
			Priority: "low",
			Skills:   &[]string{"go-development"},
		}
		createJSON, _ := json.Marshal(createBody)

		resp, err := http.Post(
			server.URL+"/api/projects/test-project/cards",
			"application/json",
			bytes.NewReader(createJSON),
		)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		cardID := card.ID

		// Patch with explicit empty skills
		patchBody := patchCardRequest{
			Skills: &[]string{},
		}
		patchJSON, _ := json.Marshal(patchBody)

		patchResp, err := http.NewRequest(
			http.MethodPatch,
			server.URL+"/api/projects/test-project/cards/"+cardID,
			bytes.NewReader(patchJSON),
		)

		require.NoError(t, err)
		patchResp.Header.Set("Content-Type", "application/json")

		resp, err = http.DefaultClient.Do(patchResp)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var patchedCard board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&patchedCard))

		// After patch with explicit empty, skills should be empty slice
		require.NotNil(t, patchedCard.Skills)
		assert.Empty(t, *patchedCard.Skills)
	})

	// The subtests below require a TaskSkillsDir. Stand up a second server
	// wired with a skills directory so that skill validation is active.
	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "go-development", "Use when Go.")
	writeSkillFile(t, skillsDir, "documentation", "Use when docs.")
	writeSkillFile(t, skillsDir, "code-review", "Use when reviewing.")

	svc2, bus2, cleanup2 := testSetup(t)
	defer cleanup2()

	router2 := NewRouter(RouterConfig{Service: svc2, Bus: bus2, TaskSkillsDir: skillsDir})

	server2 := httptest.NewServer(router2)
	defer server2.Close()

	// putProjectDefaults sets default_skills on the test-project via PUT.
	putProjectDefaults := func(t *testing.T, defaults *[]string) {
		t.Helper()

		req := updateProjectRequest{
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
			DefaultSkills: defaults,
		}

		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequest(http.MethodPut, server2.URL+"/api/projects/test-project", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		closeBody(t, resp.Body)
	}

	// createCard2 creates a fresh card on server2 and returns its ID.
	createCard2 := func(t *testing.T) string {
		t.Helper()

		body, _ := json.Marshal(createCardRequest{Title: "put-skills-test", Type: "task", Priority: "medium"})
		resp, err := http.Post(server2.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		closeBody(t, resp.Body)

		return card.ID
	}

	// putCardSkills performs a full PUT on a card with the given skills.
	// State is kept as "todo" so the state-machine validation passes.
	putCardSkills := func(t *testing.T, cardID string, skills *[]string) *http.Response {
		t.Helper()

		req := updateCardRequest{
			Title:    "put-skills-test",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
			Skills:   skills,
		}
		body, _ := json.Marshal(req)

		httpReq, _ := http.NewRequest(http.MethodPut, server2.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		require.NoError(t, err)

		return resp
	}

	// Group: project has a non-nil default_skills subset.
	// Set the default once here; all three subtests below share this state.
	withDefault := []string{"go-development", "documentation"}
	putProjectDefaults(t, &withDefault)

	t.Run("update (PUT) with subset of project default → 200", func(t *testing.T) {
		cardID := createCard2(t)

		skills := []string{"go-development"}

		resp := putCardSkills(t, cardID, &skills)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		require.NotNil(t, updated.Skills)
		assert.Equal(t, []string{"go-development"}, *updated.Skills)
	})

	t.Run("update (PUT) with skill outside project default → 400 with offending names", func(t *testing.T) {
		cardID := createCard2(t)

		// code-review exists in the skills dir but is not in the project default.
		skills := []string{"go-development", "code-review"}

		resp := putCardSkills(t, cardID, &skills)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Error, "code-review")
	})

	t.Run("patch with skill outside project default → 400", func(t *testing.T) {
		cardID := createCard2(t)

		// code-review is in the skills dir but not in the project default.
		patchBody := patchCardRequest{Skills: &[]string{"code-review"}}
		body, _ := json.Marshal(patchBody)

		httpReq, _ := http.NewRequest(http.MethodPatch, server2.URL+"/api/projects/test-project/cards/"+cardID, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Error, "code-review")
	})

	// Group: project has nil default_skills; only global availability is checked.
	// Change the default once here so git always has a real diff to commit.
	putProjectDefaults(t, nil)

	t.Run("update (PUT) with unknown skill name → 400 with offending names", func(t *testing.T) {
		cardID := createCard2(t)

		skills := []string{"totally-made-up-skill"}

		resp := putCardSkills(t, cardID, &skills)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		assert.Contains(t, apiErr.Error, "totally-made-up-skill")
	})

	t.Run("update (PUT) with empty skills list → 200, persisted as explicit empty", func(t *testing.T) {
		cardID := createCard2(t)

		emptySkills := []string{}

		resp := putCardSkills(t, cardID, &emptySkills)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		require.NotNil(t, updated.Skills)
		assert.Empty(t, *updated.Skills)
	})

	t.Run("update (PUT) with skills when project default is nil → 200", func(t *testing.T) {
		cardID := createCard2(t)

		skills := []string{"go-development", "documentation"}

		resp := putCardSkills(t, cardID, &skills)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var updated board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		require.NotNil(t, updated.Skills)
		assert.Equal(t, []string{"go-development", "documentation"}, *updated.Skills)
	})
}

func gitopsTestProvider(t testing.TB) githubauth.TokenGenerator {
	t.Helper()

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	return p
}
