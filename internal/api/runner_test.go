package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/service"
)

// boardConfigRemoteExecEnabled is a board config with remote_execution enabled
// and a repo URL for runner trigger payloads.
const boardConfigRemoteExecEnabled = `name: test-project
prefix: TEST
next_id: 1
repo: https://github.com/example/project.git
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

// --- POST /api/projects/{project}/cards/{id}/run ---

func TestRunCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create an autonomous card in todo state.
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Mock runner server that accepts trigger requests.
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{
			Enabled:   true,
			URL:       mockRunner.URL,
			APIKey:    "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
			PublicURL: "http://localhost:8080",
		},
		MCPAPIKey: "test-mcp-key",
	})
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("non-human agent rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)
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
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("no agent header allowed", func(t *testing.T) {
		// Reset the card to todo for a clean trigger.
		_, err := svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "completed", "done")
		require.NoError(t, err)

		// Re-create a fresh card since the first may now have runner_status set.
		freshCard, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Auto task 2", Type: "task", Priority: "medium",
			Autonomous: true, FeatureBranch: true,
		})
		require.NoError(t, err)

		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+freshCard.ID+"/run", nil)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestRunCard_RunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// No runner client → runner disabled.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerDisabled, apiErr.Code)
}

func TestRunCard_NonAutonomousCardNowSucceeds(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create a non-autonomous card — should now succeed (autonomous gate removed).
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Normal task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload runner.TriggerPayload
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	// Non-autonomous card with empty body now succeeds with Interactive=false.
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Non-autonomous card should NOT have feature_branch/create_pr auto-enabled.
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.False(t, updated.FeatureBranch, "non-autonomous card should not auto-enable feature_branch")
	assert.False(t, updated.CreatePR, "non-autonomous card should not auto-enable create_pr")
	assert.False(t, receivedPayload.Interactive, "Interactive should be false")
}

func TestRunCard_CardNotInTodo(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Move to in_progress via patch.
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, service.PatchCardInput{
		State: &inProgress,
	})
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInvalidTransition, apiErr.Code)
}

func TestRunCard_AlreadyQueued(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Set runner_status to queued.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "queued", "already queued")
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerError, apiErr.Code)
}

func TestRunCard_CardNotFound(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/TEST-999/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRunCard_WebhookFailure(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Mock runner that always fails.
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"container failed"}`))
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerError, apiErr.Code)

	// Verify runner_status was reverted to "failed".
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.RunnerStatus)
}

func TestRunCard_PerProjectDisabled(t *testing.T) {
	boardConfigDisabled := `name: test-project
prefix: TEST
next_id: 1
repo: https://github.com/example/project.git
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
`
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigDisabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerDisabled, apiErr.Code)
}

// --- POST /api/projects/{project}/cards/{id}/stop ---

func TestStopCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Set runner_status to running so stop is valid.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "running")
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("non-human agent rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/stop", nil)
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
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/stop", nil)
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var respCard board.Card
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
		assert.Equal(t, "killed", respCard.RunnerStatus)
	})
}

func TestStopCard_RunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/stop", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerDisabled, apiErr.Code)
}

func TestStopCard_NotRunning(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Idle task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/stop", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerNotRunning, apiErr.Code)
}

func TestStopCard_CardNotFound(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/TEST-999/stop", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- POST /api/projects/{project}/stop-all ---

func TestStopAll_HumanOnly(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("non-human agent rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/stop-all", nil)
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
	})

	t.Run("human agent allowed with no active cards", func(t *testing.T) {
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/stop-all", nil)
		req.Header.Set("X-Agent-ID", "human:alice")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var result stopAllResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Empty(t, result.AffectedCards)
	})
}

func TestStopAll_RunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/stop-all", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerDisabled, apiErr.Code)
}

func TestStopAll_StopsActiveCards(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create multiple cards with various runner states.
	card1, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Running task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card1.ID, "running", "running")
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Queued task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card2.ID, "queued", "queued")
	require.NoError(t, err)

	// Card with no runner_status should not be affected.
	_, err = svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Idle task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/stop-all", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result stopAllResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Len(t, result.AffectedCards, 2)
	assert.Contains(t, result.AffectedCards, card1.ID)
	assert.Contains(t, result.AffectedCards, card2.ID)

	// Verify cards were updated to killed.
	updated1, err := svc.GetCard(ctx, "test-project", card1.ID)
	require.NoError(t, err)
	assert.Equal(t, "killed", updated1.RunnerStatus)

	updated2, err := svc.GetCard(ctx, "test-project", card2.ID)
	require.NoError(t, err)
	assert.Equal(t, "killed", updated2.RunnerStatus)
}

