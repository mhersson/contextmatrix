package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// fakeRunner records each run and whether the card was autonomous when it ran.
type fakeRunner struct {
	calls         []string
	sawAutonomous bool
}

func setupRunCard(t *testing.T, runErr error) (*testEnv, *fakeRunner) {
	t.Helper()

	slot := &RunnerSlot{}
	env := setupMCP(t, func(c *ServerConfig) { c.CardRunner = slot })
	f := &fakeRunner{}

	slot.Set(func(ctx context.Context, project, cardID string) (*board.Card, error) {
		f.calls = append(f.calls, project+"/"+cardID)

		card, err := env.svc.GetCard(ctx, project, cardID)
		if err != nil {
			return nil, err
		}

		f.sawAutonomous = card.Autonomous

		return card, runErr
	})

	return env, f
}

func runCard(t *testing.T, env *testEnv, cardID, agentID string) (*mcp.CallToolResult, error) {
	t.Helper()

	return callToolRaw(t, env, "run_card", map[string]any{"card_id": cardID, "agent_id": agentID})
}

func TestRunCardTool(t *testing.T) {
	ctx := context.Background()

	t.Run("a non-human caller is refused before the runner", func(t *testing.T) {
		env, f := setupRunCard(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")

		result, err := runCard(t, env, card.ID, "agent:x")
		assert.Contains(t, errorText(result, err), "run_card is human-only")
		assert.Empty(t, f.calls)
	})

	t.Run("a todo card is set autonomous before the runner starts it", func(t *testing.T) {
		env, f := setupRunCard(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")

		result, err := runCard(t, env, card.ID, "human:alice")
		require.False(t, resultIsError(result, err), errorText(result, err))
		assert.Equal(t, []string{"test-project/" + card.ID}, f.calls)
		assert.True(t, f.sawAutonomous)
	})

	for _, tc := range []struct {
		name, state, workerStatus string
		imported                  bool
	}{
		{name: "a card not in todo", state: "in_progress"},
		{name: "a queued worker", workerStatus: "queued"},
		{name: "a running worker", workerStatus: "running"},
		{name: "an unvetted import", imported: true},
	} {
		t.Run(tc.name+" is left untouched and the refusal reaches the caller", func(t *testing.T) {
			env, f := setupRunCard(t, errors.New("launch refused"))

			input := service.CreateCardInput{Title: "Run me", Type: "task", Priority: "medium"}
			if tc.imported {
				input.Source = &board.Source{System: "jira", ExternalID: "PROJ-7", ExternalURL: "https://acme.atlassian.net/browse/PROJ-7"}
			}

			card, err := env.svc.CreateCard(ctx, "test-project", input)
			require.NoError(t, err)

			if tc.state != "" {
				_, err = env.svc.TransitionTo(ctx, "test-project", card.ID, tc.state)
				require.NoError(t, err)
			}

			if tc.workerStatus != "" {
				_, err = env.svc.UpdateWorkerStatus(ctx, "test-project", card.ID, tc.workerStatus, "seed")
				require.NoError(t, err)
			}

			result, err := runCard(t, env, card.ID, "human:alice")
			assert.Contains(t, errorText(result, err), "launch refused")
			assert.Len(t, f.calls, 1)

			stored, err := env.svc.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)
			assert.False(t, stored.Autonomous)
		})
	}

	t.Run("an empty slot refuses without touching the card", func(t *testing.T) {
		env := setupMCP(t, func(c *ServerConfig) { c.CardRunner = &RunnerSlot{} })
		card := createTestCard(t, env, "Run me", "task", "medium")

		result, err := runCard(t, env, card.ID, "human:alice")
		assert.Contains(t, errorText(result, err), "no execution backend is configured")

		stored, err := env.svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.False(t, stored.Autonomous)
	})

	t.Run("the tool is absent without a slot", func(t *testing.T) {
		tools, err := setupMCP(t).session.ListTools(ctx, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tools.Tools)

		for _, tool := range tools.Tools {
			assert.NotEqual(t, "run_card", tool.Name)
		}
	})
}
