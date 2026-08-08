package mcp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// TestGetSkill_IncludeCard pins the get_skill include_card opt-out: false
// replaces the injected body with a pointer note while the metadata header
// stays; omitted (nil) defaults to true, matching current behavior.
func TestGetSkill_IncludeCard(t *testing.T) {
	env := setupMCP(t)

	t.Run("false omits the body", func(t *testing.T) {
		card := createBodyCard(t, env, "Create plan body opt-out", "MARKER-BODY-CONTENT", nil)

		result := callTool(t, env, "get_skill", map[string]any{
			"skill_name":   "create-plan",
			"card_id":      card.ID,
			"include_card": false,
		})
		require.False(t, result.IsError)

		var out getSkillOutput
		unmarshalResult(t, result, &out)
		assert.NotContains(t, out.Content, "MARKER-BODY-CONTENT")
		assert.Contains(t, out.Content, "## Card: "+card.ID, "metadata header must remain")
		assert.Contains(t, out.Content,
			fmt.Sprintf("[Card body omitted (include_card=false). Run get_card(card_id='%s') to read it.]", card.ID))
	})

	t.Run("omitted defaults to true", func(t *testing.T) {
		card := createBodyCard(t, env, "Create plan body default", "MARKER-BODY-CONTENT", nil)

		result := callTool(t, env, "get_skill", map[string]any{
			"skill_name": "create-plan",
			"card_id":    card.ID,
		})
		require.False(t, result.IsError)

		var out getSkillOutput
		unmarshalResult(t, result, &out)
		assert.Contains(t, out.Content, "MARKER-BODY-CONTENT")
		assert.NotContains(t, out.Content, "[Card body omitted (include_card=false)")
	})
}

// TestStartWorkflow_IncludeCard pins the same opt-out on start_workflow.
func TestStartWorkflow_IncludeCard(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Start workflow opt-out", "MARKER-BODY-CONTENT", nil)

	result, err := callToolRaw(t, env, "start_workflow", map[string]any{
		"card_id":      card.ID,
		"include_card": false,
	})
	require.False(t, resultIsError(result, err), "start_workflow should succeed: %s", errorText(result, err))

	var out startWorkflowOutput
	unmarshalResult(t, result, &out)
	assert.NotContains(t, out.Content, "MARKER-BODY-CONTENT")
	assert.Contains(t, out.Content,
		fmt.Sprintf("[Card body omitted (include_card=false). Run get_card(card_id='%s') to read it.]", card.ID))
}

// TestStartReview_IncludeCard pins the same opt-out on start_review.
func TestStartReview_IncludeCard(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Start review opt-out", "MARKER-BODY-CONTENT", nil)

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "agent-A",
	})

	result, err := callToolRaw(t, env, "start_review", map[string]any{
		"project":      "test-project",
		"card_id":      card.ID,
		"agent_id":     "agent-A",
		"include_card": false,
	})
	require.False(t, resultIsError(result, err), "start_review should succeed: %s", errorText(result, err))

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.NotContains(t, out.Content, "MARKER-BODY-CONTENT")
	assert.Contains(t, out.Content,
		fmt.Sprintf("[Card body omitted (include_card=false). Run get_card(card_id='%s') to read it.]", card.ID))

	// The transition side effect must still happen even though the skill's
	// body injection was skipped.
	getResult := callTool(t, env, "get_card", map[string]any{"card_id": card.ID})
	require.False(t, getResult.IsError)

	var got board.Card
	unmarshalResult(t, getResult, &got)
	assert.Equal(t, "review", got.State, "card must still be transitioned to review")
}
