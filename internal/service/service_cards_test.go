package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func TestCreateCard_SkillsInheritance(t *testing.T) {
	t.Run("nil parent skills, nil subtask skills → nil", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "parent", Type: "task", Priority: "low",
		})
		require.NoError(t, err)

		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
		})
		require.NoError(t, err)
		assert.Nil(t, child.Skills, "nil parent + nil subtask should yield nil")
	})

	t.Run("populated parent skills, nil subtask skills → inherit", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		skills := []string{"go-development"}
		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "parent", Type: "task", Priority: "low",
			Skills: &skills,
		})
		require.NoError(t, err)

		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
		})
		require.NoError(t, err)
		require.NotNil(t, child.Skills)
		assert.Equal(t, []string{"go-development"}, *child.Skills)
	})

	t.Run("explicit empty subtask skills → preserved (not inherited)", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		parentSkills := []string{"go-development"}
		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "parent", Type: "task", Priority: "low",
			Skills: &parentSkills,
		})
		require.NoError(t, err)

		empty := []string{}
		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
			Skills: &empty,
		})
		require.NoError(t, err)
		require.NotNil(t, child.Skills)
		assert.Empty(t, *child.Skills, "explicit empty must be preserved")
	})

	t.Run("explicit subtask skills override parent", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		parentSkills := []string{"go-development"}
		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "parent", Type: "task", Priority: "low",
			Skills: &parentSkills,
		})
		require.NoError(t, err)

		childSkills := []string{"typescript-react"}
		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
			Skills: &childSkills,
		})
		require.NoError(t, err)
		require.NotNil(t, child.Skills)
		assert.Equal(t, []string{"typescript-react"}, *child.Skills)
	})
}

// TestListCardsPage_WalkAllPages seeds a project with 50 cards, walks the
// paginated listing with limit=10, and asserts the walk visits every card
// exactly once in ID-ascending order. Guards against off-by-one slicing and
// cursor-skip regressions.
func TestListCardsPage_WalkAllPages(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	const total = 50

	for i := range total {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	var (
		seen    []string
		cursor  string
		pageNum int
	)

	for {
		pageNum++
		require.LessOrEqual(t, pageNum, 100, "pagination exceeded reasonable page count")

		page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{
			Limit:  10,
			Cursor: cursor,
		})
		require.NoError(t, err)

		if pageNum == 1 {
			assert.True(t, page.HasTotal, "first page must include total")
			assert.Equal(t, total, page.Total)
		} else {
			assert.False(t, page.HasTotal, "subsequent pages must not include total")
		}

		for _, c := range page.Items {
			seen = append(seen, c.ID)
		}

		if page.NextCursor == "" {
			break
		}

		cursor = page.NextCursor
	}

	assert.Len(t, seen, total)

	// Ordering should be ID-ascending, stable across pages.
	sorted := slices.Clone(seen)
	slices.Sort(sorted)
	assert.Equal(t, sorted, seen, "paginated walk must be ID-ascending")
}

// TestListCardsPage_InvalidCursor verifies a non-base64url cursor returns
// ErrInvalidCursor so the handler can map it to 400.
func TestListCardsPage_InvalidCursor(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{
		Limit:  10,
		Cursor: "!!!not-base64url",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCursor)
}

// TestListCardsPage_EmptyProject covers the zero-card case: Items is empty,
// NextCursor is empty, and Total is populated on the first (and only) page.
func TestListCardsPage_EmptyProject(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{Limit: 10})
	require.NoError(t, err)

	assert.Empty(t, page.Items)
	assert.Empty(t, page.NextCursor)
	assert.True(t, page.HasTotal)
	assert.Equal(t, 0, page.Total)
}

// TestListCardsPage_CursorEncoding confirms the emitted NextCursor decodes
// back to the last-seen card ID - clients must treat it as opaque but the
// server contract is documented base64url(lastID).
func TestListCardsPage_CursorEncoding(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	for i := range 3 {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.NextCursor)

	decoded, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, page.Items[0].ID, string(decoded))
}

