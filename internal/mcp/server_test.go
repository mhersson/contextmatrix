package mcp

import (
	"context"
	"encoding/json"
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

// testProjectConfig returns a project config with all states needed for testing.
func testProjectConfig() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"blocked", "review", "todo"},
			"blocked":     {"in_progress", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

// testEnv holds all components needed for MCP server tests.
type testEnv struct {
	session           *mcp.ClientSession
	svc               *service.CardService
	store             storage.Store
	boardsDir         string
	workflowSkillsDir string
	cancel            context.CancelFunc
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

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	// Create workflow-skills directory with stub skill files (including Agent Configuration for model parsing)
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
	}
	for name, model := range skillModels {
		content := fmt.Sprintf("# %s\n\n## Agent Configuration\n\n- **Model:** %s — Test model.\n\n---\n\nSkill instructions here.", name, model)
		require.NoError(t, os.WriteFile(filepath.Join(workflowSkillsDir, name), []byte(content), 0o644))
	}

	// Create MCP server and connect in-memory
	server := NewServer(svc, workflowSkillsDir)

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
		"check_agent_health",
		"get_ready_tasks",
		"report_usage",
		"recalculate_costs",
		"create_project",
		"update_project",
		"delete_project",
		"start_workflow",
		"start_review",
		"get_skill",
		"report_push",
		"increment_review_attempts",
		"promote_to_autonomous",
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

	var output completeTaskOutput
	unmarshalResult(t, result, &output)

	assert.Equal(t, "review", output.Card.State, "main task should stop at review")
	assert.Empty(t, output.Card.AssignedAgent, "agent should be released after completion")

	// Verify next_step is informational only (no action directives)
	assert.Contains(t, output.NextStep, "review", "next_step should reference review")
	assert.Contains(t, output.NextStep, "TEST-001", "next_step should include the card ID")

	// Verify log entry was added
	require.NotEmpty(t, output.Card.ActivityLog)
	lastLog := output.Card.ActivityLog[len(output.Card.ActivityLog)-1]
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

	var output completeTaskOutput
	unmarshalResult(t, result, &output)

	assert.Equal(t, "done", output.Card.State, "subtask should go all the way to done")
	assert.Empty(t, output.Card.AssignedAgent)
	// When there is only one subtask, this is the last subtask done — the
	// response should include an informational next_step about documentation.
	assert.NotEmpty(t, output.NextStep, "last subtask completion should include next_step about documentation")

	// Verify via service layer
	stored, err := env.svc.GetCard(ctx, "test-project", "TEST-002")
	require.NoError(t, err)
	assert.Equal(t, "done", stored.State)
}

// TestCompleteTask_LastSubtaskInfoMessage verifies that completing the last
// subtask includes an informational message about all subtasks being done
// and the parent staying in in_progress for documentation.
func TestCompleteTask_LastSubtaskInfoMessage(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create parent card
	createTestCard(t, env, "Parent task", "feature", "high")

	// Create a single subtask (so completing it makes parent the last one done)
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Only subtask",
		"type":     "task",
		"priority": "medium",
		"parent":   "TEST-001",
	})

	// Claim the subtask
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
	})

	// Complete the last (only) subtask — parent stays in in_progress
	result := callTool(t, env, "complete_task", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
		"summary":  "Only subtask done",
	})
	require.False(t, result.IsError)

	var output completeTaskOutput
	unmarshalResult(t, result, &output)

	// Subtask itself should be done
	assert.Equal(t, "done", output.Card.State, "subtask should be done")
	assert.Empty(t, output.Card.AssignedAgent)

	// Parent stays in in_progress — orchestrator transitions after documentation
	parent, err := env.svc.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "in_progress", parent.State, "parent should stay in in_progress for documentation")

	// next_step should reference documentation, not review
	assert.Contains(t, output.NextStep, "documentation", "next_step should reference documentation")
	assert.Contains(t, output.NextStep, "TEST-001", "next_step should reference the parent card ID")
}

// TestCompleteTask_NonLastSubtaskNoReviewSkill verifies that completing a
// subtask when siblings are still pending does NOT include a review skill.
func TestCompleteTask_NonLastSubtaskNoReviewSkill(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create parent card
	parent := createTestCard(t, env, "Parent task", "feature", "high")

	// Create two subtasks so completing one is not the last
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "First subtask",
		"type":     "task",
		"priority": "medium",
		"parent":   "TEST-001",
	})
	callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Second subtask",
		"type":     "task",
		"priority": "medium",
		"parent":   "TEST-001",
	})

	// Register both subtasks in parent's Subtasks list so maybeTransitionParent
	// can correctly determine that not all siblings are done. In real usage,
	// create-plan does this when it creates the subtasks.
	_, err := env.svc.UpdateCard(ctx, "test-project", parent.ID, service.UpdateCardInput{
		Title:    parent.Title,
		Type:     parent.Type,
		State:    parent.State,
		Priority: parent.Priority,
		Subtasks: []string{"TEST-002", "TEST-003"},
	})
	require.NoError(t, err)

	// Claim and complete only the first subtask
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
	})

	result := callTool(t, env, "complete_task", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-002",
		"agent_id": "agent-sub",
		"summary":  "First subtask done",
	})
	require.False(t, result.IsError)

	var output completeTaskOutput
	unmarshalResult(t, result, &output)

	assert.Equal(t, "done", output.Card.State, "completed subtask should be done")

	// Parent should still be in_progress, not review
	parentCard, gerr := env.svc.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, gerr)
	assert.Equal(t, "in_progress", parentCard.State, "parent should remain in_progress while sibling is pending")

	// No next_step since siblings are still pending
	assert.Empty(t, output.NextStep, "should not have next_step when siblings still pending")
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

