package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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

// signHMACAt computes the same HMAC-SHA256 signature runner.SignRequestHeaders
// produces, but lets the test specify the timestamp so we can exercise the
// clock-skew rejection path without adding a test-only helper to the
// production runner package.
func signHMACAt(t *testing.T, key, method, path string, body []byte, ts string) string {
	t.Helper()

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

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
			Enabled: true,
			URL:     mockRunner.URL,
			APIKey:  "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// Non-autonomous card with empty body now succeeds with Interactive=false.
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	// Any "Run now" trigger (including non-autonomous) should auto-enable feature_branch/create_pr.
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, updated.FeatureBranch, "run now should auto-enable feature_branch")
	assert.True(t, updated.CreatePR, "run now should auto-enable create_pr")
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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeRunnerConflict, apiErr.Code)
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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	origBackoff := runner.BackoffBase
	runner.BackoffBase = time.Millisecond

	t.Cleanup(func() { runner.BackoffBase = origBackoff })

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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeRunnerUnavailable, apiErr.Code)

	// Verify runner_status was reverted to "failed".
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.RunnerStatus)
}

// TestRunCard_ContextCancelledDuringWebhook verifies that when the HTTP client
// disconnects (cancelling r.Context()) while the runner webhook is in-flight,
// the revert to "failed" still succeeds because the handler uses
// context.WithoutCancel for the rollback path.
func TestRunCard_ContextCancelledDuringWebhook(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Cancel-during-webhook task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// triggerReady is closed when the mock webhook handler is entered so the
	// test can cancel the request context at exactly the right moment.
	triggerReady := make(chan struct{})
	// triggerUnblock is closed by the test to let the handler return (simulating
	// a slow downstream after the context was cancelled).
	triggerUnblock := make(chan struct{})

	// Mock runner that blocks until triggerUnblock is closed, simulating a slow
	// remote endpoint that outlives the HTTP client connection.
	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(triggerReady)
		<-triggerUnblock
		// Return an error so the revert branch is exercised.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"slow failure"}`))
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{
			Enabled: true,
			URL:     mockRunner.URL,
			APIKey:  "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// Build a request with a cancellable context.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx,
		"POST", server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)
	require.NoError(t, err)

	// Fire the request in the background.
	errCh := make(chan error, 1)

	go func() {
		resp, doErr := http.DefaultClient.Do(req)
		if doErr == nil {
			_ = resp.Body.Close()
		}

		errCh <- doErr
	}()

	// Wait until the webhook handler is entered, then cancel the request context
	// to simulate the client disconnecting mid-webhook.
	<-triggerReady
	reqCancel()

	// Give the handler a moment to observe the cancellation, then unblock it so
	// it can return the error response and the revert branch runs.
	//
	// Wall-clock wait: the handler observes context cancellation via
	// http.Request.Context, which is driven by the OS scheduler. The fake
	// clock abstraction does not affect http.Request.Context, so this sleep
	// stays real.
	time.Sleep(20 * time.Millisecond)
	close(triggerUnblock)

	// The client-side Do() call will have returned a context-cancelled error;
	// that's fine — we care about the card state, not the HTTP response.
	<-errCh

	// Allow a short window for the server goroutine to complete the revert.
	require.Eventually(t, func() bool {
		upd, getErr := svc.GetCard(ctx, "test-project", card.ID)

		return getErr == nil && upd.RunnerStatus == "failed"
	}, 2*time.Second, 10*time.Millisecond)

	// The card must have been reverted to "failed", not left in "queued".
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.RunnerStatus,
		"card must be reverted to failed even when client context is cancelled mid-webhook")
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
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

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
	origBackoff := runner.BackoffBase
	runner.BackoffBase = time.Millisecond

	t.Cleanup(func() { runner.BackoffBase = origBackoff })

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
	assert.Equal(t, ErrCodeRunnerUnavailable, apiErr.Code)
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

	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, http.MethodPost, "/api/runner/status", bodyBytes)

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

			sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, http.MethodPost, "/api/runner/status", bodyBytes)

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
	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, http.MethodPost, "/api/runner/status", bodyBytes)

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

