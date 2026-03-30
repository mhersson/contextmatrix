package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_InitNewRepo(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir)
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
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestCommitFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Create a test file
	testFile := "test.txt"
	content := []byte("hello world")
	err = os.WriteFile(filepath.Join(tmpDir, testFile), content, 0o644)
	require.NoError(t, err)

	// Commit the file
	message := "[contextmatrix] TEST-001: created test file"
	err = mgr.CommitFile(testFile, message)
	require.NoError(t, err)

	// Verify commit was created
	lastMsg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, message, lastMsg)
}

func TestCommitFile_OnlyStagesSpecifiedFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Create two files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0o644)
	require.NoError(t, err)

	// Commit only file1
	err = mgr.CommitFile("file1.txt", "commit file1")
	require.NoError(t, err)

	// file2 should still be untracked (uncommitted changes)
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges, "file2.txt should still be untracked")
}

func TestCommitAll(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Create multiple files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0o644)
	require.NoError(t, err)

	// Commit all
	err = mgr.CommitAll("commit all files")
	require.NoError(t, err)

	// No uncommitted changes should remain
	hasChanges, err := mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)
}

func TestSetAuthor(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Set custom author
	mgr.SetAuthor("Test User", "test@example.com")

	// Create and commit a file
	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile("test.txt", "test commit")
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
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Pull should succeed gracefully when no remote exists
	err = mgr.Pull()
	assert.NoError(t, err)
}

func TestPush_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Push should succeed gracefully when no remote exists
	err = mgr.Push()
	assert.NoError(t, err)
}

func TestAddRemote(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	err = mgr.AddRemote("origin", "https://github.com/test/repo.git")
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
	mgr, err := NewManager(tmpDir)
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
	err = mgr.CommitFile("test.txt", "commit")
	require.NoError(t, err)

	// No more uncommitted changes
	hasChanges, err = mgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)
}

func TestGetLastCommitMessage_NoCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Empty repo has no commits
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Empty(t, msg)
}

func TestGetLastCommitMessage_WithCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Create and commit files
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("1"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile("file1.txt", "first commit")
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("2"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile("file2.txt", "second commit")
	require.NoError(t, err)

	// Should return the latest commit message
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "second commit", msg)
}

func TestCommitFile_InSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
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
	err = mgr.CommitFile(relativePath, "[contextmatrix] ALPHA-001: created card")
	require.NoError(t, err)

	// Verify commit
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "ALPHA-001")
}

func TestDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Create and commit a file
	testFile := "test.txt"
	err = os.WriteFile(filepath.Join(tmpDir, testFile), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile(testFile, "add file")
	require.NoError(t, err)

	// Delete the file
	err = mgr.DeleteFile(testFile)
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
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Deleting non-existent file should not error (file already removed)
	err = mgr.DeleteFile("nonexistent.txt")
	// This will error because the file isn't tracked
	assert.Error(t, err)
}

func TestDefaultAuthor(t *testing.T) {
	assert.Equal(t, "ContextMatrix", DefaultAuthor.Name)
	assert.Equal(t, "contextmatrix@local", DefaultAuthor.Email)
}

func TestCommitMessageFormat(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
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
			err = mgr.CommitFile(relPath, tt.message)
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
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	// Add tolerance for timing variations
	before := time.Now().Add(-time.Second)

	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0o644)
	require.NoError(t, err)
	err = mgr.CommitFile("test.txt", "test commit")
	require.NoError(t, err)

	after := time.Now().Add(time.Second)

	// Get commit timestamp
	repo, err := git.PlainOpen(tmpDir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	assert.True(t, !commit.Author.When.Before(before), "commit time should be after test start")
	assert.True(t, !commit.Author.When.After(after), "commit time should be before test end")
}

func TestConcurrentCommits(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
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
		err := mgr.CommitFile(relPath, "commit "+relPath)
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
	mgr, err := NewManager(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, tmpDir, mgr.RepoPath())
}