func TestStopAll_WebhookFailure(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"fail"}`))
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/stop-all", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerError, apiErr.Code)
}

// --- POST /api/runner/status ---

func TestRunnerStatusUpdate_ValidSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Runner task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"
	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := fmt.Sprintf(`{"card_id":"%s","project":"test-project","runner_status":"running","message":"container started"}`, card.ID)
	bodyBytes := []byte(body)

	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, bodyBytes)

	req, _ := http.NewRequest("POST", server.URL+"/api/runner/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var respCard board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
	assert.Equal(t, "running", respCard.RunnerStatus)
}

func TestRunnerStatusUpdate_InvalidSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"
	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`
	bodyBytes := []byte(body)

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, _ := http.NewRequest("POST", server.URL+"/api/runner/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
}

func TestRunnerStatusUpdate_MissingSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"
	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`

	t.Run("missing X-Signature-256 header", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/runner/status",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
	})

	t.Run("missing X-Webhook-Timestamp header", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/runner/status",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-256", "sha256=abc")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
	})

	t.Run("missing sha256= prefix", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/runner/status",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-256", "not-a-valid-prefix")
		req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
	})
}

func TestRunnerStatusUpdate_InvalidCallbackStatus(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"
	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	// "queued" and "killed" are not valid runner callback statuses.
	for _, badStatus := range []string{"queued", "killed", "unknown"} {
		t.Run(badStatus, func(t *testing.T) {
			body := fmt.Sprintf(`{"card_id":"TEST-001","project":"test-project","runner_status":"%s"}`, badStatus)
			bodyBytes := []byte(body)

			sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, bodyBytes)

			req, _ := http.NewRequest("POST", server.URL+"/api/runner/status", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Signature-256", sigHeader)
			req.Header.Set("X-Webhook-Timestamp", tsHeader)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer closeBody(t, resp.Body)

			assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
			var apiErr APIError
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		})
	}
}

func TestRunnerStatusUpdate_NoAPIKeyConfigured(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	// Runner without API key configured.
	runnerClient := runner.NewClient("http://localhost:9090", "")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: ""},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`

	req, _ := http.NewRequest("POST", server.URL+"/api/runner/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", "sha256=abc")
	req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInvalidSignature, apiErr.Code)
}

func TestRunnerStatusUpdate_InvalidJSON(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"
	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	bodyBytes := []byte("this is not json")
	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, bodyBytes)

	req, _ := http.NewRequest("POST", server.URL+"/api/runner/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

// --- POST /api/projects/{project}/cards/{id}/message ---

func newRunningCardSetup(t *testing.T) (*service.CardService, *events.Bus, func(), *board.Card) {
	t.Helper()
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Running task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)
	// Set runner_status to running.
	card, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)
	return svc, bus, cleanup, card
}

func TestMessageCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":"hello"}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
}

func TestMessageCard_RunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Runner: nil})
	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":"hello"}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerDisabled, apiErr.Code)
}

func TestMessageCard_NotRunning(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	for _, status := range []string{"", "queued", "failed", "killed"} {
		t.Run("status="+status, func(t *testing.T) {
			card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
				Title: "Task " + status, Type: "task", Priority: "medium",
			})
			require.NoError(t, err)
			if status != "" {
				_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, status, "set status")
				require.NoError(t, err)
			}

			body := strings.NewReader(`{"content":"hello"}`)
			req, _ := http.NewRequest("POST",
				server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer closeBody(t, resp.Body)

			assert.Equal(t, http.StatusConflict, resp.StatusCode)
			var apiErr APIError
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeRunnerNotRunning, apiErr.Code)
		})
	}
}

func TestMessageCard_EmptyContent(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":""}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
}

func TestMessageCard_ContentTooLarge(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	// Content of 8193 bytes (maxMessageContentSize + 1).
	oversized := strings.Repeat("x", maxMessageContentSize+1)
	bodyJSON := fmt.Sprintf(`{"content":%q}`, oversized)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message",
		strings.NewReader(bodyJSON))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestMessageCard_HappyPath(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	var receivedPayload runner.MessagePayload
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":"please clarify the task"}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var result messageResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result.OK)
	// UUID format check: 8-4-4-4-12 hex digits.
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, result.MessageID)

	// Verify the forwarded payload.
	assert.Equal(t, card.ID, receivedPayload.CardID)
	assert.Equal(t, "test-project", receivedPayload.Project)
	assert.Equal(t, result.MessageID, receivedPayload.MessageID)
	assert.Equal(t, "please clarify the task", receivedPayload.Content)
}

