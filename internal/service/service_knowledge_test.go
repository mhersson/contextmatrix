package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func knowledgeInput(p, s *string) UpdateProjectInput {
	in := remoteExecBaseInput(nil)
	in.KnowledgeProvider = p
	in.KnowledgeScope = s

	return in
}

func TestUpdateProject_KnowledgeSetPreserveClear(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	projectDir := filepath.Join(tmpDir, "boards", "test-project")

	cfg, err := svc.UpdateProject(ctx, "test-project", knowledgeInput(new(" intent "), new(" payments ")))
	require.NoError(t, err)
	assert.Equal(t, "intent", cfg.KnowledgeProvider)
	assert.Equal(t, "payments", cfg.KnowledgeScope)

	reloaded, err := board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "intent", reloaded.KnowledgeProvider)
	assert.Equal(t, "payments", reloaded.KnowledgeScope)

	cfg, err = svc.UpdateProject(ctx, "test-project", knowledgeInput(nil, nil))
	require.NoError(t, err)
	assert.Equal(t, "intent", cfg.KnowledgeProvider, "nil must preserve")
	assert.Equal(t, "payments", cfg.KnowledgeScope, "nil must preserve")

	cfg, err = svc.UpdateProject(ctx, "test-project", knowledgeInput(new(""), new("")))
	require.NoError(t, err)
	assert.Empty(t, cfg.KnowledgeProvider)
	assert.Empty(t, cfg.KnowledgeScope)

	raw, err := os.ReadFile(filepath.Join(projectDir, ".board.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "knowledge_provider:")
	assert.NotContains(t, string(raw), "knowledge_scope:")
}

func TestUpdateProject_KnowledgeValidation(t *testing.T) {
	long := strings.Repeat("s", 201)

	tests := []struct {
		name            string
		provider, scope *string
		field           string
	}{
		{"provider without scope", new("intent"), new(""), "knowledge_scope"},
		{"provider set, scope never set", new("intent"), nil, "knowledge_scope"},
		{"scope too long", new("intent"), &long, "knowledge_scope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			ctx := context.Background()

			_, err := svc.UpdateProject(ctx, "test-project", knowledgeInput(tt.provider, tt.scope))
			require.ErrorIs(t, err, board.ErrInvalidProjectConfig)
			assert.Contains(t, err.Error(), tt.field)

			cfg, err := svc.GetProject(ctx, "test-project")
			require.NoError(t, err)
			assert.Empty(t, cfg.KnowledgeProvider)
			assert.Empty(t, cfg.KnowledgeScope)
		})
	}
}
