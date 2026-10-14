package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/service"
)

// mcpRunner builds a router with a capture backend and returns the run_card
// runner NewRouter bound into the slot.
func mcpRunner(t *testing.T) (mcpserver.CardRunner, *captureBackend, *service.CardService, *service.PlaybookService) {
	t.Helper()

	svc, pbSvc, bus, _ := playbookTestSetup(t)
	cb := newCaptureBackend(t)
	slot := &mcpserver.RunnerSlot{}

	NewRouter(RouterConfig{
		Service: svc, Bus: bus, Playbooks: pbSvc, Backend: cb.client(),
		AgentBackendCfg: &config.AgentBackendConfig{APIKey: "aaaabbbbccccddddeeeeffffgggghhhhiiiijjjj"},
		MCPCardRunner:   slot,
	})
	require.NotNil(t, slot.Get(), "a configured backend must bind the runner")

	return slot.Get(), cb, svc, pbSvc
}

func TestMCPRunCard_NoBackendLeavesSlotEmpty(t *testing.T) {
	svc, _, bus, _ := playbookTestSetup(t)
	slot := &mcpserver.RunnerSlot{}

	NewRouter(RouterConfig{Service: svc, Bus: bus, MCPCardRunner: slot})

	assert.Nil(t, slot.Get())
}

func TestMCPRunCard_TriggersRunAutoWithTheUppercaseID(t *testing.T) {
	ctx := context.Background()
	run, cb, svc, _ := mcpRunner(t)

	card, err := svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "Run me", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	_, err = run(ctx, "test-project", strings.ToLower(card.ID))
	require.NoError(t, err)

	trigger := cb.lastTrigger(t)
	assert.Equal(t, card.ID, trigger.CardID, "Stop sends the uppercase ID, so the trigger must carry it")
	assert.False(t, trigger.Interactive)
}

func TestMCPRunCard_PlaybookLockRefused(t *testing.T) {
	ctx := context.Background()
	run, _, svc, pbSvc := mcpRunner(t)
	card := lockCardInPlaybook(t, svc, pbSvc)

	now := time.Now().UTC()
	_, err := pbSvc.SetRunIf(ctx, "rollout", nil, &board.PlaybookRun{Status: board.RunStatusRunning, StartedAt: now, UpdatedAt: now}, "human:alice")
	require.NoError(t, err)

	_, err = run(ctx, "test-project", card.ID)

	var lf *launchFailure
	require.ErrorAs(t, err, &lf)
	assert.Equal(t, ErrCodePlaybookRunActive, lf.code)
}