func TestClaimCard_NoAutoTransitionFromReview(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create card and move it to review state
	createTestCard(t, env, "Review me", "task", "medium")

	// Transition: todo -> in_progress -> review
	_, err := env.svc.TransitionTo(ctx, "test-project", "TEST-001", "in_progress")
	require.NoError(t, err)
	_, err = env.svc.TransitionTo(ctx, "test-project", "TEST-001", "review")
	require.NoError(t, err)

	// Claim the card in review state — should NOT auto-transition to in_progress
	result := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "review-agent",
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	assert.Equal(t, "review", card.State, "claim should NOT auto-transition from review")
	assert.Equal(t, "review-agent", card.AssignedAgent)
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

func TestCheckAgentHealth(t *testing.T) {
	env := setupMCP(t)

	// Create parent
	parent := createTestCard(t, env, "Health check parent", "feature", "high")

	// Create 3 subtasks
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub A", "type": "task",
		"priority": "medium", "parent": parent.ID,
	})
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub B", "type": "task",
		"priority": "medium", "parent": parent.ID,
	})
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub C", "type": "task",
		"priority": "medium", "parent": parent.ID,
	})

	// Claim Sub A (TEST-002) — will be "active"
	callTool(t, env, "claim_card", map[string]any{
		"project": "test-project", "card_id": "TEST-002", "agent_id": "agent-a",
	})

	// Sub B (TEST-003) stays unclaimed — "unassigned"

	// Complete Sub C (TEST-004) via claim + complete
	callTool(t, env, "claim_card", map[string]any{
		"project": "test-project", "card_id": "TEST-004", "agent_id": "agent-c",
	})
	callTool(t, env, "complete_task", map[string]any{
		"project": "test-project", "card_id": "TEST-004",
		"agent_id": "agent-c", "summary": "Done",
	})

	// Check health
	result := callTool(t, env, "check_agent_health", map[string]any{
		"project":   "test-project",
		"parent_id": parent.ID,
	})
	require.False(t, result.IsError)

	var output checkAgentHealthOutput
	unmarshalResult(t, result, &output)

	assert.Equal(t, parent.ID, output.ParentID)
	assert.Equal(t, int64(1800), output.TimeoutSeconds)
	assert.Equal(t, int64(900), output.WarningSeconds)
	require.Len(t, output.Subtasks, 3)

	// Build map for easier assertions
	byID := make(map[string]AgentHealthStatus)
	for _, s := range output.Subtasks {
		byID[s.CardID] = s
	}

	assert.Equal(t, "active", byID["TEST-002"].Status)
	assert.Equal(t, "agent-a", byID["TEST-002"].AssignedAgent)
	assert.NotNil(t, byID["TEST-002"].SecondsSinceHbeat)

	assert.Equal(t, "unassigned", byID["TEST-003"].Status)
	assert.Empty(t, byID["TEST-003"].AssignedAgent)

	assert.Equal(t, "completed", byID["TEST-004"].Status)

	assert.Contains(t, output.Summary, "1 active")
	assert.Contains(t, output.Summary, "1 completed")
}

func TestCheckAgentHealth_Stalled(t *testing.T) {
	env := setupMCP(t)

	parent := createTestCard(t, env, "Stall test parent", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Stalling sub", "type": "task",
		"priority": "medium", "parent": parent.ID,
	})

	// Claim the subtask
	callTool(t, env, "claim_card", map[string]any{
		"project": "test-project", "card_id": "TEST-002", "agent_id": "agent-stale",
	})

	// Manipulate heartbeat to 31 minutes ago via store
	ctx := context.Background()
	card, err := env.svc.GetCard(ctx, "test-project", "TEST-002")
	require.NoError(t, err)

	staleTime := time.Now().Add(-31 * time.Minute)
	card.LastHeartbeat = &staleTime
	err = env.store.UpdateCard(ctx, "test-project", card)
	require.NoError(t, err)

	// Check health
	result := callTool(t, env, "check_agent_health", map[string]any{
		"project":   "test-project",
		"parent_id": parent.ID,
	})
	require.False(t, result.IsError)

	var output checkAgentHealthOutput
	unmarshalResult(t, result, &output)

	require.Len(t, output.Subtasks, 1)
	assert.Equal(t, "stalled", output.Subtasks[0].Status)
	assert.Equal(t, "agent-stale", output.Subtasks[0].AssignedAgent)
	assert.NotNil(t, output.Subtasks[0].SecondsSinceHbeat)
	assert.GreaterOrEqual(t, *output.Subtasks[0].SecondsSinceHbeat, int64(1860))
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

// TestGetReadyTasks_VettingFilter verifies that unvetted external cards are excluded,
// vetted external cards are included, and internal cards (no source) are always included.
func TestGetReadyTasks_VettingFilter(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create an internal card (no source) — should always appear
	internalCard := createTestCard(t, env, "Internal task", "task", "medium")

	// Create an unvetted external card via store directly (source set, vetted=false)
	unvettedCard := createTestCard(t, env, "Unvetted external task", "task", "medium")
	storedUnvetted, err := env.svc.GetCard(ctx, "test-project", unvettedCard.ID)
	require.NoError(t, err)

	storedUnvetted.Source = &board.Source{System: "github", ExternalID: "42", ExternalURL: "https://github.com/org/repo/issues/42"}
	storedUnvetted.Vetted = false
	require.NoError(t, env.store.UpdateCard(ctx, "test-project", storedUnvetted))

	// Create a vetted external card via store directly (source set, vetted=true)
	vettedCard := createTestCard(t, env, "Vetted external task", "task", "medium")
	storedVetted, err := env.svc.GetCard(ctx, "test-project", vettedCard.ID)
	require.NoError(t, err)

	storedVetted.Source = &board.Source{System: "github", ExternalID: "99", ExternalURL: "https://github.com/org/repo/issues/99"}
	storedVetted.Vetted = true
	require.NoError(t, env.store.UpdateCard(ctx, "test-project", storedVetted))

	// Get ready tasks
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

	assert.True(t, readyIDs[internalCard.ID], "internal card (no source) should always be ready")
	assert.True(t, readyIDs[vettedCard.ID], "vetted external card should be ready")
	assert.False(t, readyIDs[unvettedCard.ID], "unvetted external card must be excluded")
}

