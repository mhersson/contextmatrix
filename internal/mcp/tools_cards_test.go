package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

// TestGetCard_IncludeActivityLog pins the include_activity_log opt-out:
// false clears the log on the response; omitted (nil) defaults to true.
func TestGetCard_IncludeActivityLog(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "Activity log opt-out", "task", "medium")

	// Claim appends a "claimed" activity log entry.
	claimResult := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "agent-A",
	})
	require.False(t, claimResult.IsError)

	t.Run("false clears the activity log", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project":              "test-project",
			"card_id":              card.ID,
			"include_activity_log": false,
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		assert.Empty(t, got.ActivityLog)

		require.NotEmpty(t, result.Content)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.NotContains(t, text.Text, "activity_log", "omitempty must drop the field, not emit an empty array")
	})

	t.Run("omitted defaults to true", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project": "test-project",
			"card_id": card.ID,
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		require.NotEmpty(t, got.ActivityLog, "claiming a todo card appends at least a state_changed entry")
	})
}

// TestGetCard_Sections pins the strict body-section filter: a caller-supplied
// sections list keeps only matching H2 sections, no intro, and an unmatched
// request returns an empty body rather than the full one.
func TestGetCard_Sections(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Sections filter", multiSectionBody, nil)

	t.Run("matching section only, no intro", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"sections": []string{"Plan"},
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		assert.Equal(t, "## Plan\n\n1. SUBTASK: Fix the bug\n", got.Body)
	})

	t.Run("intro included when requested alongside a section", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"sections": []string{"intro", "Plan"},
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		assert.Contains(t, got.Body, "Original request.")
		assert.Contains(t, got.Body, "1. SUBTASK: Fix the bug")
		assert.NotContains(t, got.Body, "Diagnosis")
	})

	t.Run("no match returns empty body, not the full body", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"sections": []string{"Nonexistent Section"},
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		assert.Empty(t, got.Body)
	})

	t.Run("omitted defaults to the full body", func(t *testing.T) {
		result := callTool(t, env, "get_card", map[string]any{
			"project": "test-project",
			"card_id": card.ID,
		})
		require.False(t, result.IsError)

		var got board.Card
		unmarshalResult(t, result, &got)
		assert.Equal(t, multiSectionBody, got.Body)
	})
}

// TestGetCard_SectionsImageScanOnTrimmedBody guards the ordering requirement:
// image attachment must scan the sections-filtered body, not the original -
// an image referenced only by a removed section must not be attached.
func TestGetCard_SectionsImageScanOnTrimmedBody(t *testing.T) {
	env := setupMCPImages(t)

	cardID, ids := createImageCard(t, env)
	require.Len(t, ids, 2)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project":  "test-project",
			"card_id":  cardID,
			"sections": []string{"Screenshot one"},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Text block + exactly 1 ImageContent: the second image lived only in the
	// filtered-out "Screenshot two" section.
	require.Len(t, result.Content, 2)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "Screenshot one")
	assert.NotContains(t, text.Text, "Screenshot two")

	_, isImg := result.Content[1].(*mcp.ImageContent)
	assert.True(t, isImg)
}

// TestGetCard_SectionsEmptyBodySkipsImageScan is the degenerate case: a
// no-match sections filter empties the body entirely, so no images should be
// attached at all even though the original body referenced some.
func TestGetCard_SectionsEmptyBodySkipsImageScan(t *testing.T) {
	env := setupMCPImages(t)

	cardID, _ := createImageCard(t, env)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project":  "test-project",
			"card_id":  cardID,
			"sections": []string{"Nonexistent"},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var got board.Card
	require.NoError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.Empty(t, got.Body)
}

func TestGetCard_SectionsAndIncludeActivityLogCombined(t *testing.T) {
	env := setupMCP(t)

	card := createBodyCard(t, env, "Combined opt-outs", multiSectionBody, nil)

	claimResult := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "agent-A",
	})
	require.False(t, claimResult.IsError)

	result := callTool(t, env, "get_card", map[string]any{
		"project":              "test-project",
		"card_id":              card.ID,
		"sections":             []string{"Plan"},
		"include_activity_log": false,
	})
	require.False(t, result.IsError)

	var got board.Card
	unmarshalResult(t, result, &got)
	assert.Equal(t, "## Plan\n\n1. SUBTASK: Fix the bug\n", got.Body)
	assert.Empty(t, got.ActivityLog)
	assert.Equal(t, card.ID, got.ID, "card id must be preserved")
}
