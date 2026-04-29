package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// callToolRaw calls an MCP tool and returns the raw result without fataling on
// IsError so that tests can inspect error responses.
func callToolRaw(t *testing.T, env *testEnv, name string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()

	return env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// resultIsError returns true when the tool result contains an error — either a
// protocol-level error (non-nil err) or an IsError result payload.
func resultIsError(result *mcp.CallToolResult, err error) bool {
	if err != nil {
		return true
	}

	return result != nil && result.IsError
}

// errorText extracts the error string from a failed tool result.
func errorText(result *mcp.CallToolResult, err error) string {
	if err != nil {
		return err.Error()
	}

	if result == nil || len(result.Content) == 0 {
		return ""
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}

	return tc.Text
}

// TestUpdateCard_AgentOwnership verifies that update_card rejects writes from an
// agent that does not own the card and allows writes from the owning agent.
func TestUpdateCard_AgentOwnership(t *testing.T) {
	t.Run("mismatched agent_id is rejected", func(t *testing.T) {
		env := setupMCP(t)

		// Create and claim a card with agent-A.
		createTestCard(t, env, "Ownership test", "task", "medium")

		claimResult := callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})
		require.False(t, claimResult.IsError, "claim should succeed")

		// Agent-B tries to update — must be rejected.
		result, err := callToolRaw(t, env, "update_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-B",
			"body":     "body written by wrong agent",
		})

		require.True(t, resultIsError(result, err), "update_card by wrong agent should fail")
		assert.Contains(t, errorText(result, err), "agent")

		// Body must not have changed.
		getResult := callTool(t, env, "get_card", map[string]any{
			"card_id": "TEST-001",
		})
		require.False(t, getResult.IsError)

		var card board.Card
		unmarshalResult(t, getResult, &card)
		assert.NotContains(t, card.Body, "body written by wrong agent")
	})

	t.Run("matching agent_id succeeds", func(t *testing.T) {
		env := setupMCP(t)

		createTestCard(t, env, "Matching agent", "task", "low")

		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})

		result, err := callToolRaw(t, env, "update_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
			"body":     "correct agent updated this",
		})

		require.False(t, resultIsError(result, err), "update_card by owning agent should succeed")

		var updated board.Card
		unmarshalResult(t, result, &updated)
		assert.Contains(t, updated.Body, "correct agent updated this")
	})

	t.Run("omitted agent_id on claimed card succeeds (backward compat)", func(t *testing.T) {
		env := setupMCP(t)

		createTestCard(t, env, "No agent id", "task", "low")

		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})

		// Caller omits agent_id entirely — ownership check is skipped.
		result, err := callToolRaw(t, env, "update_card", map[string]any{
			"project": "test-project",
			"card_id": "TEST-001",
			"body":    "runner update without agent_id",
		})

		require.False(t, resultIsError(result, err), "update_card without agent_id should succeed")

		var updated board.Card
		unmarshalResult(t, result, &updated)
		assert.Contains(t, updated.Body, "runner update without agent_id")
	})
}

// TestTransitionCard_AgentOwnership verifies that transition_card rejects state
// changes from an agent that does not own the card and allows transitions from
// the owning agent. The service-layer check replaces the former handler-level
// GetCard + manual comparison.
func TestTransitionCard_AgentOwnership(t *testing.T) {
	t.Run("mismatched agent_id is rejected", func(t *testing.T) {
		env := setupMCP(t)

		createTestCard(t, env, "Transition ownership", "task", "medium")

		// Transition to in_progress first so it can be claimed.
		callTool(t, env, "transition_card", map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"new_state": "in_progress",
		})

		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})

		// Agent-B tries to transition — must be rejected.
		result, err := callToolRaw(t, env, "transition_card", map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"agent_id":  "agent-B",
			"new_state": "todo",
		})

		require.True(t, resultIsError(result, err), "transition by wrong agent should fail")
		assert.Contains(t, errorText(result, err), "agent")

		// State must not have changed.
		getResult := callTool(t, env, "get_card", map[string]any{
			"card_id": "TEST-001",
		})
		require.False(t, getResult.IsError)

		var card board.Card
		unmarshalResult(t, getResult, &card)
		assert.Equal(t, "in_progress", card.State)
	})

	t.Run("matching agent_id succeeds", func(t *testing.T) {
		env := setupMCP(t)

		createTestCard(t, env, "Matching transition agent", "task", "low")

		// Claim card (auto-transitions to in_progress).
		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})

		result, err := callToolRaw(t, env, "transition_card", map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"agent_id":  "agent-A",
			"new_state": "todo",
		})

		require.False(t, resultIsError(result, err), "transition by owning agent should succeed")

		var card board.Card
		unmarshalResult(t, result, &card)
		assert.Equal(t, "todo", card.State)
	})

	t.Run("omitted agent_id on claimed card succeeds (backward compat)", func(t *testing.T) {
		env := setupMCP(t)

		createTestCard(t, env, "No agent id transition", "task", "low")

		callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
		})

		// Caller omits agent_id — ownership check is skipped.
		result, err := callToolRaw(t, env, "transition_card", map[string]any{
			"project":   "test-project",
			"card_id":   "TEST-001",
			"new_state": "todo",
		})

		require.False(t, resultIsError(result, err), "transition without agent_id should succeed")

		var card board.Card
		unmarshalResult(t, result, &card)
		assert.Equal(t, "todo", card.State)
	})
}