// TestClaimCard_UnvettedExternal verifies that claiming an unvetted external card returns an error.
func TestClaimCard_UnvettedExternal(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Create a card and mark it as an unvetted external import
	card := createTestCard(t, env, "Unvetted import", "task", "medium")
	stored, err := env.svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	stored.Source = &board.Source{System: "github", ExternalID: "7", ExternalURL: "https://github.com/org/repo/issues/7"}
	stored.Vetted = false
	require.NoError(t, env.store.UpdateCard(ctx, "test-project", stored))

	// Agent tries to claim the unvetted card — must fail
	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "claim_card",
		Arguments: map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "agent-x",
		},
	})
	if err != nil {
		assert.Contains(t, err.Error(), "vetted")

		return
	}

	require.True(t, result.IsError, "claiming unvetted external card should produce an error result")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "vetted")
}

func TestListPrompts(t *testing.T) {
	env := setupMCP(t)

	result, err := env.session.ListPrompts(context.Background(), nil)
	require.NoError(t, err)

	expectedPrompts := []string{
		"create-task",
		"init-project",
		"start-workflow",
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
	// Prompt handlers now return raw skill content, not delegation wrappers.
	assert.Contains(t, content.Text, "ContextMatrix Workflow Rules")
	assert.Contains(t, content.Text, "Skill instructions here.")
	assert.Contains(t, content.Text, "Build a new login page")
	assert.NotContains(t, content.Text, "Subagent Required")
	assert.NotContains(t, content.Text, "`Agent` tool")
	assert.NotContains(t, content.Text, "Do NOT execute it inline")
	assert.NotContains(t, content.Text, "## Agent Configuration")
}

// TestPrompt_StartWorkflow_NonAutonomous verifies that start-workflow routes to
// the create-plan prompt when the card does not have autonomous: true.
func TestPrompt_StartWorkflow_NonAutonomous(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "HITL feature", "feature", "high")

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "start-workflow",
		Arguments: map[string]string{"card_id": "TEST-001"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)

	// Must produce the create-plan skill content (HITL path — start-workflow
	// inlines the create-plan dispatch when the card is not autonomous).
	assert.Contains(t, content.Text, "create-plan.md")
	assert.Contains(t, content.Text, "Skill instructions here.")
	assert.Contains(t, content.Text, "TEST-001")
	assert.Contains(t, content.Text, "ContextMatrix Workflow Rules")
	// Agent Configuration section must be stripped.
	assert.NotContains(t, content.Text, "## Agent Configuration")
	// Must NOT be the autonomous prompt content.
	assert.NotContains(t, content.Text, "autonomous orchestrator")
	// Must NOT contain old delegation-wrapper text.
	assert.NotContains(t, content.Text, "Planning Workflow")
	assert.NotContains(t, content.Text, "Plan Drafting — Always Inline")
}

// TestPrompt_StartWorkflow_Autonomous verifies that start-workflow routes to
// the run-autonomous prompt when the card has autonomous: true.
func TestPrompt_StartWorkflow_Autonomous(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Autonomous feature", "feature", "high")

	autonomous := true
	_, err := env.svc.PatchCard(context.Background(), "test-project", "TEST-001", service.PatchCardInput{
		Autonomous: &autonomous,
	})
	require.NoError(t, err)

	result, err := env.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      "start-workflow",
		Arguments: map[string]string{"card_id": "TEST-001"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Messages)

	content, ok := result.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)

	// Must produce the run-autonomous prompt content (stripped of Agent Configuration):
	// the card context is injected, and the stub skill body appears.
	assert.Contains(t, content.Text, "run-autonomous.md")
	assert.Contains(t, content.Text, "Skill instructions here.")
	assert.Contains(t, content.Text, "TEST-001")
	// The card's autonomous flag must appear in the injected card context.
	assert.Contains(t, content.Text, "**Autonomous:** true")
	// Must NOT contain the HITL planning workflow.
	assert.NotContains(t, content.Text, "Planning Workflow")
	assert.NotContains(t, content.Text, "Plan Drafting")
	// Agent Configuration section must be stripped.
	assert.NotContains(t, content.Text, "## Agent Configuration")
}

// TestTool_StartWorkflow_NonAutonomous verifies the start_workflow tool returns
// full create-plan skill content for non-autonomous cards.
func TestTool_StartWorkflow_NonAutonomous(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "HITL feature", "feature", "high")

	result := callTool(t, env, "start_workflow", map[string]any{
		"card_id": "TEST-001",
	})
	require.False(t, result.IsError)

	var out startWorkflowOutput
	unmarshalResult(t, result, &out)

	assert.Equal(t, "create-plan", out.SkillName)
	assert.True(t, out.Inline, "start_workflow should always return inline=true")
	assert.Contains(t, out.Content, "INLINE EXECUTION")
	assert.Contains(t, out.Content, "create-plan.md")
	assert.Contains(t, out.Content, "Skill instructions here.")
	assert.Contains(t, out.Content, "TEST-001")
	assert.NotContains(t, out.Content, "## Agent Configuration")
}

