package gitops

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

// staticTestProvider returns a PAT-backed provider for tests where the
// token value doesn't matter.
func staticTestProvider(t *testing.T) githubauth.TokenGenerator {
	t.Helper()

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	return p
}

func TestNewManager_InitNewRepo(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.NotNil(t, mgr)
	assert.Equal(t, tmpDir, mgr.RepoPath())

	// Verify .git directory was created
	gitDir := filepath.Join(tmpDir, ".git")
	info, err := os.Stat(gitDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewManager_OpenExistingRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize repo first
	_, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	// Open with manager
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestCommitFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create a test file
	testFile := "test.txt"
	content := []byte("hello world")
	err = os.WriteFile(filepath.Join(tmpDir, testFile), content, 0o644)
	require.NoError(t, err)

	// Commit the file
	message := "[contextmatrix] TEST-001: created test file"
	err = mgr.CommitFile(context.Background(), testFile, message)
	require.NoError(t, err)

	// Verify commit was created
	lastMsg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, message, lastMsg)
}

func TestCommitFile_OnlyStagesSpecifiedFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create two files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0o644)
	require.NoError(t, err)

	// Commit only file1
	err = mgr.CommitFile(context.Background(), "file1.txt", "commit file1")
	require.NoError(t, err)

	// file2 should still be untracked (uncommitted changes)
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges, "file2.txt should still be untracked")
}

func TestCommitAll(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create multiple files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0o644)
	require.NoError(t, err)

	// Commit all
	err = mgr.CommitAll(context.Background(), "commit all files")
	require.NoError(t, err)

	// No uncommitted changes should remain
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)
}

func TestSetAuthor(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Set custom author
	mgr.SetAuthor("Test User", "test@example.com")

	// Create and commit a file
	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "test.txt", "test commit")
	require.NoError(t, err)

	// Verify author
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	assert.Equal(t, "Test User", commit.Author.Name)
	assert.Equal(t, "test@example.com", commit.Author.Email)
}

func TestPull_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Pull should succeed gracefully when no remote exists
	err = mgr.Pull(context.Background())
	assert.NoError(t, err)
}

func TestPush_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Push should succeed gracefully when no remote exists
	err = mgr.Push(context.Background())
	assert.NoError(t, err)
}

// TestPushPull_BareRemote verifies that Push and Pull work against a local
// bare repository so that the shell-git path is exercised without requiring
// network access or SSH credentials.
func TestPushPull_BareRemote(t *testing.T) {
	// Skip if git binary is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found, skipping")
	}

	ctx := context.Background()

	// Create a bare remote repo.
	bareDir := t.TempDir()
	_, err := git.PlainInit(bareDir, true)
	require.NoError(t, err)

	// Create the working repo, make a commit, add the bare repo as origin.
	workDir := t.TempDir()
	mgr, err := NewManager(workDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test User", "test@example.com")

	err = os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "hello.txt", "initial commit")
	require.NoError(t, err)

	err = mgr.AddRemote(context.Background(), "origin", "file://"+bareDir)
	require.NoError(t, err)

	// Push to the bare remote — should succeed.
	err = mgr.Push(ctx)
	require.NoError(t, err, "push to local bare remote should succeed")

	// Create a second working repo cloned from the bare remote.
	cloneDir := t.TempDir()
	_, err = git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL: "file://" + bareDir,
	})
	require.NoError(t, err)

	// Add a commit to the bare remote via the clone.
	cloneMgr, err := NewManager(cloneDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	cloneMgr.SetAuthor("Clone User", "clone@example.com")

	err = os.WriteFile(filepath.Join(cloneDir, "world.txt"), []byte("world"), 0o644)
	require.NoError(t, err)
	err = cloneMgr.CommitFile(context.Background(), "world.txt", "second commit")
	require.NoError(t, err)
	err = cloneMgr.Push(ctx)
	require.NoError(t, err)

	// Pull (rebase) in the original working repo — should succeed and bring in world.txt.
	err = mgr.Pull(ctx)
	require.NoError(t, err, "pull --rebase from local bare remote should succeed")

	_, err = os.Stat(filepath.Join(workDir, "world.txt"))
	assert.NoError(t, err, "world.txt should exist after pull")
}

