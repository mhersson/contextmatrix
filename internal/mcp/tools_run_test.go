package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// setupMCPWithRunner builds the same environment as setupMCP but wires a
// RunnerSlot into the MCP server so run_card is registered. Mirrors
// setupMCPWithPlaybooks' construction.
func setupMCPWithRunner(t *testing.T, slot *RunnerSlot) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectConfig()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	workflowSkillsDir := filepath.Join(tmpDir, "workflow-skills")
	require.NoError(t, os.MkdirAll(workflowSkillsDir, 0o755))

	server := NewServer(ServerConfig{
		Service:           svc,
		WorkflowSkillsDir: workflowSkillsDir,
		CardRunner:        slot,
	})

	ctx, cancel := context.WithCancel(context.Background())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = session.Close()

		cancel()
	})

	return &testEnv{
		session:           session,
		svc:               svc,
		store:             store,
		boardsDir:         boardsDir,
		workflowSkillsDir: workflowSkillsDir,
		cancel:            cancel,
	}
}

func TestRunCard_HumanOnly(t *testing.T) {
	ctx := context.Background()

	t.Run("agent_id without human: prefix is rejected and runner not called", func(t *testing.T) {
		var calls []string

		slot := &RunnerSlot{}
		env := setupMCPWithRunner(t, slot)
		slot.Set(func(ctx context.Context, project, cardID string) (*board.Card, error) {
			calls = append(calls, project+"/"+cardID)

			return nil, nil
		})

		card := createTestCard(t, env, "Run gate test", "task", "medium")

		result, err := callToolRaw(t, env, "run_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "agent:x",
		})
		require.True(t, resultIsError(result, err), "non-human agent must produce an error result")
		assert.Contains(t, errorText(result, err), "run_card is human-only")
		assert.Empty(t, calls, "runner must not be called for a non-human caller")
	})

	t.Run("human:alice on a todo card sets autonomous and calls the runner once", func(t *testing.T) {
		var calls []string

		var sawAutonomous bool

		slot := &RunnerSlot{}
		env := setupMCPWithRunner(t, slot)
		slot.Set(func(ctx context.Context, project, cardID string) (*board.Card, error) {
			calls = append(calls, project+"/"+cardID)

			c, err := env.svc.GetCard(ctx, project, cardID)
			if err != nil {
				return nil, err
			}

			sawAutonomous = c.Autonomous

			return c, nil
		})

		card := createTestCard(t, env, "Run me", "task", "medium")
		require.False(t, card.Autonomous, "fresh todo card should not be autonomous")

		result, err := callToolRaw(t, env, "run_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.False(t, resultIsError(result, err), "human run on todo card should succeed: %s", errorText(result, err))

		require.Equal(t, []string{"test-project/" + card.ID}, calls)
		assert.True(t, sawAutonomous, "runner must observe autonomous=true")

		stored, err := env.svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, stored.Autonomous, "card must be autonomous afterwards")
	})

	t.Run("a card not in todo is not modified and the runner's error reaches the caller", func(t *testing.T) {
		var calls []string

		slot := &RunnerSlot{}
		env := setupMCPWithRunner(t, slot)
		slot.Set(func(ctx context.Context, project, cardID string) (*board.Card, error) {
			calls = append(calls, project+"/"+cardID)

			return nil, errors.New("card must be in todo state to run")
		})

		card := createTestCard(t, env, "Already running", "task", "medium")
		_, err := env.svc.TransitionTo(ctx, "test-project", card.ID, "in_progress")
		require.NoError(t, err)

		result, err := callToolRaw(t, env, "run_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.True(t, resultIsError(result, err), "run on in_progress card must error")
		assert.Contains(t, errorText(result, err), "card must be in todo state to run")
		assert.Equal(t, []string{"test-project/" + card.ID}, calls, "runner is still called so the launch can refuse")

		stored, err := env.svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.False(t, stored.Autonomous, "a card not in todo must never be set autonomous")
	})

	t.Run("an empty slot reports no execution backend", func(t *testing.T) {
		env := setupMCPWithRunner(t, &RunnerSlot{}) // never Set

		card := createTestCard(t, env, "No backend", "task", "medium")

		result, err := callToolRaw(t, env, "run_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.True(t, resultIsError(result, err), "unset slot must produce an error")
		assert.Contains(t, errorText(result, err), "no execution backend is configured")
	})

	t.Run("run_card is not registered without a slot", func(t *testing.T) {
		env := setupMCP(t) // no CardRunner in ServerConfig

		card := createTestCard(t, env, "No tool", "task", "medium")

		result, err := callToolRaw(t, env, "run_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.True(t, resultIsError(result, err), "run_card must not exist without a configured slot")
	})
}
