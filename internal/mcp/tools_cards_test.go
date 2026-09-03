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

// TestUpdateCard_DependsOn covers the update_card depends_on field: setting it
// returns the new list on the summary, and a later call that omits it leaves
// the list untouched.
func TestUpdateCard_DependsOn(t *testing.T) {
	env := setupMCP(t)

	other := createTestCard(t, env, "Dependency target", "task", "medium")
	card := createTestCard(t, env, "Depends on target", "task", "medium")

	result := callTool(t, env, "update_card", map[string]any{
		"project":    "test-project",
		"card_id":    card.ID,
		"depends_on": []string{other.ID},
	})
	require.False(t, result.IsError)

	var updated CardSummary
	unmarshalResult(t, result, &updated)
	assert.Equal(t, []string{other.ID}, updated.DependsOn)

	result2 := callTool(t, env, "update_card", map[string]any{
		"project": "test-project",
		"card_id": card.ID,
		"title":   "Depends on target, renamed",
	})
	require.False(t, result2.IsError)

	var updated2 CardSummary
	unmarshalResult(t, result2, &updated2)
	assert.Equal(t, "Depends on target, renamed", updated2.Title)
	assert.Equal(t, []string{other.ID}, updated2.DependsOn, "omitting depends_on must leave the list unchanged")
}

// activityHasAction reports whether the card's activity log contains an entry
// with the given Action.
func activityHasAction(card *board.Card, action string) bool {
	for _, entry := range card.ActivityLog {
		if entry.Action == action {
			return true
		}
	}

	return false
}

// TestCreateCardSelfContainmentWarnings covers create_card's lint wiring: a
// body referencing the card author's local filesystem produces one advisory
// warning on the result and a matching activity-log entry, without blocking
// creation.
func TestCreateCardSelfContainmentWarnings(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Self-containment check",
		"type":     "task",
		"priority": "medium",
		"body":     "See /home/user/design.md for the spec.",
	})
	require.False(t, result.IsError)

	var created cardMutationResult
	unmarshalResult(t, result, &created)

	assert.Len(t, created.Warnings, 1)
	assert.NotEmpty(t, created.ID)

	card, err := env.store.GetCard(ctx, "test-project", created.ID)
	require.NoError(t, err)
	assert.True(t, activityHasAction(card, "self_containment_warning"),
		"expected a self_containment_warning activity entry, got: %+v", card.ActivityLog)
}

// TestCreateCardCleanBodyNoWarnings covers the no-op path: a body that
// references only in-repo content produces no warnings and no activity entry.
func TestCreateCardCleanBodyNoWarnings(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	result := callTool(t, env, "create_card", map[string]any{
		"project":  "test-project",
		"title":    "Clean body",
		"type":     "task",
		"priority": "medium",
		"body":     "update internal/api/auth.go",
	})
	require.False(t, result.IsError)

	var created cardMutationResult
	unmarshalResult(t, result, &created)

	assert.Empty(t, created.Warnings)

	card, err := env.store.GetCard(ctx, "test-project", created.ID)
	require.NoError(t, err)
	assert.False(t, activityHasAction(card, "self_containment_warning"),
		"clean body must not produce a self_containment_warning entry, got: %+v", card.ActivityLog)
}

// TestUpdateCardSelfContainmentWarnings covers update_card's lint wiring:
// patching the body to reference a home-relative path produces one advisory
// warning and a matching activity-log entry.
func TestUpdateCardSelfContainmentWarnings(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	card := createTestCard(t, env, "Update target", "task", "medium")

	result := callTool(t, env, "update_card", map[string]any{
		"project": "test-project",
		"card_id": card.ID,
		"body":    "see ~/notes.md",
	})
	require.False(t, result.IsError)

	var updated cardMutationResult
	unmarshalResult(t, result, &updated)

	assert.Len(t, updated.Warnings, 1)

	after, err := env.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, activityHasAction(after, "self_containment_warning"),
		"expected a self_containment_warning activity entry, got: %+v", after.ActivityLog)
}

func activityCount(card *board.Card, action string) int {
	n := 0

	for _, entry := range card.ActivityLog {
		if entry.Action == action {
			n++
		}
	}

	return n
}

func TestUpdateCardSelfContainmentWarnsOnlyForNewSignals(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	card := createTestCard(t, env, "Rewritten target", "task", "medium")

	// First write introduces one signal: one warning, one log entry.
	result := callTool(t, env, "update_card", map[string]any{
		"project": "test-project", "card_id": card.ID, "body": "see ~/notes.md",
	})
	require.False(t, result.IsError)

	// The agent rewrites the whole body every phase; the old signal is still
	// there and nothing new arrived.
	result = callTool(t, env, "update_card", map[string]any{
		"project": "test-project", "card_id": card.ID, "body": "see ~/notes.md\n\n## Plan\n\n- step one",
	})
	require.False(t, result.IsError)

	var second cardMutationResult
	unmarshalResult(t, result, &second)
	assert.Empty(t, second.Warnings, "a signal already on the card is not re-warned")

	after, err := env.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, activityCount(after, "self_containment_warning"), "log=%+v", after.ActivityLog)

	// A genuinely new signal warns and logs again.
	result = callTool(t, env, "update_card", map[string]any{
		"project": "test-project", "card_id": card.ID, "body": "see ~/notes.md and file:///tmp/x.md",
	})
	require.False(t, result.IsError)

	var third cardMutationResult
	unmarshalResult(t, result, &third)
	assert.Len(t, third.Warnings, 1)

	after, err = env.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, activityCount(after, "self_containment_warning"))
}

