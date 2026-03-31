package mcp

import (
	"context"
	"encoding/json"
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

// testProjectConfig returns a project config with all states needed for testing.
func testProjectConfig() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "blocked", "review", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"blocked", "review", "todo"},
			"blocked":     {"in_progress", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
		},
	}
}

// testEnv holds all components needed for MCP server tests.
type testEnv struct {
	session   *mcp.ClientSession
	svc       *service.CardService
	boardsDir string
	skillsDir string
	cancel    context.CancelFunc
}

// setupMCP creates a full test environment: boards dir, project, service layer,
// MCP server, and an in-process client session.
func setupMCP(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Create test project
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectConfig()))

	// Create dependencies
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil)

	// Create skills directory with stub skill files
	skillsDir := filepath.Join(tmpDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	for _, name := range []string{"create-task.md", "create-plan.md", "execute-task.md", "review-task.md", "document-task.md", "init-project.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(skillsDir, name), []byte("# "+name+"\nSkill instructions here."), 0o644))
	}

	// Create MCP server and connect in-memory
	server := NewServer(svc, skillsDir)

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
		session:   session,
		svc:       svc,
		boardsDir: boardsDir,
		skillsDir: skillsDir,
		cancel:    cancel,
	}
}

// callTool is a helper that calls an MCP tool and returns the result.
func callTool(t *testing.T, env *testEnv, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	require.NoError(t, err)
	return result
}

// unmarshalResult extracts JSON text content from a CallToolResult into the target struct.
func unmarshalResult(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	require.NotEmpty(t, result.Content, "expected non-empty content")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), target))
}

// createTestCard creates a card via MCP tool and returns the result.
func createTestCard(t *testing.T, env *testEnv, title, typ, priority string) *board.Card {
	t.Helper()
	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    title,
		"type":     typ,
		"priority": priority,
	})
	require.False(t, result.IsError, "create_card should not error")
	var card board.Card
	unmarshalResult(t, result, &card)
	return &card
}

// --- Tests ---

func TestListTools(t *testing.T) {
	env := setupMCP(t)

	result, err := env.session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	expectedTools := []string{
		"list_projects",
		"list_cards",
		"get_card",
		"create_card",
		"update_card",
		"transition_card",
		"claim_card",
		"release_card",
		"heartbeat",
		"add_log",
		"get_task_context",
		"complete_task",
		"get_subtask_summary",
		"get_ready_tasks",
		"report_usage",
		"create_project",
		"update_project",
		"delete_project",
		"get_skill",
	}

	assert.Len(t, result.Tools, len(expectedTools), "expected %d tools", len(expectedTools))

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "missing tool: %s", name)
	}
}

func TestListProjects(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "list_projects", map[string]any{})
	require.False(t, result.IsError)

	var output listProjectsOutput
	unmarshalResult(t, result, &output)

	require.Len(t, output.Projects, 1)
	assert.Equal(t, "test-project", output.Projects[0].Name)
	assert.Equal(t, "TEST", output.Projects[0].Prefix)
	assert.Contains(t, output.Projects[0].States, "todo")
	assert.Contains(t, output.Projects[0].States, "done")
}

func TestCreateAndGetCard(t *testing.T) {
	env := setupMCP(t)

	// Create a card
	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Implement feature X",
		"type":     "feature",
		"priority": "high",
		"labels":   []string{"backend", "api"},
		"body":     "## Description\nBuild feature X.",
	})
	require.False(t, result.IsError)

	var created board.Card
	unmarshalResult(t, result, &created)

	assert.Equal(t, "TEST-001", created.ID)
	assert.Equal(t, "Implement feature X", created.Title)
	assert.Equal(t, "test-project", created.Project)
	assert.Equal(t, "feature", created.Type)
	assert.Equal(t, "todo", created.State)
	assert.Equal(t, "high", created.Priority)
	assert.Equal(t, []string{"backend", "api"}, created.Labels)
	assert.Contains(t, created.Body, "## Description\nBuild feature X.")
	assert.False(t, created.Created.IsZero())

	// Get the same card back
	getResult := callTool(t, env, "get_card", map[string]any{
		"project": "test-project",
		"card_id": "TEST-001",
	})
	require.False(t, getResult.IsError)

	var fetched board.Card
	unmarshalResult(t, getResult, &fetched)

	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, created.Title, fetched.Title)
	assert.Contains(t, fetched.Body, "## Description\nBuild feature X.")
	assert.Equal(t, created.Priority, fetched.Priority)
}

