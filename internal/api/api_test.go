package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
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
states: [todo, in_progress, done, stalled]
types: [task, bug, feature]
priorities: [low, medium, high]
transitions:
  todo: [in_progress]
  in_progress: [done, todo]
  done: [todo]
  stalled: [todo, in_progress]
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".board.yaml"), []byte(boardConfig), 0o644))

	// Initialize git
	git, err := gitops.NewManager(tmpDir)
	require.NoError(t, err)

	// Initialize store
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	// Initialize event bus
	bus := events.NewBus()

	// Initialize lock manager
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// Initialize service
	svc := service.NewCardService(store, git, lockMgr, bus, boardsDir)

	cleanup := func() {
		// Temp directory is automatically cleaned up by t.TempDir()
	}

	return svc, bus, cleanup
}

func TestListProjects(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/projects")
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, "http://localhost:5173", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORSPreflight(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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

	router := NewRouter(svc, bus)
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
