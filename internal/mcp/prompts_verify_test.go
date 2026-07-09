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
		out := formatCardContext(card, "test-project", "make test")
		assert.Contains(t, out, "- **Verify command:** make test")
	})

	t.Run("omits verify line when empty", func(t *testing.T) {
		out := formatCardContext(card, "test-project", "")
		assert.NotContains(t, out, "Verify command")
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