// TestTool_StartWorkflow_Autonomous verifies the start_workflow tool returns
// full run-autonomous skill content for autonomous cards.
func TestTool_StartWorkflow_Autonomous(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Autonomous feature", "feature", "high")

	autonomous := true
	_, err := env.svc.PatchCard(context.Background(), "test-project", "TEST-001", service.PatchCardInput{
		Autonomous: &autonomous,
	})
	require.NoError(t, err)

	result := callTool(t, env, "start_workflow", map[string]any{
		"card_id": "TEST-001",
	})
	require.False(t, result.IsError)

	var out startWorkflowOutput
	unmarshalResult(t, result, &out)

	assert.Equal(t, "run-autonomous", out.SkillName)
	assert.True(t, out.Inline, "start_workflow should always return inline=true")
	assert.Contains(t, out.Content, "INLINE EXECUTION")
	assert.Contains(t, out.Content, "run-autonomous.md")
	assert.Contains(t, out.Content, "TEST-001")
	assert.Contains(t, out.Content, "**Autonomous:** true")
	assert.NotContains(t, out.Content, "## Agent Configuration")
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
		"states":     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		"types":      []string{"task", "bug"},
		"priorities": []string{"low", "high"},
		"transitions": map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
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
		"states":     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		"types":      []string{"task", "bug", "feature"},
		"priorities": []string{"low", "medium", "high", "critical"},
		"transitions": map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
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
		"states":     []string{"todo", "done", "stalled", "not_planned"},
		"types":      []string{"task"},
		"priorities": []string{"low"},
		"transitions": map[string][]string{
			"todo":        {"done"},
			"done":        {"todo"},
			"stalled":     {"todo"},
			"not_planned": {"todo"},
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

// TestCreateCard_SubtaskTypeEnforced verifies that creating a card via MCP with
// a parent always results in type=subtask regardless of the type passed in.
func TestCreateCard_SubtaskTypeEnforced(t *testing.T) {
	env := setupMCP(t)

	// Create parent card
	parent := createTestCard(t, env, "Parent task", "feature", "high")

	// Create a subtask passing type="task" explicitly — backend should override to "subtask"
	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Child card",
		"type":     "task",
		"priority": "medium",
		"parent":   parent.ID,
	})
	require.False(t, result.IsError, "create_card with parent should not error")

	var card board.Card
	unmarshalResult(t, result, &card)

	assert.Equal(t, "subtask", card.Type, "type should be overridden to 'subtask' when parent is set")
	assert.Equal(t, parent.ID, card.Parent)

	// Also verify with type="bug" — it should still be overridden
	result2 := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Another child",
		"type":     "bug",
		"priority": "low",
		"parent":   parent.ID,
	})
	require.False(t, result2.IsError)

	var card2 board.Card
	unmarshalResult(t, result2, &card2)
	assert.Equal(t, "subtask", card2.Type, "type should be overridden regardless of passed type value")
}

// TestCreateCard_TypePreservedWithoutParent verifies that creating a card via
// MCP without a parent preserves the type as given.
func TestCreateCard_TypePreservedWithoutParent(t *testing.T) {
	env := setupMCP(t)

	for _, typ := range []string{"task", "bug", "feature"} {
		result := callTool(t, env, "create_card", map[string]any{
			"project":  "test-project",
			"title":    "Card type " + typ,
			"type":     typ,
			"priority": "medium",
		})
		require.False(t, result.IsError, "create_card type=%s should not error", typ)

		var card board.Card
		unmarshalResult(t, result, &card)
		assert.Equal(t, typ, card.Type, "type=%s should be preserved when no parent is set", typ)
		assert.Empty(t, card.Parent, "card should have no parent")
	}
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
	// Prompt handlers now return raw skill content, not delegation wrappers.
	assert.Contains(t, text, "ContextMatrix Workflow Rules")
	assert.Contains(t, text, "Skill instructions here.")
	assert.Contains(t, text, "my-new-project")
	assert.NotContains(t, text, "Subagent Required")
	assert.NotContains(t, text, "`Agent` tool")
	assert.NotContains(t, text, "Do NOT execute it inline")
	assert.NotContains(t, text, "## Agent Configuration")
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
	assert.Equal(t, "sonnet", out.Model)
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
	assert.Equal(t, "sonnet", out.Model)
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
	assert.Equal(t, "sonnet", out.Model)
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
	assert.Equal(t, "opus", out.Model)
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
	assert.Equal(t, "sonnet", out.Model)
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

func TestGetSkill_Brainstorming(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Improve the dashboard", "task", "medium")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "brainstorming",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "brainstorming", out.SkillName)
	assert.Equal(t, "sonnet", out.Model)
	assert.Contains(t, out.Content, card.ID, "skill content should include the card ID context")
	assert.Contains(t, out.Content, "Improve the dashboard", "skill content should include the card title")
}

func TestGetSkill_SystematicDebugging(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Login crashes on submit", "bug", "high")

	// systematic-debugging is NOT inline-eligible — it always runs as a
	// sub-agent. Even when caller_model matches, Inline must be false so
	// the orchestrator spawns it via the Agent tool with worktree isolation.
	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "systematic-debugging",
		"card_id":      card.ID,
		"caller_model": "sonnet",
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "systematic-debugging", out.SkillName)
	assert.Equal(t, "sonnet", out.Model)
	assert.False(t, out.Inline, "systematic-debugging must not be inline-eligible")
	assert.Contains(t, out.Content, card.ID, "skill content should include the card ID context")
	assert.Contains(t, out.Content, "Login crashes on submit", "skill content should include the card title")
}

func TestGetSkill_BrainstormingInline(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Inline test card", "task", "low")

	// brainstorming is on the inline-eligible whitelist; when caller_model
	// matches the skill model (both Sonnet), the response must come back
	// with Inline: true wrapped in the lifecycle envelope. This is required
	// for create-plan to run brainstorming inline rather than spawning a
	// sub-agent (sub-agents have no chat channel for dialogue).
	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "brainstorming",
		"card_id":      card.ID,
		"caller_model": "sonnet",
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.Inline, "brainstorming should be inline-eligible when caller_model matches")
	assert.Contains(t, out.Content, "INLINE EXECUTION", "inline response must include the lifecycle envelope")
}

func TestWorkflowPreambleInjected(t *testing.T) {
	env := setupMCP(t)

	// Test skills that don't require a card_id
	skills := []struct {
		name string
		args map[string]any
	}{
		{"create-task", map[string]any{"skill_name": "create-task"}},
		{"create-task-with-desc", map[string]any{"skill_name": "create-task", "description": "Fix the login bug"}},
		{"init-project", map[string]any{"skill_name": "init-project"}},
	}

	for _, s := range skills {
		t.Run(s.name, func(t *testing.T) {
			result := callTool(t, env, "get_skill", s.args)
			require.False(t, result.IsError)

			var out getSkillOutput
			unmarshalResult(t, result, &out)
			assert.Contains(t, out.Content, "ContextMatrix Workflow Rules",
				"skill %q content should contain workflow preamble", s.name)
			assert.Contains(t, out.Content, "Never work on a card without claiming it first",
				"skill %q preamble should include claim rule", s.name)
			assert.Contains(t, out.Content, "Always use MCP tools for ContextMatrix interactions",
				"skill %q preamble should include MCP-only rule", s.name)
		})
	}

	// Also test a skill that requires a card_id
	card := createTestCard(t, env, "Preamble test", "task", "medium")
	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "execute-task",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Contains(t, out.Content, "ContextMatrix Workflow Rules",
		"execute-task should contain workflow preamble")
}