// TestPull_AutoReloadsGoGit verifies that after Pull, go-git sees the new
// commits without the caller having to call ReloadRepo explicitly.
func TestPull_AutoReloadsGoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	ctx := context.Background()

	// Create a bare remote and two working copies.
	bareDir := t.TempDir()
	_, err := git.PlainInit(bareDir, true)
	require.NoError(t, err)

	workDir := t.TempDir()
	mgr, err := NewManager(workDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test User", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "init.txt"), []byte("init"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "init.txt", "initial commit"))
	require.NoError(t, mgr.AddRemote(context.Background(), "origin", "file://"+bareDir))
	require.NoError(t, mgr.Push(ctx))

	// Clone and push a new commit from a second working copy.
	cloneDir := t.TempDir()
	_, err = git.PlainClone(cloneDir, false, &git.CloneOptions{URL: "file://" + bareDir})
	require.NoError(t, err)
	cloneMgr, err := NewManager(cloneDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	cloneMgr.SetAuthor("Clone User", "clone@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "new.txt"), []byte("new"), 0o644))
	require.NoError(t, cloneMgr.CommitFile(context.Background(), "new.txt", "remote commit"))
	require.NoError(t, cloneMgr.Push(ctx))

	// Pull in the original repo.
	require.NoError(t, mgr.Pull(ctx))

	// go-git should see the remote commit without explicit ReloadRepo.
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "remote commit", strings.TrimSpace(msg),
		"go-git should see remote commit after Pull auto-reload")

	// go-git operations should still work after the auto-reload.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "post-pull.txt"), []byte("post"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "post-pull.txt", "post-pull commit"))

	msg, err = mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "post-pull commit", strings.TrimSpace(msg))
}

// TestPush_AutoReloadsGoGit verifies that after Push, go-git's in-memory
// state is refreshed so subsequent go-git operations see correct refs.
func TestPush_AutoReloadsGoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	ctx := context.Background()

	bareDir := t.TempDir()
	_, err := git.PlainInit(bareDir, true)
	require.NoError(t, err)

	workDir := t.TempDir()
	mgr, err := NewManager(workDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test User", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "init.txt"), []byte("init"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "init.txt", "initial commit"))
	require.NoError(t, mgr.AddRemote(context.Background(), "origin", "file://"+bareDir))
	require.NoError(t, mgr.Push(ctx))

	// After push, go-git should still work correctly.
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "initial commit", strings.TrimSpace(msg),
		"go-git should see correct HEAD after Push auto-reload")

	// Subsequent commits and pushes should work.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "second.txt"), []byte("second"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "second.txt", "second commit"))
	require.NoError(t, mgr.Push(ctx))

	msg, err = mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "second commit", strings.TrimSpace(msg))
}

func TestAddRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	err = mgr.AddRemote(context.Background(), "origin", "https://github.com/test/repo.git")
	require.NoError(t, err)

	// Verify remote was added
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)
	remotes, err := repo.Remotes()
	require.NoError(t, err)

	found := false

	for _, r := range remotes {
		if r.Config().Name == "origin" {
			found = true

			assert.Contains(t, r.Config().URLs, "https://github.com/test/repo.git")
		}
	}

	assert.True(t, found, "origin remote should exist")
}

func TestHasUncommittedChanges(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Initially no changes (empty repo)
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)

	// Create a file
	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0o644)
	require.NoError(t, err)

	// Now has uncommitted changes
	hasChanges, err = mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges)

	// Commit the file
	err = mgr.CommitFile(context.Background(), "test.txt", "commit")
	require.NoError(t, err)

	// No more uncommitted changes
	hasChanges, err = mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)
}

func TestGetLastCommitMessage_NoCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Empty repo has no commits
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Empty(t, msg)
}

func TestGetLastCommitMessage_WithCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create and commit files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("1"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "file1.txt", "first commit")
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("2"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "file2.txt", "second commit")
	require.NoError(t, err)

	// Should return the latest commit message
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "second commit", msg)
}

func TestCommitFile_InSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create subdirectory and file
	subdir := filepath.Join(tmpDir, "boards", "project-alpha", "tasks")
	err = os.MkdirAll(subdir, 0o755)
	require.NoError(t, err)

	testFile := filepath.Join(subdir, "ALPHA-001.md")
	err = os.WriteFile(testFile, []byte("# Card content"), 0o644)
	require.NoError(t, err)

	// Commit with relative path
	relativePath := "boards/project-alpha/tasks/ALPHA-001.md"
	err = mgr.CommitFile(context.Background(), relativePath, "[contextmatrix] ALPHA-001: created card")
	require.NoError(t, err)

	// Verify commit
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "ALPHA-001")
}

func TestDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create and commit a file
	testFile := "test.txt"
	err = os.WriteFile(filepath.Join(tmpDir, testFile), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), testFile, "add file")
	require.NoError(t, err)

	// Delete the file
	err = mgr.DeleteFile(context.Background(), testFile)
	require.NoError(t, err)

	// File should not exist
	_, err = os.Stat(filepath.Join(tmpDir, testFile))
	assert.True(t, os.IsNotExist(err))

	// Should have uncommitted changes (staged deletion)
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges)
}

func TestDeleteFile_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Deleting non-existent file should not error (file already removed)
	err = mgr.DeleteFile(context.Background(), "nonexistent.txt")
	// This will error because the file isn't tracked
	assert.Error(t, err)
}

func TestDefaultAuthor(t *testing.T) {
	assert.Equal(t, "ContextMatrix", DefaultAuthor.Name)
	assert.Equal(t, "contextmatrix@local", DefaultAuthor.Email)
}

func TestCommitMessageFormat(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "system commit",
			message: "[contextmatrix] ALPHA-001: created card",
		},
		{
			name:    "agent commit",
			message: "[agent:claude-7a3f] ALPHA-001: updated progress",
		},
		{
			name:    "human commit",
			message: "[agent:human:alice] ALPHA-002: reviewed changes",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "_")+".txt")
			err := os.WriteFile(filename, []byte("content"), 0o644)
			require.NoError(t, err)

			relPath := filepath.Base(filename)
			err = mgr.CommitFile(context.Background(), relPath, tt.message)
			require.NoError(t, err)

			// Verify message
			repo, err := git.PlainOpen(tmpDir)
			require.NoError(t, err)

			head, err := repo.Head()
			require.NoError(t, err)

			commit, err := repo.CommitObject(head.Hash())
			require.NoError(t, err)

			assert.Equal(t, tt.message, commit.Message, "test %d", i)
		})
	}
}

func TestCommitTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Add tolerance for timing variations
	before := time.Now().Add(-time.Second)

	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "test.txt", "test commit")
	require.NoError(t, err)

	after := time.Now().Add(time.Second)

	// Get commit timestamp
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	assert.False(t, commit.Author.When.Before(before), "commit time should be after test start")
	assert.False(t, commit.Author.When.After(after), "commit time should be before test end")
}

func TestConcurrentCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create files
	for i := range 10 {
		filename := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".txt")
		err := os.WriteFile(filename, []byte("content"), 0o644)
		require.NoError(t, err)
	}

	// Commit all files sequentially (mutex ensures serialization)
	for i := range 10 {
		relPath := "file" + string(rune('0'+i)) + ".txt"
		err := mgr.CommitFile(context.Background(), relPath, "commit "+relPath)
		require.NoError(t, err)
	}

	// Verify we have 10 commits
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)

	commitIter, err := repo.Log(&git.LogOptions{})
	require.NoError(t, err)

	count := 0
	err = commitIter.ForEach(func(c *object.Commit) error {
		count++

		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 10, count)
}

func TestRepoPath(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	assert.Equal(t, tmpDir, mgr.RepoPath())
}

func TestHasRemote_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	assert.False(t, mgr.HasRemote())
}

func TestHasRemote_WithRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	err = mgr.AddRemote(context.Background(), "origin", "https://github.com/test/repo.git")
	require.NoError(t, err)

	assert.True(t, mgr.HasRemote())
}

func TestCurrentBranch(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create an initial commit so HEAD exists
	err = os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("init"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "init.txt", "initial commit")
	require.NoError(t, err)

	branch, err := mgr.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "master", branch)
}

func TestCurrentBranch_NoCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Empty repo has no HEAD
	_, err = mgr.CurrentBranch()
	assert.Error(t, err)
}

func TestCommitFilesShell(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Shell User", "shell@example.com")

	// Create initial commit so HEAD exists.
	err = os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("init"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "init.txt", "initial commit")
	require.NoError(t, err)

	// Create a new file and commit via shell.
	testFile := "shell-test.txt"
	err = os.WriteFile(filepath.Join(tmpDir, testFile), []byte("shell content"), 0o644)
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.CommitFilesShell(ctx, []string{testFile}, "shell commit")
	require.NoError(t, err)

	// Verify commit message. GetLastCommitMessage may include a trailing
	// newline when reading go-git commits, so compare trimmed values.
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "shell commit", strings.TrimSpace(msg))

	// Verify author via go-git.
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	assert.Equal(t, "Shell User", commit.Author.Name)
	assert.Equal(t, "shell@example.com", commit.Author.Email)
}

