package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mcpRunSlotRouter builds the router side of the run_card slot: a mock task
// backend that captures every trigger payload, a RunnerSlot bound by
// NewRouter, and a fresh card. Mirrors runMobTrigger's setup. It returns the
// bound runner, a pointer to the last decoded trigger payload, the card and
// the card service.
func mcpRunSlotRouter(t *testing.T, card service.CreateCardInput) (mcpserver.CardRunner, *backend.TriggerPayload, *board.Card, *service.CardService) {
	t.Helper()

	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	ctx := context.Background()

	cardOut, err := svc.CreateCard(ctx, "test-project", card)
	require.NoError(t, err)

	var received backend.TriggerPayload

	mockBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)

		writeJSON(w, http.StatusOK, protocol.SuccessResponse{OK: true})
	}))
	t.Cleanup(mockBackend.Close)

	backendClient := backend.NewClient(mockBackend.URL, "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj")
	slot := &mcpserver.RunnerSlot{}

	NewRouter(RouterConfig{
		Service: svc, Bus: bus, Backend: backendClient,
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		MCPCardRunner:   slot,
	})

	run := slot.Get()
	require.NotNil(t, run, "NewRouter must bind mcpRunCard into the slot when a backend is configured")

	return run, &received, cardOut, svc
}

func TestMCPRunCard_NoBackendLeavesSlotEmpty(t *testing.T) {
	svc, bus, cleanup := testSetupWithRemoteExecution(t, boardConfigRemoteExec)
	t.Cleanup(cleanup)

	slot := &mcpserver.RunnerSlot{}
	NewRouter(RouterConfig{Service: svc, Bus: bus, Backend: nil, MCPCardRunner: slot})

	assert.Nil(t, slot.Get(), "without a backend the slot must stay unbound")
}

func TestMCPRunCard_NormalisesCardID(t *testing.T) {
	ctx := context.Background()

	run, received, card, _ := mcpRunSlotRouter(t, service.CreateCardInput{
		Title: "MCP run task", Type: "task", Priority: "medium",
	})

	_, err := run(ctx, "test-project", strings.ToLower(card.ID))
	require.NoError(t, err)

	assert.Equal(t, card.ID, received.CardID, "the trigger must carry the uppercase ID that Stop sends")
}

func TestMCPRunCard_PlaybookLockRefused(t *testing.T) {
	ctx := context.Background()

	svc, pbSvc, bus, _ := playbookTestSetup(t)
	cb := newCaptureBackend(t)
	slot := &mcpserver.RunnerSlot{}

	NewRouter(RouterConfig{
		Service: svc, Bus: bus, Playbooks: pbSvc, Backend: cb.client(),
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		MCPCardRunner:   slot,
	})

	card := lockCardInPlaybook(t, svc, pbSvc)

	now := time.Now().UTC()
	_, err := pbSvc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusRunning, StartedAt: now, UpdatedAt: now}, "human:alice")
	require.NoError(t, err)

	run := slot.Get()
	require.NotNil(t, run)

	_, err = run(ctx, "test-project", card.ID)

	lf, ok := errors.AsType[*launchFailure](err)
	require.True(t, ok, "the early lock answer must be a typed launchFailure, got %v", err)
	assert.Equal(t, ErrCodePlaybookRunActive, lf.code)
	assert.Contains(t, lf.message, "rollout")
}

func TestMCPRunCard_TriggersNonInteractive(t *testing.T) {
	ctx := context.Background()

	run, received, card, _ := mcpRunSlotRouter(t, service.CreateCardInput{
		Title: "MCP run task", Type: "task", Priority: "medium", // not autonomous
	})

	_, err := run(ctx, "test-project", card.ID)
	require.NoError(t, err)

	assert.Equal(t, card.ID, received.CardID)
	assert.False(t, received.Interactive, "a run_card trigger is always a non-interactive (Run Auto) run")
}

func TestMCPRunCard_SecondCallRefusedWorkerConflict(t *testing.T) {
	ctx := context.Background()

	run, _, card, _ := mcpRunSlotRouter(t, service.CreateCardInput{
		Title: "MCP run task", Type: "task", Priority: "medium", // not autonomous
	})

	_, err := run(ctx, "test-project", card.ID)
	require.NoError(t, err)

	_, err = run(ctx, "test-project", card.ID)
	require.Error(t, err)

	var lf *launchFailure
	require.ErrorAs(t, err, &lf, "a queued or running card must return a typed launchFailure")
	assert.Contains(t, lf.message, "already being executed by a worker")
}

func TestMCPRunCard_CardNotInTodoRefused(t *testing.T) {
	ctx := context.Background()

	run, _, card, svc := mcpRunSlotRouter(t, service.CreateCardInput{
		Title: "MCP run task", Type: "task", Priority: "medium", // not autonomous
	})

	_, err := svc.TransitionTo(ctx, "test-project", card.ID, "in_progress")
	require.NoError(t, err)

	_, err = run(ctx, "test-project", card.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "card must be in todo state to run")
}