func TestGetSkill_MissingCardID(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{"skill_name": "create-plan"})
	require.True(t, result.IsError, "expected error result for missing card_id")
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "card_id")
}

func TestParseSkillModel(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "sonnet model",
			content: "## Agent Configuration\n\n- **Model:** claude-sonnet-4-6 — Workhorse.\n\n---\n\nInstructions.",
			want:    "sonnet",
		},
		{
			name:    "opus model",
			content: "## Agent Configuration\n\n- **Model:** claude-opus-4-6 — Planning.\n\n---\n\nInstructions.",
			want:    "opus",
		},
		{
			name:    "haiku model",
			content: "## Agent Configuration\n\n- **Model:** claude-haiku-4-5 — Fast.\n\n---\n\nInstructions.",
			want:    "haiku",
		},
		{
			name:    "no config section",
			content: "# Skill\n\nJust instructions.",
			want:    "",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseSkillModel(tt.content))
		})
	}
}

func TestStripAgentConfig(t *testing.T) {
	input := "# Skill\n\n## Agent Configuration\n\n- **Model:** claude-sonnet-4-6 — Test.\n\n---\n\nInstructions here."
	got := stripAgentConfig(input)
	assert.NotContains(t, got, "Agent Configuration")
	assert.NotContains(t, got, "claude-sonnet")
	assert.Contains(t, got, "Instructions here.")
	assert.Contains(t, got, "# Skill")
}

// TestGetSkill_StripsAgentConfig verifies that get_skill output never contains
// the "## Agent Configuration" section — that metadata is for the orchestrator.
func TestGetSkill_StripsAgentConfig(t *testing.T) {
	env := setupMCP(t)

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "execute-task",
		"card_id":    createTestCard(t, env, "Strip test", "task", "medium").ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.NotContains(t, out.Content, "## Agent Configuration",
		"get_skill content should not contain Agent Configuration section")
	// Model is still parsed and returned separately
	assert.Equal(t, "sonnet", out.Model)
	// Skill body instructions should still be present
	assert.Contains(t, out.Content, "Skill instructions here.")
}

// TestBuildInlineExecutionPrompt verifies the lifecycle-enforcing inline wrapper.
func TestBuildInlineExecutionPrompt(t *testing.T) {
	content := "## ContextMatrix Workflow Rules\n\nSome rules.\n\nSkill instructions here."
	cardID := "ALPHA-003"
	skillName := "review-task"

	result := buildInlineExecutionPrompt(content, cardID, skillName)

	// Must contain lifecycle enforcement gates.
	assert.Contains(t, result, "INLINE EXECUTION")
	assert.Contains(t, result, "Lifecycle Checkpoints Required")
	assert.Contains(t, result, "claim_card")
	assert.Contains(t, result, "heartbeat")
	assert.Contains(t, result, "release_card")
	assert.Contains(t, result, "report_usage")
	assert.Contains(t, result, cardID)
	assert.Contains(t, result, skillName)

	// Must contain the skill content.
	assert.Contains(t, result, "Skill instructions here.")
	assert.Contains(t, result, "BEGIN SKILL INSTRUCTIONS")
	assert.Contains(t, result, "END SKILL INSTRUCTIONS")

	// Must contain verification step.
	assert.Contains(t, result, "get_card")
}

// TestIsInlineEligible verifies the inline eligibility whitelist.
func TestIsInlineEligible(t *testing.T) {
	assert.True(t, isInlineEligible("review-task"))
	assert.True(t, isInlineEligible("create-plan"))
	assert.True(t, isInlineEligible("brainstorming"))
	assert.False(t, isInlineEligible("execute-task"))
	assert.False(t, isInlineEligible("document-task"))
	assert.False(t, isInlineEligible("create-task"))
	assert.False(t, isInlineEligible("init-project"))
	assert.False(t, isInlineEligible("systematic-debugging"))
	assert.False(t, isInlineEligible("nonexistent"))
}

// TestGetSkill_InlineWhenModelMatches verifies that get_skill returns inline=true
// when caller_model matches the skill model and the skill is inline-eligible.
func TestGetSkill_InlineWhenModelMatches(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Inline review test", "feature", "high")
	// Create a subtask so review-task has something to review.
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub for inline", "type": "task",
		"priority": "medium", "parent": card.ID,
	})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "review-task",
		"card_id":      card.ID,
		"caller_model": "opus", // review-task requires opus — match
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.Inline, "inline should be true when caller model matches")
	assert.Equal(t, "opus", out.Model)
	assert.Contains(t, out.Content, "INLINE EXECUTION")
	assert.Contains(t, out.Content, "Lifecycle Checkpoints Required")
}

// TestGetSkill_InlineCaseInsensitive verifies that caller_model matching is
// case-insensitive — agents may pass "Opus" from their system context.
func TestGetSkill_InlineCaseInsensitive(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Case test", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub for case", "type": "task",
		"priority": "medium", "parent": card.ID,
	})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "review-task",
		"card_id":      card.ID,
		"caller_model": "Opus", // Capital O — should still match "opus"
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.Inline, "inline should be true even with different casing")
}

// TestGetSkill_InlineFullModelID verifies that get_skill returns inline=true
// when caller_model is a full model ID like "claude-opus-4-6" rather than
// the short family name "opus". Agents commonly pass the full ID from their
// system context.
func TestGetSkill_InlineFullModelID(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Full model ID test", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub for full ID", "type": "task",
		"priority": "medium", "parent": card.ID,
	})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "review-task",
		"card_id":      card.ID,
		"caller_model": "claude-opus-4-6", // full model ID — should match "opus"
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.Inline, "inline should be true when full model ID matches skill family")
	assert.Contains(t, out.Content, "INLINE EXECUTION")
}

