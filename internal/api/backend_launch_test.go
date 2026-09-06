package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureBackend is a mock task backend that records every trigger payload.
type captureBackend struct {
	mu       sync.Mutex
	triggers []protocol.TriggerPayload
	kills    []protocol.KillPayload
	server   *httptest.Server
}

func newCaptureBackend(t *testing.T) *captureBackend {
	t.Helper()

	cb := &captureBackend{}
	cb.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		switch r.URL.Path {
		case "/trigger":
			var p protocol.TriggerPayload

			_ = json.NewDecoder(r.Body).Decode(&p)
			cb.triggers = append(cb.triggers, p)
		case "/kill":
			var p protocol.KillPayload

			_ = json.NewDecoder(r.Body).Decode(&p)
			cb.kills = append(cb.kills, p)
		}

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	t.Cleanup(cb.server.Close)

	return cb
}

func (cb *captureBackend) lastTrigger(t *testing.T) protocol.TriggerPayload {
	t.Helper()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	require.NotEmpty(t, cb.triggers)

	return cb.triggers[len(cb.triggers)-1]
}

func (cb *captureBackend) client() *backend.Client {
	return backend.NewClient(cb.server.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
}

func newLaunchHandlers(t *testing.T, svc *service.CardService, cb *captureBackend) *backendHandlers {
	t.Helper()

	return &backendHandlers{
		svc:        svc,
		backend:    cb.client(),
		backendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		mcpAPIKey:  "test-mcp-key",
	}
}

func TestLaunch_PlaybookOptionsReachTheTrigger(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "Auto task", Type: "task", Priority: "medium", Autonomous: new(true), BaseBranch: "playbook/rollout",
	})
	require.NoError(t, err)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	got, err := h.launch(ctx, "test-project", card.ID, launchOptions{createBaseBranch: true, baseBranchFrom: "main"})
	require.NoError(t, err)
	assert.Equal(t, "queued", got.WorkerStatus)

	p := cb.lastTrigger(t)
	assert.Equal(t, card.ID, p.CardID)
	assert.Equal(t, "playbook/rollout", p.BaseBranch)
	assert.True(t, p.CreateBaseBranch)
	assert.Equal(t, "main", p.BaseBranchFrom)
	assert.False(t, p.Interactive, "autonomous cards never run interactive")
}

func TestLaunch_RefusalsAreTypedFailures(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	// First launch succeeds and leaves the card queued.
	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	// Second launch is refused: worker in flight.
	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})

	var lf *launchFailure

	require.ErrorAs(t, err, &lf)
	assert.Equal(t, http.StatusConflict, lf.status)
	assert.Equal(t, ErrCodeWorkerConflict, lf.code)
	assert.Contains(t, lf.Error(), "already being executed")

	// No backend: a typed 503.
	h.backend = nil
	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.ErrorAs(t, err, &lf)
	assert.Equal(t, http.StatusServiceUnavailable, lf.status)
	assert.Equal(t, ErrCodeBackendDisabled, lf.code)

	// Unknown card: a plain service error, not a launchFailure.
	h.backend = cb.client()
	_, err = h.launch(ctx, "test-project", "TEST-999", launchOptions{})
	require.Error(t, err)
	assert.NotErrorAs(t, err, &lf)
}

func TestStop_KillsThenMarksKilled(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	_, err = h.stop(ctx, "test-project", card.ID)

	var lf *launchFailure

	require.ErrorAs(t, err, &lf)
	assert.Equal(t, ErrCodeWorkerNotRunning, lf.code)

	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	got, err := h.stop(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "killed", got.WorkerStatus)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	require.Len(t, cb.kills, 1)
	assert.Equal(t, card.ID, cb.kills[0].CardID)
}

// TestPlaybookLaunch_SanitizesServiceErrors pins the reason the playbook
// runner persists: it is committed to the boards repo and rendered in the
// UI, so a raw service error's text (absolute paths, .git paths, remote
// URLs) must never reach it.
func TestPlaybookLaunch_SanitizesServiceErrors(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	// launch's own error for an unknown card is a raw service error.
	_, raw := h.launch(ctx, "test-project", "TEST-999", launchOptions{})
	require.Error(t, raw)

	var lf *launchFailure

	require.NotErrorAs(t, raw, &lf)

	err := h.playbookLaunch(ctx, "test-project", "TEST-999", playbookrun.LaunchOptions{})
	require.Error(t, err)
	assert.NotErrorAs(t, err, &lf)
	assert.Equal(t, sanitizeErrorDetails(raw), err.Error(), "the reason is the sanitized class, never the raw text")
	assert.NotContains(t, err.Error(), "/", "no filesystem path reaches the persisted reason")

	// A typed refusal is already human and sanitized, so it passes through
	// with its status and code intact.
	card, cardErr := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
	require.NoError(t, cardErr)
	require.NoError(t, h.playbookLaunch(ctx, "test-project", card.ID, playbookrun.LaunchOptions{}))

	err = h.playbookLaunch(ctx, "test-project", card.ID, playbookrun.LaunchOptions{})
	require.ErrorAs(t, err, &lf)
	assert.Equal(t, ErrCodeWorkerConflict, lf.code)
}
