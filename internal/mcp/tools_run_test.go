package mcp

import (
	"context"
	"encoding/json"
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

// fakePlayer records each Play and answers with err, or with a minimal
// detail of a runnable playbook.
type fakePlayer struct {
	calls []string
	err   error
}

func (f *fakePlayer) Play(_ context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	f.calls = append(f.calls, id+"|"+agentID)

	if f.err != nil {
		return nil, f.err
	}

	return &service.PlaybookDetail{ID: id, Runnable: true}, nil
}

func setupRunPlaybook(t *testing.T, playErr error) (*testEnv, *fakePlayer) {
	t.Helper()

	f := &fakePlayer{err: playErr}
	env := setupMCPWithPlaybooks(t, func(c *ServerConfig) { c.PlaybookPlayer = f })

	return env, f
}

// newPlaybook creates a playbook over the given cards through the MCP tool
// and returns its id.
func newPlaybook(t *testing.T, env *testEnv, title string, cardIDs ...string) string {
	t.Helper()

	entries := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		entries = append(entries, map[string]any{"type": "card", "project": "test-project", "card": id})
	}

	result, err := callToolRaw(t, env, "create_playbook", map[string]any{"agent_id": "human:alice", "title": title, "entries": entries})
	require.False(t, resultIsError(result, err), errorText(result, err))

	var summary service.PlaybookSummary
	require.NoError(t, json.Unmarshal([]byte(resultText(result)), &summary))

	return summary.ID
}

func playbookRunnable(t *testing.T, env *testEnv, id string) bool {
	t.Helper()

	result, err := callToolRaw(t, env, "get_playbook", map[string]any{"id": id})
	require.False(t, resultIsError(result, err), errorText(result, err))

	var detail service.PlaybookDetail
	require.NoError(t, json.Unmarshal([]byte(resultText(result)), &detail))

	return detail.Runnable
}

func TestRunPlaybookTool(t *testing.T) {
	ctx := context.Background()

	t.Run("a non-human caller is refused before any write", func(t *testing.T) {
		env, f := setupRunPlaybook(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")
		id := newPlaybook(t, env, "Ship it", card.ID)

		result, err := callToolRaw(t, env, "run_playbook", map[string]any{"id": id, "agent_id": "agent:x"})
		assert.Contains(t, errorText(result, err), "run_playbook is human-only")
		assert.Empty(t, f.calls)
		assert.False(t, playbookRunnable(t, env, id))
	})

	t.Run("a playbook that is not runnable is made runnable, then played", func(t *testing.T) {
		env, f := setupRunPlaybook(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")
		id := newPlaybook(t, env, "Ship it", card.ID)

		result, err := callToolRaw(t, env, "run_playbook", map[string]any{"id": id, "agent_id": "human:alice"})
		require.False(t, resultIsError(result, err), errorText(result, err))

		assert.Equal(t, []string{id + "|human:alice"}, f.calls)
		assert.True(t, playbookRunnable(t, env, id))

		forced, err := env.svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.True(t, forced.Autonomous)
		assert.True(t, forced.CreatePR)
		assert.True(t, forced.AwaitCI)
		assert.True(t, forced.MergePR)
		assert.Equal(t, "playbook/"+id, forced.BaseBranch)
	})

	t.Run("a runnable playbook that gained an unvetted card is still played", func(t *testing.T) {
		env, f := setupRunPlaybook(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")
		id := newPlaybook(t, env, "Ship it", card.ID)

		result, err := callToolRaw(t, env, "run_playbook", map[string]any{"id": id, "agent_id": "human:alice"})
		require.False(t, resultIsError(result, err), errorText(result, err))

		// Re-asserting runnable would re-run the vetting check and refuse this
		// card, while Play holds the run on it until it is vetted.
		imported, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "Imported", Type: "task", Priority: "medium",
			Source: &board.Source{System: "jira", ExternalID: "PROJ-9", ExternalURL: "https://acme.atlassian.net/browse/PROJ-9"},
		})
		require.NoError(t, err)

		result, err = callToolRaw(t, env, "add_playbook_entry", map[string]any{
			"agent_id": "human:alice", "playbook": id, "type": "card", "project": "test-project", "card": imported.ID,
		})
		require.False(t, resultIsError(result, err), errorText(result, err))

		result, err = callToolRaw(t, env, "run_playbook", map[string]any{"id": id, "agent_id": "human:alice"})
		require.False(t, resultIsError(result, err), errorText(result, err))
		assert.Len(t, f.calls, 2)
	})

	t.Run("a card owned by another runnable playbook stops make-runnable and Play", func(t *testing.T) {
		env, f := setupRunPlaybook(t, nil)
		card := createTestCard(t, env, "Run me", "task", "medium")
		first := newPlaybook(t, env, "First", card.ID)
		second := newPlaybook(t, env, "Second", card.ID)

		result, err := callToolRaw(t, env, "run_playbook", map[string]any{"id": first, "agent_id": "human:alice"})
		require.False(t, resultIsError(result, err), errorText(result, err))

		result, err = callToolRaw(t, env, "run_playbook", map[string]any{"id": second, "agent_id": "human:alice"})
		assert.Contains(t, errorText(result, err), "make runnable")
		assert.Equal(t, []string{first + "|human:alice"}, f.calls, "the second playbook is never played")
		assert.False(t, playbookRunnable(t, env, second))
	})

	t.Run("a refused Play reaches the caller and the playbook stays runnable", func(t *testing.T) {
		env, f := setupRunPlaybook(t, errors.New("no execution backend is configured"))
		card := createTestCard(t, env, "Run me", "task", "medium")
		id := newPlaybook(t, env, "Ship it", card.ID)

		result, err := callToolRaw(t, env, "run_playbook", map[string]any{"id": id, "agent_id": "human:alice"})
		assert.Contains(t, errorText(result, err), "no execution backend is configured")
		assert.Len(t, f.calls, 1)
		assert.True(t, playbookRunnable(t, env, id))
	})

	t.Run("the tool is absent without a player", func(t *testing.T) {
		env := setupMCPWithPlaybooks(t)

		res, err := env.session.ListTools(ctx, nil)
		require.NoError(t, err)

		for _, tool := range res.Tools {
			assert.NotEqual(t, "run_playbook", tool.Name)
		}
	})
}