// TestGetSkill_DelegatesWhenModelMismatch verifies that get_skill returns
// inline=false when caller_model does not match the skill model.
func TestGetSkill_DelegatesWhenModelMismatch(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Mismatch test", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub for mismatch", "type": "task",
		"priority": "medium", "parent": card.ID,
	})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "review-task",
		"card_id":      card.ID,
		"caller_model": "sonnet", // review-task requires opus — mismatch
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.False(t, out.Inline, "inline should be false when caller model mismatches")
	assert.NotContains(t, out.Content, "INLINE EXECUTION")
}

// TestGetSkill_DelegatesWhenCallerModelAbsent verifies backward compatibility:
// when caller_model is not provided, inline is always false.
func TestGetSkill_DelegatesWhenCallerModelAbsent(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "No caller model test", "feature", "high")
	callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Sub for absent", "type": "task",
		"priority": "medium", "parent": card.ID,
	})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "review-task",
		"card_id":    card.ID,
		// No caller_model — backward compat
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.False(t, out.Inline, "inline should be false when caller_model is absent")
	assert.NotContains(t, out.Content, "INLINE EXECUTION")
}

// TestGetSkill_InlineNotEligibleSkill verifies that non-whitelisted skills
// always return inline=false even when model matches.
func TestGetSkill_InlineNotEligibleSkill(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Not eligible test", "task", "medium")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "execute-task",
		"card_id":      card.ID,
		"caller_model": "sonnet", // execute-task requires sonnet — match, but not eligible
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.False(t, out.Inline, "inline should be false for non-eligible skills even with model match")
	assert.NotContains(t, out.Content, "INLINE EXECUTION")
}

// TestGetSkill_CreatePlanInline verifies that create-plan returns inline=true
// when caller_model matches sonnet (the skill's model).
func TestGetSkill_CreatePlanInline(t *testing.T) {
	env := setupMCP(t)
	card := createTestCard(t, env, "Plan inline test", "feature", "high")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name":   "create-plan",
		"card_id":      card.ID,
		"caller_model": "sonnet", // create-plan uses sonnet — match
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.True(t, out.Inline, "inline should be true for create-plan with sonnet caller")
	assert.Equal(t, "sonnet", out.Model)
	assert.Contains(t, out.Content, "INLINE EXECUTION")
}

// --- Tests for project-less tool calls (project resolved from card ID) ---

func TestGetCard_WithoutProject(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "No-project get", "task", "medium")

	// Call get_card without project — should resolve from card ID
	result := callTool(t, env, "get_card", map[string]any{
		"card_id": card.ID,
	})
	require.False(t, result.IsError, "get_card without project should succeed")

	var got board.Card
	unmarshalResult(t, result, &got)
	assert.Equal(t, card.ID, got.ID)
	assert.Equal(t, "No-project get", got.Title)
}

func TestGetTaskContext_WithoutProject(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "Context no-project", "task", "high")

	result := callTool(t, env, "get_task_context", map[string]any{
		"card_id": card.ID,
	})
	require.False(t, result.IsError, "get_task_context without project should succeed")

	var output getTaskContextOutput
	unmarshalResult(t, result, &output)
	require.NotNil(t, output.Card)
	assert.Equal(t, card.ID, output.Card.ID)
	require.NotNil(t, output.Config)
	assert.Equal(t, "test-project", output.Config.Name)
}

func TestCompleteTask_WithoutProject(t *testing.T) {
	env := setupMCP(t)

	// Create and claim a card (with project, since create_card requires it)
	card := createTestCard(t, env, "Complete no-project", "task", "medium")
	claimResult := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "test-agent",
	})
	require.False(t, claimResult.IsError)

	// Complete without project — should resolve from card ID
	result := callTool(t, env, "complete_task", map[string]any{
		"card_id":  card.ID,
		"agent_id": "test-agent",
		"summary":  "Done without project param",
	})
	require.False(t, result.IsError, "complete_task without project should succeed")

	var output completeTaskOutput
	unmarshalResult(t, result, &output)
	assert.Equal(t, "review", output.Card.State)
}

func TestClaimCard_WithoutProject(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "Claim no-project", "task", "low")

	result := callTool(t, env, "claim_card", map[string]any{
		"card_id":  card.ID,
		"agent_id": "agent-1",
	})
	require.False(t, result.IsError, "claim_card without project should succeed")

	var got board.Card
	unmarshalResult(t, result, &got)
	assert.Equal(t, "agent-1", got.AssignedAgent)
	assert.Equal(t, "in_progress", got.State)
}

