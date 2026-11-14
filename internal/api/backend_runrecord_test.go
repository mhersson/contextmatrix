package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

// recordingRuns is a CardRunRecorder that keeps what launch and stop tell it.
// On MarkHumanStop it reads the card's worker_status through svc, so a test
// sees what the card said at the moment of the call.
type recordingRuns struct {
	svc *service.CardService

	mu          sync.Mutex
	opened      []state.LaunchFacts
	humanStops  []string
	statusAtStp []string
}

func (r *recordingRuns) OpenCardRun(_ context.Context, _, _, _ string, _ time.Time, facts state.LaunchFacts) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.opened = append(r.opened, facts)

	return nil
}

func (r *recordingRuns) RecordRunStatus(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}

func (r *recordingRuns) MarkHumanStop(ctx context.Context, project, cardID string) error {
	card, err := r.svc.GetCard(ctx, project, cardID)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.humanStops = append(r.humanStops, cardID)
	r.statusAtStp = append(r.statusAtStp, card.WorkerStatus)

	return nil
}

func recordingHandlers(t *testing.T) (*backendHandlers, *service.CardService, *captureBackend, *recordingRuns) {
	t.Helper()

	svc, _, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	cb := newCaptureBackend(t)
	h := newLaunchHandlers(t, svc, cb)
	runs := &recordingRuns{svc: svc}
	h.cardRuns = runs

	return h, svc, cb, runs
}

func TestLaunchRecordsOriginAndFacts(t *testing.T) {
	launchers := map[string]struct {
		origin string
		run    func(h *backendHandlers, project, id string) error
	}{
		"rest": {state.OriginREST, func(h *backendHandlers, project, id string) error {
			_, err := h.launch(context.Background(), project, id, launchOptions{origin: state.OriginREST})

			return err
		}},
		"mcp": {state.OriginMCP, func(h *backendHandlers, project, id string) error {
			_, err := h.mcpRunCard(context.Background(), project, id)

			return err
		}},
		"playbook": {state.OriginPlaybook, func(h *backendHandlers, project, id string) error {
			return h.playbookLaunch(context.Background(), project, id, playbookrun.LaunchOptions{})
		}},
	}

	for name, tc := range launchers {
		t.Run(name, func(t *testing.T) {
			h, svc, _, runs := recordingHandlers(t)
			card, err := svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
				Title: "Auto task", Type: "task", Priority: "medium", Autonomous: new(true),
			})
			require.NoError(t, err)

			require.NoError(t, tc.run(h, "test-project", card.ID))

			queued, err := svc.GetCard(context.Background(), "test-project", card.ID)
			require.NoError(t, err)

			require.Len(t, runs.opened, 1)
			assert.Equal(t, tc.origin, runs.opened[0].Origin)
			assert.True(t, runs.opened[0].Autonomous)
			assert.Equal(t, board.TakeEditSnapshot(queued), runs.opened[0].Snapshot)
		})
	}
}

func TestRunCardEndpointRecordsRESTOrigin(t *testing.T) {
	h, svc, _, runs := recordingHandlers(t)
	card := createRunnable(t, svc, service.CreateCardInput{})

	req := httptest.NewRequest(http.MethodPost, "/api/projects/test-project/cards/"+card.ID+"/run", nil)
	req.SetPathValue("project", "test-project")
	req.SetPathValue("id", card.ID)

	rec := httptest.NewRecorder()
	h.runCard(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	require.Len(t, runs.opened, 1)
	assert.Equal(t, state.OriginREST, runs.opened[0].Origin)
}

func TestStopMarksHumanStopBeforeKilled(t *testing.T) {
	h, svc, _, runs := recordingHandlers(t)
	card := createRunnable(t, svc, service.CreateCardInput{})
	ctx := context.Background()

	_, err := h.launch(ctx, "test-project", card.ID, launchOptions{origin: state.OriginREST})
	require.NoError(t, err)

	got, err := h.stop(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "killed", got.WorkerStatus)

	assert.Equal(t, []string{card.ID}, runs.humanStops)
	assert.Equal(t, []string{"queued"}, runs.statusAtStp, "the mark lands before the card reads killed")
}

func TestStopAllMarksHumanStopPerCard(t *testing.T) {
	h, svc, _, runs := recordingHandlers(t)
	ctx := context.Background()

	running := createRunnable(t, svc, service.CreateCardInput{})
	_, err := svc.UpdateWorkerStatus(ctx, "test-project", running.ID, "running", "running")
	require.NoError(t, err)

	idle := createRunnable(t, svc, service.CreateCardInput{})

	req := httptest.NewRequest(http.MethodPost, "/api/projects/test-project/stop-all", nil)
	req.SetPathValue("project", "test-project")

	rec := httptest.NewRecorder()
	h.stopAll(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, []string{running.ID}, runs.humanStops)
	assert.Equal(t, []string{"running"}, runs.statusAtStp)
	assert.NotContains(t, runs.humanStops, idle.ID)
}

func TestLaunchWebhookFailureRecordsNoRun(t *testing.T) {
	h, svc, _, runs := recordingHandlers(t)

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(down.Close)

	h.backend = backend.NewClient(down.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")

	card := createRunnable(t, svc, service.CreateCardInput{})

	_, err := h.launch(context.Background(), "test-project", card.ID, launchOptions{origin: state.OriginREST})
	require.Error(t, err)
	assert.Empty(t, runs.opened)
}