func TestMessageCard_FansOutChatInputEvent(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg:         config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		RunnerEventBuffer: buf,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := strings.NewReader(`{"content":"hello from human"}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/message", body)
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	evs := buf.Since(card.ID, 0)
	require.Len(t, evs, 1)
	assert.Equal(t, "chat_input", evs[0].Type)
	assert.Equal(t, "hello from human", evs[0].Data)
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
		Autonomous: true, FeatureBranch: true, CreatePR: true,
	})
	require.NoError(t, err)
	card, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "running")
	require.NoError(t, err)

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

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	// Guard: already-autonomous card short-circuits before calling the runner webhook.
	// Still 202 (accepted) — the idempotent path returns the current card.
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var respCard board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
	assert.True(t, respCard.Autonomous, "card should remain autonomous")
	assert.Equal(t, 0, promoteCalled, "idempotency guard must skip runner webhook when card is already autonomous")

	// No extra log entry added (idempotent).
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	for _, entry := range updated.ActivityLog {
		assert.NotEqual(t, "promoted", entry.Action, "idempotent promote must not add a log entry")
	}
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

	// Verify flags and log entry are persisted.
	ctx := context.Background()
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, updated.Autonomous)
	assert.True(t, updated.FeatureBranch)
	assert.True(t, updated.CreatePR)

	// Verify the activity log contains the promote entry with the right agent.
	var found bool

	for _, entry := range updated.ActivityLog {
		if entry.Action == "promoted" {
			found = true

			assert.Equal(t, "human:alice", entry.Agent, "promote log agent must match X-Agent-ID")
			assert.Equal(t, "Promoted to autonomous mode", entry.Message)
		}
	}

	assert.True(t, found, "promote activity log entry must be present")
}

func TestPromoteCard_FansOutPromotionEvent(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card := newInteractiveRunningCard(t, svc)

	mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer mockRunner.Close()

	runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	buf := events.NewRunnerEventBuffer(100, time.Hour)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg:         config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		RunnerEventBuffer: buf,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)
	req.Header.Set("X-Agent-ID", "human:alice")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	evs := buf.Since(card.ID, 0)
	require.Len(t, evs, 1)
	assert.Equal(t, "promotion", evs[0].Type)
	assert.Equal(t, "{}", evs[0].Data)
}

func TestPromoteCard_WebhookFailure_RetainsFlag(t *testing.T) {
	// The new design: PromoteToAutonomous sets the flag first (server-authoritative).
	// If the runner webhook subsequently fails, we return 502 but do NOT revert the
	// autonomous flag — the flag flip already committed to git and is the source of truth.
	// The runner-side handlePromote is responsible for failing closed (no stdin write).
	origBackoff := runner.BackoffBase
	runner.BackoffBase = time.Millisecond

	t.Cleanup(func() { runner.BackoffBase = origBackoff })

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
	assert.Equal(t, ErrCodeRunnerUnavailable, apiErr.Code)

	// Autonomous flag stays set (server is authoritative; runner webhook failure is a
	// delivery problem, not a flag problem). The runner-side handlePromote will see
	// the flag is now set on the card and can retry or the human can re-promote.
	ctx := context.Background()
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, updated.Autonomous, "autonomous flag must remain set after webhook failure")
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
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.True(t, receivedPayload.Interactive, "Interactive should be true in payload")
		// HITL run should auto-enable feature_branch/create_pr just like autonomous runs.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, updated.FeatureBranch, "HITL run should auto-enable feature_branch")
		assert.True(t, updated.CreatePR, "HITL run should auto-enable create_pr")
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
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.False(t, receivedPayload.Interactive, "Interactive should be false")
		// Autonomous card with empty body should auto-enable feature_branch/create_pr.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, updated.FeatureBranch, "autonomous card should auto-enable feature_branch")
		assert.True(t, updated.CreatePR, "autonomous card should auto-enable create_pr")
	})

	t.Run("autonomous with interactive body auto-enables feature_branch/create_pr", func(t *testing.T) {
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
			RunnerCfg: config.RunnerConfig{Enabled: true, URL: mockRunner.URL, APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.True(t, receivedPayload.Interactive, "Interactive should be true in payload")
		// Autonomous+interactive should auto-enable feature_branch/create_pr like all Run now triggers.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, updated.FeatureBranch, "autonomous+interactive should auto-enable feature_branch")
		assert.True(t, updated.CreatePR, "autonomous+interactive should auto-enable create_pr")
	})

	t.Run("HITL run on card with feature_branch already true does not redundantly patch", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		var (
			triggerCount    int
			receivedPayload runner.TriggerPayload
		)

		mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			triggerCount++
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

		// Create a card that already has feature_branch=true.
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Already feature branched", Type: "task", Priority: "medium",
			FeatureBranch: true,
		})
		require.NoError(t, err)

		body := strings.NewReader(`{"interactive":true}`)
		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", body)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.True(t, receivedPayload.Interactive, "Interactive should be true in payload")
		// Patch is skipped when feature_branch is already true — flags preserved as-is.
		updated, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, updated.FeatureBranch, "feature_branch should remain true")
		assert.False(t, updated.CreatePR, "create_pr stays false — patch was skipped since feature_branch was already set")
		// Exactly one trigger webhook should have fired.
		assert.Equal(t, 1, triggerCount, "runner should be triggered exactly once")
	})
}

// TestPromoteCard_RecursionGuard verifies that when the runner's /promote handler
// calls back into CM's /promote endpoint (simulating the original infinite-recursion
// bug), the idempotency guard on CM's side short-circuits on the second call and
// does NOT forward the webhook again.
//
// Without the guard this test would spin up goroutines indefinitely until the
// 2-second client deadline fires; with the guard the top-level call returns 200
// and the fake runner receives exactly one POST /promote.
func TestPromoteCard_RecursionGuard(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create a non-autonomous card in the "running" state (ready to be promoted).
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Interactive task for recursion test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	card, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "interactive session")
	require.NoError(t, err)

	// cmURL is set after the CM server starts; the fake runner closure captures the pointer.
	var cmURL atomic.Value

	// promoteCallCount tracks how many times the fake runner's /promote handler fires.
	var promoteCallCount atomic.Int32

	// Fake runner: when it receives POST /promote, it calls back into CM's /promote
	// endpoint synchronously — this reproduces the original buggy runner behaviour.
	fakeRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			count := promoteCallCount.Add(1)

			// Only call back on the first invocation to avoid spinning up a truly
			// unbounded number of goroutines in the "without guard" scenario.
			// One callback is enough to demonstrate the recursion.
			if count == 1 {
				base, _ := cmURL.Load().(string)
				if base != "" {
					callbackURL := base + "/api/projects/test-project/cards/" + card.ID + "/promote"
					// Use a short per-call timeout so the test fails fast if the guard is absent.
					callbackClient := &http.Client{Timeout: 500 * time.Millisecond}
					cbReq, _ := http.NewRequest("POST", callbackURL, nil)
					// No X-Agent-ID → treated as human (no agent prefix guard needed here).
					cbResp, cbErr := callbackClient.Do(cbReq)
					if cbErr == nil && cbResp != nil {
						_ = cbResp.Body.Close()
					}
				}
			}
		}

		writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
	}))
	defer fakeRunner.Close()

	runnerClient := runner.NewClient(fakeRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{
			Enabled: true,
			URL:     fakeRunner.URL,
			APIKey:  "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
	})

	cmServer := httptest.NewServer(router)
	defer cmServer.Close()

	cmURL.Store(cmServer.URL)

	// Issue the top-level promote with a 2-second deadline.
	// Without the guard the fake runner's callback will call CM again → CM calls the
	// runner again → the fan-out continues until the 2-second deadline fires.
	// With the guard CM sees autonomous==true on the callback and short-circuits.
	topLevelClient := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("POST",
		cmServer.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)

	resp, err := topLevelClient.Do(req)

	require.NoError(t, err, "top-level promote must not time out (guard must short-circuit the callback)")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode, "top-level promote must return 202")

	// The fake runner must have been called exactly once — the callback from the runner
	// must NOT have triggered a second outbound webhook from CM.
	assert.Equal(t, int32(1), promoteCallCount.Load(),
		"fake runner must receive exactly one POST /promote; guard must block the recursive call")
}

// --- GET /api/v1/cards/{project}/{id}/autonomous ---
//
// The runner calls this during /promote to confirm the card's autonomous
// flag. HMAC signing is the primary auth path; a Bearer fallback is kept
// during the runner-upgrade window (CTXRUN-048).

const testRunnerAPIKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

func setupAutonomousEndpoint(t *testing.T, autonomous bool) (*httptest.Server, string, func()) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Promote target", Type: "task", Priority: "medium", Autonomous: autonomous,
	})
	require.NoError(t, err)

	runnerClient := runner.NewClient("http://localhost:9090", testRunnerAPIKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: testRunnerAPIKey},
	})

	server := httptest.NewServer(router)

	return server, card.ID, func() {
		server.Close()
		cleanup()
	}
}

func TestGetCardAutonomous_HMAC_Valid(t *testing.T) {
	for _, autonomous := range []bool{true, false} {
		t.Run(fmt.Sprintf("autonomous=%v", autonomous), func(t *testing.T) {
			server, cardID, cleanup := setupAutonomousEndpoint(t, autonomous)
			defer cleanup()

			path := "/api/v1/cards/test-project/" + cardID + "/autonomous"
			sig, ts := runner.SignRequestHeaders(testRunnerAPIKey, http.MethodGet, path, nil)

			req, _ := http.NewRequest("GET", server.URL+path, nil)
			req.Header.Set("X-Signature-256", sig)
			req.Header.Set("X-Webhook-Timestamp", ts)

			resp, err := http.DefaultClient.Do(req)

			require.NoError(t, err)
			defer closeBody(t, resp.Body)

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var body cardAutonomousResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, autonomous, body.Autonomous)
		})
	}
}

func TestGetCardAutonomous_HMAC_BadSignature(t *testing.T) {
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, _ := http.NewRequest("GET", server.URL+"/api/v1/cards/test-project/"+cardID+"/autonomous", nil)
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

func TestGetCardAutonomous_HMAC_MissingTimestamp(t *testing.T) {
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	path := "/api/v1/cards/test-project/" + cardID + "/autonomous"
	sig, _ := runner.SignRequestHeaders(testRunnerAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetCardAutonomous_HMAC_ExpiredTimestamp(t *testing.T) {
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	// Signed 10 minutes ago — outside DefaultMaxClockSkew (5 min).
	staleTs := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	// Compute the signature over the stale timestamp so the signature
	// itself is valid — only the clock-skew check should fail.
	path := "/api/v1/cards/test-project/" + cardID + "/autonomous"
	staleSig := signHMACAt(t, testRunnerAPIKey, http.MethodGet, path, nil, staleTs)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", staleSig)
	req.Header.Set("X-Webhook-Timestamp", staleTs)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetCardAutonomous_BearerRejected(t *testing.T) {
	// Bearer is not accepted even with the correct shared secret — the
	// runner must HMAC-sign this endpoint.
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	req, _ := http.NewRequest("GET", server.URL+"/api/v1/cards/test-project/"+cardID+"/autonomous", nil)
	req.Header.Set("Authorization", "Bearer "+testRunnerAPIKey)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetCardAutonomous_NoAuth(t *testing.T) {
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	req, _ := http.NewRequest("GET", server.URL+"/api/v1/cards/test-project/"+cardID+"/autonomous", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetCardAutonomous_CardNotFound(t *testing.T) {
	server, _, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	path := "/api/v1/cards/test-project/TEST-999/autonomous"
	sig, ts := runner.SignRequestHeaders(testRunnerAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetCardAutonomous_RunnerDisabled(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Promote target", Type: "task", Priority: "medium", Autonomous: true,
	})
	require.NoError(t, err)

	// Runner intentionally nil — route must not be registered.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/v1/cards/test-project/" + card.ID + "/autonomous"
	sig, ts := runner.SignRequestHeaders(testRunnerAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestRunCard_TaskSkillsResolution verifies the four resolution cases for
// task_skills in the TriggerPayload: card.Skills wins over project default,
// project default used when card has nil, nil when neither is set, and explicit
// empty card skills wins over project default.
func TestRunCard_TaskSkillsResolution(t *testing.T) {
	ctx := context.Background()

	// boardConfigWithDefaultSkills adds default_skills to the project config.
	const boardConfigWithDefaultSkills = `name: test-project
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
default_skills:
  - go-development
`

	triggerCard := func(t *testing.T, svc *service.CardService, bus *events.Bus, cardID string) runner.TriggerPayload {
		t.Helper()

		var capturedPayload runner.TriggerPayload

		mockRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

			writeJSON(w, http.StatusOK, runner.WebhookResponse{OK: true})
		}))
		t.Cleanup(mockRunner.Close)

		runnerClient := runner.NewClient(mockRunner.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Runner: runnerClient,
			RunnerCfg: config.RunnerConfig{
				Enabled: true,
				URL:     mockRunner.URL,
				APIKey:  "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
			},
		})

		server := httptest.NewServer(router)
		t.Cleanup(server.Close)

		req, _ := http.NewRequest("POST",
			server.URL+"/api/projects/test-project/cards/"+cardID+"/run", nil)

		resp, err := http.DefaultClient.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusAccepted, resp.StatusCode)

		return capturedPayload
	}

	t.Run("card.Skills wins over project default", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithDefaultSkills)
		defer cleanup()

		cardSkills := []string{"python-development"}
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Card with skills", Type: "task", Priority: "medium",
			Skills: &cardSkills,
		})
		require.NoError(t, err)

		payload := triggerCard(t, svc, bus, card.ID)

		require.NotNil(t, payload.TaskSkills, "TaskSkills must not be nil")
		assert.Equal(t, []string{"python-development"}, *payload.TaskSkills,
			"card.Skills must win over project default_skills")
	})

	t.Run("falls through to project default when card has nil", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithDefaultSkills)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Card without skills", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		payload := triggerCard(t, svc, bus, card.ID)

		require.NotNil(t, payload.TaskSkills, "TaskSkills must not be nil")
		assert.Equal(t, []string{"go-development"}, *payload.TaskSkills,
			"project default_skills must be used when card has nil Skills")
	})

	t.Run("nil when neither set", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "No skills anywhere", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		payload := triggerCard(t, svc, bus, card.ID)

		assert.Nil(t, payload.TaskSkills, "TaskSkills must be nil when neither card nor project has skills")
	})

	t.Run("explicit empty card skills wins over project default", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithDefaultSkills)
		defer cleanup()

		emptySkills := []string{}
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Card with empty skills", Type: "task", Priority: "medium",
			Skills: &emptySkills,
		})
		require.NoError(t, err)

		payload := triggerCard(t, svc, bus, card.ID)

		require.NotNil(t, payload.TaskSkills, "TaskSkills must not be nil for explicit empty slice")
		assert.Empty(t, *payload.TaskSkills,
			"explicit empty card Skills must win over project default_skills")
	})
}

// --- POST /api/runner/skill-engaged ---

func TestAPI_RunnerSkillEngaged(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	// Create a real card so RecordSkillEngaged can find it.
	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Skill test", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	runnerClient := runner.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Runner: runnerClient,
		RunnerCfg: config.RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := map[string]any{
		"card_id":    card.ID,
		"project":    "test-project",
		"skill_name": "go-development",
	}

	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, http.MethodPost, "/api/runner/skill-engaged", bodyBytes)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/runner/skill-engaged", bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAPI_RunnerSkillEngaged_InvalidSignature(t *testing.T) {
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

	bodyBytes := []byte(`{"card_id":"ALPHA-001","project":"alpha","skill_name":"go-development"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/runner/skill-engaged", bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAPI_RunnerSkillEngaged_MissingFields(t *testing.T) {
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

	// Missing skill_name.
	bodyBytes := []byte(`{"card_id":"ALPHA-001","project":"alpha"}`)
	sigHeader, tsHeader := runner.SignRequestHeaders(apiKey, http.MethodPost, "/api/runner/skill-engaged", bodyBytes)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/runner/skill-engaged", bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