// TestCreateCardDuplicateSubtaskDoesNotRewarn pins what the create path lints:
// the duplicate-subtask guard returns the pre-existing card, so linting the
// submitted text keeps that no-op create from re-warning about a signal
// already on the card it hands back.
func TestCreateCardDuplicateSubtaskDoesNotRewarn(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	parent := createTestCard(t, env, "Dedup parent", "feature", "high")

	result := callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Shared subtask title", "type": "task",
		"priority": "medium", "parent": parent.ID, "body": "see ~/notes.md",
	})
	require.False(t, result.IsError)

	var first cardMutationResult
	unmarshalResult(t, result, &first)
	assert.Len(t, first.Warnings, 1)

	// Same title under the same parent: the dedup guard returns the existing
	// card instead of creating one, and this body carries no signal.
	result = callTool(t, env, "create_card", map[string]any{
		"project": "test-project", "title": "Shared subtask title", "type": "task",
		"priority": "medium", "parent": parent.ID, "body": "update internal/api/auth.go",
	})
	require.False(t, result.IsError)

	var second cardMutationResult
	unmarshalResult(t, result, &second)
	assert.Equal(t, first.ID, second.ID, "a duplicate subtask must return the existing card")
	assert.Empty(t, second.Warnings, "a deduped create must not re-warn about the existing card")

	existing, err := env.store.GetCard(ctx, "test-project", first.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, activityCount(existing, "self_containment_warning"), "log=%+v", existing.ActivityLog)
}

// setupMCPTwoProjects mirrors setupMCP but registers a second project
// ("other-project") with a repo and a repos[] entry, and gives the primary
// project ("test-project") its own repo too - so the self-containment lint's
// foreign-repo collection has both a same-project repo to exclude and a
// foreign one (in both Repo and Repos[] form) to match against.
func setupMCPTwoProjects(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	primary := testProjectConfig()
	primary.Repo = "https://github.com/acme/test-project"
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, primary))

	other := testProjectConfig()
	other.Name = "other-project"
	other.Prefix = "OTHER"
	other.Repo = "https://github.com/acme/other-project"
	other.Repos = []board.Repo{{Name: "docs", URL: "https://github.com/acme/other-project-docs"}}
	otherDir := filepath.Join(boardsDir, "other-project")
	require.NoError(t, os.MkdirAll(filepath.Join(otherDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(otherDir, other))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

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
	}
}

// TestCreateCardSelfContainmentForeignRepo covers the foreign-repo branch of
// the lint end to end: a body referencing another project's repo (in its
// owner/name form, derived from Repo) warns, the SAME reference to the card's
// OWN project repo does not (own project is excluded from the foreign-repo
// set), and a reference to a foreign project's repos[] entry also warns.
func TestCreateCardSelfContainmentForeignRepo(t *testing.T) {
	env := setupMCPTwoProjects(t)
	ctx := context.Background()

	t.Run("foreign repo (Repo field) triggers one warning", func(t *testing.T) {
		result := callTool(t, env, "create_card", map[string]any{
			"project":  "test-project",
			"title":    "Cross-project reference",
			"type":     "task",
			"priority": "medium",
			"body":     "This depends on acme/other-project landing first.",
		})
		require.False(t, result.IsError)

		var created cardMutationResult
		unmarshalResult(t, result, &created)
		require.Len(t, created.Warnings, 1)

		card, err := env.store.GetCard(ctx, "test-project", created.ID)
		require.NoError(t, err)
		assert.True(t, activityHasAction(card, "self_containment_warning"),
			"expected a self_containment_warning activity entry, got: %+v", card.ActivityLog)
	})

	t.Run("foreign repo (repos[] entry) triggers one warning", func(t *testing.T) {
		result := callTool(t, env, "create_card", map[string]any{
			"project":  "test-project",
			"title":    "Cross-project docs reference",
			"type":     "task",
			"priority": "medium",
			"body":     "See acme/other-project-docs for the write-up.",
		})
		require.False(t, result.IsError)

		var created cardMutationResult
		unmarshalResult(t, result, &created)
		assert.Len(t, created.Warnings, 1)
	})

	t.Run("own project's repo does not trigger", func(t *testing.T) {
		result := callTool(t, env, "create_card", map[string]any{
			"project":  "test-project",
			"title":    "Same-project reference",
			"type":     "task",
			"priority": "medium",
			"body":     "This depends on acme/test-project landing first.",
		})
		require.False(t, result.IsError)

		var created cardMutationResult
		unmarshalResult(t, result, &created)
		assert.Empty(t, created.Warnings)

		card, err := env.store.GetCard(ctx, "test-project", created.ID)
		require.NoError(t, err)
		assert.False(t, activityHasAction(card, "self_containment_warning"),
			"a card's own project repo must not trigger the foreign-repo warning, got: %+v", card.ActivityLog)
	})
}

// TestForeignRepoRefs unit-tests foreignRepoRefs directly: it must exclude
// the current project, collect both Repo and every repos[].URL from every
// other project, and return nil (rather than erroring the caller) when
// ListProjects itself fails.
func TestForeignRepoRefs(t *testing.T) {
	env := setupMCPTwoProjects(t)

	t.Run("collects foreign Repo and repos[].URL, excludes own project", func(t *testing.T) {
		refs := foreignRepoRefs(context.Background(), env.svc, "test-project")
		assert.Contains(t, refs, "https://github.com/acme/other-project")
		assert.Contains(t, refs, "https://github.com/acme/other-project-docs")
		assert.NotContains(t, refs, "https://github.com/acme/test-project")
	})

	t.Run("ListProjects failure returns nil, not an error", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		refs := foreignRepoRefs(cancelledCtx, env.svc, "test-project")
		assert.Nil(t, refs)
	})
}
