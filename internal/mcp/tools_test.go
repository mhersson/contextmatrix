package mcp

import (
	"context"
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
