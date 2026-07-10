package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

// fakeTokenProvider is a githubauth.TokenGenerator test double for exercising
// runCard's project-scoped git-token minting without a real GitHub App/PAT.
type fakeTokenProvider struct {
	token     string
	expiresAt time.Time
	err       error
}

func (f *fakeTokenProvider) GenerateToken(_ context.Context) (string, time.Time, error) {
	return f.token, f.expiresAt, f.err
}

// signHMACAt computes the same HMAC-SHA256 signature protocol.SignRequestHeaders
// produces, but lets the test specify the timestamp so we can exercise the
// clock-skew rejection path without adding a test-only helper to the
// production backend package.
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
// and a repo URL for backend trigger payloads.
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

	// Mock backend server that accepts trigger requests.
	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
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
		_, err := svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "completed", "done")
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

func TestRunCard_BackendDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// No backend client → backend disabled.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

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
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

func TestRunCard_NonAutonomousCardNowSucceeds(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create a non-autonomous card — it must succeed (no autonomous-only gate).
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Normal task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// Non-autonomous card with empty body succeeds with Interactive=false.
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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "queued", "already queued")
	require.NoError(t, err)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendConflict, apiErr.Code)
}

func TestRunCard_CardNotFound(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	origBackoff := backend.BackoffBase
	backend.BackoffBase = time.Millisecond

	t.Cleanup(func() { backend.BackoffBase = origBackoff })

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium",
		Autonomous: true, FeatureBranch: true,
	})
	require.NoError(t, err)

	// Mock backend that always fails.
	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"container failed"}`))
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendUnavailable, apiErr.Code)

	// Verify runner_status was reverted to "failed".
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.RunnerStatus)
}

// TestRunCard_ContextCancelledDuringWebhook verifies that when the HTTP client
// disconnects (cancelling r.Context()) while the backend webhook is in-flight,
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

	// Mock backend that blocks until triggerUnblock is closed, simulating a slow
	// remote endpoint that outlives the HTTP client connection.
	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(triggerReady)
		<-triggerUnblock
		// Return an error so the revert branch is exercised.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"slow failure"}`))
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

// --- Trigger minting: project git token + LLM endpoint (S6b token authority) ---

// TestRunCard_ProviderForProject_MintsGitToken asserts that when
// ProviderForProject is wired, runCard resolves a token provider for the
// project and attaches the minted token + its RFC3339 expiry to the trigger
// payload sent to the backend.
func TestRunCard_ProviderForProject_MintsGitToken(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	fakeExpiry := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	fakeProvider := &fakeTokenProvider{token: "ghs_faketoken", expiresAt: fakeExpiry}

	var gotProject string

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: func(_ context.Context, project string) (githubauth.TokenGenerator, string, error) {
			gotProject = project

			return fakeProvider, "https://api.github.com", nil
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "test-project", gotProject)
	assert.Equal(t, "ghs_faketoken", receivedPayload.GitToken)
	assert.Equal(t, fakeExpiry.UTC().Format(time.RFC3339), receivedPayload.GitTokenExpiresAt)
}

// TestRunCard_ProviderForProject_PATZeroExpiry_ExpiryOmitted asserts that a
// provider returning a zero time.Time (the PAT-style "no server-managed TTL"
// case) leaves GitTokenExpiresAt empty rather than formatting the Go zero
// value ("0001-01-01T00:00:00Z") onto the wire.
func TestRunCard_ProviderForProject_PATZeroExpiry_ExpiryOmitted(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	fakeProvider := &fakeTokenProvider{token: "pat-token", expiresAt: time.Time{}}

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "pat-token", receivedPayload.GitToken)
	assert.Empty(t, receivedPayload.GitTokenExpiresAt,
		"zero-value expiry must be omitted, never formatted as 0001-01-01...")
}

// TestRunCard_ProviderForProject_CredentialUnavailable asserts the fail-closed
// contract: a broken/unresolvable project credential binding rejects the
// trigger with 409, records a visible activity-log entry on the card, and
// NEVER calls the backend client (no silent fallback to an instance
// credential).
func TestRunCard_ProviderForProject_CredentialUnavailable(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var backendCalled atomic.Bool

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled.Store(true)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return nil, "", auth.ErrCredentialUnavailable
		},
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
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Equal(t, "project credential unavailable", apiErr.Error)

	assert.False(t, backendCalled.Load(), "backend client must never be called when credential minting fails")

	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotEmpty(t, updated.ActivityLog)

	last := updated.ActivityLog[len(updated.ActivityLog)-1]
	assert.Equal(t, "system", last.Agent)
	assert.Equal(t, "run rejected: project credential unavailable (test-project)", last.Message)

	// runCard set runner_status to "queued" before minting; the rejection must
	// revert it to "failed" (mirroring the webhook-failure path) or the
	// already-queued guard would 409 every future trigger of this card.
	assert.Equal(t, "failed", updated.RunnerStatus,
		"rejected trigger must revert runner_status to failed, not leave the card stuck queued")
}

// TestRunCard_ProviderForProject_GenerateTokenFails covers the second
// fail-closed branch: the provider resolves but minting the token itself
// errors (e.g. GitHub App exchange failure). Same 409 + activity-log
// treatment as an unresolvable provider.
func TestRunCard_ProviderForProject_GenerateTokenFails(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var backendCalled atomic.Bool

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled.Store(true)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	fakeProvider := &fakeTokenProvider{err: fmt.Errorf("request token: github api returned status 401")}

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		},
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
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)

	assert.False(t, backendCalled.Load(), "backend client must never be called when token minting fails")

	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotEmpty(t, updated.ActivityLog)

	last := updated.ActivityLog[len(updated.ActivityLog)-1]
	assert.Equal(t, "system", last.Agent)
	assert.Equal(t, "run rejected: project credential unavailable (test-project)", last.Message)

	// Same revert contract as the provider-resolution failure: the card must
	// not be left stuck in "queued" after the 409.
	assert.Equal(t, "failed", updated.RunnerStatus,
		"rejected trigger must revert runner_status to failed, not leave the card stuck queued")
}