func TestUpdateCard(t *testing.T) {
	env := setupMCP(t)

	// Create a card
	createTestCard(t, env, "Original title", "task", "low")

	// Update title and body
	newTitle := "Updated title"
	newBody := "## Updated\nNew body content."
	result := callTool(t, env, "update_card", map[string]any{
		"project": "test-project",
		"card_id": "TEST-001",
		"title":   newTitle,
		"body":    newBody,
	})
	require.False(t, result.IsError)

	var updated board.Card
	unmarshalResult(t, result, &updated)

	assert.Equal(t, "Updated title", updated.Title)
	assert.Equal(t, "## Updated\nNew body content.", updated.Body)
	// Priority should remain unchanged
	assert.Equal(t, "low", updated.Priority)
}

func TestTransitionCard(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Transition test", "task", "medium")

	// Transition todo -> in_progress
	result := callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-001",
		"new_state": "in_progress",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)
	assert.Equal(t, "in_progress", card.State)
}

func TestTransitionCard_Invalid(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Invalid transition test", "task", "medium")

	// Try todo -> done (not allowed by transitions config)
	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "transition_card",
		Arguments: map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"new_state": "done",
		},
	})

	// The SDK wraps tool handler errors as IsError results for regular errors,
	// or returns an rpc error. Either way we should detect the failure.
	if err != nil {
		// Protocol-level error is also acceptable
		assert.Contains(t, err.Error(), "transition")
		return
	}
	require.True(t, result.IsError, "invalid transition should produce an error result")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "transition")
}

func TestClaimAndRelease(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Claim test", "task", "medium")

	// Claim the card
	claimResult := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-abc",
	})
	require.False(t, claimResult.IsError)

	var claimed board.Card
	unmarshalResult(t, claimResult, &claimed)
	assert.Equal(t, "agent-abc", claimed.AssignedAgent)
	assert.NotNil(t, claimed.LastHeartbeat)

	// Release the card
	releaseResult := callTool(t, env, "release_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-abc",
	})
	require.False(t, releaseResult.IsError)

	var released board.Card
	unmarshalResult(t, releaseResult, &released)
	assert.Empty(t, released.AssignedAgent)
	assert.Nil(t, released.LastHeartbeat)
}

func TestHeartbeat(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Heartbeat test", "task", "medium")

	// Claim first
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-hb",
	})

	// Send heartbeat
	result := callTool(t, env, "heartbeat", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-hb",
	})
	require.False(t, result.IsError)

	// Verify card still has the agent assigned
	getResult := callTool(t, env, "get_card", map[string]any{
		"project": "test-project",
		"card_id": "TEST-001",
	})
	var card board.Card
	unmarshalResult(t, getResult, &card)
	assert.Equal(t, "agent-hb", card.AssignedAgent)
	assert.NotNil(t, card.LastHeartbeat)
}

