package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	svc, pbSvc, bus, _ := playbookTestSetup(t)
	cb := newCaptureBackend(t)

	runner := playbookrun.New(playbookrun.Config{
		Playbooks: pbSvc, Lister: playbookStoreOf(t, pbSvc), Cards: svc, Bus: bus,
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
	runner.Start(t.Context())
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })

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
