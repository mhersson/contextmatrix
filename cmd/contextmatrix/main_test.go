package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitops"
)

func staticMainTestProvider(t *testing.T) githubauth.TokenGenerator {
	t.Helper()

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	return p
}

func TestDirHasGit_PresentAbsent(t *testing.T) {
	tmpDir := t.TempDir()

	// No .git yet — should return false.
	assert.False(t, dirHasGit(tmpDir), "dir without .git should return false")

	// Create .git directory.
	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))

	assert.True(t, dirHasGit(tmpDir), "dir with .git directory should return true")

	// Empty string — should return false.
	assert.False(t, dirHasGit(""), "empty string should return false")
}

// TestStartupPullTaskSkills_SwallowsError verifies that a pull failure
// (unreachable remote) is logged as a warning but does not panic or propagate.
func TestStartupPullTaskSkills_SwallowsError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found, skipping")
	}

	tmpDir := t.TempDir()

	mgr, err := gitops.NewManager(tmpDir, "", "test", staticMainTestProvider(t))
	require.NoError(t, err)

	// Add a bogus remote so hasRemote() returns true and PullFastForward is attempted.
	bogusURL := "https://127.0.0.1:1/does-not-exist.git"
	require.NoError(t, mgr.AddRemote(t.Context(), "origin", bogusURL))

	// Must not panic; error is swallowed with a Warn log.
	startupPullTaskSkills(true, bogusURL, mgr)
}