// TestRunCard_ProviderForProject_BrokenBindingNeverFallsBackToInstance
// hardens the fail-closed contract from a stronger angle than
// TestRunCard_ProviderForProject_CredentialUnavailable above: that test's
// injected ProviderForProject unconditionally returns an error, so it proves
// the handler reacts correctly to a resolution failure but never gives
// resolution an actual working instance provider it could have (wrongly)
// substituted. This test wires a resolver — mirroring
// cmd/contextmatrix/main.go's providerForProject — around a real
// *auth.Service holding a genuinely resolvable credential, plus a genuinely
// working instance-level fallback provider, then proves both halves of the
// contract in the same setup: the broken binding still 409s and never
// reaches the task backend, AND the very same resolver hands back the
// instance provider for an unbound project — so the 409 is not an artifact
// of "nothing here ever works".
//
// The resolution logic itself is no longer only a hand-typed replica here:
// cmd/contextmatrix/provider.go extracts it into the named
// newProviderForProject, directly covered by TestNewProviderForProject in
// cmd/contextmatrix/provider_test.go. This test stays to pin the
// handler-side half of the contract that a resolver-only test cannot reach:
// a resolution error 409s, never calls the task backend, and reverts
// runner_status to failed — i.e. runCard applies no fallback of its own on
// top of whatever the resolver returns.
func TestRunCard_ProviderForProject_BrokenBindingNeverFallsBackToInstance(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Real credential-pool Service, seeded with one genuinely resolvable
	// credential — rules out "TokenProviderFor always errors regardless of
	// input" as a false explanation for the 409 asserted below.
	authStore, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)

	defer func() { _ = authStore.Close() }()

	authSvc := auth.NewService(authStore, time.Hour)

	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)
	authSvc.SetCredentialKey(credKey)
	authSvc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })

	require.NoError(t, authSvc.CreateCredential(ctx, auth.CredentialInput{
		Name: "good-cred", Kind: authstore.CredentialKindPAT, Secret: "good-secret",
	}, "human:root"))

	_, _, _, err = authSvc.TokenProviderFor(ctx, "good-cred")
	require.NoError(t, err, "sanity: this authSvc genuinely resolves a real credential")

	// The instance-level fallback: healthy and reachable, but must never be
	// substituted for test-project's broken binding below.
	instanceProvider := &fakeTokenProvider{token: "instance-token"}

	const instanceAPIBase = "https://instance.example/api"

	// Mirrors cmd/contextmatrix/main.go's providerForProject: the project's
	// binding wins when set (fail-closed on a broken one), else the instance
	// provider. test-project is bound to a name never registered above — a
	// typo'd binding, or a credential deleted after the .board.yaml binding
	// was made.
	resolver := func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error) {
		credName := ""
		if project == "test-project" {
			credName = "broken-cred"
		}

		if credName == "" {
			return instanceProvider, instanceAPIBase, nil
		}

		provider, apiBase, _, err := authSvc.TokenProviderFor(ctx, credName)
		if err != nil {
			return nil, "", err
		}

		return provider, apiBase, nil
	}

	// Contrast first: the same resolver DOES hand back the instance provider
	// for a project with no binding — proving the fallback path is genuinely
	// reachable in this exact setup, not merely unwired.
	unboundProvider, unboundAPIBase, err := resolver(ctx, "unbound-project")
	require.NoError(t, err)
	assert.Same(t, instanceProvider, unboundProvider, "unbound project resolves to the instance provider")
	assert.Equal(t, instanceAPIBase, unboundAPIBase)

	var backendCalled atomic.Bool

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled.Store(true)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg:    &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: resolver,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.False(t, backendCalled.Load(),
		"backend must never be called — a broken binding must not fall back to the working instance credential")

	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.RunnerStatus,
		"rejected trigger must revert runner_status to failed, not leave the card stuck queued")
}

// TestRunCard_ProviderForProject_NoneMode_ReturnsInstanceProvider hardens the
// "none mode" branch of the fail-closed contract: mirrors
// cmd/contextmatrix/main.go's providerForProject closure with authSvc == nil
// (auth.mode "none" — no credential pool exists at all) and proves the
// resolver returns the instance provider without ever dereferencing the nil
// credential service. A missing or reordered nil-guard here would
// nil-pointer-panic on every run trigger in a none-mode deployment.
// test-project carries a (would-be) binding so the assertion exercises the
// "authSvc == nil" arm of the guard specifically, not the already-covered
// "no binding configured" arm.
//
// The nil-guard logic itself is directly covered by TestNewProviderForProject
// (cmd/contextmatrix/provider_test.go), which exercises the real
// newProviderForProject constructor rather than this mirrored resolver. This
// test stays to pin the handler-side half: the resolved instance provider's
// token actually reaches the task backend's trigger payload (202, not just
// "no panic").
func TestRunCard_ProviderForProject_NoneMode_ReturnsInstanceProvider(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var authSvc *auth.Service // nil: auth.mode "none" — no credential pool at all

	instanceProvider := &fakeTokenProvider{token: "instance-token"}

	const instanceAPIBase = "https://instance.example/api"

	// Mirrors cmd/contextmatrix/main.go's providerForProject. test-project
	// carries a (would-be) binding so the "authSvc == nil" half of the OR
	// guard is what actually decides the outcome below, not the "no binding"
	// half.
	resolver := func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error) {
		credName := ""
		if project == "test-project" {
			credName = "some-bound-credential"
		}

		if credName == "" || authSvc == nil {
			return instanceProvider, instanceAPIBase, nil
		}

		// Unreachable while the guard above holds — a nil authSvc here would
		// nil-pointer-panic inside TokenProviderFor.
		provider, apiBase, _, err := authSvc.TokenProviderFor(ctx, credName)
		if err != nil {
			return nil, "", err
		}

		return provider, apiBase, nil
	}

	var (
		directProvider githubauth.TokenGenerator
		directAPIBase  string
		directErr      error
	)

	require.NotPanics(t, func() {
		directProvider, directAPIBase, directErr = resolver(ctx, "test-project")
	}, "nil auth service must never be dereferenced")

	require.NoError(t, directErr)
	assert.Same(t, instanceProvider, directProvider,
		"nil auth service must resolve to the instance provider, not error")
	assert.Equal(t, instanceAPIBase, directAPIBase)

	// End to end: the trigger-mint path (runCard) actually uses this
	// resolver's result — the token that reaches the wire is the instance
	// provider's token.
	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg:    &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		ProviderForProject: resolver,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "instance-token", receivedPayload.GitToken)
}

