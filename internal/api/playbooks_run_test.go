package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/playbookrun"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runnablePlaybookServer builds a router with a capture backend and a
// playbook runner, plus one runnable playbook holding one todo card.
func runnablePlaybookServer(t *testing.T) (*httptest.Server, *captureBackend, *service.CardService, *service.PlaybookService, *board.Card) {
	t.Helper()

	return runnablePlaybookServerWith(t, func(svc *service.CardService) playbookrun.Cards { return svc })
}

// runnablePlaybookServerCancellable is runnablePlaybookServer with the
// runner started on a context the test cancels, to drive a shutdown.
func runnablePlaybookServerCancellable(t *testing.T) (
	*httptest.Server, *captureBackend, *service.CardService, *playbookrun.Runner, *board.Card, context.CancelFunc,
) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	var runner *playbookrun.Runner

	server, cb, svc, _, card := runnablePlaybookServerOpts(ctx, t, func(svc *service.CardService) playbookrun.Cards { return svc }, &runner)

	return server, cb, svc, runner, card, cancel
}

// runnablePlaybookServerWith is runnablePlaybookServer with the runner's
// card reader built by wrap.
func runnablePlaybookServerWith(
	t *testing.T, wrap func(*service.CardService) playbookrun.Cards,
) (*httptest.Server, *captureBackend, *service.CardService, *service.PlaybookService, *board.Card) {
	t.Helper()

	return runnablePlaybookServerOpts(t.Context(), t, wrap, nil)
}

// runnablePlaybookServerOpts starts the runner on startCtx and, when out is
// non-nil, hands the runner back through it.
func runnablePlaybookServerOpts(
	startCtx context.Context, t *testing.T, wrap func(*service.CardService) playbookrun.Cards, out **playbookrun.Runner,
) (*httptest.Server, *captureBackend, *service.CardService, *service.PlaybookService, *board.Card) {
	t.Helper()

	svc, pbSvc, bus, _ := playbookTestSetup(t)
	cb := newCaptureBackend(t)

	runner := playbookrun.New(playbookrun.Config{
		Playbooks: pbSvc, Lister: playbookStoreOf(t, pbSvc), Cards: wrap(svc), Bus: bus,
		Clock: clock.Fake(time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)), Instance: "", Tick: time.Hour,
	})

	router := NewRouter(RouterConfig{
		Service: svc, Bus: bus, Playbooks: pbSvc, PlaybookRunner: runner,
		Backend: cb.client(), AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"}, MCPAPIKey: "test-mcp-key",
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// Start after the router, which wires the launcher and stopper. Until it
	// is called the runner's walker parent is already cancelled, so a walker
	// a Play spawned would exit before launching anything. t.Context() is
	// cancelled just before the cleanups, so the Shutdown below joins them.
	runner.Start(startCtx)
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

	if out != nil {
		*out = runner
	}

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "First", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	_, err = pbSvc.Create(ctx, service.CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []service.PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "test-project", Card: card.ID}},
	})
	require.NoError(t, err)

	runnable := true
	base := "main"
	_, err = pbSvc.UpdateMeta(ctx, "rollout", service.UpdatePlaybookInput{Runnable: &runnable, BaseBranch: &base}, "human:alice")
	require.NoError(t, err)

	return server, cb, svc, pbSvc, card
}