func TestAddLog(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Log test", "task", "medium")

	result := callTool(t, env, "add_log", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-log",
		"action":   "status_update",
		"message":  "Started working on the task",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	require.Len(t, card.ActivityLog, 1)
	assert.Equal(t, "agent-log", card.ActivityLog[0].Agent)
	assert.Equal(t, "status_update", card.ActivityLog[0].Action)
	assert.Equal(t, "Started working on the task", card.ActivityLog[0].Message)
	assert.False(t, card.ActivityLog[0].Timestamp.IsZero())
}

func TestCompleteTask_MainTask(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Complete me", "task", "medium")

	// Claim the card (auto-transitions todo -> in_progress)
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-done",
	})

	// Complete the main task (no parent) — should auto-walk to review, not done
	result := callTool(t, env, "complete_task", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-done",
		"summary":  "All tests passing, feature implemented",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	assert.Equal(t, "review", card.State, "main task should stop at review")
	assert.Empty(t, card.AssignedAgent, "agent should be released after completion")

	// Verify log entry was added
	require.NotEmpty(t, card.ActivityLog)
	lastLog := card.ActivityLog[len(card.ActivityLog)-1]
	assert.Equal(t, "completed", lastLog.Action)
	assert.Equal(t, "All tests passing, feature implemented", lastLog.Message)
	assert.Equal(t, "agent-done", lastLog.Agent)
}

func TestCompleteTask_Subtask(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create parent card
	createTestCard(t, env, "Parent task", "feature", "high")

	// Create subtask with parent set
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Subtask",
		"type":     "task",
		"priority": "medium",
		"parent":   "TEST-001",
	})

	// Claim the subtask (auto-transitions to in_progress)
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
	})

	// Complete the subtask — should auto-walk all the way to done
	result := callTool(t, env, "complete_task", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
		"summary":  "Subtask done",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	assert.Equal(t, "done", card.State, "subtask should go all the way to done")
	assert.Empty(t, card.AssignedAgent)

	// Verify via service layer
	stored, err := env.svc.GetCard(ctx, "test-project", "TEST-002")
	require.NoError(t, err)
	assert.Equal(t, "done", stored.State)
}

func TestClaimCard_AutoTransition(t *testing.T) {
	env := setupMCP(t)

	// Create card (starts in todo)
	createTestCard(t, env, "Claim me", "task", "medium")

	// Claim should auto-transition to in_progress
	result := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-auto",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	assert.Equal(t, "in_progress", card.State, "claim should auto-transition to in_progress")
	assert.Equal(t, "agent-auto", card.AssignedAgent)
}

func TestGetTaskContext(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create a parent card
	parent := createTestCard(t, env, "Parent task", "feature", "high")

	// Create child cards with parent set
	child1Result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Child task 1",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})
	require.False(t, child1Result.IsError)
	var child1 board.Card
	unmarshalResult(t, child1Result, &child1)

	child2Result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Child task 2",
		"type":     "task",
		"priority": "low",
		"parent":   parent.ID,
	})
	require.False(t, child2Result.IsError)
	var child2 board.Card
	unmarshalResult(t, child2Result, &child2)

	// Get task context for child1
	result := callTool(t, env, "get_task_context", map[string]any{
		"project": "test-project",
		"card_id": child1.ID,
	})
	require.False(t, result.IsError)

	var output getTaskContextOutput
	unmarshalResult(t, result, &output)

	// Verify the card itself
	require.NotNil(t, output.Card)
	assert.Equal(t, child1.ID, output.Card.ID)

	// Verify parent
	require.NotNil(t, output.Parent, "parent should be returned")
	assert.Equal(t, parent.ID, output.Parent.ID)
	assert.Equal(t, "Parent task", output.Parent.Title)

	// Verify siblings (child2 should be there, child1 should not)
	require.Len(t, output.Siblings, 1, "should have exactly one sibling")
	assert.Equal(t, child2.ID, output.Siblings[0].ID)

	// Verify project config
	require.NotNil(t, output.Config)
	assert.Equal(t, "test-project", output.Config.Name)

	// Also test a card with no parent
	noParentResult := callTool(t, env, "get_task_context", map[string]any{
		"project": "test-project",
		"card_id": parent.ID,
	})
	require.False(t, noParentResult.IsError)

	var noParentOutput getTaskContextOutput
	unmarshalResult(t, noParentResult, &noParentOutput)
	assert.Nil(t, noParentOutput.Parent, "parent card should have no parent")
	assert.Empty(t, noParentOutput.Siblings, "parent card should have no siblings")

	_ = ctx // context used implicitly through env
}