// TestCreatePlanSkill_AutonomousGates verifies that workflow-skills/create-plan.md
// contains the autonomous-mode conditional branches at all four user-prompt
// gates: plan-approval, execution, review-approval, and commit/push/PR. This is
// a regression guard — if the autonomous branches are removed from the skill
// file, this test will fail.
func TestCreatePlanSkill_AutonomousGates(t *testing.T) {
	// Read the real skill file directly. The working directory for go test is
	// the package directory (internal/mcp), so ../../workflow-skills reaches the repo root.
	skillPath := filepath.Join("..", "..", "workflow-skills", "create-plan.md")
	data, err := os.ReadFile(skillPath)
	require.NoError(t, err, "workflow-skills/create-plan.md must be readable")

	content := string(data)

	// Every gate must call get_card to re-read the autonomous flag.
	assert.Contains(t, content, "autonomous: true",
		"create-plan.md must reference autonomous: true")

	// Gate 1 (Phase 2): plan-approval gate — autonomous skips the plan approval prompt.
	assert.Contains(t, content, "Phase 2: Plan Approval Gate",
		"create-plan.md must have Phase 2: Plan Approval Gate")
	assert.Regexp(t, `(?si)Phase 2: Plan Approval Gate.*autonomous: true.*skip this phase`,
		content,
		"create-plan.md Phase 2 must have autonomous skip instruction")

	// Gate 2 (Phase 4): execution gate — autonomous skips "Want me to start execution?" prompt.
	assert.Contains(t, content, "Phase 4: Execution Gate",
		"create-plan.md must have Phase 4: Execution Gate")
	assert.Regexp(t, `(?si)Phase 4: Execution Gate.*autonomous: true.*skip this phase`,
		content,
		"create-plan.md Phase 4 must have autonomous skip instruction")

	// Gate 3 (Phase 8): review approval gate — autonomous skips "Do you approve this work" prompt.
	assert.Contains(t, content, "Phase 8: Review Decision Gate",
		"create-plan.md must have Phase 8: Review Decision Gate")
	assert.Contains(t, content, "AUTONOMOUS_HALTED",
		"create-plan.md must emit AUTONOMOUS_HALTED when review cycles are exhausted")
	assert.Contains(t, content, "increment_review_attempts",
		"create-plan.md must call increment_review_attempts in the autonomous review gate")

	// Gate 4 (Phase 9): commit/push/PR gate — autonomous skips both prompts.
	assert.Contains(t, content, "Phase 9: Commit/Push/PR Gate",
		"create-plan.md must have Phase 9: Commit/Push/PR Gate")
	// Phase 9 must distinguish the autonomous mode and wire it to the
	// auto-commit path (no user prompt).
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*Autonomous`,
		content,
		"create-plan.md Phase 9 must reference the Autonomous mode")
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*Auto-commit`,
		content,
		"create-plan.md Phase 9 must have auto-commit step")
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*Push the feature branch`,
		content,
		"create-plan.md Phase 9 must have push step")
}

// TestCreatePlanSkill_Phase9RunnerContextBranches verifies that Phase 9 of
// workflow-skills/create-plan.md contains both the runner-context auto-commit branch
// for remote HITL and the local-HITL user prompts. This is a regression guard
// so that accidental removal of either branch is caught.
func TestCreatePlanSkill_Phase9RunnerContextBranches(t *testing.T) {
	skillPath := filepath.Join("..", "..", "workflow-skills", "create-plan.md")
	data, err := os.ReadFile(skillPath)
	require.NoError(t, err, "workflow-skills/create-plan.md must be readable")

	content := string(data)

	// Remote HITL branch must be present in Phase 9.
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*Remote HITL`,
		content,
		"create-plan.md Phase 9 must reference Remote HITL")

	// Remote HITL path must detect runner context via CM_CARD_ID presence,
	// not via the older CM_INTERACTIVE check (which produced ambiguous
	// agent-side reads when the env var was unset).
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*CM_CARD_ID`,
		content,
		"create-plan.md Phase 9 must use CM_CARD_ID to detect runner context")

	// Auto-commit path (autonomous + remote HITL) must forbid prompting. The
	// "do not prompt" rule must appear after the Remote HITL branch reference
	// so the agent cannot miss it on the way through.
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*Remote HITL.*Do\s+not\s+prompt\s+the\s+user`,
		content,
		"create-plan.md Phase 9 auto-commit path must forbid prompting the user")

	// Local HITL path must retain the original "Want me to commit" prompt.
	assert.Contains(t, content, "Want me to commit these changes?",
		"create-plan.md Phase 9 must retain local-HITL commit prompt")

	// Local HITL path must retain the original "Want me to push" prompt.
	assert.Contains(t, content, "Want me to push and create a PR?",
		"create-plan.md Phase 9 must retain local-HITL push prompt")

	// Local HITL path must be gated on the runner-context detection landing
	// on the `local` outcome.
	assert.Regexp(t, `(?si)Phase 9: Commit/Push/PR Gate.*`+"`local`"+`.*Local HITL`,
		content,
		"create-plan.md Phase 9 must describe local HITL as the `local` runner-context outcome")
}

// TestCreatePlanSkillIsSelfContained verifies that workflow-skills/create-plan.md is a
// linear Phase 1-10 skill with autonomous rechecks at each HITL gate. This is
// the primary regression guard ensuring the skill can be executed top-to-bottom
// by a Remote HITL container agent without an orchestrator wrapper.
func TestCreatePlanSkillIsSelfContained(t *testing.T) {
	// Read the real skill file directly. The working directory for go test is
	// the package directory (internal/mcp), so ../../workflow-skills reaches the repo root.
	skillPath := filepath.Join("..", "..", "workflow-skills", "create-plan.md")
	data, err := os.ReadFile(skillPath)
	require.NoError(t, err, "workflow-skills/create-plan.md must be readable")

	content := string(data)

	// All 10 phases must be present as top-level headings.
	phases := []string{
		"Phase 1: Plan Drafting",
		"Phase 2: Plan Approval Gate",
		"Phase 3: Subtask Creation",
		"Phase 4: Execution Gate",
		"Phase 5: Execution",
		"Phase 6: Documentation",
		"Phase 7: Review",
		"Phase 8: Review Decision Gate",
		"Phase 9: Commit/Push/PR Gate",
		"Phase 10: Finalization",
	}
	for _, phase := range phases {
		assert.Contains(t, content, phase,
			"create-plan.md must contain: "+phase)
	}

	// Old split-phase heading must not exist.
	assert.NotContains(t, content, "# After subtasks are created — Execution (orchestrator)",
		"create-plan.md must not contain the old split-phase heading")

	// PLAN_DRAFTED structured output must be present.
	assert.Contains(t, content, "PLAN_DRAFTED",
		"create-plan.md must emit PLAN_DRAFTED structured output")
	assert.Contains(t, content, "subtask_count",
		"create-plan.md must include subtask_count in PLAN_DRAFTED output")
	assert.Contains(t, content, "plan_summary",
		"create-plan.md must include plan_summary in PLAN_DRAFTED output")

	// Phase 2 (plan approval) must open with get_card autonomous check.
	assert.Regexp(t, `(?s)Phase 2: Plan Approval Gate.*?get_card`,
		content,
		"Phase 2 must open with a get_card call")

	// Phase 4 (execution gate) must open with get_card autonomous check.
	assert.Regexp(t, `(?s)Phase 4: Execution Gate.*?get_card`,
		content,
		"Phase 4 must open with a get_card call")

	// Phase 8 (review decision) must open with get_card autonomous check.
	assert.Regexp(t, `(?s)Phase 8: Review Decision Gate.*?get_card`,
		content,
		"Phase 8 must open with a get_card call")

	// Phase 9 (commit/push/PR) must open with get_card autonomous check.
	assert.Regexp(t, `(?s)Phase 9: Commit/Push/PR Gate.*?get_card`,
		content,
		"Phase 9 must open with a get_card call")

	// Phase 3 must contain subtask dedupe instruction.
	assert.Regexp(t, `(?s)Phase 3: Subtask Creation.*?non-terminal`,
		content,
		"Phase 3 must contain subtask dedupe instruction (non-terminal check)")

	// Rejection loop must not re-fetch the skill.
	assert.Contains(t, content, "Rejection Loop",
		"create-plan.md must contain Rejection Loop section")
	assert.Regexp(t, `(?s)Rejection Loop.*?Do NOT call.*?get_skill`,
		content,
		"Rejection Loop must instruct agent NOT to call get_skill recursively")

	// Phase 10 finalization must contain mandatory release_card instruction.
	assert.Regexp(t, `(?s)Phase 10: Finalization.*?release_card`,
		content,
		"Phase 10 must contain release_card call")

	// report_push must be referenced for push scenarios.
	assert.Contains(t, content, "report_push",
		"create-plan.md must call report_push on successful push")

	// Phase 5 must force sub-agent spawning for execute-task. Inline execution
	// would break context isolation and parallel worktree handling, so the
	// skill MUST explicitly forbid inline even if get_skill returns inline:true.
	assert.Contains(t, content, "Phase 5: Execution (always sub-agents)",
		"Phase 5 heading must declare (always sub-agents) discipline")
	assert.Regexp(t, `(?s)Phase 5: Execution \(always sub-agents\).*?Do NOT\s+execute\s+inline.*?Phase 6`,
		content,
		"Phase 5 must forbid inline execution even if inline:true is returned")
}