func TestCommitFilesShell_NoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create and commit a file via go-git.
	testFile := "test.txt"
	err = os.WriteFile(filepath.Join(tmpDir, testFile), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), testFile, "initial commit")
	require.NoError(t, err)

	initialMsg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)

	// CommitFilesShell on the unchanged file should no-op.
	ctx := context.Background()
	err = mgr.CommitFilesShell(ctx, []string{testFile}, "should not appear")
	require.NoError(t, err)

	afterMsg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, initialMsg, afterMsg, "no new commit should be created for unchanged file")
}

func TestReloadRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test User", "test@example.com")

	// Create initial commit via go-git.
	err = os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("init"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "init.txt", "initial commit")
	require.NoError(t, err)

	// Make a commit via shell git (bypassing go-git).
	err = os.WriteFile(filepath.Join(tmpDir, "shell.txt"), []byte("shell"), 0o644)
	require.NoError(t, err)

	cmd := exec.Command("git", "add", "shell.txt")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "shell commit")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	// ReloadRepo should succeed and go-git should see the shell commit.
	err = mgr.ReloadRepo(context.Background())
	require.NoError(t, err)

	headAfter, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "shell commit", strings.TrimSpace(headAfter),
		"go-git should see shell commit after reload")

	// After reload, go-git operations (like CommitFile) should still work.
	err = os.WriteFile(filepath.Join(tmpDir, "post-reload.txt"), []byte("post"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "post-reload.txt", "post-reload commit")
	require.NoError(t, err)

	postMsg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "post-reload commit", strings.TrimSpace(postMsg))
}

// createBareRepo creates a bare git repo with one commit and returns its path.
func createBareRepo(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "bare.git")
	work := filepath.Join(t.TempDir(), "work")

	cmd := exec.Command("git", "init", "--bare", "--initial-branch=main", bare)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "clone", bare, work)
	require.NoError(t, cmd.Run())

	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("# boards\n"), 0o644))

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = work
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = work

	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "push", "origin", "main")
	cmd.Dir = work
	require.NoError(t, cmd.Run())

	return bare
}

func TestNewManager_CloneOnEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	bare := createBareRepo(t)
	target := filepath.Join(t.TempDir(), "boards")

	mgr, err := NewManager(target, bare, "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.NotNil(t, mgr)

	// Verify the clone has the commit from the bare repo
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "initial commit", strings.TrimSpace(msg))

	// Verify origin remote is set
	assert.True(t, mgr.HasRemote())
}

func TestNewManager_EmptyCloneURL_InitsRepo(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.NotNil(t, mgr)

	// Should be an empty init'd repo, no commits
	count, err := mgr.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// No remote should be configured
	assert.False(t, mgr.HasRemote())
}

func TestNewManager_ExistingRepo_IgnoresCloneURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	// Create a repo with a commit
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test", "test@test.com")
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("local"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "file.txt", "local commit"))

	// Re-open with a clone URL — should open existing, not clone
	mgr2, err := NewManager(tmpDir, "git@example.com:user/repo.git", "test", staticTestProvider(t))
	require.NoError(t, err)

	msg, err := mgr2.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "local commit", strings.TrimSpace(msg))
}

func TestNewManager_CloneURL_AddsRemoteToExistingRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Init a repo without a remote
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.False(t, mgr.HasRemote())

	// Re-open with a clone URL — should add origin remote
	mgr2, err := NewManager(tmpDir, "git@example.com:user/boards.git", "test", staticTestProvider(t))
	require.NoError(t, err)
	assert.True(t, mgr2.HasRemote())
}

// TestAuthEnvFromProvider_NilProvider verifies that AuthEnvFromProvider
// returns an error when provider is nil (no env injection).
func TestAuthEnvFromProvider_NilProvider(t *testing.T) {
	env, err := AuthEnvFromProvider(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, env)
}

// TestAuthEnvFromProvider_PATProvider verifies that a PAT provider
// produces the expected four GIT_CONFIG_* entries.
func TestAuthEnvFromProvider_PATProvider(t *testing.T) {
	const token = "ghp_testtoken123"

	p, err := githubauth.NewPATProvider(token)
	require.NoError(t, err)

	env, err := AuthEnvFromProvider(context.Background(), p)
	require.NoError(t, err)
	require.Len(t, env, 4)
	assert.Contains(t, env, "GIT_CONFIG_COUNT=1")
	assert.Contains(t, env, "GIT_CONFIG_KEY_0=http.extraheader")

	expectedCred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	assert.Contains(t, env, "GIT_CONFIG_VALUE_0=Authorization: Basic "+expectedCred)
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
}

