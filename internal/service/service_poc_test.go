package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func pocBaseInput(poc *bool) UpdateProjectInput {
	in := remoteExecBaseInput(nil)
	in.POC = poc

	return in
}

func TestUpdateProject_POCSetPreserveClear(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	projectDir := filepath.Join(tmpDir, "boards", "test-project")

	cfg, err := svc.UpdateProject(ctx, "test-project", pocBaseInput(new(true)))
	require.NoError(t, err)
	assert.True(t, cfg.POC)

	reloaded, err := board.LoadProjectConfig(projectDir)
	require.NoError(t, err)
	assert.True(t, reloaded.POC, "poc must round-trip through .board.yaml")

	cfg, err = svc.UpdateProject(ctx, "test-project", pocBaseInput(nil))
	require.NoError(t, err)
	assert.True(t, cfg.POC, "nil poc must preserve the flag")

	cfg, err = svc.UpdateProject(ctx, "test-project", pocBaseInput(new(false)))
	require.NoError(t, err)
	assert.False(t, cfg.POC)

	// Absent means false is the file contract boardmerge and other instances parse.
	raw, err := os.ReadFile(filepath.Join(projectDir, ".board.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "poc:", "a false flag is omitted from .board.yaml")
}
