package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestReportParked(t *testing.T) {
	t.Run("owning agent parks the card", func(t *testing.T) {
		env := setupMCP(t)
		createTestCard(t, env, "Park me", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result := callTool(t, env, "report_parked", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-A",
			"reason":   "review parked: attempts cap exhausted without approval",
		})
		require.False(t, result.IsError)

		getResult := callTool(t, env, "get_card", map[string]any{"card_id": "TEST-001"})
		require.False(t, getResult.IsError)

		var card board.Card
		unmarshalResult(t, getResult, &card)
		assert.Equal(t, "parked", card.WorkerStatus)
	})

	t.Run("non-owning agent is rejected", func(t *testing.T) {
		env := setupMCP(t)
		createTestCard(t, env, "Park me not", "task", "medium")
		callTool(t, env, "claim_card", map[string]any{
			"project": "test-project", "card_id": "TEST-001", "agent_id": "agent-A",
		})

		result, err := callToolRaw(t, env, "report_parked", map[string]any{
			"project":  "test-project",
			"card_id":  "TEST-001",
			"agent_id": "agent-B",
			"reason":   "review parked: attempts cap exhausted without approval",
		})
		require.True(t, resultIsError(result, err))
		assert.Contains(t, errorText(result, err), "agent")
	})
}