// TestListCardsPage_FilterDoesNotAffectTotal confirms Total reflects the
// un-filtered project size. This lets clients show "showing X of Y" while a
// filter is active.
func TestListCardsPage_FilterDoesNotAffectTotal(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// 2 tasks + 1 bug = 3 total.
	for i := range 2 {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Task %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Bug",
		Type:     "bug",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Filter down to bugs only; total should still report 3.
	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{Type: "bug"}, PageOpts{Limit: 10})
	require.NoError(t, err)

	assert.Len(t, page.Items, 1)
	assert.True(t, page.HasTotal)
	assert.Equal(t, 3, page.Total, "Total must reflect un-filtered project size")
}

func TestCreateCard_RejectsBadSkillNames(t *testing.T) {
	cases := []struct {
		name  string
		input []string
	}{
		{"path traversal", []string{"../etc/passwd"}},
		{"uppercase", []string{"Go-Development"}},
		{"space", []string{"go development"}},
		{"leading dash", []string{"-go-development"}},
		{"leading dot", []string{".secret"}},
		{"slash", []string{"a/b"}},
		{"empty string", []string{""}},
		{"dotdot", []string{".."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			skills := c.input
			_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
				Title:    "t",
				Type:     "task",
				Priority: "low",
				Skills:   &skills,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid skill name")
		})
	}
}

func TestCreateCard_AcceptsValidSkillNames(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	skills := []string{"go-development", "documentation", "v1.0", "code_review"}
	card, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
		Title:    "t",
		Type:     "task",
		Priority: "low",
		Skills:   &skills,
	})
	require.NoError(t, err)
	require.NotNil(t, card.Skills)
	assert.Equal(t, skills, *card.Skills)
}

func TestUpdateCard_RejectsBadSkillNames(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card with valid skills.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "parent",
		Type:     "task",
		Priority: "low",
		Skills:   nil,
	})
	require.NoError(t, err)

	// Try to update with invalid skill names.
	badSkills := []string{"../etc/passwd"}
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:    card.Title,
		Type:     card.Type,
		Priority: card.Priority,
		Skills:   &badSkills,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

func TestPatchCard_RejectsBadSkillNames(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "test",
		Type:     "task",
		Priority: "low",
	})
	require.NoError(t, err)

	// Try to patch with invalid skill names.
	badSkills := []string{"Uppercase"}
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Skills: &badSkills,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

func TestPatchCard_AcceptsValidSkillNames(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card without skills.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "test",
		Type:     "task",
		Priority: "low",
	})
	require.NoError(t, err)

	// Patch with valid skills.
	skills := []string{"go-development", "code-review"}
	updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Skills: &skills,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Skills)
	assert.Equal(t, skills, *updated.Skills)
}

func TestPatchCard_TypeChange(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("changes type to allowed value", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "switch type",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		newType := "feature"
		updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			Type: &newType,
		})
		require.NoError(t, err)
		assert.Equal(t, "feature", updated.Type)
	})

	t.Run("rejects setting type to subtask directly", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "no manual subtask",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		newType := "subtask"
		_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			Type: &newType,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})

	t.Run("rejects type not in project's allowed list", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "unknown type",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		newType := "epic"
		_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			Type: &newType,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})

	t.Run("rejects changing type on a subtask card", func(t *testing.T) {
		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "parent",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		subtask, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "child",
			Type:     "task",
			Priority: "low",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		require.Equal(t, board.SubtaskType, subtask.Type)

		newType := "feature"
		_, err = svc.PatchCard(ctx, "test-project", subtask.ID, PatchCardInput{
			Type: &newType,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})
}

func TestPatchCard_PhaseValidation(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "phase test",
		Type:     "task",
		Priority: "low",
	})
	require.NoError(t, err)

	bad := "shipping"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: &bad})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid phase")

	good := "plan"
	got, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: &good})
	require.NoError(t, err)
	assert.Equal(t, "plan", got.Phase)

	empty := ""
	got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: &empty})
	require.NoError(t, err)
	assert.Empty(t, got.Phase)
}

