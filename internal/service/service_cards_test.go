package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
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
// back to the last-seen card ID — clients must treat it as opaque but the
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

func TestTrimActivityLog_UnderCapNoOp(t *testing.T) {
	in := []board.ActivityEntry{
		{Action: "claimed"},
		{Action: stateChangedAction, Message: "todo -> in_progress"},
		{Action: "released"},
	}
	out := trimActivityLog(in)
	assert.Equal(t, in, out, "trim must be a no-op when under cap")
}

func TestTrimActivityLog_DropsNonStateChangedFirst(t *testing.T) {
	// 60 entries: 55 non-state-changed entries followed by 5 state_changed.
	// After trim to maxActivityLogEntries (50), all 5 state_changed entries
	// must survive — the regression the helper prevents.
	in := make([]board.ActivityEntry, 0, 60)
	for i := range 55 {
		in = append(in, board.ActivityEntry{Action: "claimed", Message: fmt.Sprintf("entry %d", i)})
	}

	stateChanges := []board.ActivityEntry{
		{Action: stateChangedAction, Message: "todo -> in_progress"},
		{Action: stateChangedAction, Message: "in_progress -> stalled"},
		{Action: stateChangedAction, Message: "stalled -> in_progress"},
		{Action: stateChangedAction, Message: "in_progress -> review"},
		{Action: stateChangedAction, Message: "review -> done"},
	}
	in = append(in, stateChanges...)

	out := trimActivityLog(in)
	require.LessOrEqual(t, len(out), maxActivityLogEntries)

	surviving := 0

	for _, e := range out {
		if e.Action == stateChangedAction {
			surviving++
		}
	}

	assert.Equal(t, len(stateChanges), surviving, "all state_changed entries must be preserved")
}

func TestTrimActivityLog_AllStateChangedOverflow(t *testing.T) {
	// 60 state_changed entries with no non-state-changed to drop: helper
	// must fall back to dropping oldest state_changed entries to enforce
	// the cap.
	in := make([]board.ActivityEntry, 60)
	for i := range in {
		in[i] = board.ActivityEntry{
			Action:  stateChangedAction,
			Message: fmt.Sprintf("s%d -> s%d", i, i+1),
		}
	}

	out := trimActivityLog(in)
	require.Len(t, out, maxActivityLogEntries)
	// The 10 oldest got dropped; first surviving entry is index 10.
	assert.Equal(t, "s10 -> s11", out[0].Message)
	assert.Equal(t, "s59 -> s60", out[len(out)-1].Message)
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