// --- promote_to_autonomous tool tests ---

func TestPromoteToAutonomous_MCP(t *testing.T) {
	ctx := context.Background()

	t.Run("flips autonomous flag on a non-autonomous card", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Promote me", "task", "medium")
		assert.False(t, card.Autonomous)

		result := callTool(t, env, "promote_to_autonomous", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.False(t, result.IsError, "promote_to_autonomous should not error")

		var updated board.Card
		unmarshalResult(t, result, &updated)
		assert.True(t, updated.Autonomous, "card must be autonomous after promote")

		// Verify via get_card.
		getResult := callTool(t, env, "get_card", map[string]any{
			"project": "test-project",
			"card_id": card.ID,
		})
		require.False(t, getResult.IsError)

		var fetched board.Card
		unmarshalResult(t, getResult, &fetched)
		assert.True(t, fetched.Autonomous)
		require.Len(t, fetched.ActivityLog, 1)
		assert.Equal(t, "promoted", fetched.ActivityLog[0].Action)
		assert.Equal(t, "Promoted to autonomous mode", fetched.ActivityLog[0].Message)
		assert.Equal(t, "human:alice", fetched.ActivityLog[0].Agent)
	})

	t.Run("idempotent: already-autonomous card succeeds without extra log entry", func(t *testing.T) {
		env := setupMCP(t)

		// Create card with autonomous already true (requires service-layer direct call).
		card, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title:      "Already autonomous",
			Type:       "task",
			Priority:   "medium",
			Autonomous: true,
		})
		require.NoError(t, err)

		result := callTool(t, env, "promote_to_autonomous", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:bob",
		})
		require.False(t, result.IsError, "idempotent promote must not error")

		var updated board.Card
		unmarshalResult(t, result, &updated)
		assert.True(t, updated.Autonomous)
		assert.Empty(t, updated.ActivityLog, "no extra log entry for idempotent promote")
	})

	t.Run("resolves project from card ID when project omitted", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "No project", "task", "medium")

		result := callTool(t, env, "promote_to_autonomous", map[string]any{
			"card_id":  card.ID,
			"agent_id": "human:carol",
		})
		require.False(t, result.IsError, "promote must work without explicit project")

		var updated board.Card
		unmarshalResult(t, result, &updated)
		assert.True(t, updated.Autonomous)
	})

	t.Run("errors on done card", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Done card", "task", "medium")

		// Transition to done.
		_, err := env.svc.TransitionTo(ctx, "test-project", card.ID, "in_progress")
		require.NoError(t, err)
		_, err = env.svc.TransitionTo(ctx, "test-project", card.ID, "done")
		require.NoError(t, err)

		result := callTool(t, env, "promote_to_autonomous", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		assert.True(t, result.IsError, "promote must error for done card")
	})
}

func TestCreateCard_AcceptsSkills(t *testing.T) {
	env := setupMCP(t)

	skills := []string{"go-development"}
	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Skill test card",
		"type":     "task",
		"priority": "medium",
		"skills":   skills,
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)
	assert.NotNil(t, card.Skills)
	assert.Equal(t, skills, *card.Skills)
}

func TestUpdateCard_AcceptsSkills(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "Update skill test", "task", "low")

	skills := []string{"typescript-react"}
	result := callTool(t, env, "update_card", map[string]any{
		"project": "test-project",
		"card_id": card.ID,
		"skills":  skills,
	})
	require.False(t, result.IsError)

	var updated board.Card
	unmarshalResult(t, result, &updated)
	assert.NotNil(t, updated.Skills)
	assert.Equal(t, skills, *updated.Skills)
}

func TestCreateCard_PreservesSkillsThroughDependsOn(t *testing.T) {
	env := setupMCP(t)

	// Create a blocker card so depends_on points at a real ID
	blocker := createTestCard(t, env, "Blocker card", "task", "medium")

	// Create main card with both skills AND depends_on
	skills := []string{"go-development"}
	result := callTool(t, env, "create_card", map[string]any{
		"project":    "test-project",
		"title":      "Main card with skills and deps",
		"type":       "task",
		"priority":   "medium",
		"skills":     skills,
		"depends_on": []string{blocker.ID},
	})
	require.False(t, result.IsError)

	var card board.Card
	unmarshalResult(t, result, &card)

	// Assert: returned card has the skills set (proving the depends_on follow-up
	// did not strip them)
	assert.NotNil(t, card.Skills)
	assert.Equal(t, skills, *card.Skills)
	assert.Equal(t, []string{blocker.ID}, card.DependsOn)
}
