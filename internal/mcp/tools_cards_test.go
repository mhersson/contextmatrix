package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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

		// board.Card.Body has no `omitempty` (deliberately, per the strict
		// filter's contract): pin that a sections no-match serializes body as
		// an explicit "", not a dropped key. A stray omitempty added later
		// would make this indistinguishable from a card with no body at all.
		require.NotEmpty(t, result.Content)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, `"body":""`, "body must serialize as an explicit empty string, not be omitted")
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

// TestGetCard_SectionsWithIncludeImagesFalse combines a sections filter with
// include_images:false: no images attach (the caller opted out explicitly),
// and the body is still trimmed to the requested section - the two opt-ins
// are independent knobs, not mutually exclusive.
func TestGetCard_SectionsWithIncludeImagesFalse(t *testing.T) {
	env := setupMCPImages(t)

	cardID, ids := createImageCard(t, env)
	require.Len(t, ids, 2)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project":        "test-project",
			"card_id":        cardID,
			"sections":       []string{"Screenshot one"},
			"include_images": false,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Text-only: no ImageContent blocks even though "Screenshot one" itself
	// references an image, because include_images:false short-circuits
	// attachImagesToResult before it scans the (already trimmed) body.
	require.Len(t, result.Content, 1)

	var got board.Card
	unmarshalResult(t, result, &got)
	assert.Equal(t, fmt.Sprintf("## Screenshot one\n\n![one](/api/images/%s)\n", ids[0]), got.Body)
	assert.NotContains(t, got.Body, "Screenshot two")
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

// TestReportUsage_SourceValidation pins the MCP-boundary validation for the
// report_usage source field: only "", "self", and "collector" are accepted,
// and a bogus value is rejected before it ever reaches the service layer.
// The valid-value subtests matter as much as the bogus one here: the MCP SDK
// schema rejects any unrecognized JSON key outright, so "bogus" alone would
// "pass" for the wrong reason (unknown field) even before source exists as a
// real, validated input.
func TestReportUsage_SourceValidation(t *testing.T) {
	env := setupMCP(t)

	for _, tc := range []struct {
		name    string
		source  string
		wantErr bool
	}{
		{name: "self is accepted", source: "self", wantErr: false},
		{name: "collector is accepted", source: "collector", wantErr: false},
		{name: "bogus is rejected", source: "bogus", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := createTestCard(t, env, "Source validation "+tc.name, "task", "medium")

			result := callTool(t, env, "report_usage", map[string]any{
				"project":           "test-project",
				"card_id":           card.ID,
				"agent_id":          "agent-1",
				"prompt_tokens":     int64(100),
				"completion_tokens": int64(50),
				"source":            tc.source,
			})
			assert.Equal(t, tc.wantErr, result.IsError)
		})
	}
}

// TestReportUsage_OnBehalfOf pins on_behalf_of end-to-end through the MCP
// boundary: the claim-holding agent reports usage on behalf of a different
// identity, the call succeeds on agent_id's ownership, and the persisted
// bucket is keyed on on_behalf_of rather than agent_id.
func TestReportUsage_OnBehalfOf(t *testing.T) {
	env := setupMCP(t)

	card := createTestCard(t, env, "On-behalf-of MCP test", "task", "medium")

	claimResult := callTool(t, env, "claim_card", map[string]any{
		"project":  "test-project",
		"card_id":  card.ID,
		"agent_id": "orchestrator-mcp",
	})
	require.False(t, claimResult.IsError)

	result := callTool(t, env, "report_usage", map[string]any{
		"project":           "test-project",
		"card_id":           card.ID,
		"agent_id":          "orchestrator-mcp",
		"on_behalf_of":      "exec-mcp",
		"model":             "claude-sonnet-4-6",
		"prompt_tokens":     int64(100),
		"completion_tokens": int64(50),
	})
	require.False(t, result.IsError, "report_usage with on_behalf_of should not error when agent_id holds the claim")

	getResult := callTool(t, env, "get_card", map[string]any{
		"project": "test-project",
		"card_id": card.ID,
	})
	require.False(t, getResult.IsError)

	var fetched board.Card
	unmarshalResult(t, getResult, &fetched)

	require.Len(t, fetched.UsageBreakdown, 1)
	assert.Equal(t, "exec-mcp", fetched.UsageBreakdown[0].Agent,
		"bucket must be keyed on on_behalf_of, not agent_id")
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

// parkNotPlanned puts a card in not_planned with the given claim, seeded
// straight into the store. The default test config has no todo -> not_planned
// edge, and a live claim on a not_planned card is an anomaly the service layer
// clears on entry - both are exactly the shapes these regressions need.
func parkNotPlanned(t *testing.T, env *testEnv, cardID, agentID string) {
	t.Helper()

	ctx := context.Background()

	card, err := env.store.GetCard(ctx, "test-project", cardID)
	require.NoError(t, err)

	card.State = board.StateNotPlanned
	card.AssignedAgent = agentID

	require.NoError(t, env.store.UpdateCard(ctx, "test-project", card))
}

// TestNotPlannedCardCannotBeWorked is the regression for the reported incident:
// a human cancelled a subtask, and hours later an agent claimed it, implemented
// it, and drove it to done via not_planned -> todo -> done. Both halves of that
// sequence are now refused.
func TestNotPlannedCardCannotBeWorked(t *testing.T) {
	ctx := context.Background()

	t.Run("claim_card refuses a cancelled card", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Cancelled subtask", "task", "medium")
		parkNotPlanned(t, env, card.ID, "")

		result := callTool(t, env, "claim_card", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "agent-A",
		})
		require.True(t, result.IsError, "claiming a not_planned card must fail")

		after, err := env.store.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, board.StateNotPlanned, after.State)
		assert.Empty(t, after.AssignedAgent)
	})

	t.Run("complete_task does not walk a cancelled card to done", func(t *testing.T) {
		env := setupMCP(t)

		card := createTestCard(t, env, "Cancelled subtask with a stray claim", "task", "medium")
		parkNotPlanned(t, env, card.ID, "agent-A")

		result := callTool(t, env, "complete_task", map[string]any{
			"project":  "test-project",
			"card_id":  card.ID,
			"agent_id": "agent-A",
			"summary":  "feat: work that was never wanted",
		})
		require.True(t, result.IsError, "completing a not_planned card must fail")

		after, err := env.store.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.Equal(t, board.StateNotPlanned, after.State, "the card must not be resurrected")
	})
}