// TestPromoteToAutonomous_HumanOnly verifies that the promote_to_autonomous MCP
// tool enforces the human-only gate added to service.PromoteToAutonomous.
func TestPromoteToAutonomous_HumanOnly(t *testing.T) {
	ctx := context.Background()

	t.Run("agent agent_id is rejected with human-required error", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Promote gate test", "task", "medium")

		result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
			Name: "promote_to_autonomous",
			Arguments: map[string]any{
				"project":  "test-project",
				"card_id":  card.ID,
				"agent_id": "agent:foo",
			},
		})
		// The SDK may surface the error as a protocol-level error or as an IsError result.
		if err != nil {
			assert.Contains(t, err.Error(), "promote requires human agent")

			return
		}

		require.True(t, result.IsError, "non-human agent must produce an error result")
		textContent, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "expected TextContent in error result")
		assert.Contains(t, textContent.Text, "promote requires human agent")
	})

	t.Run("empty agent_id is rejected", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Empty agent test", "task", "medium")

		result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
			Name: "promote_to_autonomous",
			Arguments: map[string]any{
				"project":  "test-project",
				"card_id":  card.ID,
				"agent_id": "",
			},
		})
		if err != nil {
			assert.Contains(t, err.Error(), "promote requires human agent")

			return
		}

		require.True(t, result.IsError, "empty agent_id must produce an error result")
		textContent, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "expected TextContent in error result")
		assert.Contains(t, textContent.Text, "promote requires human agent")
	})

	t.Run("human:alice succeeds on non-terminal card", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Human promote MCP test", "task", "medium")

		result := callTool(t, env, "promote_to_autonomous", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "human:alice",
		})
		require.False(t, result.IsError, "human agent must succeed")

		var promoted board.Card
		unmarshalResult(t, result, &promoted)
		assert.True(t, promoted.Autonomous, "autonomous flag must be set after successful promote")
	})

	t.Run("errors.Is chain: ErrPromoteRequiresHuman is detectable via service layer", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "ErrorIs test card", "task", "medium")

		_, err := env.svc.PromoteToAutonomous(ctx, "test-project", card.ID, "agent:bar")
		require.Error(t, err)
		assert.ErrorIs(t, err, service.ErrPromoteRequiresHuman,
			"service.ErrPromoteRequiresHuman must be detectable via errors.Is through the wrap chain")
	})
}

func TestHITLMarkerTools_ReturnAcknowledged(t *testing.T) {
	t.Run("discovery_complete", func(t *testing.T) {
		env := setupMCP(t)
		card := createTestCard(t, env, "discovery marker", "task", "medium")
		result := callTool(t, env, "discovery_complete", map[string]any{
			"project":        "test-project",
			"card_id":        card.ID,
			"design_summary": "REST API with two endpoints",
		})
		require.False(t, result.IsError)
	})

	t.Run("plan_complete", func(t *testing.T) {
		env := setupMCP(t)
		card := createTestCard(t, env, "plan marker", "task", "medium")
		result := callTool(t, env, "plan_complete", map[string]any{
			"project":      "test-project",
			"card_id":      card.ID,
			"plan_summary": "decompose into 2 subtasks",
			"subtasks": []any{
				map[string]any{"title": "Implement A", "description": "do A"},
				map[string]any{"title": "Implement B", "description": "do B"},
			},
		})
		require.False(t, result.IsError)
	})

	t.Run("review_approve", func(t *testing.T) {
		env := setupMCP(t)
		card := createTestCard(t, env, "review approve marker", "task", "medium")
		result := callTool(t, env, "review_approve", map[string]any{
			"project": "test-project",
			"card_id": card.ID,
			"summary": "looks good",
		})
		require.False(t, result.IsError)
	})

	t.Run("review_revise", func(t *testing.T) {
		env := setupMCP(t)
		card := createTestCard(t, env, "review revise marker", "task", "medium")
		result := callTool(t, env, "review_revise", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"summary":  "needs work",
			"feedback": "use REST not GraphQL",
		})
		require.False(t, result.IsError)
	})
}

