package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/require"
)

func TestCardServiceGetProjectKB(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	boardsDir := filepath.Join(tmpDir, "boards")

	// Add the new fields to the existing test-project's .board.yaml
	// by re-saving the project config with Repos + JiraProjectKey.
	cfg, err := svc.GetProject(context.Background(), "test-project")
	require.NoError(t, err)

	cfg.JiraProjectKey = "PAY"
	cfg.Repos = []board.RepoSpec{
		{Slug: "auth-svc", URL: "https://github.com/acme/auth-svc.git"},
	}
	require.NoError(t, svc.store.SaveProject(context.Background(), cfg))

	// Drop KB files into the boards dir.
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "_kb", "repos"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "_kb", "jira-projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(boardsDir, "_kb", "repos", "auth-svc.md"), []byte("# auth-svc kb\nworld"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(boardsDir, "_kb", "jira-projects", "PAY.md"), []byte("# PAY"), 0o644))

	kb, err := svc.GetProjectKB(context.Background(), "test-project")
	require.NoError(t, err)
	require.Contains(t, kb.Repos["auth-svc"], "world")
	require.Contains(t, kb.JiraProject, "PAY")
}

func TestCardServiceGetProjectKBProjectNotFound(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.GetProjectKB(context.Background(), "no-such-project")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no-such-project",
		"wrapped error should mention the missing project name")
}

func TestCardServiceGetProjectKBWithRepoFilter(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	boardsDir := filepath.Join(tmpDir, "boards")

	cfg, err := svc.GetProject(context.Background(), "test-project")
	require.NoError(t, err)

	cfg.Repos = []board.RepoSpec{
		{Slug: "r1", URL: "https://example.com/r1.git"},
		{Slug: "r2", URL: "https://example.com/r2.git"},
	}
	require.NoError(t, svc.store.SaveProject(context.Background(), cfg))

	require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, "_kb", "repos"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(boardsDir, "_kb", "repos", "r1.md"), []byte("r1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(boardsDir, "_kb", "repos", "r2.md"), []byte("r2"), 0o644))

	kb, err := svc.GetProjectKB(context.Background(), "test-project", "r1")
	require.NoError(t, err)
	require.Len(t, kb.Repos, 1)
	require.Contains(t, kb.Repos, "r1")
}
