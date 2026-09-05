package mcp

import (
	"context"
	"fmt"
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

// setupMCPWithPlaybooks builds the same environment as setupMCP but also
// wires the playbook subsystem (store + service) into the MCP server, for
// tests exercising the playbook tools. Mirrors setupMCP's construction.
func setupMCPWithPlaybooks(t *testing.T) *testEnv {
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

	pbStore, err := storage.NewFilesystemPlaybookStore(boardsDir)
	require.NoError(t, err)

	pbSvc := service.NewPlaybookService(pbStore, store, bus, nil, false)

	workflowSkillsDir := filepath.Join(tmpDir, "workflow-skills")
	require.NoError(t, os.MkdirAll(workflowSkillsDir, 0o755))

	skillModels := map[string]string{
		"create-task.md":          "claude-sonnet-4-6",
		"create-plan.md":          "claude-sonnet-4-6",
		"execute-task.md":         "claude-sonnet-4-6",
		"review-task.md":          "claude-opus-4-6",
		"document-task.md":        "claude-sonnet-4-6",
		"init-project.md":         "claude-sonnet-4-6",
		"run-autonomous.md":       "claude-sonnet-4-6",
		"brainstorming.md":        "claude-sonnet-4-6",
		"systematic-debugging.md": "claude-sonnet-4-6",
		"plan-draft.md":           "claude-opus-4-6",
	}
	for name, model := range skillModels {
		content := fmt.Sprintf("# %s\n\n## Agent Configuration\n\n- **Model:** %s - Test model.\n\n---\n\nSkill instructions here.", name, model)
		require.NoError(t, os.WriteFile(filepath.Join(workflowSkillsDir, name), []byte(content), 0o644))
	}

	server := NewServer(ServerConfig{
		Service:           svc,
		WorkflowSkillsDir: workflowSkillsDir,
		Playbooks:         pbSvc,
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
		pb:                pbSvc,
	}
}

func TestPlaybookTools_CreateListGet(t *testing.T) {
	env := setupMCPWithPlaybooks(t)
	card := createTestCard(t, env, "Target card", "task", "medium")

	result := callTool(t, env, "create_playbook", map[string]any{
		"agent_id": "human:alice",
		"title":    "MCP Rollout",
		"entries": []map[string]any{
			{"type": "card", "project": "test-project", "card": card.ID, "note": "merge first"},
			{"type": "manual", "text": "redeploy"},
		},
	})
	require.False(t, result.IsError)

	var summary service.PlaybookSummary
	unmarshalResult(t, result, &summary)
	assert.Equal(t, "mcp-rollout", summary.ID)
	assert.Equal(t, 2, summary.Total)

	result = callTool(t, env, "list_playbooks", map[string]any{"agent_id": "human:alice"})

	var list listPlaybooksOutput
	unmarshalResult(t, result, &list)
	require.Len(t, list.Playbooks, 1)
	assert.Equal(t, []int{1}, list.Playbooks[0].Gates)
	require.NotNil(t, list.Playbooks[0].Next)
	assert.Equal(t, card.ID, list.Playbooks[0].Next.Card)

	result = callTool(t, env, "get_playbook", map[string]any{"agent_id": "human:alice", "id": "mcp-rollout"})

	var detail service.PlaybookDetail
	unmarshalResult(t, result, &detail)
	require.Len(t, detail.Entries, 2)
	assert.Equal(t, "Target card", detail.Entries[0].CardTitle)
	assert.Equal(t, "human:alice", detail.CreatedBy)
	assert.Equal(t, "merge first", detail.Entries[0].Note)
}

// TestPlaybookTools_EntryLifecycle walks a playbook through its full entry
// lifecycle: create with one manual entry, add a second manual entry, check
// the second entry off (verifying done_by attribution), move it to the
// front, then remove the original first entry.
func TestPlaybookTools_EntryLifecycle(t *testing.T) {
	env := setupMCPWithPlaybooks(t)

	result := callTool(t, env, "create_playbook", map[string]any{
		"agent_id": "human:alice",
		"title":    "Entry Lifecycle",
		"entries": []map[string]any{
			{"type": "manual", "text": "step one"},
		},
	})
	require.False(t, result.IsError)

	var created service.PlaybookSummary
	unmarshalResult(t, result, &created)
	assert.Equal(t, 1, created.Total)

	addResult := callTool(t, env, "add_playbook_entry", map[string]any{
		"agent_id": "human:alice",
		"playbook": created.ID,
		"type":     "manual",
		"text":     "step two",
	})
	require.False(t, addResult.IsError)

	var afterAdd service.PlaybookSummary
	unmarshalResult(t, addResult, &afterAdd)
	assert.Equal(t, 2, afterAdd.Total)

	doneResult := callTool(t, env, "update_playbook_entry", map[string]any{
		"agent_id": "human:bob",
		"playbook": created.ID,
		"entry":    "e2",
		"done":     true,
	})
	require.False(t, doneResult.IsError)

	detail, err := env.pb.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, detail.Entries, 2)
	assert.True(t, detail.Entries[1].Done)
	assert.Equal(t, "human:bob", detail.Entries[1].DoneBy, "done_by should be stamped from the caller's agent_id")

	moveResult := callTool(t, env, "update_playbook_entry", map[string]any{
		"agent_id": "human:alice",
		"playbook": created.ID,
		"entry":    "e2",
		"position": 0,
	})
	require.False(t, moveResult.IsError)

	detail, err = env.pb.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, detail.Entries, 2)
	assert.Equal(t, "e2", detail.Entries[0].ID, "e2 should now be first")
	assert.Equal(t, "e1", detail.Entries[1].ID)

	removeResult := callTool(t, env, "remove_playbook_entry", map[string]any{
		"agent_id": "human:alice",
		"playbook": created.ID,
		"entry":    "e1",
	})
	require.False(t, removeResult.IsError)

	var afterRemove service.PlaybookSummary
	unmarshalResult(t, removeResult, &afterRemove)
	assert.Equal(t, 1, afterRemove.Total)
}