func TestGetSubtaskSummary(t *testing.T) {
	env := setupMCP(t)

	// Create a parent card
	parent := createTestCard(t, env, "Epic task", "feature", "high")

	// Create subtasks in various states
	for _, title := range []string{"Subtask A", "Subtask B"} {
		callTool(t, env, "create_card", map[string]any{
			"project":  "test-project",
			"title":    title,
			"type":     "task",
			"priority": "medium",
			"parent":   parent.ID,
		})
	}
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Subtask C",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})

	// Transition Subtask A (TEST-002) to in_progress
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-002",
		"new_state": "in_progress",
	})

	// Transition Subtask B (TEST-003) to in_progress -> review -> done
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-003",
		"new_state": "in_progress",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-003",
		"new_state": "review",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-003",
		"new_state": "done",
	})

	// Get subtask summary
	result := callTool(t, env, "get_subtask_summary", map[string]any{
		"project":   "test-project",
		"parent_id": parent.ID,
	})
	require.False(t, result.IsError)

	var output getSubtaskSummaryOutput
	unmarshalResult(t, result, &output)

	assert.Equal(t, parent.ID, output.ParentID)
	assert.Equal(t, 3, output.Total)
	assert.Equal(t, 1, output.Counts["todo"], "should have 1 todo")
	assert.Equal(t, 1, output.Counts["in_progress"], "should have 1 in_progress")
	assert.Equal(t, 1, output.Counts["done"], "should have 1 done")
}

func TestGetReadyTasks(t *testing.T) {
	env := setupMCP(t)

	// Create a parent card
	parent := createTestCard(t, env, "Project plan", "feature", "high")

	// Create task A (no deps, should be ready)
	taskA := createTestCard(t, env, "Task A - no deps", "task", "medium")
	callTool(t, env, "update_card", map[string]any{
		"project": "test-project",
		"card_id": taskA.ID,
	})

	// Create task B (no deps, should be ready)
	taskB := createTestCard(t, env, "Task B - no deps", "task", "medium")

	// Create task C that depends on task A (not ready since A is todo)
	taskCResult := callTool(t, env, "create_card", map[string]any{
		"project":    "test-project",
		"title":      "Task C - depends on A",
		"type":       "task",
		"priority":   "medium",
		"depends_on": []string{taskA.ID},
	})
	require.False(t, taskCResult.IsError)
	var taskC board.Card
	unmarshalResult(t, taskCResult, &taskC)

	// Get ready tasks (should include A and B, but not C since A is not done)
	result := callTool(t, env, "get_ready_tasks", map[string]any{
		"project": "test-project",
	})
	require.False(t, result.IsError)

	var output getReadyTasksOutput
	unmarshalResult(t, result, &output)

	readyIDs := make(map[string]bool)
	for _, card := range output.Cards {
		readyIDs[card.ID] = true
	}

	assert.True(t, readyIDs[taskA.ID], "Task A should be ready (no deps)")
	assert.True(t, readyIDs[taskB.ID], "Task B should be ready (no deps)")
	assert.True(t, readyIDs[parent.ID], "Parent should be ready (no deps)")
	assert.False(t, readyIDs[taskC.ID], "Task C should NOT be ready (dep A not done)")

	// Now complete task A so task C becomes ready
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  taskA.ID,
		"agent_id": "agent-x",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   taskA.ID,
		"new_state": "in_progress",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   taskA.ID,
		"new_state": "review",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   taskA.ID,
		"new_state": "done",
	})

	// Get ready tasks again
	result2 := callTool(t, env, "get_ready_tasks", map[string]any{
		"project": "test-project",
	})
	require.False(t, result2.IsError)

	var output2 getReadyTasksOutput
	unmarshalResult(t, result2, &output2)

	readyIDs2 := make(map[string]bool)
	for _, card := range output2.Cards {
		readyIDs2[card.ID] = true
	}

	assert.True(t, readyIDs2[taskC.ID], "Task C should now be ready (dep A is done)")
	// Task A should not be ready because it is done, not todo
	assert.False(t, readyIDs2[taskA.ID], "Task A should not be ready (state is done)")
	assert.True(t, readyIDs2[taskB.ID], "Task B should still be ready")
}

