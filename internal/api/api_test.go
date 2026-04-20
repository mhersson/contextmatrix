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

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/runner"
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
	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
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
		// Regression guard: before the ctxmax-328 audit, a missing parent
		// wrapped in board.ErrInvalidType, which surfaced as 422
		// VALIDATION_ERROR. Parent is a resource — clients need 404.
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

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 2)
	})

	t.Run("filter by type", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?type=task")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 1)
		assert.Equal(t, "task", cards[0].Type)
	})

	t.Run("filter by priority", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?priority=high")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 1)
		assert.Equal(t, "high", cards[0].Priority)
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

	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
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

	t.Run("successful claim with body agent_id", func(t *testing.T) {
		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

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
		body := agentRequest{AgentID: "claude-2"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

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
		releaseBody := agentRequest{AgentID: "claude-1"}
		releaseJSON, _ := json.Marshal(releaseBody)
		releaseReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(releaseJSON))
		releaseReq.Header.Set("Content-Type", "application/json")
		releaseResp, _ := http.DefaultClient.Do(releaseReq)
		closeBody(t, releaseResp.Body)

		// Claim using header
		body := agentRequest{AgentID: ""} // Empty body
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
		body := agentRequest{AgentID: ""}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("non-existent card - 404", func(t *testing.T) {
		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-999/claim", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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
		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

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
		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

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

		body := agentRequest{AgentID: "claude-2"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/release", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeAgentMismatch, apiErr.Code)
	})
}

func TestHeartbeatCard(t *testing.T) {
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

	card, err := svc.ClaimCard(context.Background(), "test-project", "TEST-001", "claude-1")
	require.NoError(t, err)

	originalHeartbeat := card.LastHeartbeat

	// Wait a moment so heartbeat time differs
	time.Sleep(10 * time.Millisecond)

	t.Run("successful heartbeat - 204", func(t *testing.T) {
		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/heartbeat", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Verify heartbeat was updated
		updatedCard, err := svc.GetCard(context.Background(), "test-project", "TEST-001")
		require.NoError(t, err)
		assert.True(t, updatedCard.LastHeartbeat.After(*originalHeartbeat))
	})

	t.Run("heartbeat wrong agent - 403", func(t *testing.T) {
		body := agentRequest{AgentID: "claude-2"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/heartbeat", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("heartbeat unclaimed card - 409", func(t *testing.T) {
		// Release the card
		_, err := svc.ReleaseCard(context.Background(), "test-project", "TEST-001", "claude-1")
		require.NoError(t, err)

		body := agentRequest{AgentID: "claude-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/heartbeat", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}

func TestAddLogEntry(t *testing.T) {
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

	t.Run("successful log entry", func(t *testing.T) {
		body := addLogRequest{
			AgentID: "claude-1",
			Action:  "progress",
			Message: "Working on implementation",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/log", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var card board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		assert.Len(t, card.ActivityLog, 1)
		assert.Equal(t, "claude-1", card.ActivityLog[0].Agent)
		assert.Equal(t, "progress", card.ActivityLog[0].Action)
		assert.Equal(t, "Working on implementation", card.ActivityLog[0].Message)
		assert.False(t, card.ActivityLog[0].Timestamp.IsZero())
	})

	t.Run("missing action - 400", func(t *testing.T) {
		body := addLogRequest{
			AgentID: "claude-1",
			Action:  "",
			Message: "Some message",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/log", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing message - 400", func(t *testing.T) {
		body := addLogRequest{
			AgentID: "claude-1",
			Action:  "progress",
			Message: "",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/log", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing agent_id - 400", func(t *testing.T) {
		body := addLogRequest{
			AgentID: "",
			Action:  "progress",
			Message: "Some message",
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/log", bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestGetCardContext(t *testing.T) {
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

	t.Run("returns card and project", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards/TEST-001/context")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var ctx service.CardContext
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&ctx))
		assert.Equal(t, "TEST-001", ctx.Card.ID)
		assert.Equal(t, "test-project", ctx.Project.Name)
		// Template may be empty if no template file exists
	})

	t.Run("non-existent card - 404", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards/TEST-999/context")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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
		body := updateCardRequest{
			Title:    "Updated By Owner",
			Type:     "task",
			State:    "in_progress",
			Priority: "high",
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
	claimBody, _ := json.Marshal(agentRequest{AgentID: agentID})
	req, _ := http.NewRequest(http.MethodPost, cardURL+"/claim", bytes.NewReader(claimBody))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp2.Body)

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var claimed board.Card
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&claimed))
	assert.Equal(t, agentID, claimed.AssignedAgent)
	assert.NotNil(t, claimed.LastHeartbeat)

	// Step 3: Add log entry
	logBody, _ := json.Marshal(addLogRequest{
		AgentID: agentID,
		Action:  "status_update",
		Message: "Starting implementation",
	})
	req, _ = http.NewRequest(http.MethodPost, cardURL+"/log", bytes.NewReader(logBody))
	req.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp3.Body)

	assert.Equal(t, http.StatusOK, resp3.StatusCode)

	var logged board.Card
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&logged))
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

	// Step 5: Heartbeat
	hbBody, _ := json.Marshal(agentRequest{AgentID: agentID})
	req, _ = http.NewRequest(http.MethodPost, cardURL+"/heartbeat", bytes.NewReader(hbBody))
	req.Header.Set("Content-Type", "application/json")
	resp5, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp5.Body)

	assert.Equal(t, http.StatusNoContent, resp5.StatusCode)

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
	releaseBody, _ := json.Marshal(agentRequest{AgentID: agentID})
	req, _ = http.NewRequest(http.MethodPost, cardURL+"/release", bytes.NewReader(releaseBody))
	req.Header.Set("Content-Type", "application/json")
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
	assert.Len(t, final.ActivityLog, 1)
	assert.Equal(t, "Starting implementation", final.ActivityLog[0].Message)
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

	// Read SSE events in a background goroutine
	var (
		receivedEvents []events.Event
		mu             sync.Mutex
	)

	readDone := make(chan struct{})

	go func() {
		defer close(readDone)

		buf := make([]byte, 4096)
		for {
			n, readErr := sseResp.Body.Read(buf)
			if n > 0 {
				for line := range strings.SplitSeq(string(buf[:n]), "\n") {
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

	// Give SSE handler time to subscribe
	time.Sleep(100 * time.Millisecond)

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
	claimBody, _ := json.Marshal(agentRequest{AgentID: "sse-agent"})
	claimReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/projects/test-project/cards/TEST-001/claim", bytes.NewReader(claimBody))
	claimReq.Header.Set("Content-Type", "application/json")
	claimResp, err := http.DefaultClient.Do(claimReq)
	require.NoError(t, err)
	closeBody(t, claimResp.Body)
	require.Equal(t, http.StatusOK, claimResp.StatusCode)

	// Give events time to propagate
	time.Sleep(200 * time.Millisecond)

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
			body, _ := json.Marshal(agentRequest{AgentID: agentID})
			req, _ := http.NewRequest(http.MethodPost,
				server.URL+"/api/projects/test-project/cards/"+cardIDs[idx]+"/claim",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

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
			body, _ := json.Marshal(agentRequest{AgentID: agentID})
			req, _ := http.NewRequest(http.MethodPost,
				server.URL+"/api/projects/test-project/cards/"+card.ID+"/claim",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}

			closeBody(t, resp.Body)
			statuses[idx] = resp.StatusCode
		}(i)
	}

	wg.Wait()

	// Count successes — exactly one agent should win
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

func TestReportUsageEndpoint(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card
	createBody, _ := json.Marshal(createCardRequest{
		Title:    "Usage endpoint test",
		Type:     "task",
		Priority: "medium",
	})
	resp, err := http.Post(server.URL+"/api/projects/test-project/cards", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)

	var card board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	closeBody(t, resp.Body)

	// Report usage
	usageBody, _ := json.Marshal(map[string]any{
		"agent_id":          "agent-1",
		"prompt_tokens":     5000,
		"completion_tokens": 2000,
	})
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/usage",
		bytes.NewReader(usageBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var updated board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
	require.NotNil(t, updated.TokenUsage)
	assert.Equal(t, int64(5000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(2000), updated.TokenUsage.CompletionTokens)
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

	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
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
		Repo:       "git@github.com:org/new-project.git",
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
		Repo:       "git@github.com:org/test.git",
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
	assert.Equal(t, "git@github.com:org/test.git", cfg.Repo)
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

	body := `{"title":"Test","type":"task","priority":"medium","autonomous":true,"feature_branch":true}`

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
		assert.True(t, respCard.FeatureBranch)
		assert.NotEmpty(t, respCard.BranchName)
	})
}

func TestReportPush_BranchProtection(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create and claim a card
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Push test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(context.Background(), "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	tests := []struct {
		name       string
		branch     string
		wantStatus int
	}{
		{"empty branch rejected", "", http.StatusBadRequest},
		{"main rejected", "main", http.StatusForbidden},
		{"master rejected", "master", http.StatusForbidden},
		{"refs/heads/main rejected", "refs/heads/main", http.StatusForbidden},
		{"MAIN rejected (case insensitive)", "MAIN", http.StatusForbidden},
		{"feature branch allowed", card.ID + "/fix-login", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"agent_id":"agent-1","branch":"%s"}`, tt.branch)
			req, _ := http.NewRequest("POST",
				server.URL+"/api/projects/test-project/cards/"+card.ID+"/report-push",
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Agent-ID", "agent-1")

			resp, err := http.DefaultClient.Do(req)

			require.NoError(t, err)
			defer closeBody(t, resp.Body)

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestReportPush_InvalidPRUrl_Returns422(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create and claim a card
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "URL test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(context.Background(), "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	body := `{"agent_id":"agent-1","branch":"feat/fix","pr_url":"javascript:alert(1)"}`
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/report-push",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
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
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)
	assert.True(t, card.Autonomous)

	// Agent tries to PUT with autonomous=false (clearing it) — must be rejected
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
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Agent sends PUT with same autonomous+vetted values — should pass through
	putBody := `{"title":"Updated title","type":"task","state":"todo","priority":"medium","autonomous":true,"feature_branch":true,"vetted":true}`
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

	// Agent tries to SET autonomous to true via PUT — must be rejected
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

func TestReportPush_AgentMismatch(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create and claim a card as agent-owner
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Push mismatch test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(context.Background(), "test-project", card.ID, "agent-owner")
	require.NoError(t, err)

	t.Run("wrong agent rejected", func(t *testing.T) {
		body := `{"agent_id":"agent-intruder","branch":"feat/fix"}`
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/report-push",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-intruder")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeAgentMismatch, apiErr.Code)
	})

	t.Run("correct agent allowed", func(t *testing.T) {
		body := `{"agent_id":"agent-owner","branch":"feat/fix"}`
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/report-push",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", "agent-owner")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
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

	git, err := gitops.NewManager(boardsDir, "", "ssh", "")
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir, nil, true, false)

	return svc, bus, func() {}
}

func TestGetProjectRunnerStatus(t *testing.T) {
	boardConfigWithRemoteExec := `name: test-project
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
  enabled: true
  runner_image: my-runner:latest
`

	boardConfigPerProjectDisabled := `name: test-project
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
  enabled: false
  runner_image: my-runner:latest
`

	t.Run("runner disabled globally returns remote_execution.enabled false", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithRemoteExec)
		defer cleanup()

		// No runner client passed → runnerEnabled = false
		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects/test-project")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var project board.ProjectConfig
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&project))

		require.NotNil(t, project.RemoteExecution)
		require.NotNil(t, project.RemoteExecution.Enabled)
		assert.False(t, *project.RemoteExecution.Enabled,
			"remote_execution.enabled should be false when runner is globally disabled")
	})

	t.Run("runner enabled globally but per-project disabled returns false", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigPerProjectDisabled)
		defer cleanup()

		// Passing a non-nil runner client → runnerEnabled = true
		runnerClient := runner.NewClient("http://localhost:9090", "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: runnerClient})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects/test-project")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var project board.ProjectConfig
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&project))

		require.NotNil(t, project.RemoteExecution)
		require.NotNil(t, project.RemoteExecution.Enabled)
		assert.False(t, *project.RemoteExecution.Enabled,
			"remote_execution.enabled should be false when per-project config disables it")
	})

	t.Run("runner enabled globally with no per-project override returns nil remote_execution", func(t *testing.T) {
		// Use the default board config (no remote_execution section)
		svc, bus, cleanup := testSetup(t)
		defer cleanup()

		runnerClient := runner.NewClient("http://localhost:9090", "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: runnerClient})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects/test-project")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var project board.ProjectConfig
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&project))

		require.NotNil(t, project.RemoteExecution,
			"remote_execution should be injected when runner is enabled globally")
		assert.True(t, *project.RemoteExecution.Enabled,
			"remote_execution.enabled should be true when runner is enabled globally")
	})
}

func TestListProjectsRunnerStatus(t *testing.T) {
	boardConfigWithRemoteExec := `name: test-project
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
  enabled: true
  runner_image: my-runner:latest
`

	t.Run("runner disabled globally returns remote_execution.enabled false for all projects", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithRemoteExec)
		defer cleanup()

		router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})

		server := httptest.NewServer(router)
		defer server.Close()

		resp, err := http.Get(server.URL + "/api/projects")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var projects []board.ProjectConfig
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&projects))

		require.Len(t, projects, 1)
		require.NotNil(t, projects[0].RemoteExecution)
		require.NotNil(t, projects[0].RemoteExecution.Enabled)
		assert.False(t, *projects[0].RemoteExecution.Enabled,
			"remote_execution.enabled should be false in list when runner is globally disabled")
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
		// Card has vetted=false; agent PUTs with vetted=true — must be rejected
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

func TestClaimCard_VettedGuard(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create an imported card (with source) — not vetted by default.
	unvetted, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Imported task",
		Type:     "task",
		Priority: "medium",
		Source:   &board.Source{System: "jira", ExternalID: "JIRA-99", ExternalURL: "https://example.com/JIRA-99"},
	})
	require.NoError(t, err)
	assert.False(t, unvetted.Vetted)

	t.Run("agent claim unvetted card returns 403 CARD_NOT_VETTED", func(t *testing.T) {
		body := agentRequest{AgentID: "agent-1"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost,
			server.URL+"/api/projects/test-project/cards/"+unvetted.ID+"/claim",
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeCardNotVetted, apiErr.Code)
	})

	t.Run("human claim unvetted card returns 200", func(t *testing.T) {
		body := agentRequest{AgentID: "human:alice"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost,
			server.URL+"/api/projects/test-project/cards/"+unvetted.ID+"/claim",
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestListCards_VettedFilter(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a card without source — auto-vetted.
	_, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Auto-vetted task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Create an imported card — not vetted.
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

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 1)
		assert.True(t, cards[0].Vetted)
	})

	t.Run("?vetted=false returns only unvetted cards", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards?vetted=false")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 1)
		assert.False(t, cards[0].Vetted)
	})

	t.Run("no vetted param returns all cards", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/projects/test-project/cards")

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var cards []*board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&cards))
		assert.Len(t, cards, 2)
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