// TestPlaybookTools_Errors exercises the four documented failure paths:
// a bad card reference on create, a duplicate card entry on add, an invalid
// done=true on a card entry, and an unknown playbook id on get.
func TestPlaybookTools_Errors(t *testing.T) {
	env := setupMCPWithPlaybooks(t)
	card := createTestCard(t, env, "Referenced card", "task", "medium")

	t.Run("create_playbook with a nonexistent card entry fails", func(t *testing.T) {
		result, err := callToolRaw(t, env, "create_playbook", map[string]any{
			"agent_id": "human:alice",
			"title":    "Bad Ref",
			"entries": []map[string]any{
				{"type": "card", "project": "test-project", "card": "TEST-999"},
			},
		})
		require.True(t, resultIsError(result, err))
		assert.Contains(t, errorText(result, err), "entry 0")
	})

	t.Run("add_playbook_entry duplicating an existing card fails", func(t *testing.T) {
		created := callTool(t, env, "create_playbook", map[string]any{
			"agent_id": "human:alice",
			"title":    "Dup Target",
			"entries": []map[string]any{
				{"type": "card", "project": "test-project", "card": card.ID},
			},
		})
		require.False(t, created.IsError)

		var summary service.PlaybookSummary
		unmarshalResult(t, created, &summary)

		result, err := callToolRaw(t, env, "add_playbook_entry", map[string]any{
			"agent_id": "human:alice",
			"playbook": summary.ID,
			"type":     "card",
			"project":  "test-project",
			"card":     card.ID,
		})
		require.True(t, resultIsError(result, err))
		assert.Contains(t, errorText(result, err), "duplicate")
	})

	t.Run("update_playbook_entry done=true on a card entry fails", func(t *testing.T) {
		created := callTool(t, env, "create_playbook", map[string]any{
			"agent_id": "human:alice",
			"title":    "Card Done Reject",
			"entries": []map[string]any{
				{"type": "card", "project": "test-project", "card": card.ID},
			},
		})
		require.False(t, created.IsError)

		var summary service.PlaybookSummary
		unmarshalResult(t, created, &summary)

		result, err := callToolRaw(t, env, "update_playbook_entry", map[string]any{
			"agent_id": "human:alice",
			"playbook": summary.ID,
			"entry":    "e1",
			"done":     true,
		})
		require.True(t, resultIsError(result, err))
	})

	t.Run("get_playbook with unknown id fails", func(t *testing.T) {
		result, err := callToolRaw(t, env, "get_playbook", map[string]any{
			"agent_id": "human:alice",
			"id":       "does-not-exist",
		})
		require.True(t, resultIsError(result, err))
		assert.Contains(t, errorText(result, err), "not found")
	})
}

// TestPlaybookTools_AbsentWhenDisabled verifies that when the playbook
// subsystem is not wired into ServerConfig, playbook tools do not register
// at all rather than registering and failing at call time.
func TestPlaybookTools_AbsentWhenDisabled(t *testing.T) {
	env := setupMCP(t) // no Playbooks in ServerConfig
	result, err := callToolRaw(t, env, "list_playbooks", map[string]any{})
	assert.True(t, resultIsError(result, err), "playbook tools must not register when the subsystem is disabled")
}

func TestPlaybookTools_CreateWithBoardsRepo(t *testing.T) {
	env := setupMCPWithPlaybooks(t)

	result := callTool(t, env, "create_playbook", map[string]any{"agent_id": "human:alice", "title": "Rollout", "boards_repo": "boards"})
	require.False(t, result.IsError)

	var summary service.PlaybookSummary
	unmarshalResult(t, result, &summary)
	assert.Equal(t, "rollout", summary.ID)
	assert.Equal(t, "boards", summary.BoardsRepo)

	result = callTool(t, env, "create_playbook", map[string]any{"agent_id": "human:alice", "title": "Elsewhere", "boards_repo": "nope"})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "unknown boards_repo")
}