func TestTransitionCard_BlockedByDependency(t *testing.T) {
	env := setupMCP(t)

	// Create dependency card (stays in todo)
	depCard := createTestCard(t, env, "Dependency", "task", "medium")

	// Create card that depends on depCard
	result := callTool(t, env, "create_card", map[string]any{
		"project":    "test-project",
		"title":      "Depends on dep",
		"type":       "task",
		"priority":   "medium",
		"depends_on": []string{depCard.ID},
	})
	require.False(t, result.IsError)
	var card board.Card
	unmarshalResult(t, result, &card)

	// Try to transition to in_progress — should fail
	blocked := callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   card.ID,
		"new_state": "in_progress",
	})
	require.True(t, blocked.IsError, "transition should be blocked by unmet dependency")

	// Complete the dependency: todo -> in_progress -> review -> done
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   depCard.ID,
		"new_state": "in_progress",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   depCard.ID,
		"new_state": "review",
	})
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   depCard.ID,
		"new_state": "done",
	})

	// Now transition should succeed
	success := callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   card.ID,
		"new_state": "in_progress",
	})
	require.False(t, success.IsError, "transition should succeed after dep is done")
}

func TestGetReadyTasks_ScopedToParent(t *testing.T) {
	env := setupMCP(t)

	// Create a parent
	parent := createTestCard(t, env, "Scoped parent", "feature", "high")

	// Create two tasks under the parent
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Child under parent",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})

	// Create a task NOT under the parent
	createTestCard(t, env, "Orphan task", "task", "medium")

	// Get ready tasks scoped to parent
	result := callTool(t, env, "get_ready_tasks", map[string]any{
		"project":   "test-project",
		"parent_id": parent.ID,
	})
	require.False(t, result.IsError)

	var output getReadyTasksOutput
	unmarshalResult(t, result, &output)

	// Should only include the child under the parent
	require.Len(t, output.Cards, 1)
	assert.Equal(t, "TEST-002", output.Cards[0].ID)
}

func TestListPrompts(t *testing.T) {
	env := setupMCP(t)

	result, err := env.session.ListPrompts(context.Background(), nil)
	require.NoError(t, err)

	expectedPrompts := []string{
		"create-task",
		"create-plan",
		"execute-task",
		"review-task",
		"document-task",
		"init-project",
	}

	assert.Len(t, result.Prompts, len(expectedPrompts), "expected %d prompts", len(expectedPrompts))

	promptNames := make(map[string]bool)
	for _, p := range result.Prompts {
		promptNames[p.Name] = true
	}
	for _, name := range expectedPrompts {
		assert.True(t, promptNames[name], "missing prompt: %s", name)
	}
}

func TestListCards(t *testing.T) {
	env := setupMCP(t)

	// Create several cards
	createTestCard(t, env, "Task one", "task", "high")
	createTestCard(t, env, "Bug two", "bug", "critical")
	createTestCard(t, env, "Feature three", "feature", "low")

	// List all cards
	result := callTool(t, env, "list_cards", map[string]any{
		"project": "test-project",
	})
	require.False(t, result.IsError)

	var output listCardsOutput
	unmarshalResult(t, result, &output)
	assert.Len(t, output.Cards, 3)

	// List filtered by type
	filteredResult := callTool(t, env, "list_cards", map[string]any{
		"project": "test-project",
		"type":    "bug",
	})
	require.False(t, filteredResult.IsError)

	var filteredOutput listCardsOutput
	unmarshalResult(t, filteredResult, &filteredOutput)
	require.Len(t, filteredOutput.Cards, 1)
	assert.Equal(t, "bug", filteredOutput.Cards[0].Type)
}