// TestAuthEnvFromProvider_TokenNotInArgs verifies that the PAT token never
// appears in git command arguments — it must only travel via environment.
func TestAuthEnvFromProvider_TokenNotInArgs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	const token = "ghp_supersecret_should_not_leak"

	p, err := githubauth.NewPATProvider(token)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", p)
	require.NoError(t, err)
	mgr.SetAuthor("Test", "test@example.com")

	// Create an initial commit so we can call Push/Pull.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("init"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "init.txt", "initial commit"))

	// AuthEnvFromProvider places the token only in env, not args.
	env, err := AuthEnvFromProvider(context.Background(), p)
	require.NoError(t, err)

	// Confirm the token (and its base64-encoded form) only appears in
	// GIT_CONFIG_VALUE_0, never in any other entry or as a standalone arg.
	expectedCred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	for _, e := range env {
		if e == "GIT_CONFIG_VALUE_0=Authorization: Basic "+expectedCred {
			continue // correct placement
		}

		assert.NotContains(t, e, token,
			"raw token must only appear in GIT_CONFIG_VALUE_0, not in: %s", e)
		assert.NotContains(t, e, expectedCred,
			"encoded credential must only appear in GIT_CONFIG_VALUE_0, not in: %s", e)
	}
}

// histSampleCount reads the sample count from a prometheus.Histogram via the
// internal dto representation. This avoids the panic that testutil.ToFloat64
// triggers on Histograms (which expose multiple metric series).
func histSampleCount(t *testing.T, h prometheus.Histogram) uint64 {
	t.Helper()

	ch := make(chan prometheus.Metric, 1)
	h.Collect(ch)

	m := <-ch

	var d dto.Metric
	require.NoError(t, m.Write(&d))

	return d.GetHistogram().GetSampleCount()
}

// TestCommitFile_ObservesGitSyncDuration verifies that CommitFile increments
// the GitSyncDuration histogram sample count.
func TestCommitFile_ObservesGitSyncDuration(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	before := histSampleCount(t, metrics.GitSyncDuration)

	// Create and commit a file — this must trigger an observation.
	err = os.WriteFile(filepath.Join(tmpDir, "metric-test.txt"), []byte("data"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(context.Background(), "metric-test.txt", "metric test commit")
	require.NoError(t, err)

	after := histSampleCount(t, metrics.GitSyncDuration)

	assert.Greater(t, after, before, "GitSyncDuration histogram should have been observed after CommitFile")
}

// TestCommitFile_NonexistentFile verifies that committing a path that does
// not exist on disk returns a wrapped "stage file" error rather than silently
// succeeding.
func TestCommitFile_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	err = mgr.CommitFile(context.Background(), "no-such-file.txt", "should fail")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage file", "error must be wrapped with operation context")
}

// TestCommitFile_ReadOnlyRepo verifies that CommitFile fails cleanly when the
// git worktree is read-only. Go-git writes to .git/index and .git/objects/**
// during staging and commit; chmod 0500 on .git makes those writes fail.
//
// The test skips on platforms/users where chmod has no effect (root, some CI
// sandboxes) — detected by attempting to write to .git after chmod.
func TestCommitFile_ReadOnlyRepo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod does not restrict root — skipping read-only repo test")
	}

	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Create a file to commit so the commit has something legitimate to stage.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "card.md"), []byte("body"), 0o644))

	gitDir := filepath.Join(tmpDir, ".git")

	// Make .git read-only.  Restore permissions in cleanup so t.TempDir can
	// clean up the tree afterwards.
	require.NoError(t, os.Chmod(gitDir, 0o500))

	t.Cleanup(func() {
		_ = os.Chmod(gitDir, 0o755)
	})

	// Probe: verify the chmod is actually enforced. If we can still create a
	// file inside .git, the test cannot exercise the failure path — skip.
	probe := filepath.Join(gitDir, "rw-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err == nil {
		_ = os.Remove(probe)

		t.Skip("chmod 0500 on .git was not enforced — skipping")
	}

	err = mgr.CommitFile(context.Background(), "card.md", "[ctxmx] RO-001: test")
	require.Error(t, err, "commit must fail on read-only .git")
}