func TestPlaybooksAPI_PlayTriggersTheFirstCard(t *testing.T) {
	server, cb, svc, _, card := runnablePlaybookServer(t)

	t.Run("agents cannot play", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "agent-1")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var detail service.PlaybookDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	require.NotNil(t, detail.Run)
	assert.Equal(t, board.RunStatusRunning, detail.Run.Status)
	assert.Equal(t, "human:alice", detail.Run.StartedBy)

	// The walker launches the first card with the playbook branch options.
	require.Eventually(t, func() bool {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		return len(cb.triggers) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	cb.mu.Lock()
	assert.Len(t, cb.triggers, 1, "the first card is triggered exactly once")
	cb.mu.Unlock()

	p := cb.lastTrigger(t)
	assert.Equal(t, card.ID, p.CardID)
	assert.Equal(t, "playbook/rollout", p.BaseBranch)
	assert.True(t, p.CreateBaseBranch)
	assert.Equal(t, "main", p.BaseBranchFrom)

	got, err := svc.GetCard(context.Background(), "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", got.WorkerStatus)
	assert.True(t, got.HasPlaybookSettings("playbook/rollout"))

	t.Run("second play is refused", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookRunActive, apiErr.Code)
	})

	t.Run("hand run on the queued card is refused by the lock", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/projects/test-project/cards/"+card.ID+"/run", nil, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookRunActive, apiErr.Code, "the lock answers before the worker-conflict guard")
	})

	t.Run("stop kills the worker and stops the run", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/stop", nil, "human:alice")
		defer closeBody(t, resp.Body)

		require.Equal(t, http.StatusAccepted, resp.StatusCode)

		var detail service.PlaybookDetail
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
		assert.Equal(t, board.RunStatusStopped, detail.Run.Status)

		cb.mu.Lock()
		kills := len(cb.kills)
		cb.mu.Unlock()
		assert.Equal(t, 1, kills)

		got, err := svc.GetCard(context.Background(), "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, "killed", got.WorkerStatus)
	})

	t.Run("stop on an inactive run is 409", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/stop", nil, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusConflict, resp.StatusCode)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookRunInactive, apiErr.Code)
	})
}

func TestPlaybooksAPI_RunAndStopOnUnknownPlaybookAre404(t *testing.T) {
	server, _, _, _, _ := runnablePlaybookServer(t)

	for _, action := range []string{"run", "stop"} {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/nope/"+action, nil, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode, action)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodePlaybookNotFound, apiErr.Code, action)
	}
}

func TestPlaybooksAPI_RunWithoutRunnerIs503(t *testing.T) {
	svc, pbSvc, bus, cleanup := playbookTestSetup(t)
	defer cleanup()

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc})

	server := httptest.NewServer(router)
	defer server.Close()

	for _, action := range []string{"run", "stop"} {
		resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/"+action, nil, "human:alice")
		defer closeBody(t, resp.Body)

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, action)

		var apiErr APIError
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
		assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code, action)
	}
}

func TestPlaybooksAPI_StopAnswers502WhenTheKillFails(t *testing.T) {
	server, cb, _, pbSvc, _ := runnablePlaybookServer(t)

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	closeBody(t, resp.Body)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	require.Eventually(t, func() bool {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		return len(cb.triggers) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	cb.mu.Lock()
	cb.failKills = true
	cb.mu.Unlock()

	resp = doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/stop", nil, "human:alice")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBackendUnavailable, apiErr.Code)

	detail, err := pbSvc.Get(context.Background(), "rollout")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusStopped, detail.Run.Status, "the run is stopped even though the kill failed")
}

func TestPlaybooksAPI_PlayWithoutBackendIs503(t *testing.T) {
	svc, pbSvc, bus, _ := playbookTestSetup(t)
	runner := playbookrun.New(playbookrun.Config{Playbooks: pbSvc, Lister: playbookStoreOf(t, pbSvc), Cards: svc, Bus: bus, Tick: time.Hour})
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc, PlaybookRunner: runner})

	server := httptest.NewServer(router)
	defer server.Close()

	ctx := context.Background()
	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "First", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = pbSvc.Create(ctx, service.CreatePlaybookInput{
		Title: "Rollout", AgentID: "human:alice",
		Entries: []service.PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "test-project", Card: card.ID}},
	})
	require.NoError(t, err)

	runnable := true
	_, err = pbSvc.UpdateMeta(ctx, "rollout", service.UpdatePlaybookInput{Runnable: &runnable}, "human:alice")
	require.NoError(t, err)

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeBackendDisabled, apiErr.Code)
}