// TestStartReview_HappyPath verifies the fused transition + skill-load: claim
// auto-transitions to in_progress, then start_review moves the card to review
// AND returns the review-task skill in one call.
func TestStartReview_HappyPath(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Review fusion happy", "feature", "high")

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-A",
	})

	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project":      "test-project",
		"card_id":      "TEST-001",
		"agent_id":     "agent-A",
		"caller_model": "opus",
	})

	require.False(t, resultIsError(result, err), "start_review should succeed: %s", errorText(result, err))

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Equal(t, "review-task", out.SkillName)
	assert.Equal(t, "opus", out.Model)
	assert.NotEmpty(t, out.Content, "skill content must be present")
	assert.Contains(t, out.Content, "TEST-001", "card context must be injected")
	assert.True(t, out.Inline, "review-task is inline-eligible when caller_model matches")
	assert.Contains(t, out.Content, "INLINE EXECUTION", "inline response must include the lifecycle envelope")

	getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
	require.False(t, getResult.IsError)

	var card board.Card
	unmarshalResult(t, getResult, &card)
	assert.Equal(t, "review", card.State, "card must be transitioned to review")
}

// TestStartReview_ForbiddenTransition verifies that start_review rejects a
// transition that the project's state machine forbids and does not load the
// skill or change state. Uses blocked -> review (forbidden in the test fixture).
func TestStartReview_ForbiddenTransition(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Forbidden review", "feature", "high")

	// Claim the card (auto-transitions todo -> in_progress).
	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-A",
	})

	// Transition to blocked (allowed: in_progress -> blocked).
	callTool(t, env, "transition_card", map[string]any{
		"project":   "test-project",
		"card_id":   "TEST-001",
		"agent_id":  "agent-A",
		"new_state": "blocked",
	})

	// blocked -> review is forbidden by the test fixture transitions.
	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-A",
	})

	require.True(t, resultIsError(result, err), "start_review from blocked must fail")
	assert.Contains(t, errorText(result, err), "transition")

	getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
	require.False(t, getResult.IsError)

	var card board.Card
	unmarshalResult(t, getResult, &card)
	assert.Equal(t, "blocked", card.State, "card state must not change on forbidden transition")
}

// TestStartReview_MissingAgentID verifies that omitting agent_id is rejected
// by the schema layer before any state mutation can occur.
func TestStartReview_MissingAgentID(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Missing agent_id review", "feature", "high")

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-A",
	})

	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project": "test-project",
		"card_id": "TEST-001",
	})

	require.True(t, resultIsError(result, err), "start_review without agent_id must fail")

	getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
	require.False(t, getResult.IsError)

	var card board.Card
	unmarshalResult(t, getResult, &card)
	assert.Equal(t, "in_progress", card.State, "card state must not change when agent_id is missing")
}

// TestStartReview_AgentMismatch verifies that start_review rejects a call from
// an agent that does not own the claim, and the card state is unchanged.
func TestStartReview_AgentMismatch(t *testing.T) {
	env := setupMCP(t)

	createTestCard(t, env, "Mismatched review", "feature", "high")

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-A",
	})

	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-B",
	})

	require.True(t, resultIsError(result, err), "start_review by non-owner must fail")
	assert.Contains(t, errorText(result, err), "agent")

	getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
	require.False(t, getResult.IsError)

	var card board.Card
	unmarshalResult(t, getResult, &card)
	assert.Equal(t, "in_progress", card.State, "card state must not change on agent mismatch")
}

// TestStartReview_MissingCard verifies that start_review fails before any
// transition is attempted when the card does not exist.
func TestStartReview_MissingCard(t *testing.T) {
	env := setupMCP(t)

	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-999",
		"agent_id": "agent-A",
	})

	require.True(t, resultIsError(result, err), "start_review for unknown card must fail")
}