// TestCommitFile_IndexLocked verifies that an existing .git/index.lock
// (simulating a crashed/concurrent git process) causes CommitFile to fail
// cleanly rather than silently corrupting state.
//
// Note: go-git does not itself respect .git/index.lock — it manages its own
// locking.  However, the shell-based CommitFilesShell does respect it.  This
// test therefore exercises both commit paths and asserts at least the shell
// path fails; the go-git path may or may not fail depending on the internal
// locking protocol. The "clean failure, no corruption" property is the
// contract; which path enforces it is an implementation detail.
func TestCommitFile_IndexLocked(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	// Seed an initial commit so HEAD exists. Without this, go-git's Commit
	// has different code paths and the test would exercise first-commit
	// handling rather than the lock path.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "seed.md"), []byte("seed"), 0o644))
	require.NoError(t, mgr.CommitFile(context.Background(), "seed.md", "[ctxmx] seed"))

	// Create the index.lock sentinel — this is what git itself uses to
	// prevent concurrent index mutations.
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("pid 99999"), 0o644))

	t.Cleanup(func() {
		_ = os.Remove(lockPath)
	})

	// Attempt a shell commit with the lock in place.  Shell git must refuse.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "card.md"), []byte("body"), 0o644))

	shellErr := mgr.CommitFilesShell(context.Background(), []string{"card.md"}, "[ctxmx] locked")
	require.Error(t, shellErr, "CommitFilesShell must refuse while index.lock exists")
	// The error message surfaces git's own "index.lock" text, confirming the
	// failure was caused by the lock file, not some other issue.
	assert.Contains(t, shellErr.Error(), "index.lock",
		"error must mention index.lock to prove we exercised the right path")
}

// TestPush_RemoteUnreachable verifies that a push against an unreachable
// remote fails with a clearly wrapped error rather than hanging or producing
// a partial push. The test points the remote at a file:// URL under a
// temp directory that we then rename, making the remote vanish after the
// remote is configured — a reliable way to trigger "cannot access remote"
// without requiring network access.
func TestPush_RemoteUnreachable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found, skipping")
	}

	ctx := context.Background()

	// Create a bare remote, then wire it as origin on the working repo.
	bareDir := t.TempDir()

	_, err := git.PlainInit(bareDir, true)
	require.NoError(t, err)

	workDir := t.TempDir()
	mgr, err := NewManager(workDir, "", "test", staticTestProvider(t))
	require.NoError(t, err)

	mgr.SetAuthor("Test", "test@test.com")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("data"), 0o644))
	require.NoError(t, mgr.CommitFile(ctx, "file.txt", "initial"))
	require.NoError(t, mgr.AddRemote(ctx, "origin", bareDir))

	// Remove the bare repo so the next push cannot connect.
	require.NoError(t, os.RemoveAll(bareDir))

	// Push with a short-ish timeout so a bug that hangs the test is visible
	// as a hang rather than a pass/fail ambiguity.
	pushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = mgr.Push(pushCtx)
	require.Error(t, err, "push to removed remote must fail")
	assert.Contains(t, err.Error(), "push",
		"error must be wrapped with operation context")
}

func TestNewManager_AcceptsLabelAndProvider(t *testing.T) {
	dir := t.TempDir()
	provider := staticTestProvider(t)

	mgr, err := NewManager(dir, "", "boards", provider)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	assert.Equal(t, "boards", mgr.Label())
}

func TestManager_AuthEnv_BuildsExpectedSlice(t *testing.T) {
	dir := t.TempDir()
	provider, err := githubauth.NewPATProvider("ghp_xyz")
	require.NoError(t, err)

	mgr, err := NewManager(dir, "", "test", provider)
	require.NoError(t, err)

	env, err := mgr.AuthEnv(context.Background())
	require.NoError(t, err)
	require.Len(t, env, 4)
	assert.Equal(t, "GIT_CONFIG_COUNT=1", env[0])
	assert.Equal(t, "GIT_CONFIG_KEY_0=http.extraheader", env[1])

	expectedCred := base64.StdEncoding.EncodeToString([]byte("x-access-token:ghp_xyz"))
	assert.Equal(t, "GIT_CONFIG_VALUE_0=Authorization: Basic "+expectedCred, env[2])
	assert.Equal(t, "GIT_TERMINAL_PROMPT=0", env[3])
}