// TestRunCard_NoProviderForProject_BackwardsCompat asserts that when
// ProviderForProject is not wired (pre-token-authority configs, and most
// existing tests), runCard neither attaches a git token nor rejects the
// trigger — the payload goes out exactly as it did before token authority.
func TestRunCard_NoProviderForProject_BackwardsCompat(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	// No ProviderForProject set.
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Empty(t, receivedPayload.GitToken)
	assert.Empty(t, receivedPayload.GitTokenExpiresAt)
	assert.Nil(t, receivedPayload.LLMEndpoint)

	// No credential rejection means no activity entry was added by this path.
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	for _, entry := range updated.ActivityLog {
		assert.NotEqual(t, "run-rejected", entry.Action)
	}
}

// TestRunCard_LLMEndpoint_PresentWhenConfigured asserts that a configured
// RouterConfig.LLMEndpoint is attached to every trigger payload verbatim.
func TestRunCard_LLMEndpoint_PresentWhenConfigured(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	endpoint := &protocol.LLMEndpoint{Type: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-test-key"}

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		LLMEndpoint:     endpoint,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.NotNil(t, receivedPayload.LLMEndpoint)
	assert.Equal(t, *endpoint, *receivedPayload.LLMEndpoint)
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
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "running")
	require.NoError(t, err)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

func TestStopCard_BackendDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

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
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

func TestStopCard_NotRunning(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Idle task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendNotRunning, apiErr.Code)
}

func TestStopCard_CardNotFound(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

func TestStopAll_BackendDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

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
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

func TestStopAll_StopsActiveCards(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create multiple cards with various runner_status values.
	card1, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Running task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card1.ID, "running", "running")
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Queued task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card2.ID, "queued", "queued")
	require.NoError(t, err)

	// Card with no runner_status should not be affected.
	_, err = svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Idle task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	origBackoff := backend.BackoffBase
	backend.BackoffBase = time.Millisecond

	t.Cleanup(func() { backend.BackoffBase = origBackoff })

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"fail"}`))
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendUnavailable, apiErr.Code)
}

// --- POST /api/agent/status ---

func TestWorkerStatusUpdate_ValidSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Runner task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	backendClient := backend.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := fmt.Sprintf(`{"card_id":"%s","project":"test-project","runner_status":"running","message":"container started"}`, card.ID)
	bodyBytes := []byte(body)

	sigHeader, tsHeader := protocol.SignRequestHeaders(apiKey, http.MethodPost, "/api/agent/status", bodyBytes)

	req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", bytes.NewReader(bodyBytes))
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

// Backend callbacks mount at the fixed config.AgentCallbackPath — prove the
// agent backend's callbacks land at /api/agent.
func TestAgentBackendCallbackMount(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Agent task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	agentClient := backend.NewClient("http://localhost:9091", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: agentClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := fmt.Sprintf(`{"card_id":"%s","project":"test-project","runner_status":"running","message":"container started"}`, card.ID)
	bodyBytes := []byte(body)

	sigHeader, tsHeader := protocol.SignRequestHeaders(apiKey, http.MethodPost, "/api/agent/status", bodyBytes)

	req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", bytes.NewReader(bodyBytes))
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

func TestWorkerStatusUpdate_InvalidSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	backendClient := backend.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`
	bodyBytes := []byte(body)

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", bytes.NewReader(bodyBytes))
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

func TestWorkerStatusUpdate_MissingSignature(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	backendClient := backend.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`

	t.Run("missing X-Signature-256 header", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/agent/status",
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
		req, _ := http.NewRequest("POST", server.URL+"/api/agent/status",
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
		req, _ := http.NewRequest("POST", server.URL+"/api/agent/status",
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

func TestWorkerStatusUpdate_InvalidCallbackStatus(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	backendClient := backend.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// "queued" and "killed" are not valid callback statuses.
	for _, badStatus := range []string{"queued", "killed", "unknown"} {
		t.Run(badStatus, func(t *testing.T) {
			body := fmt.Sprintf(`{"card_id":"TEST-001","project":"test-project","runner_status":"%s"}`, badStatus)
			bodyBytes := []byte(body)

			sigHeader, tsHeader := protocol.SignRequestHeaders(apiKey, http.MethodPost, "/api/agent/status", bodyBytes)

			req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", bytes.NewReader(bodyBytes))
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

func TestWorkerStatusUpdate_NoAPIKeyConfigured(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	// Backend without API key configured.
	backendClient := backend.NewClient("http://localhost:9090", "")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: ""},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"card_id":"TEST-001","project":"test-project","runner_status":"running"}`

	req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", strings.NewReader(body))
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

func TestWorkerStatusUpdate_InvalidJSON(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	const apiKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

	backendClient := backend.NewClient("http://localhost:9090", apiKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: apiKey},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	bodyBytes := []byte("this is not json")
	sigHeader, tsHeader := protocol.SignRequestHeaders(apiKey, http.MethodPost, "/api/agent/status", bodyBytes)

	req, _ := http.NewRequest("POST", server.URL+"/api/agent/status", bytes.NewReader(bodyBytes))
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
	card, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)

	return svc, bus, cleanup, card
}

func TestMessageCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

func TestMessageCard_BackendDisabled(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil})

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
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

func TestMessageCard_NotRunning(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
				_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, status, "set status")
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
			assert.Equal(t, ErrCodeBackendNotRunning, apiErr.Code)
		})
	}
}

func TestMessageCard_EmptyContent(t *testing.T) {
	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

	var receivedPayload backend.MessagePayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	origBackoff := backend.BackoffBase
	backend.BackoffBase = time.Millisecond

	t.Cleanup(func() { backend.BackoffBase = origBackoff })

	svc, bus, cleanup, card := newRunningCardSetup(t)
	defer cleanup()

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"runner error"}`))
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendUnavailable, apiErr.Code)
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
	card, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "interactive session started")
	require.NoError(t, err)

	return card
}

func TestPromoteCard_HumanOnly(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card := newInteractiveRunningCard(t, svc)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendNotRunning, apiErr.Code)
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
	card, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "running")
	require.NoError(t, err)

	var promoteCalled int

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			promoteCalled++
		}

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	// Guard: already-autonomous card short-circuits before calling the backend webhook.
	// Still 202 (accepted) — the idempotent path returns the current card.
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var respCard board.Card
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&respCard))
	assert.True(t, respCard.Autonomous, "card should remain autonomous")
	assert.Equal(t, 0, promoteCalled, "idempotency guard must skip the backend webhook when card is already autonomous")

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

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			promoteCalled++
		}

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

func TestPromoteCard_WebhookFailure_RevertsFlag(t *testing.T) {
	// Updated design: when the backend /promote webhook fails after the API has
	// already flipped autonomous/feature_branch/create_pr, the handler reverts
	// those changes so the card's declared mode matches the agent's actual mode
	// inside the container. The backend-side /promote handler already fails closed
	// (no stdin write) when the webhook fails, leaving the agent in HITL mode;
	// reverting the card flags avoids a silent contract violation where the
	// card claims autonomous but the in-container agent never received that.
	//
	// The revert is recorded with a "promote-webhook-failed" activity-log entry
	// so operators reconciling a half-promoted card can see why without
	// grepping server logs.
	origBackoff := backend.BackoffBase
	backend.BackoffBase = time.Millisecond

	t.Cleanup(func() { backend.BackoffBase = origBackoff })

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card := newInteractiveRunningCard(t, svc)

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/promote" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error":"promote failed"}`))

			return
		}

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
	assert.Equal(t, ErrCodeBackendUnavailable, apiErr.Code)

	// Autonomous flag is reverted along with the feature_branch/create_pr
	// flags this handler enabled. Operators see a "promote-webhook-failed"
	// activity entry that explains the revert.
	ctx := context.Background()
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.False(t, updated.Autonomous, "autonomous flag must be reverted after webhook failure")
	assert.False(t, updated.FeatureBranch, "feature_branch must be reverted after webhook failure (handler enabled it)")
	assert.False(t, updated.CreatePR, "create_pr must be reverted after webhook failure (handler enabled it)")

	var foundRevert bool

	for _, entry := range updated.ActivityLog {
		if entry.Action == "promote-webhook-failed" {
			foundRevert = true

			break
		}
	}

	assert.True(t, foundRevert, "promote-webhook-failed activity entry must be present after revert")
}

// An autonomous card must run the FSM, never the linear HITL path, even when
// the run request body explicitly asks for interactive. CM forces it off.
func TestRunCard_AutonomousForcesNonInteractive(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium", Autonomous: true,
	})
	require.NoError(t, err)

	var receivedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	// Body explicitly requests interactive=true; the autonomous flag must win.
	body := strings.NewReader(`{"interactive":true}`)
	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.False(t, receivedPayload.Interactive,
		"autonomous card must trigger non-interactive (FSM) regardless of request body")
}

// --- runCard interactive extensions ---

func TestRunCard_Interactive(t *testing.T) {
	ctx := context.Background()

	t.Run("non-autonomous with interactive body succeeds", func(t *testing.T) {
		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		defer cleanup()

		var receivedPayload backend.TriggerPayload

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		defer mockBackend.Close()

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		var receivedPayload backend.TriggerPayload

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		defer mockBackend.Close()

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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

		var receivedPayload backend.TriggerPayload

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		defer mockBackend.Close()

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
		// CM forces interactive off for autonomous cards: they must run the
		// backend autonomous path, never HITL, regardless of the request body.
		assert.False(t, receivedPayload.Interactive,
			"autonomous card must trigger non-interactive regardless of request body")
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
			receivedPayload backend.TriggerPayload
		)

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			triggerCount++
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		defer mockBackend.Close()

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
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
		assert.Equal(t, 1, triggerCount, "backend should be triggered exactly once")
	})
}