func TestClaimCard_AlreadyClaimed(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Contested card", "task", "medium")

	// First agent claims
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-first",
	})

	// Second agent tries to claim
	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "claim_card",
		Arguments: map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-second",
		},
	})

	if err != nil {
		assert.Contains(t, err.Error(), "claim")
		return
	}
	require.True(t, result.IsError, "should fail when card already claimed")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "claim")
}

func TestGetCard_NotFound(t *testing.T) {
	env := setupMCP(t)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project": "test-project",
			"card_id": "TEST-999",
		},
	})

	if err != nil {
		// Protocol-level error is acceptable
		return
	}
	require.True(t, result.IsError, "get_card for nonexistent card should error")
}

func TestCreateCard_WithDependsOn(t *testing.T) {
	env := setupMCP(t)

	// Create the dependency card first
	dep := createTestCard(t, env, "Dependency task", "task", "high")

	// Create a card that depends on it
	result := callTool(t, env, "create_card", map[string]any{
		"project":    "test-project",
		"title":      "Dependent task",
		"type":       "task",
		"priority":   "medium",
		"depends_on": []string{dep.ID},
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)
	assert.Equal(t, []string{dep.ID}, card.DependsOn)
}

func TestPrompt_CreateTask(t *testing.T) {
	env := setupMCP(t)

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "create-task",
		Arguments: map[string]string{"description": "Build a new login page"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, "Build a new login page")
	assert.Contains(t, content.Text, "create-task.md")
}

func TestPrompt_CreatePlan(t *testing.T) {
	env := setupMCP(t)

	// Create a card to plan
	createTestCard(t, env, "Big feature", "feature", "high")

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "create-plan",
		Arguments: map[string]string{"card_id": "TEST-001"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, "TEST-001")
	assert.Contains(t, content.Text, "Big feature")
}

func TestPrompt_ExecuteTask(t *testing.T) {
	env := setupMCP(t)

	// Create parent and child
	parent := createTestCard(t, env, "Parent for execute", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Child to execute",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Sibling task",
		"type":     "task",
		"priority": "low",
		"parent":   parent.ID,
	})

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "execute-task",
		Arguments: map[string]string{"card_id": "TEST-002"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, "TEST-002")
	assert.Contains(t, content.Text, "Child to execute")
	// Should include parent context
	assert.Contains(t, content.Text, parent.ID)
	// Should include sibling info
	assert.Contains(t, content.Text, "Sibling task")
}

func TestPrompt_ReviewTask(t *testing.T) {
	env := setupMCP(t)

	// Create parent and subtasks
	parent := createTestCard(t, env, "Review parent", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Sub A for review",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "review-task",
		Arguments: map[string]string{"card_id": parent.ID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, parent.ID)
	assert.Contains(t, content.Text, "Review parent")
	assert.Contains(t, content.Text, "Sub A for review")
}

func TestPrompt_DocumentTask(t *testing.T) {
	env := setupMCP(t)

	// Create parent and subtasks
	parent := createTestCard(t, env, "Document parent", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Sub A for docs",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "document-task",
		Arguments: map[string]string{"card_id": parent.ID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, content.Text, parent.ID)
	assert.Contains(t, content.Text, "Document parent")
	assert.Contains(t, content.Text, "Sub A for docs")
}

func TestPrompt_CreatePlan_MissingCardID(t *testing.T) {
	env := setupMCP(t)

	_, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "create-plan",
		Arguments: map[string]string{},
	})
	require.Error(t, err)
}