func TestPlaybooksAPI_PlayOnNotRunnableIs409(t *testing.T) {
	svc, pbSvc, bus, _ := playbookTestSetup(t)
	runner := playbookrun.New(playbookrun.Config{Playbooks: pbSvc, Lister: playbookStoreOf(t, pbSvc), Cards: svc, Bus: bus, Tick: time.Hour})
	router := NewRouter(RouterConfig{Service: svc, Bus: bus, Playbooks: pbSvc, PlaybookRunner: runner})

	server := httptest.NewServer(router)
	defer server.Close()

	_, err := pbSvc.Create(context.Background(), service.CreatePlaybookInput{
		Title: "Plain", AgentID: "human:alice",
		Entries: []service.PlaybookEntryInput{{Type: board.EntryTypeManual, Text: "deploy"}},
	})
	require.NoError(t, err)

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/plain/run", nil, "human:alice")
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodePlaybookNotRunnable, apiErr.Code)
}

func TestPlaybooksAPI_DeleteDuringRunIs409(t *testing.T) {
	server, _, _, pbSvc, _ := runnablePlaybookServer(t)

	play := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	closeBody(t, play.Body)
	require.Equal(t, http.StatusAccepted, play.StatusCode)

	resp := doJSON(t, http.MethodDelete, server.URL+"/api/playbooks/rollout", nil, "human:alice")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusConflict, resp.StatusCode)

	var apiErr APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodePlaybookRunActive, apiErr.Code)

	_, err := pbSvc.Get(context.Background(), "rollout")
	require.NoError(t, err, "the playbook is still there")
}

// hookCards is the runner's card reader with a one-shot hook that runs right
// after a read of one card, so a test can act between that read and the
// decision made from it.
type hookCards struct {
	*service.CardService

	mu    sync.Mutex
	card  string
	after func()
}

func (h *hookCards) arm(card string, after func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.card, h.after = card, after
}

func (h *hookCards) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	card, err := h.CardService.GetCard(ctx, project, id)

	h.mu.Lock()

	var hook func()
	if h.after != nil && strings.EqualFold(id, h.card) {
		hook, h.after = h.after, nil
	}

	h.mu.Unlock()

	if hook != nil {
		hook()
	}

	return card, err
}

// postAsHuman posts with no body as human:bob without the require helpers,
// so a goroutine can call it; it returns the status and the error code.
func postAsHuman(url string) (int, string) {
	req, err := http.NewRequest(http.MethodPost, url, http.NoBody)
	if err != nil {
		return 0, err.Error()
	}

	req.Header.Set("X-Requested-With", "contextmatrix")
	req.Header.Set("X-Agent-ID", "human:bob")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()

	var apiErr APIError

	_ = json.NewDecoder(resp.Body).Decode(&apiErr)

	return resp.StatusCode, apiErr.Code
}

// addSecondEntry adds a second todo card to the rollout playbook.
func addSecondEntry(t *testing.T, svc *service.CardService, pbSvc *service.PlaybookService) *board.Card {
	t.Helper()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "Second", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	_, err = pbSvc.AddEntry(ctx, "rollout", service.PlaybookEntryInput{Type: board.EntryTypeCard, Project: "test-project", Card: card.ID}, "human:alice")
	require.NoError(t, err)

	return card
}

// runEntryCard is the card of the rollout run's current entry, or "".
func runEntryCard(ctx context.Context, pbSvc *service.PlaybookService) string {
	d, err := pbSvc.Get(ctx, "rollout")
	if err != nil || d.Run == nil {
		return ""
	}

	for _, e := range d.Entries {
		if e.ID == d.Run.Entry {
			return e.Card
		}
	}

	return ""
}