// TestGetProjectKBRoundTrip verifies the full KB flow: per-repo and
// jira-project layers are returned when their files exist on disk and the
// project's registry/JiraProjectKey are configured.
func TestGetProjectKBRoundTrip(t *testing.T) {
	env := setupMCP(t)

	// Update test-project config with Repos + JiraProjectKey
	cfg, err := env.store.GetProject(context.Background(), "test-project")
	require.NoError(t, err)

	cfg.JiraProjectKey = "PAY"
	cfg.Repos = []board.RepoSpec{
		{Slug: "r1", URL: "https://example.com/r1.git"},
	}
	require.NoError(t, env.store.SaveProject(context.Background(), cfg))

	// Drop KB files into the boards dir.
	require.NoError(t, os.MkdirAll(filepath.Join(env.boardsDir, "_kb", "repos"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(env.boardsDir, "_kb", "jira-projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.boardsDir, "_kb", "repos", "r1.md"), []byte("# r1 kb\nworld"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(env.boardsDir, "_kb", "jira-projects", "PAY.md"), []byte("# PAY"), 0o644))

	result := callTool(t, env, "get_project_kb", map[string]any{
		"project": "test-project",
	})
	require.False(t, result.IsError)

	var out getProjectKBOutput
	unmarshalResult(t, result, &out)
	require.Contains(t, out.Repos, "r1")
	assert.Contains(t, out.Repos["r1"], "world")
	assert.Contains(t, out.JiraProject, "PAY")
}

// TestGetProjectKBWithRepoFilter verifies the optional repo_slug input
// narrows the per-repo layer to a single registered slug.
func TestGetProjectKBWithRepoFilter(t *testing.T) {
	env := setupMCP(t)

	cfg, err := env.store.GetProject(context.Background(), "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.RepoSpec{
		{Slug: "r1", URL: "https://example.com/r1.git"},
		{Slug: "r2", URL: "https://example.com/r2.git"},
	}
	require.NoError(t, env.store.SaveProject(context.Background(), cfg))

	require.NoError(t, os.MkdirAll(filepath.Join(env.boardsDir, "_kb", "repos"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.boardsDir, "_kb", "repos", "r1.md"), []byte("r1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(env.boardsDir, "_kb", "repos", "r2.md"), []byte("r2"), 0o644))

	result := callTool(t, env, "get_project_kb", map[string]any{
		"project":   "test-project",
		"repo_slug": "r1",
	})
	require.False(t, result.IsError)

	var out getProjectKBOutput
	unmarshalResult(t, result, &out)
	require.Len(t, out.Repos, 1)
	assert.Contains(t, out.Repos, "r1")
	assert.NotContains(t, out.Repos, "r2")
}

// TestGetProjectKBProjectMissing verifies the tool surfaces an error when
// the named project does not exist.
func TestGetProjectKBProjectMissing(t *testing.T) {
	env := setupMCP(t)
	result, err := callToolRaw(t, env, "get_project_kb", map[string]any{
		"project": "no-such-project",
	})
	require.True(t, resultIsError(result, err))
}

// TestGetProjectKBProjectRequired verifies that omitting the project field is
// rejected.
func TestGetProjectKBProjectRequired(t *testing.T) {
	env := setupMCP(t)
	result, err := callToolRaw(t, env, "get_project_kb", map[string]any{})
	require.True(t, resultIsError(result, err))
}

// TestReportPushAcceptsRepo verifies the MCP report_push tool forwards the
// new repo field through to the service layer and that one PushRecord is
// appended per call.
func TestReportPushAcceptsRepo(t *testing.T) {
	env := setupMCP(t)
	createTestCard(t, env, "Repo push", "task", "medium")

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-1",
	})

	result := callTool(t, env, "report_push", map[string]any{
		"project":  "test-project",
		"card_id":  "TEST-001",
		"agent_id": "agent-1",
		"repo":     "auth-svc",
		"branch":   "cm/TEST-001",
		"pr_url":   "https://github.com/acme/auth-svc/pull/42",
	})
	require.False(t, result.IsError, errorText(result, nil))

	card, err := env.svc.GetCard(context.Background(), "test-project", "TEST-001")
	require.NoError(t, err)
	require.Len(t, card.PushRecords, 1)
	assert.Equal(t, "auth-svc", card.PushRecords[0].Repo)
}