func TestUpdateCard_Priority(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Priority test", "task", "low")

	result := callTool(t, env, "update_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"priority": "critical",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)
	assert.Equal(t, "critical", card.Priority)
	assert.Equal(t, "Priority test", card.Title) // Title unchanged
}

func TestMultipleTransitions(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Multi-transition", "task", "medium")

	transitions := []struct {
		state string
	}{
		{"in_progress"},
		{"blocked"},
		{"in_progress"},
		{"review"},
		{"done"},
	}

	for _, tt := range transitions {
		result := callTool(t, env, "transition_card", map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"new_state": tt.state,
		})
		require.False(t, result.IsError, "transition to %s should succeed", tt.state)

		var card board.Card
		unmarshalResult(t, result, &card)
		assert.Equal(t, tt.state, card.State)
	}
}

func TestAddMultipleLogs(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Multi-log", "task", "medium")

	entries := []struct {
		action  string
		message string
	}{
		{"note", "Started investigation"},
		{"status_update", "Found the root cause"},
		{"blocker", "Need API access"},
		{"decision", "Will use approach B"},
	}

	for _, e := range entries {
		result := callTool(t, env, "add_log", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-multi",
			"action":   e.action,
			"message":  e.message,
		})
		require.False(t, result.IsError)
	}

	// Verify all entries are present
	getResult := callTool(t, env, "get_card", map[string]any{
		"project": "test-project",
		"card_id": "TEST-001",
	})
	var card board.Card
	unmarshalResult(t, getResult, &card)

	require.Len(t, card.ActivityLog, len(entries))
	for i, e := range entries {
		assert.Equal(t, e.action, card.ActivityLog[i].Action)
		assert.Equal(t, e.message, card.ActivityLog[i].Message)
	}
}

func TestReportUsage(t *testing.T) {
	env := setupMCP(t)

	// Create a card
	card := createTestCard(t, env, "Usage test", "task", "medium")

	// Report usage
	result := callTool(t, env, "report_usage", map[string]any{
		"project":           "test-project",
		"card_id":           card.ID,
		"agent_id":          "agent-1",
		"prompt_tokens":     int64(5000),
		"completion_tokens": int64(1500),
	})
	require.False(t, result.IsError)

	var updated board.Card
	unmarshalResult(t, result, &updated)

	require.NotNil(t, updated.TokenUsage)
	assert.Equal(t, int64(5000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(1500), updated.TokenUsage.CompletionTokens)

	// Report again — verify accumulation
	result = callTool(t, env, "report_usage", map[string]any{
		"project":           "test-project",
		"card_id":           card.ID,
		"agent_id":          "agent-1",
		"prompt_tokens":     int64(3000),
		"completion_tokens": int64(1000),
	})
	require.False(t, result.IsError)

	unmarshalResult(t, result, &updated)
	assert.Equal(t, int64(8000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(2500), updated.TokenUsage.CompletionTokens)
}

func TestCreateProject_MCP(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "create_project", map[string]any{
		"name":       "new-project",
		"prefix":     "NEW",
		"repo":       "git@github.com:org/new-project.git",
		"states":     []string{"todo", "in_progress", "done", "stalled"},
		"types":      []string{"task", "bug"},
		"priorities": []string{"low", "high"},
		"transitions": map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
		},
	})
	require.False(t, result.IsError, "create_project should not error")

	var cfg board.ProjectConfig
	unmarshalResult(t, result, &cfg)
	assert.Equal(t, "new-project", cfg.Name)
	assert.Equal(t, "NEW", cfg.Prefix)
	assert.Equal(t, 1, cfg.NextID)

	// Verify project is listable
	listResult := callTool(t, env, "list_projects", map[string]any{})
	require.False(t, listResult.IsError)
	var listOutput listProjectsOutput
	unmarshalResult(t, listResult, &listOutput)
	assert.Len(t, listOutput.Projects, 2) // test-project + new-project
}

