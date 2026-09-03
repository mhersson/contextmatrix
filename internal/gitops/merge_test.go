package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRun executes a command for test setup and fails the test on a non-zero
// exit. Git invocations get a hermetic identity plus disabled signing so the
// suite does not depend on the developer's global git config.
func gitRun(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()

	if name == "git" {
		args = append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
	}

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))

	return string(out)
}

// initClonePair creates a bare upstream plus a clone managed by a Manager,
// seeds a.txt with "base\n", and pushes it. Returns the manager and both
// directory paths.
func initClonePair(t *testing.T) (mgr *Manager, upstream, clone string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found, skipping")
	}

	root := t.TempDir()
	upstream = filepath.Join(root, "upstream.git")
	clone = filepath.Join(root, "clone")

	gitRun(t, "", "git", "init", "--bare", "--initial-branch=main", upstream)
	gitRun(t, "", "git", "clone", upstream, clone)

	mgr, err := NewManager(clone, "", "test", staticTestProvider(t))
	require.NoError(t, err)
	mgr.SetAuthor("Test User", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(clone, "a.txt"), []byte("base\n"), 0o644))
	require.NoError(t, mgr.CommitFilesShell(context.Background(), []string{"a.txt"}, "base"))
	gitRun(t, clone, "git", "push", "origin", "HEAD")

	return mgr, upstream, clone
}

// makeConflict pushes a remote edit of a.txt from a second clone and commits
// a different local edit, then fetches. Returns the manager and clone path.
func makeConflict(t *testing.T) (*Manager, string) {
	t.Helper()

	mgr, upstream, clone := initClonePair(t)

	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, "", "git", "clone", upstream, other)
	require.NoError(t, os.WriteFile(filepath.Join(other, "a.txt"), []byte("theirs\n"), 0o644))
	gitRun(t, other, "git", "commit", "-am", "theirs")
	gitRun(t, other, "git", "push", "origin", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(clone, "a.txt"), []byte("ours\n"), 0o644))
	require.NoError(t, mgr.CommitFilesShell(context.Background(), []string{"a.txt"}, "ours"))
	gitRun(t, clone, "git", "fetch", "origin")

	return mgr, clone
}

func TestMerge_ConflictAndResolve(t *testing.T) {
	mgr, clone := makeConflict(t)
	ctx := context.Background()

	branch, err := mgr.CurrentBranch()
	require.NoError(t, err)

	remote := "origin/" + branch

	require.Error(t, mgr.MergeFastForward(ctx, remote))
	require.ErrorIs(t, mgr.Merge(ctx, remote), ErrMergeConflict)
	assert.True(t, mgr.MergeInProgress())

	paths, err := mgr.UnmergedPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []UnmergedPath{{Path: "a.txt", HasBase: true, HasOurs: true, HasTheirs: true}}, paths)

	theirs, err := mgr.ShowStage(ctx, 3, "a.txt")
	require.NoError(t, err)
	assert.Equal(t, "theirs\n", string(theirs))

	missing, err := mgr.ShowStage(ctx, 1, "nope.txt")
	require.NoError(t, err)
	assert.Nil(t, missing)

	require.NoError(t, os.WriteFile(filepath.Join(clone, "a.txt"), []byte("merged\n"), 0o644))
	require.NoError(t, mgr.StagePaths(ctx, []string{"a.txt"}))
	require.NoError(t, mgr.CommitMerge(ctx, "merge"))
	assert.False(t, mgr.MergeInProgress())

	clean, dirty, err := mgr.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean, dirty)

	ahead, err := mgr.AheadCount(ctx, branch)
	require.NoError(t, err)
	assert.Equal(t, 2, ahead)

	short, err := mgr.RevParseShort(ctx, "HEAD")
	require.NoError(t, err)
	assert.NotEmpty(t, short)
	assert.NotContains(t, short, "\n")
}

// TestShowStage_AbsentStageUnderForeignLocale pins the locale plumbing.
// An absent merge stage is recognised from git's own error text, and git
// translates that text, so a server started from a localized desktop session
// would fail every add/add and modify/delete resolution. Every git child runs
// with the message locale pinned instead, whatever the process environment
// says.
func TestShowStage_AbsentStageUnderForeignLocale(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LANGUAGE", "de:fr")

	mgr, _ := makeConflict(t)
	ctx := context.Background()

	branch, err := mgr.CurrentBranch()
	require.NoError(t, err)

	require.ErrorIs(t, mgr.Merge(ctx, "origin/"+branch), ErrMergeConflict)

	// The child process sees the pinned locale, not the caller's.
	seen, err := mgr.runGitOutput(ctx, "-c", "alias.showlocale=!printenv LC_ALL LANGUAGE", "showlocale")
	require.NoError(t, err)
	assert.Equal(t, []string{"C", ""}, strings.Split(strings.TrimSuffix(seen, "\n"), "\n"))

	// Stage 1 of an untracked path and stage 2 of a path only the remote has
	// are both absent, and both must read as absent rather than as a failure.
	missing, err := mgr.ShowStage(ctx, 1, "nope.txt")
	require.NoError(t, err)
	assert.Nil(t, missing)

	theirs, err := mgr.ShowStage(ctx, 3, "a.txt")
	require.NoError(t, err)
	assert.Equal(t, "theirs\n", string(theirs))
}