// requireOneWorkerOnTheRunEntry waits for the run to name a card with a live
// worker, then checks no other card has one and exactly one trigger went out.
func requireOneWorkerOnTheRunEntry(t *testing.T, cb *captureBackend, svc *service.CardService, pbSvc *service.PlaybookService, ids ...string) {
	t.Helper()

	ctx := context.Background()
	isLive := func(id string) bool {
		c, err := svc.GetCard(ctx, "test-project", id)

		return err == nil && (c.WorkerStatus == "queued" || c.WorkerStatus == "running")
	}
	triggers := func() int {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		return len(cb.triggers)
	}

	require.Eventually(t, func() bool {
		current := runEntryCard(ctx, pbSvc)

		return current != "" && isLive(current) && triggers() == 1
	}, 2*time.Second, 10*time.Millisecond)

	assert.Never(t, func() bool { return triggers() > 1 }, 100*time.Millisecond, 10*time.Millisecond, "a second trigger")

	current := runEntryCard(ctx, pbSvc)

	for _, id := range ids {
		if id != current {
			assert.False(t, isLive(id), "%s runs beside the run's card %s", id, current)
		}
	}
}

// TestPlaybooksAPI_HandRunRacingPlayIsRefused: a hand run of the second
// card reaches its queued write while Play is deciding where the run starts.
// The hand run waits on Play's write and is refused; the run starts the first
// card, and one worker runs.
func TestPlaybooksAPI_HandRunRacingPlayIsRefused(t *testing.T) {
	hc := &hookCards{}
	server, cb, svc, pbSvc, first := runnablePlaybookServerWith(t, func(s *service.CardService) playbookrun.Cards {
		hc.CardService = s

		return hc
	})
	second := addSecondEntry(t, svc, pbSvc)

	type result struct {
		status int
		code   string
	}

	handRun := make(chan result, 1)

	hc.arm(second.ID, func() {
		done := make(chan struct{})

		go func() {
			defer close(done)

			status, code := postAsHuman(server.URL + "/api/projects/test-project/cards/" + second.ID + "/run")
			handRun <- result{status, code}
		}()

		// Long enough for an unguarded hand run to finish; a guarded one
		// waits on the playbook lock this read is taken under.
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
	})

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var got result
	select {
	case got = <-handRun:
	case <-time.After(5 * time.Second):
		t.Fatal("the hand run never answered")
	}

	assert.Equal(t, http.StatusConflict, got.status)
	assert.Equal(t, ErrCodePlaybookRunActive, got.code)

	requireOneWorkerOnTheRunEntry(t, cb, svc, pbSvc, first.ID, second.ID)
	assert.Equal(t, first.ID, runEntryCard(context.Background(), pbSvc))
}

// TestPlaybooksAPI_HandRunAndPlayRaceToOneWorker fires Play and a hand run
// of the second card together, many times. Whichever wins, one worker runs
// and the run names it.
func TestPlaybooksAPI_HandRunAndPlayRaceToOneWorker(t *testing.T) {
	for round := range 20 {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			server, cb, svc, pbSvc, first := runnablePlaybookServer(t)
			second := addSecondEntry(t, svc, pbSvc)

			start := make(chan struct{})

			var wg sync.WaitGroup

			for _, url := range []string{
				server.URL + "/api/playbooks/rollout/run",
				server.URL + "/api/projects/test-project/cards/" + second.ID + "/run",
			} {
				wg.Go(func() {
					<-start
					postAsHuman(url)
				})
			}

			close(start)
			wg.Wait()

			requireOneWorkerOnTheRunEntry(t, cb, svc, pbSvc, first.ID, second.ID)
		})
	}
}

// TestPlaybooksAPI_ShutdownMidTriggerRevertsTheCard: a shutdown during a slow
// trigger aborts it, and the launcher reverts the card instead of leaving it
// queued with no worker.
func TestPlaybooksAPI_ShutdownMidTriggerRevertsTheCard(t *testing.T) {
	server, cb, svc, runner, card, cancel := runnablePlaybookServerCancellable(t)

	hold := make(chan struct{})
	entered := make(chan struct{}, 1)

	cb.mu.Lock()
	cb.holdTriggers, cb.triggerEntered = hold, entered
	cb.mu.Unlock()
	t.Cleanup(func() { close(hold) })

	resp := doJSON(t, http.MethodPost, server.URL+"/api/playbooks/rollout/run", nil, "human:alice")
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the trigger never reached the backend")
	}

	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()

	require.NoError(t, runner.Shutdown(sctx))

	got, err := svc.GetCard(context.Background(), "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.WorkerStatus)
}