// TestPromoteCard_RecursionGuard verifies that when the backend's /promote handler
// calls back into CM's /promote endpoint (simulating the original infinite-recursion
// bug), the idempotency guard on CM's side short-circuits on the second call and
// does NOT forward the webhook again.
//
// Without the guard this test would spin up goroutines indefinitely until the
// 2-second client deadline fires; with the guard the top-level call returns 200
// and the fake backend receives exactly one POST /promote.
func TestPromoteCard_RecursionGuard(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	// Create a non-autonomous card in the "running" state (ready to be promoted).
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Interactive task for recursion test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	card, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "interactive session")
	require.NoError(t, err)

	// cmURL is set after the CM server starts; the fake backend closure captures the pointer.
	var cmURL atomic.Value

	// promoteCallCount tracks how many times the fake backend's /promote handler fires.
	var promoteCallCount atomic.Int32

	// Fake backend: when it receives POST /promote, it calls back into CM's /promote
	// endpoint synchronously — this reproduces a backend that verifies by re-POSTing.
	fakeBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer fakeBackend.Close()

	backendClient := backend.NewClient(fakeBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
	})

	cmServer := httptest.NewServer(router)
	defer cmServer.Close()

	cmURL.Store(cmServer.URL)

	// Issue the top-level promote with a 2-second deadline.
	// Without the guard the fake backend's callback will call CM again → CM calls the
	// backend again → the fan-out continues until the 2-second deadline fires.
	// With the guard CM sees autonomous==true on the callback and short-circuits.
	topLevelClient := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("POST",
		cmServer.URL+"/api/projects/test-project/cards/"+card.ID+"/promote", nil)
	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:tester")

	resp, err := topLevelClient.Do(req)

	require.NoError(t, err, "top-level promote must not time out (guard must short-circuit the callback)")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode, "top-level promote must return 202")

	// The fake backend must have been called exactly once — the callback from the backend
	// must NOT have triggered a second outbound webhook from CM.
	assert.Equal(t, int32(1), promoteCallCount.Load(),
		"fake backend must receive exactly one POST /promote; guard must block the recursive call")
}

// TestRunCard_ModelInPayload verifies that the model field in TriggerPayload
// is the backend entry's default_model (per-card pin overrides are resolved
// agent-side). A non-default value is used so the test proves the config is
// threaded through rather than matching defaults by accident.
func TestRunCard_ModelInPayload(t *testing.T) {
	ctx := context.Background()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	var capturedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey:       "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
			DefaultModel: "deepseek/deepseek-v4-flash",
		},
	})

	server := httptest.NewServer(router)
	defer server.Close()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Model card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "deepseek/deepseek-v4-flash", capturedPayload.Model,
		"trigger model must be the backend entry's default_model")
}

// --- GET /api/v1/cards/{project}/{id}/autonomous ---
//
// The agent backend calls this during /promote to fail-closed confirm the
// card's autonomous flag. HMAC-signed GET only.

const testBackendAPIKey = "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"

func setupAutonomousEndpoint(t *testing.T, autonomous bool) (*httptest.Server, string, func()) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Promote target", Type: "task", Priority: "medium", Autonomous: autonomous,
	})
	require.NoError(t, err)

	backendClient := backend.NewClient("http://localhost:9090", testBackendAPIKey)
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: testBackendAPIKey},
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
			sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

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
	sig, _ := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

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
	staleSig := signHMACAt(t, testBackendAPIKey, http.MethodGet, path, nil, staleTs)

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
	// backend must HMAC-sign this endpoint.
	server, cardID, cleanup := setupAutonomousEndpoint(t, true)
	defer cleanup()

	req, _ := http.NewRequest("GET", server.URL+"/api/v1/cards/test-project/"+cardID+"/autonomous", nil)
	req.Header.Set("Authorization", "Bearer "+testBackendAPIKey)

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
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetCardAutonomous_BackendDisabled(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "Promote target", Type: "task", Priority: "medium", Autonomous: true,
	})
	require.NoError(t, err)

	// Backend intentionally nil — route must not be registered.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/v1/cards/test-project/" + card.ID + "/autonomous"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

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

	triggerCard := func(t *testing.T, svc *service.CardService, bus *events.Bus, cardID string) backend.TriggerPayload {
		t.Helper()

		var capturedPayload backend.TriggerPayload

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		t.Cleanup(mockBackend.Close)

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{
				APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
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

// --- SelectionContext assembly for agent backend ---

type stubCatalog struct {
	candidates []protocol.CandidateModel
}

func (s *stubCatalog) Candidates(_ context.Context) []protocol.CandidateModel {
	return s.candidates
}

type stubBlacklist struct {
	slugs []string
}

func (s *stubBlacklist) BlacklistedSlugs(_ context.Context) ([]string, error) {
	return s.slugs, nil
}

type stubOutcomeStats struct {
	stats []sqlite.OutcomeStats
	err   error
}

func (s *stubOutcomeStats) ModelOutcomeStats(_ context.Context) ([]sqlite.OutcomeStats, error) {
	return s.stats, s.err
}

func TestRunCardAttachesSelectionForAgentBackend(t *testing.T) {
	const (
		candidateSlug   = "z-ai/glm-5.2"
		projectFavSlug  = "anthropic/claude-opus-4.8"
		blacklistedSlug = "bad/model"
	)

	// Board config carries a project-level favorites block so the
	// project-override merge runs end-to-end through runCard (the handler reads
	// ProjectConfig.Favorites via GetProject at trigger time).
	const boardConfigWithProjectFavorites = `name: test-project
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
favorites:
  critical:
    reviewer: ["anthropic/claude-opus-4.8"]
`

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigWithProjectFavorites)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Agent task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	cat := &stubCatalog{
		candidates: []protocol.CandidateModel{
			{Slug: candidateSlug, CoderPrior: 0.9, ReviewerPrior: 0.8},
		},
	}
	bl := &stubBlacklist{slugs: []string{blacklistedSlug}}

	// Global favorites live on the backend config; the "critical" tier is
	// supplied only by the project config above, so a project-originated rule
	// in the captured payload proves the merge ran both sources.
	globalFavs := map[string]board.TierFavorites{
		"complex": {All: []string{candidateSlug}},
	}

	var capturedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey:       "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
			DefaultModel: "openrouter/auto",
			Favorites:    globalFavs,
		},
		Catalog:   cat,
		Blacklist: bl,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Selection must be present for agent backend.
	require.NotNil(t, capturedPayload.Selection, "Selection must be non-nil for agent backend")

	// Candidates must contain the stub candidate.
	require.Len(t, capturedPayload.Selection.Candidates, 1)
	assert.Equal(t, candidateSlug, capturedPayload.Selection.Candidates[0].Slug)

	// Blacklist must contain the stub slug.
	assert.Contains(t, capturedPayload.Selection.Blacklist, blacklistedSlug)

	// The merged favorites must include both the global (complex/all) rule and
	// the project-originated (critical/reviewer) rule, proving runCard merges
	// backend + project config end-to-end.
	var foundGlobalComplexAll, foundProjectCriticalReviewer bool

	for _, fr := range capturedPayload.Selection.Favorites {
		switch {
		case fr.Tier == "complex" && fr.Role == "":
			assert.Contains(t, fr.Models, candidateSlug)

			foundGlobalComplexAll = true
		case fr.Tier == "critical" && fr.Role == "reviewer":
			assert.Contains(t, fr.Models, projectFavSlug)

			foundProjectCriticalReviewer = true
		}
	}

	assert.True(t, foundGlobalComplexAll, "global complex/all favorite rule must be present")
	assert.True(t, foundProjectCriticalReviewer, "project critical/reviewer favorite rule must be present")
}

