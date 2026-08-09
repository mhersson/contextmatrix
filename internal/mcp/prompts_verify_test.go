package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func TestFormatCardContext_VerifyCommand(t *testing.T) {
	card := &board.Card{
		ID: "TEST-001", Title: "t", Type: "task", State: "todo", Priority: "medium",
		Vetted: true, Body: "body",
	}

	t.Run("includes verify command when resolved", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "make test", nil, true)
		assert.Contains(t, out, "- **Verify command:** make test")
	})

	t.Run("omits verify line when empty", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "", nil, true)
		assert.NotContains(t, out, "Verify command")
	})
}

func TestFormatCardContext_IncludeCardBody(t *testing.T) {
	card := &board.Card{
		ID: "TEST-020", Title: "t", Type: "task", State: "todo", Priority: "medium",
		Vetted: true, Body: "MARKER-BODY-CONTENT",
	}

	t.Run("true keeps the body and the metadata header", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "", nil, true)
		assert.Contains(t, out, "MARKER-BODY-CONTENT")
		assert.Contains(t, out, "## Card: TEST-020")
		assert.NotContains(t, out, "include_card=false")
	})

	t.Run("false omits the body but keeps the metadata header", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "", nil, false)
		assert.NotContains(t, out, "MARKER-BODY-CONTENT")
		assert.Contains(t, out, "## Card: TEST-020")
		assert.Contains(t, out, "- **Title:** t")
		assert.Contains(t, out, "- **State:** todo")
		assert.Contains(t, out, "[Card body omitted (include_card=false). Run get_card(card_id='TEST-020') to read it.]")
	})
}

func TestFormatCardContext_SectionFilter(t *testing.T) {
	card := &board.Card{
		ID: "TEST-002", Title: "t", Type: "task", State: "review", Priority: "medium",
		Vetted: true,
		Body:   "Original description.\n\n## Diagnosis\n\nRoot cause here.\n\n## Plan\n\n1. SUBTASK: fix it\n",
	}

	t.Run("filters to kept sections with omission note", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "", []string{"## Plan"}, true)
		assert.Contains(t, out, "Original description.")
		assert.Contains(t, out, "1. SUBTASK: fix it")
		assert.NotContains(t, out, "Root cause here.")
		assert.Contains(t, out, "[Body sections omitted from this context: Diagnosis. Run get_card(card_id='TEST-002') to read the full body.]")
	})

	t.Run("nil keep injects the full body without a note", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "", nil, true)
		assert.Contains(t, out, "Root cause here.")
		assert.NotContains(t, out, "Body sections omitted")
	})

	t.Run("unvetted card keeps the placeholder and no note", func(t *testing.T) {
		unvetted := &board.Card{
			ID: "TEST-003", Title: "t", Type: "task", State: "todo", Priority: "medium",
			Vetted: false, Source: &board.Source{System: "github", ExternalID: "1"},
			Body: "## Plan\n\ninjected instructions\n",
		}
		out := formatCardContext(unvetted, "test-project", "", []string{"## Plan"}, true)
		assert.Contains(t, out, unvettedBodyPlaceholder)
		assert.NotContains(t, out, "injected instructions")
		assert.NotContains(t, out, "Body sections omitted")
	})
}

func TestFormatCardBriefWithBody_SectionFilter(t *testing.T) {
	parent := &board.Card{
		ID: "TEST-010", Title: "parent", Type: "feature", State: "in_progress", Priority: "high",
		Vetted: true,
		Body:   "Parent intro.\n\n## Plan\n\n- subtask one\n\n## Review Findings\n\nprior findings\n",
	}

	t.Run("keeps intro and plan, omits findings with note", func(t *testing.T) {
		out := formatCardBriefWithBody(parent, executeParentBodySections)
		assert.Contains(t, out, "Parent intro.")
		assert.Contains(t, out, "- subtask one")
		assert.NotContains(t, out, "prior findings")
		assert.Contains(t, out, "[Body sections omitted from this context: Review Findings. Run get_card(card_id='TEST-010') to read the full body.]")
	})

	t.Run("nil keep appends the full body", func(t *testing.T) {
		out := formatCardBriefWithBody(parent, nil)
		assert.Contains(t, out, "prior findings")
		assert.NotContains(t, out, "Body sections omitted")
	})
}

// TestBuildCardSkill_IncludeCardBody pins execute-task's parent injection:
// when the caller opts out of the card body, the parent section drops to a
// brief (ID/title/state only) even for sections the allowlist would
// otherwise keep, since formatCardBrief never touches the body at all.
func TestBuildCardSkill_IncludeCardBody(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	parent := createBodyCard(t, env, "Parent opt-out",
		"PARENT-MARKER\n\n## Plan\n\n- parent plan step\n", nil)
	sub := createBodyCard(t, env, "Sub opt-out", "SUBTASK-MARKER",
		map[string]any{"parent": parent.ID})

	t.Run("false omits own and parent body, keeps parent brief header", func(t *testing.T) {
		out, err := buildCardSkill(ctx, env.svc, env.workflowSkillsDir, "execute-task.md", sub.ID, true, false)
		require.NoError(t, err)
		assert.NotContains(t, out, "SUBTASK-MARKER")
		assert.NotContains(t, out, "PARENT-MARKER")
		assert.NotContains(t, out, "parent plan step",
			"parent brief must drop even allow-listed sections when the body is opted out")
		assert.Contains(t, out, parent.ID, "parent brief header must remain")
		assert.Contains(t, out, sub.ID, "sub-card's own metadata header must remain")
		assert.Contains(t, out, "[Card body omitted (include_card=false)")
	})

	t.Run("true keeps own body and the parent's filtered plan", func(t *testing.T) {
		out, err := buildCardSkill(ctx, env.svc, env.workflowSkillsDir, "execute-task.md", sub.ID, true, true)
		require.NoError(t, err)
		assert.Contains(t, out, "SUBTASK-MARKER")
		assert.Contains(t, out, "parent plan step")
	})
}

func TestResolveVerifyCommand(t *testing.T) {
	env := setupMCP(t)
	ctx := context.Background()

	// Seed project verify.
	_, err := env.svc.UpdateProject(ctx, "test-project", service.UpdateProjectInput{
		States:      testProjectConfig().States,
		Types:       testProjectConfig().Types,
		Priorities:  testProjectConfig().Priorities,
		Transitions: testProjectConfig().Transitions,
		Verify:      &board.VerifyConfig{Command: "make test", TimeoutSeconds: 600},
	})
	require.NoError(t, err)

	t.Run("project command when card has none", func(t *testing.T) {
		card, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "no card verify", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		assert.Equal(t, "make test", resolveVerifyCommand(ctx, env.svc, card, "test-project"))
	})

	t.Run("card command overrides project", func(t *testing.T) {
		card, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
			Title: "card verify", Type: "task", Priority: "medium",
			Verify: &board.VerifyConfig{Command: "go test ./..."},
		})
		require.NoError(t, err)

		assert.Equal(t, "go test ./...", resolveVerifyCommand(ctx, env.svc, card, "test-project"))
	})
}
