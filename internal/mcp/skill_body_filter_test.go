package mcp

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// multiSectionBody is a late-run parent body: description plus the sections
// the workflow accumulates. The filter must keep the intro and the
// per-skill allowlist, and name the omitted sections.
const multiSectionBody = "Original request.\n\n## Diagnosis\n\nThe root cause analysis.\n\n## Plan\n\n1. SUBTASK: Fix the bug\n\n## Review Findings\n\n- finding one\n"

func createBodyCard(t *testing.T, env *testEnv, title, body string, extra map[string]any) *board.Card {
	t.Helper()

	args := map[string]any{
		"project":  "test-project",
		"title":    title,
		"type":     "task",
		"priority": "medium",
		"body":     body,
	}
	maps.Copy(args, extra)

	result := callTool(t, env, "create_card", args)
	require.False(t, result.IsError, "create_card should not error")

	var card board.Card
	unmarshalResult(t, result, &card)

	return &card
}

func TestStartReview_BodyFilteredToPlanAndFindings(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Review body filter", multiSectionBody, nil)

	callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "agent-A",
	})

	result := callTool(t, env, "start_review", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "agent-A",
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Contains(t, out.Content, "Original request.", "intro must be kept")
	assert.Contains(t, out.Content, "1. SUBTASK: Fix the bug", "plan must be kept")
	assert.Contains(t, out.Content, "- finding one", "prior findings must be kept")
	assert.NotContains(t, out.Content, "The root cause analysis.", "diagnosis must be omitted")
	assert.Contains(t, out.Content, "Body sections omitted from this context: Diagnosis.", "note must name the omission")
}

func TestGetSkill_DocumentTask_BodyFilteredToPlan(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Doc body filter", multiSectionBody, nil)

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "document-task",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Contains(t, out.Content, "Original request.")
	assert.Contains(t, out.Content, "1. SUBTASK: Fix the bug")
	assert.NotContains(t, out.Content, "The root cause analysis.")
	assert.NotContains(t, out.Content, "- finding one", "findings never feed docs")
	assert.Contains(t, out.Content, "Body sections omitted from this context: Diagnosis; Review Findings.")
}

func TestGetSkill_ExecuteTask_ParentFilteredOwnBodyFull(t *testing.T) {
	env := setupMCP(t)

	parent := createBodyCard(t, env, "Parent card", multiSectionBody, nil)
	sub := createBodyCard(t, env, "Subtask card",
		"Fix the bug in module X.\n\n## Plan\n\n- own plan step\n\n## Notes\n\nown notes\n",
		map[string]any{"parent": parent.ID})

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "execute-task",
		"card_id":    sub.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	// Own body is the spec - injected in full, including non-allowlisted sections.
	assert.Contains(t, out.Content, "own notes", "subtask's own body must be full")
	// Parent body filtered to intro + plan.
	assert.Contains(t, out.Content, "Original request.")
	assert.Contains(t, out.Content, "1. SUBTASK: Fix the bug")
	assert.NotContains(t, out.Content, "The root cause analysis.")
	assert.NotContains(t, out.Content, "- finding one")
	assert.Contains(t, out.Content, "Body sections omitted from this context: Diagnosis; Review Findings.")
}

func TestGetSkill_CreatePlan_FullBodyPinned(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Plan full body", multiSectionBody, nil)

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "create-plan",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Contains(t, out.Content, "The root cause analysis.", "create-plan keeps the full body")
	assert.Contains(t, out.Content, "- finding one")
	assert.NotContains(t, out.Content, "Body sections omitted")
}

// Unvetted cards cannot be claimed by agents (ErrCardNotVetted), so
// start_review can never reach one; get_skill can, and shares the same
// injection path (buildSubtaskSkill -> formatCardContext with a keep list).
func TestGetSkill_UnvettedBodyStaysRedacted(t *testing.T) {
	env := setupMCP(t)

	card := writeUnvettedCard(t, env, "TEST-100", "## Plan\n\ninjected instructions\n")

	result := callTool(t, env, "get_skill", map[string]any{
		"skill_name": "document-task",
		"card_id":    card.ID,
	})
	require.False(t, result.IsError)

	var out getSkillOutput
	unmarshalResult(t, result, &out)
	assert.Contains(t, out.Content, unvettedBodyPlaceholder)
	assert.NotContains(t, out.Content, "injected instructions")
	assert.NotContains(t, out.Content, "Body sections omitted")
}