// TestRunCardTypedNilCatalogDoesNotPanic reproduces the typed-nil-interface
// footgun: a nil *modelcatalog.Builder assigned to RouterConfig.Catalog
// produces a non-nil catalogProvider interface value, so the h.catalog != nil
// guard in runCard is TRUE and Candidates is called on a nil receiver →
// mutex lock on nil → panic. The test boxes the typed-nil deliberately, then
// drives runCard and asserts no panic + 202 Accepted.
func TestRunCardTypedNilCatalogDoesNotPanic(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Agent task typed-nil", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	var capturedPayload backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	defer mockBackend.Close()

	// Box a typed nil exactly as main.go did: var catalogBuilder *modelcatalog.Builder
	// is left nil when no AA key is configured, then passed to RouterConfig.Catalog.
	// This creates a non-nil interface wrapping a nil pointer — the h.catalog != nil
	// guard passes, and Builder.Candidates panics on b.mu.Lock() without the fix.
	var nilBuilder *modelcatalog.Builder

	var typedNilCatalog catalogProvider = nilBuilder // boxes the typed nil

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{
			APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
		},
		Catalog: typedNilCatalog,
	})

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest("POST",
		server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	// Must not panic; must complete successfully.
	assert.Equal(t, http.StatusAccepted, resp.StatusCode,
		"typed-nil catalog must not panic and run must succeed")
}

// --- best_of_n trigger payload ---

// TestRunCardBestOfNPayload covers the clamp behavior for the agent backend:
// a card value under the configured max passes through unchanged, a value
// over the max is clamped down, and zero (best-of-n disabled) stays zero.
func TestRunCardBestOfNPayload(t *testing.T) {
	cases := []struct {
		name    string
		cardBoN int
		want    int
	}{
		{"below max passes through unchanged", 3, 3},
		{"above max clamps to config max", 9, 5},
		{"zero stays zero (best-of-n disabled)", 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
			defer cleanup()

			ctx := context.Background()

			card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
				Title: "Best-of-N task", Type: "task", Priority: "medium",
			})
			require.NoError(t, err)

			// Set card.BestOfN directly at the service layer — bypassing the
			// REST PATCH endpoint's 2..max_candidates validation — so this test
			// can exercise runCard's own clamp against a stored value that is
			// (deliberately, for the clamp case) already above the configured max.
			if tc.cardBoN != 0 {
				n := tc.cardBoN
				_, err = svc.PatchCard(ctx, "test-project", card.ID, service.PatchCardInput{BestOfN: &n})
				require.NoError(t, err)
			}

			var capturedPayload backend.TriggerPayload

			mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&capturedPayload)

				writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
			}))
			defer mockBackend.Close()

			backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
			router := NewRouter(RouterConfig{
				Service: svc, Bus: bus, Backend: backendClient,
				AgentBackendCfg: &config.AgentBackendConfig{
					APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
				},
				BestOfN: config.BestOfNConfig{MaxCandidates: 5},
			})

			server := httptest.NewServer(router)
			defer server.Close()

			req, _ := http.NewRequest("POST",
				server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

			resp, err := http.DefaultClient.Do(req)

			require.NoError(t, err)
			defer closeBody(t, resp.Body)

			require.Equal(t, http.StatusAccepted, resp.StatusCode)
			assert.Equal(t, tc.want, capturedPayload.BestOfN)
		})
	}
}

// --- Selection outcome stats ---