func TestPatchCard_ModelPins(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "model pin test",
		Type:     "task",
		Priority: "low",
	})
	require.NoError(t, err)

	// Set all three pins.
	orch := "anthropic/claude-opus-4"
	coder := "anthropic/claude-sonnet-4-5"
	reviewer := "openai/gpt-4o"

	got, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		ModelOrchestrator: &orch,
		ModelCoder:        &coder,
		ModelReviewer:     &reviewer,
	})
	require.NoError(t, err)
	assert.Equal(t, orch, got.ModelOrchestrator)
	assert.Equal(t, coder, got.ModelCoder)
	assert.Equal(t, reviewer, got.ModelReviewer)

	// Clear one pin with empty string (*string semantics: "" = clear, nil = untouched).
	clearCoder := ""
	got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		ModelCoder: &clearCoder,
	})
	require.NoError(t, err)
	assert.Equal(t, orch, got.ModelOrchestrator, "orchestrator pin untouched")
	assert.Empty(t, got.ModelCoder, "coder pin cleared")
	assert.Equal(t, reviewer, got.ModelReviewer, "reviewer pin untouched")

	// nil leaves existing value alone.
	got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		ModelOrchestrator: nil,
	})
	require.NoError(t, err)
	assert.Equal(t, orch, got.ModelOrchestrator, "nil = untouched")
}

func TestCreateCard_ModelPins(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:             "model pin create test",
		Type:              "task",
		Priority:          "low",
		ModelOrchestrator: "anthropic/claude-opus-4",
		ModelCoder:        "anthropic/claude-sonnet-4-5",
		ModelReviewer:     "openai/gpt-4o",
	})
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-opus-4", card.ModelOrchestrator)
	assert.Equal(t, "anthropic/claude-sonnet-4-5", card.ModelCoder)
	assert.Equal(t, "openai/gpt-4o", card.ModelReviewer)

	// Pins persist to disk, not just the returned struct.
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-opus-4", reloaded.ModelOrchestrator)
	assert.Equal(t, "anthropic/claude-sonnet-4-5", reloaded.ModelCoder)
	assert.Equal(t, "openai/gpt-4o", reloaded.ModelReviewer)
}

func TestModelPinValidation(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	valid := map[string]bool{"anthropic/claude-sonnet-4.5": true}

	svc.SetModelValidator(func(_ context.Context, slug string) bool { return valid[slug] })

	t.Run("create rejects unknown pin", func(t *testing.T) {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t", Type: "task", Priority: "medium",
			ModelCoder: "anthropic/claude-sonet-4.5", // typo
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidModelPin)
	})

	t.Run("create accepts known pin", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t2", Type: "task", Priority: "medium",
			ModelCoder: "anthropic/claude-sonnet-4.5",
		})
		require.NoError(t, err)
		assert.Equal(t, "anthropic/claude-sonnet-4.5", card.ModelCoder)
	})

	t.Run("patch rejects changed-to-unknown pin", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t3", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		bad := "vendor/none"
		_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{ModelReviewer: &bad})
		assert.ErrorIs(t, err, ErrInvalidModelPin)
	})

	t.Run("unchanged legacy pin passes update", func(t *testing.T) {
		// Simulate a pre-validation pin: allow it during create, then shrink
		// the valid set so it would fail if re-validated.
		valid["legacy/old-model"] = true
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t4", Type: "task", Priority: "medium",
			ModelOrchestrator: "legacy/old-model",
		})
		require.NoError(t, err)
		delete(valid, "legacy/old-model")

		// Full update carrying the same pin value must NOT be rejected.
		updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title: "t4 renamed", Type: "task", State: card.State, Priority: "medium",
			ModelOrchestrator: "legacy/old-model",
		})
		require.NoError(t, err)
		assert.Equal(t, "legacy/old-model", updated.ModelOrchestrator)
	})

	t.Run("clearing a pin always passes", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t5", Type: "task", Priority: "medium",
			ModelCoder: "anthropic/claude-sonnet-4.5",
		})
		require.NoError(t, err)

		empty := ""
		_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{ModelCoder: &empty})
		require.NoError(t, err)
	})

	t.Run("nil validator disables validation", func(t *testing.T) {
		svc.SetModelValidator(nil)
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "t6", Type: "task", Priority: "medium",
			ModelCoder: "anything/goes",
		})
		require.NoError(t, err)
	})
}