func TestUpdateProject_MCP(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "update_project", map[string]any{
		"project":    "test-project",
		"repo":       "git@github.com:org/test.git",
		"states":     []string{"todo", "in_progress", "review", "done", "stalled"},
		"types":      []string{"task", "bug", "feature"},
		"priorities": []string{"low", "medium", "high", "critical"},
		"transitions": map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
		},
	})
	require.False(t, result.IsError, "update_project should not error")

	var cfg board.ProjectConfig
	unmarshalResult(t, result, &cfg)
	assert.Contains(t, cfg.States, "review")
	assert.Equal(t, "git@github.com:org/test.git", cfg.Repo)
}

func TestDeleteProject_MCP(t *testing.T) {
	env := setupMCP(t)

	// Create a project to delete
	createResult := callTool(t, env, "create_project", map[string]any{
		"name":       "temp-project",
		"prefix":     "TMP",
		"states":     []string{"todo", "done", "stalled"},
		"types":      []string{"task"},
		"priorities": []string{"low"},
		"transitions": map[string][]string{
			"todo":    {"done"},
			"done":    {"todo"},
			"stalled": {"todo"},
		},
	})
	require.False(t, createResult.IsError)

	result := callTool(t, env, "delete_project", map[string]any{
		"project": "temp-project",
	})
	require.False(t, result.IsError, "delete_project should not error")

	// Verify deleted
	listResult := callTool(t, env, "list_projects", map[string]any{})
	var listOutput listProjectsOutput
	unmarshalResult(t, listResult, &listOutput)
	assert.Len(t, listOutput.Projects, 1) // only test-project remains
}

func TestInitProjectPrompt(t *testing.T) {
	env := setupMCP(t)

	// List prompts — should include init-project
	result, err := env.session.ListPrompts(context.Background(), nil)
	require.NoError(t, err)

	promptNames := make(map[string]bool)
	for _, p := range result.Prompts {
		promptNames[p.Name] = true
	}
	assert.True(t, promptNames["init-project"], "init-project prompt should be listed")

	// Get prompt with name argument
	promptResult, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "init-project",
		Arguments: map[string]string{"name": "my-new-project"},
	})
	require.NoError(t, err)
	require.Len(t, promptResult.Messages, 1)

	text := promptResult.Messages[0].Content.(*mcp.TextContent).Text
	assert.Contains(t, text, "my-new-project")
	assert.Contains(t, text, "init-project")
	assert.Contains(t, text, "test-project") // existing project should be listed
}

// --- get_skill tool tests ---

func TestGetSkill_CreateTask(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":  "create-task",
		"description": "Build a login page",
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "create-task", out.SkillName)
	assert.Contains(t, out.Content, "Build a login page")
	assert.Contains(t, out.Content, "Skill instructions here.")
}

func TestGetSkill_CreatePlan(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Auth middleware", "task", "high")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "create-plan",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "create-plan", out.SkillName)
	assert.Contains(t, out.Content, card.ID)
	assert.Contains(t, out.Content, "Auth middleware")
	assert.Contains(t, out.Content, "Skill instructions here.")
}

func TestGetSkill_ExecuteTask(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Implement JWT", "task", "high")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "execute-task",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "execute-task", out.SkillName)
	assert.Contains(t, out.Content, card.ID)
	assert.Contains(t, out.Content, "Implement JWT")
}

func TestGetSkill_ReviewTask(t *testing.T) {
	env := setupMCP(t)
	parent := createTestCard(t, env, "Auth feature", "feature", "high")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "review-task",
		"card_id":    parent.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "review-task", out.SkillName)
	assert.Contains(t, out.Content, parent.ID)
}

func TestGetSkill_InitProject(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "init-project",
		"name":       "my-project",
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "init-project", out.SkillName)
	assert.Contains(t, out.Content, "my-project")
	assert.Contains(t, out.Content, "test-project") // existing project listed
}

func TestGetSkill_UnknownSkill(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{"skill_name": "nonexistent"})
	require.True(t, result.IsError, "expected error result for unknown skill")
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "unknown skill")
}

func TestGetSkill_MissingCardID(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{"skill_name": "create-plan"})
	require.True(t, result.IsError, "expected error result for missing card_id")
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "card_id")
}
