package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureBackend is a mock task backend that records every trigger payload.
type captureBackend struct {
	mu        sync.Mutex
	triggers  []protocol.TriggerPayload
	kills     []protocol.KillPayload
	failKills bool
	// killStatus, when non-zero, is the HTTP status every /kill answers with.
	killStatus int
	server     *httptest.Server

	// holdTriggers, when set, parks every /trigger request until it is closed
	// or the request ends; triggerEntered gets a non-blocking send first.
	holdTriggers   chan struct{}
	triggerEntered chan struct{}

	// killDelay delays every /kill response; killEntered gets a non-blocking
	// send first.
	killDelay   time.Duration
	killEntered chan struct{}
}

func newCaptureBackend(t *testing.T) *captureBackend {
	t.Helper()

	cb := &captureBackend{}
	cb.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trigger" {
			cb.mu.Lock()
			hold, entered := cb.holdTriggers, cb.triggerEntered
			cb.mu.Unlock()

			if hold != nil {
				select {
				case entered <- struct{}{}:
				default:
				}

				select {
				case <-hold:
				case <-r.Context().Done():
					return
				}
			}
		}

		if r.URL.Path == "/kill" {
			cb.mu.Lock()
			delay, entered := cb.killDelay, cb.killEntered
			cb.mu.Unlock()

			if entered != nil {
				select {
				case entered <- struct{}{}:
				default:
				}
			}

			time.Sleep(delay)
		}

		cb.mu.Lock()
		defer cb.mu.Unlock()

		switch r.URL.Path {
		case "/trigger":
			var p protocol.TriggerPayload

			_ = json.NewDecoder(r.Body).Decode(&p)
			cb.triggers = append(cb.triggers, p)
		case "/kill":
			if cb.failKills {
				writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Code: "internal", Message: "kill failed"})

				return
			}

			if cb.killStatus != 0 {
				writeJSON(w, cb.killStatus, protocol.ErrorResponse{Code: "not_found", Message: "unknown card"})

				return
			}

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

	got, err := h.launch(ctx, "test-project", card.ID, launchOptions{interactive: true, createBaseBranch: true, baseBranchFrom: "main"})
	require.NoError(t, err)
	assert.Equal(t, "queued", got.WorkerStatus)

	p := cb.lastTrigger(t)
	assert.Equal(t, card.ID, p.CardID)
	assert.Equal(t, "playbook/rollout", p.BaseBranch)
	assert.True(t, p.CreateBaseBranch)
	assert.Equal(t, "main", p.BaseBranchFrom)
	assert.False(t, p.Interactive, "autonomous cards never run interactive")
}

func TestLaunch_NormalRunNeverAsksForABaseBranch(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	p := cb.lastTrigger(t)
	assert.False(t, p.CreateBaseBranch, "only playbook runs ask the worker to create the base branch")
	assert.Empty(t, p.BaseBranchFrom)
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

// TestStop_BackendNotKnowingTheCardRecordsKilled covers a /kill 404 on a card
// whose worker is queued or running: the stop succeeds, the worker is
// recorded as killed, the activity log says so, and the claim is cleared.
func TestStop_BackendNotKnowingTheCardRecordsKilled(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
			defer cleanup()

			ctx := context.Background()
			card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
			require.NoError(t, err)

			cb := newCaptureBackend(t)
			h := newLaunchHandlers(t, svc, cb)

			_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
			require.NoError(t, err)

			_, err = svc.ClaimCard(ctx, "test-project", card.ID, "claude-1")
			require.NoError(t, err)

			if status == "running" {
				_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "running")
				require.NoError(t, err)
			}

			cb.mu.Lock()
			cb.killStatus = http.StatusNotFound
			cb.mu.Unlock()

			got, err := h.stop(ctx, "test-project", card.ID)
			require.NoError(t, err)
			assert.Equal(t, "killed", got.WorkerStatus)
			assert.Empty(t, got.AssignedAgent, "the claim is cleared")

			stored, err := svc.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)
			assert.Equal(t, "killed", stored.WorkerStatus)
			require.NotEmpty(t, stored.ActivityLog)
			assert.Equal(t, "worker_status", stored.ActivityLog[len(stored.ActivityLog)-1].Action)
		})
	}
}

// TestStop_KillFailureOtherThanNotFoundKeepsStatus covers a /kill 5xx: the
// stop is a 502 and the worker status is left as it was.
func TestStop_KillFailureOtherThanNotFoundKeepsStatus(t *testing.T) {
	origBackoff := backend.BackoffBase
	backend.BackoffBase = time.Millisecond

	t.Cleanup(func() { backend.BackoffBase = origBackoff })

	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "T", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.NoError(t, err)

	cb.mu.Lock()
	cb.killStatus = http.StatusInternalServerError
	cb.mu.Unlock()

	_, err = h.stop(ctx, "test-project", card.ID)

	lf, ok := errors.AsType[*launchFailure](err)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadGateway, lf.status)
	assert.Equal(t, ErrCodeBackendUnavailable, lf.code)

	stored, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", stored.WorkerStatus)
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

func TestLaunch_RefusesUnvettedCard(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title: "PROJ-1 Imported", Type: "task", Priority: "medium",
		Source: &board.Source{System: "jira", ExternalID: "PROJ-1", ExternalURL: "https://acme.atlassian.net/browse/PROJ-1"},
	})
	require.NoError(t, err)
	require.False(t, card.Vetted)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)

	_, err = h.launch(ctx, "test-project", card.ID, launchOptions{})
	require.Error(t, err)

	lf, ok := errors.AsType[*launchFailure](err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, lf.status)
	assert.Equal(t, ErrCodeCardNotVetted, lf.code)

	cb.mu.Lock()
	assert.Empty(t, cb.triggers, "no trigger is sent")
	cb.mu.Unlock()

	got, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.WorkerStatus, "a refused launch leaves no queued status behind")
}

// TestLaunch_QueuedWriteIsACompareAndSet races two launches of one todo card.
// Both can pass the pre-read check; the queued write decides, so exactly one
// triggers and the other is a WORKER_CONFLICT.
func TestLaunch_QueuedWriteIsACompareAndSet(t *testing.T) {
	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	defer cleanup()

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)
	ctx := context.Background()

	const rounds = 20

	for round := range rounds {
		card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: fmt.Sprintf("race %d", round), Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		start := make(chan struct{})
		errs := make(chan error, 2)

		for range 2 {
			go func() {
				<-start

				_, err := h.launch(ctx, "test-project", card.ID, launchOptions{})
				errs <- err
			}()
		}

		close(start)

		conflicts := 0

		for range 2 {
			if err := <-errs; err != nil {
				lf, ok := errors.AsType[*launchFailure](err)
				require.True(t, ok, "round %d: %v", round, err)
				assert.Equal(t, ErrCodeWorkerConflict, lf.code)

				conflicts++
			}
		}

		assert.Equal(t, 1, conflicts, "round %d: exactly one launch loses", round)
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	assert.Len(t, cb.triggers, rounds, "one trigger per card")
}