func TestMerge_AbortRestoresCleanTree(t *testing.T) {
	mgr, _ := makeConflict(t)
	ctx := context.Background()

	branch, err := mgr.CurrentBranch()
	require.NoError(t, err)

	require.ErrorIs(t, mgr.Merge(ctx, "origin/"+branch), ErrMergeConflict)
	require.NoError(t, mgr.MergeAbort(ctx))
	assert.False(t, mgr.MergeInProgress())

	clean, _, err := mgr.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean)
}

// TestMerge_FastForwardAndDiff covers the non-conflicting path: the local
// clone is strictly behind, so MergeFastForward succeeds and the diff between
// the merge base and the remote tip names the changed file.
func TestMerge_FastForwardAndDiff(t *testing.T) {
	mgr, upstream, clone := initClonePair(t)
	ctx := context.Background()

	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, "", "git", "clone", upstream, other)
	require.NoError(t, os.WriteFile(filepath.Join(other, "b.txt"), []byte("added\n"), 0o644))
	gitRun(t, other, "git", "add", "b.txt")
	gitRun(t, other, "git", "commit", "-m", "add b")
	gitRun(t, other, "git", "push", "origin", "HEAD")
	gitRun(t, clone, "git", "fetch", "origin")

	branch, err := mgr.CurrentBranch()
	require.NoError(t, err)

	remote := "origin/" + branch

	base, err := mgr.MergeBase(ctx, remote)
	require.NoError(t, err)
	require.NotEmpty(t, base)

	names, err := mgr.DiffNames(ctx, base, remote)
	require.NoError(t, err)
	assert.Equal(t, []string{"b.txt"}, names)

	require.NoError(t, mgr.MergeFastForward(ctx, remote))

	ahead, err := mgr.AheadCount(ctx, branch)
	require.NoError(t, err)
	assert.Equal(t, 0, ahead)

	// go-git must see the fast-forwarded tip, proving reloadRepo ran.
	msg, err := mgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "add b")
}

// TestRemovePaths_UnstagesAndDeletes covers the delete-side primitive the
// merge resolver uses when a card is removed upstream, including the
// tolerated case where the worktree file is already gone.
func TestRemovePaths_UnstagesAndDeletes(t *testing.T) {
	mgr, _, clone := initClonePair(t)
	ctx := context.Background()

	require.NoError(t, mgr.RemovePaths(ctx, []string{"a.txt"}))
	assert.NoFileExists(t, filepath.Join(clone, "a.txt"))

	out := gitRun(t, clone, "git", "ls-files", "a.txt")
	assert.Empty(t, out, "a.txt must be gone from the index")

	// Removing a path that no longer exists is a no-op, not an error.
	require.NoError(t, mgr.RemovePaths(ctx, []string{"a.txt"}))
	require.NoError(t, mgr.RemovePaths(ctx, nil))
}

func TestIsClean_ReportsDirtyPaths(t *testing.T) {
	mgr, _, clone := initClonePair(t)
	ctx := context.Background()

	clean, dirty, err := mgr.IsClean(ctx)
	require.NoError(t, err)
	require.True(t, clean)
	assert.Empty(t, dirty)

	require.NoError(t, os.WriteFile(filepath.Join(clone, "untracked.txt"), []byte("x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clone, "a.txt"), []byte("changed\n"), 0o644))

	clean, dirty, err = mgr.IsClean(ctx)
	require.NoError(t, err)
	assert.False(t, clean)
	assert.ElementsMatch(t, []string{"a.txt", "untracked.txt"}, dirty)
}

func TestMergeFileText(t *testing.T) {
	merged, clean, err := MergeFileText(context.Background(), "a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n")
	require.NoError(t, err)
	assert.True(t, clean)
	assert.Equal(t, "A\nb\nC\n", merged)

	_, clean, err = MergeFileText(context.Background(), "a\n", "b\n", "c\n")
	require.NoError(t, err)
	assert.False(t, clean)
}