// TestRunCardSelectionCarriesOutcomeStats covers OutcomeFloor + per-candidate
// Outcomes attachment for the agent backend, plus the best-effort behavior
// (mirroring the blacklist read) when the stats read fails.
func TestRunCardSelectionCarriesOutcomeStats(t *testing.T) {
	const candidateSlug = "z-ai/glm-5.2"

	newRouterFor := func(t *testing.T, oc outcomeStatsReader) (*board.Card, *http.Client, string, *backend.TriggerPayload, *stubCatalog) {
		t.Helper()

		svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
		t.Cleanup(cleanup)

		ctx := context.Background()

		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Outcome stats task", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		cat := &stubCatalog{
			candidates: []protocol.CandidateModel{
				{Slug: candidateSlug, CoderPrior: 0.9, ReviewerPrior: 0.8},
			},
		}

		capturedPayload := &backend.TriggerPayload{}

		mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(capturedPayload)

			writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
		}))
		t.Cleanup(mockBackend.Close)

		backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
		router := NewRouter(RouterConfig{
			Service: svc, Bus: bus, Backend: backendClient,
			AgentBackendCfg: &config.AgentBackendConfig{
				APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj",
			},
			Catalog:  cat,
			Outcomes: oc,
			BestOfN:  config.BestOfNConfig{MaxCandidates: 5, OutcomeFloor: 20},
		})

		server := httptest.NewServer(router)
		t.Cleanup(server.Close)

		return card, http.DefaultClient, server.URL, capturedPayload, cat
	}

	t.Run("attaches per-candidate outcomes and floor", func(t *testing.T) {
		oc := &stubOutcomeStats{
			stats: []sqlite.OutcomeStats{
				{Model: candidateSlug, Samples: 21, Wins: 9, ExpectedWins: 7.0},
			},
		}

		card, client, serverURL, capturedPayload, _ := newRouterFor(t, oc)

		req, _ := http.NewRequest("POST", serverURL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

		resp, err := client.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		require.NotNil(t, capturedPayload.Selection)
		assert.Equal(t, 20, capturedPayload.Selection.OutcomeFloor)
		require.Len(t, capturedPayload.Selection.Candidates, 1)
		require.NotNil(t, capturedPayload.Selection.Candidates[0].Outcomes)
		assert.Equal(t, 21, capturedPayload.Selection.Candidates[0].Outcomes.Samples)
		assert.Equal(t, 9, capturedPayload.Selection.Candidates[0].Outcomes.Wins)
		assert.InDelta(t, 7.0, capturedPayload.Selection.Candidates[0].Outcomes.ExpectedWins, 0.0001)
	})

	t.Run("does not mutate the shared catalog cache when attaching outcomes", func(t *testing.T) {
		oc := &stubOutcomeStats{
			stats: []sqlite.OutcomeStats{
				{Model: candidateSlug, Samples: 21, Wins: 9, ExpectedWins: 7.0},
			},
		}

		card, client, serverURL, capturedPayload, cat := newRouterFor(t, oc)

		req, _ := http.NewRequest("POST", serverURL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

		resp, err := client.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		require.NotNil(t, capturedPayload.Selection)
		require.Len(t, capturedPayload.Selection.Candidates, 1)
		require.NotNil(t, capturedPayload.Selection.Candidates[0].Outcomes,
			"sanity: outcomes must actually be attached to the outgoing payload")

		// stubCatalog.Candidates returns its own candidates field directly (the
		// same backing array on every call, exactly like the real
		// modelcatalog.Builder). If runCard writes Outcomes onto that array in
		// place instead of a copy, this observes the mutation through the
		// catalog's own handle - proving the payload aliased the shared cache.
		require.Len(t, cat.candidates, 1)
		assert.Nil(t, cat.candidates[0].Outcomes,
			"runCard must not write through to the catalog's shared candidate slice")
	})

	t.Run("stats read error: selection still attached, outcomes nil", func(t *testing.T) {
		oc := &stubOutcomeStats{err: errors.New("stats store unavailable")}

		card, client, serverURL, capturedPayload, _ := newRouterFor(t, oc)

		req, _ := http.NewRequest("POST", serverURL+"/api/projects/test-project/cards/"+card.ID+"/run", nil)

		resp, err := client.Do(req)

		require.NoError(t, err)
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusAccepted, resp.StatusCode,
			"a stats read failure must not block the trigger")
		require.NotNil(t, capturedPayload.Selection, "selection must still be attached on stats read error")
		require.Len(t, capturedPayload.Selection.Candidates, 1)
		assert.Nil(t, capturedPayload.Selection.Candidates[0].Outcomes,
			"outcomes must be nil on stats read error (best-effort, like blacklist)")
	})
}

func TestMergeFavorites(t *testing.T) {
	t.Run("empty inputs", func(t *testing.T) {
		rules := mergeFavorites(nil, nil)
		assert.Empty(t, rules)
	})

	t.Run("global only", func(t *testing.T) {
		global := map[string]board.TierFavorites{
			"complex": {All: []string{"a/b"}},
		}
		rules := mergeFavorites(global, nil)
		require.Len(t, rules, 1)
		assert.Equal(t, "complex", rules[0].Tier)
		assert.Equal(t, []string{"a/b"}, rules[0].Models)
	})

	t.Run("project overrides global for same tier", func(t *testing.T) {
		global := map[string]board.TierFavorites{
			"complex": {All: []string{"global/model"}},
		}
		project := map[string]board.TierFavorites{
			"complex": {All: []string{"project/model"}},
		}
		rules := mergeFavorites(global, project)
		require.Len(t, rules, 1)
		assert.Equal(t, []string{"project/model"}, rules[0].Models)
	})

	t.Run("project adds tiers not in global", func(t *testing.T) {
		global := map[string]board.TierFavorites{
			"simple": {All: []string{"a/b"}},
		}
		project := map[string]board.TierFavorites{
			"critical": {All: []string{"c/d"}},
		}
		rules := mergeFavorites(global, project)
		assert.Len(t, rules, 2)
	})

	t.Run("by-role produces separate rules", func(t *testing.T) {
		global := map[string]board.TierFavorites{
			"complex": {
				ByRole: map[string][]string{
					"coder":    {"x/y"},
					"reviewer": {"p/q"},
				},
			},
		}
		rules := mergeFavorites(global, nil)
		assert.Len(t, rules, 2)

		for _, fr := range rules {
			assert.Equal(t, "complex", fr.Tier)
			assert.NotEmpty(t, fr.Role)
		}
	})
}