// TestPatchCardUpsertSection verifies the UpsertSection field on
// PatchCardInput: replace-or-append semantics via board.UpsertSection,
// mutual exclusivity with Body, heading validation, and the combined-length
// cap enforced once the current body is in hand.
func TestPatchCardUpsertSection(t *testing.T) {
	ctx := context.Background()

	const initialBody = "intro\n\n## Plan\n\nold\n"

	// newUpsertTestCard creates a card with the given body and claims it as
	// "agent-1", matching the PatchCard AgentID ownership checks exercised below.
	newUpsertTestCard := func(t *testing.T, body string) (*CardService, *board.Card) {
		t.Helper()

		svc, _, cleanup := setupTest(t)
		t.Cleanup(cleanup)

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "upsert section test", Type: "task", Priority: "medium",
			Body: body,
		})
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err)

		return svc, card
	}

	t.Run("upsert new section appends", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		got, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "Review Findings (Round 1)", Content: "- f1"},
		})
		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(got.Body, "## Review Findings (Round 1)\n\n- f1\n"),
			"body should end with the new section, got %q", got.Body)
		assert.Contains(t, got.Body, "## Plan\n\nold", "existing human text must be untouched")
	})

	t.Run("upsert existing heading replaces in place", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		got, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "Plan", Content: "new"},
		})
		require.NoError(t, err)
		assert.Contains(t, got.Body, "## Plan\n\nnew\n")
		assert.NotContains(t, got.Body, "old")
	})

	t.Run("body and upsert_section are mutually exclusive", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		newBody := "replacement"
		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			Body:          &newBody,
			UpsertSection: &SectionPatch{Heading: "Plan", Content: "new"},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "mutually exclusive")
	})

	t.Run("heading containing a newline is rejected", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "Plan\nInjected", Content: "new"},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidSectionPatch)
	})

	t.Run("empty or whitespace-only heading is rejected", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "   ", Content: "new"},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidSectionPatch)
	})

	t.Run("'#'-prefixed heading is rejected", func(t *testing.T) {
		svc, card := newUpsertTestCard(t, initialBody)

		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "## Plan", Content: "new"},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidSectionPatch)
	})

	t.Run("combined length over the body cap is rejected", func(t *testing.T) {
		big := strings.Repeat("a", maxBodyLen-100)
		svc, card := newUpsertTestCard(t, big)

		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID:       "agent-1",
			UpsertSection: &SectionPatch{Heading: "Overflow", Content: strings.Repeat("b", 1000)},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrFieldTooLong)
	})
}

// TestCreateUpdatePatchCard_MaxCapability verifies that MaxCapability
// survives create (true), PUT clears (false), PATCH nil leaves unchanged,
// and PATCH true sets.
func TestCreateUpdatePatchCard_MaxCapability(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create with MaxCapability true.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:         "max cap test",
		Type:          "task",
		Priority:      "medium",
		MaxCapability: true,
	})
	require.NoError(t, err)
	assert.True(t, card.MaxCapability)

	// Reload and verify persistence.
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, reloaded.MaxCapability)

	// PUT with MaxCapability false clears it.
	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:         "max cap test updated",
		Type:          "task",
		State:         card.State,
		Priority:      "medium",
		MaxCapability: false,
	})
	require.NoError(t, err)
	assert.False(t, updated.MaxCapability)

	// PATCH nil leaves MaxCapability unchanged.
	newTitle := "max cap test patched"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title: &newTitle,
	})
	require.NoError(t, err)
	assert.False(t, patched.MaxCapability, "nil MaxCapability must leave stored value unchanged")

	// PATCH true sets it.
	maxCap := true
	patched, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		MaxCapability: &maxCap,
	})
	require.NoError(t, err)
	assert.True(t, patched.MaxCapability)
}

// TestCreateUpdatePatchCard_DependsOnLimit verifies that create, PUT, and
// PATCH all reject a depends_on list over maxDependsOn with the same error
// the Labels count limit produces (ErrFieldTooLong), before any card
// mutation is applied.
func TestCreateUpdatePatchCard_DependsOnLimit(t *testing.T) {
	overLimit := make([]string, maxDependsOn+1)
	for i := range overLimit {
		overLimit[i] = fmt.Sprintf("TEST-%d", i+1)
	}

	t.Run("create", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{
			Title:     "t",
			Type:      "task",
			Priority:  "low",
			DependsOn: overLimit,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrFieldTooLong)
	})

	t.Run("update (PUT)", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "t",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title:     card.Title,
			Type:      card.Type,
			State:     card.State,
			Priority:  card.Priority,
			DependsOn: overLimit,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrFieldTooLong)
	})

	t.Run("patch", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		ctx := context.Background()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "t",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			DependsOn: overLimit,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrFieldTooLong)
	})
}