func TestMessageCard_WebhookFailure(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"runner error"}`))
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":"hello"}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerError, apiErr.Code)
}

// --- POST /api/projects/{project}/cards/{id}/promote ---

func newInteractiveRunningCard(t *testing.T, svc *service.CardService) *board.Card {
	t.Helper()
	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Interactive task", Type: "task", Priority: "medium",
		Autonomous: false,
	})
	require.NoError(t, err)
	card, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "interactive session started")
	require.NoError(t, err)
	return card
}

func TestPromoteCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()
	card := newInteractiveRunningCard(t, svc)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
}

func TestPromoteCard_NotRunning(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Not running task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	// card has no runner_status (empty) — not running.

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerNotRunning, apiErr.Code)
}

func TestPromoteCard_AlreadyAutonomous(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()
	// Card already autonomous and running.
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Already autonomous", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)
	card, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "running")
	require.NoError(t, err)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeAlreadyAutonomous, apiErr.Code)
}

func TestPromoteCard_HappyPath(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()
	card := newInteractiveRunningCard(t, svc)

	var promoteCalled int
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			promoteCalled++
		}
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var respCard board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
	assert.True(t, respCard.Autonomous, "card should be autonomous after promote")
	assert.True(t, respCard.FeatureBranch, "card should have feature_branch after promote")
	assert.True(t, respCard.CreatePR, "card should have create_pr after promote")
	assert.Equal(t, 1, promoteCalled, "promote webhook should be called once")

	// Verify flags are persisted.
	ctx := context.Background()
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, updated.Autonomous)
	assert.True(t, updated.FeatureBranch)
	assert.True(t, updated.CreatePR)
}

func TestPromoteCard_WebhookFailure_RevertsFlags(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()
	card := newInteractiveRunningCard(t, svc)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error":"promote failed"}`))
			return
		}
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeRunnerError, apiErr.Code)

	// Verify flags were reverted.
	ctx := context.Background()
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.False(t, updated.Autonomous, "autonomous flag should be reverted on failure")
	assert.False(t, updated.FeatureBranch, "feature_branch flag should be reverted on failure")
	assert.False(t, updated.CreatePR, "create_pr flag should be reverted on failure")
}

// --- runCard interactive extensions ---

func TestRunCard_Interactive(t *testing.T) {
	ctx := context.Background()

	t.Run("non-autonomous with interactive body succeeds", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		var receivedPayload runner.TriggerPayload
		mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
			writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
		}))
		defer mockRunner.Close()

		runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Runner: runnerClient,
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
		})
		server := httptest.NewServer(router)
		defer server.Close()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Non-auto interactive", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		body := strings.NewReader(`{"interactive":true}`)
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", body)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, receivedPayload.Interactive, "Interactive should be true in payload")
		// Non-autonomous card should NOT have feature_branch/create_pr auto-enabled.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.False(t, updated.FeatureBranch, "feature_branch should not be auto-enabled for interactive non-autonomous")
		assert.False(t, updated.CreatePR, "create_pr should not be auto-enabled for interactive non-autonomous")
	})

	t.Run("autonomous with empty body auto-enables feature_branch and create_pr", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		var receivedPayload runner.TriggerPayload
		mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
			writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
		}))
		defer mockRunner.Close()

		runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Runner: runnerClient,
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
		})
		server := httptest.NewServer(router)
		defer server.Close()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Autonomous task legacy", Type: "task", Priority: "medium",
			Autonomous: true,
		})
		require.NoError(t, err)

		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.False(t, receivedPayload.Interactive, "Interactive should be false")
		// Autonomous card with empty body should auto-enable feature_branch/create_pr.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, updated.FeatureBranch, "autonomous card should auto-enable feature_branch")
		assert.True(t, updated.CreatePR, "autonomous card should auto-enable create_pr")
	})

	t.Run("autonomous with interactive body does NOT auto-enable feature_branch/create_pr", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		var receivedPayload runner.TriggerPayload
		mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
			writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
		}))
		defer mockRunner.Close()

		runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Runner: runnerClient,
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj", PublicURL: "http://localhost:8080"},
		})
		server := httptest.NewServer(router)
		defer server.Close()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Autonomous interactive task", Type: "task", Priority: "medium",
			Autonomous: true,
		})
		require.NoError(t, err)

		body := strings.NewReader(`{"interactive":true}`)
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", body)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, receivedPayload.Interactive, "Interactive should be true in payload")
		// Autonomous+interactive should NOT auto-enable feature_branch/create_pr.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.False(t, updated.FeatureBranch, "autonomous+interactive should not auto-enable feature_branch")
		assert.False(t, updated.CreatePR, "autonomous+interactive should not auto-enable create_pr")
	})
}