// --- GET /api/agent/git-credentials ---
//
// Long runs outlive ~1h GitHub App installation tokens; the backend calls this
// mid-run to re-mint a fresh project-scoped git token. HMAC-signed like every
// backend callback, and gated on the card actually running — no free token
// faucet. Unlike task-skills-source (best-effort, instance-scoped), this is
// fail-closed on a broken project binding: never falls back to the instance
// credential.

// setupGitCredentialsEndpoint creates a card in project "test-project",
// optionally sets its runner_status, and wires providerForProject. An empty
// workerStatus leaves the card in its just-created state (RunnerStatus ""),
// exercising the not-running rejection path.
func setupGitCredentialsEndpoint(
	t *testing.T,
	workerStatus string,
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error),
) (server *httptest.Server, cardID string, cleanup func()) {
	t.Helper()

	svc, bus, svcCleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Long runner", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	if workerStatus != "" {
		_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, workerStatus, "test setup")
		require.NoError(t, err)
	}

	backendClient := backend.NewClient("http://localhost:9090", testBackendAPIKey)
	router := NewRouter(RouterConfig{
		Service:            svc,
		Bus:                bus,
		Backend:            backendClient,
		AgentBackendCfg:    &config.AgentBackendConfig{APIKey: testBackendAPIKey},
		ProviderForProject: providerForProject,
	})

	srv := httptest.NewServer(router)

	return srv, card.ID, func() {
		srv.Close()
		svcCleanup()
	}
}

func TestGetGitCredentials_RunningCard_ReturnsFreshToken(t *testing.T) {
	fakeExpiry := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	fakeProvider := &fakeTokenProvider{token: "ghs_refreshed", expiresAt: fakeExpiry}

	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running",
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "https://api.github.com", nil
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ghs_refreshed", body["token"])
	assert.Equal(t, fakeExpiry.UTC().Format(time.RFC3339), body["expires_at"])
}

func TestGetGitCredentials_PATZeroExpiry_ExpiresAtOmitted(t *testing.T) {
	fakeProvider := &fakeTokenProvider{token: "pat-token", expiresAt: time.Time{}}

	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running",
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "pat-token", body["token"])
	_, hasExpiry := body["expires_at"]
	assert.False(t, hasExpiry, "zero-value expiry must be omitted, never formatted as 0001-01-01...")
}

func TestGetGitCredentials_PATSentinelExpiry_ExpiresAtOmitted(t *testing.T) {
	// githubauth's PATProvider reports year 9999, not zero — the far-future
	// sentinel must be omitted from the wire the same way (absent = "do not
	// schedule a refresh", the PAT semantic).
	sentinel := time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
	fakeProvider := &fakeTokenProvider{token: "pat-token", expiresAt: sentinel}

	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running",
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "pat-token", body["token"])
	_, hasExpiry := body["expires_at"]
	assert.False(t, hasExpiry, "year-9999 sentinel expiry must be omitted like zero")
}

func TestGetGitCredentials_NotRunning_Conflict(t *testing.T) {
	fakeProvider := &fakeTokenProvider{token: "ghs_unused"}

	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "", // never started; RunnerStatus stays ""
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
}

func TestGetGitCredentials_UnknownCard_NotFound(t *testing.T) {
	server, _, cleanup := setupGitCredentialsEndpoint(t, "running", nil)
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=TEST-999"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeCardNotFound, apiErr.Code)
}

func TestGetGitCredentials_BadSignature_Forbidden(t *testing.T) {
	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running", nil)
	defer cleanup()

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, _ := http.NewRequest("GET",
		server.URL+"/api/agent/git-credentials?project=test-project&card_id="+cardID, nil)
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

func TestGetGitCredentials_Unsigned_Forbidden(t *testing.T) {
	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running", nil)
	defer cleanup()

	req, _ := http.NewRequest("GET",
		server.URL+"/api/agent/git-credentials?project=test-project&card_id="+cardID, nil)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestGetGitCredentials_BrokenBinding_Conflict is the fail-closed guarantee:
// a broken/unresolvable project credential binding rejects with 409 and never
// falls back to an instance credential. No GitHubTokenProvider is configured
// on this router at all — a silent fallback would show up as 200, not 409.
func TestGetGitCredentials_BrokenBinding_Conflict(t *testing.T) {
	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running",
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return nil, "", auth.ErrCredentialUnavailable
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeValidationError, apiErr.Code)
	assert.Equal(t, "project credential unavailable", apiErr.Error)
}

func TestGetGitCredentials_GenerateTokenFails_BadGateway(t *testing.T) {
	fakeProvider := &fakeTokenProvider{err: errors.New("request token: github api returned status 401")}

	server, cardID, cleanup := setupGitCredentialsEndpoint(t, "running",
		func(_ context.Context, _ string) (githubauth.TokenGenerator, string, error) {
			return fakeProvider, "", nil
		})
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + cardID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeInternalError, apiErr.Code)
}

func TestGetGitCredentials_MissingParams_BadRequest(t *testing.T) {
	server, _, cleanup := setupGitCredentialsEndpoint(t, "running", nil)
	defer cleanup()

	path := "/api/agent/git-credentials?project=test-project"
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBadRequest, apiErr.Code)
}

func TestGetGitCredentials_BackendDisabled_NotFound(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExecEnabled)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title: "No runner", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Backend intentionally nil — route must not be registered.
	router := NewRouter(RouterConfig{Service: svc, Bus: bus})

	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/agent/git-credentials?project=test-project&card_id=" + card.ID
	sig, ts := protocol.SignRequestHeaders(testBackendAPIKey, http.MethodGet, path, nil)

	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
