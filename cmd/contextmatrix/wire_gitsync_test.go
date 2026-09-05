package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
)

// wireGitRun runs a git command in dir and fails the test on error.
func wireGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()

	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// wireGitSyncUpstream creates an upstream bare repo and a clone of it with
// one project, so a boards entry passes the HasRemote gate.
func wireGitSyncUpstream(t *testing.T) (dir string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	upstream := filepath.Join(t.TempDir(), "upstream.git")
	wireGitRun(t, "", "init", "--bare", upstream)

	dir = filepath.Join(t.TempDir(), "clone")
	wireGitRun(t, "", "clone", upstream, dir)

	wireProject(t, dir, "alpha", "ALPHA")

	wireGitRun(t, dir, "add", "-A")
	wireGitRun(t, dir, "commit", "-m", "initial")
	wireGitRun(t, dir, "push", "origin", "HEAD")

	return dir
}

// wireSvcNamed builds a CardService over the bundle's repo but renames it,
// so lookups by the config name fail.
func wireSvcNamed(t *testing.T, boards *boardsBundles, name string) *service.CardService {
	t.Helper()

	boards.svcRepos[0].Name = name

	svc, err := service.NewCardServiceRepos(boards.composite, events.NewBus(), nil, boards.svcRepos...)
	require.NoError(t, err)

	return svc
}

func wireGitSyncTest(t *testing.T, entry config.BoardsConfig) (*boardsBundles, *config.Config) {
	t.Helper()

	cfg := &config.Config{Boards: config.Boards{entry}, Instance: config.InstanceConfig{ID: "lap-a"}}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	t.Cleanup(func() {
		for _, q := range boards.queues() {
			_ = q.Close(context.Background())
		}
	})

	return boards, cfg
}

func TestWireGitSync_SharedWiringFailureIsFatal(t *testing.T) {
	dir := wireGitSyncUpstream(t)

	entry := wireEntry("team", dir)
	entry.Shared = true
	entry.GitRemoteURL = "https://example.com/org/boards.git"
	entry.GitAutoPull, entry.GitAutoPush = true, true

	boards, cfg := wireGitSyncTest(t, entry)

	// The service holds the repo under a different name than the shared
	// entry's config name, so SetSyncRunnerFor cannot resolve it.
	svc := wireSvcNamed(t, boards, "mismatch")

	ctx := t.Context()

	group, err := wireGitSync(ctx, cfg, boards, svc, nil, events.NewBus())
	require.ErrorContains(t, err, "team")
	require.ErrorIs(t, err, service.ErrUnknownBoardsRepo)
	assert.Nil(t, group)
}

func TestWireGitSync_PrivateWiringFailureContinues(t *testing.T) {
	dir := wireGitSyncUpstream(t)

	entry := wireEntry("solo", dir)
	entry.GitAutoPull, entry.GitAutoPush = true, true

	boards, cfg := wireGitSyncTest(t, entry)

	// The same name mismatch fails SetOnCommitFor on a private repo, but
	// that is only logged: wiring continues and the group starts.
	svc := wireSvcNamed(t, boards, "mismatch")

	ctx := t.Context()

	group, err := wireGitSync(ctx, cfg, boards, svc, nil, events.NewBus())
	require.NoError(t, err)
	require.NotNil(t, group)
	assert.True(t, group.Enabled())
}

// wireGitOut runs a git command in dir and returns its trimmed stdout.
func wireGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)

	return strings.TrimSpace(string(out))
}

// wireDirtyBoardYAML appends a YAML comment to alpha/.board.yaml so the clone
// carries an uncommitted edit that stays a valid project config.
func wireDirtyBoardYAML(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, "alpha", ".board.yaml")

	orig, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(orig, []byte("# left behind\n")...), 0o644))
}

// TestWireGitSync_PushesRecoveredCommit: the startup sweep commits before the
// syncer exists, so nothing fires the on-commit hook for it. Wiring must
// queue a push itself or the recovered commit stays local until the next
// card mutation - indefinitely on an idle instance.
func TestWireGitSync_PushesRecoveredCommit(t *testing.T) {
	dir := wireGitSyncUpstream(t)
	wireDirtyBoardYAML(t, dir)

	entry := wireEntry("solo", dir)
	entry.GitAutoPull, entry.GitAutoPush = true, true

	boards, cfg := wireGitSyncTest(t, entry)

	local := wireGitOut(t, dir, "rev-parse", "HEAD")
	require.NotEqual(t, "initial", wireGitOut(t, dir, "log", "-1", "--format=%s"), "precondition: sweep committed")

	svc, err := service.NewCardServiceRepos(boards.composite, events.NewBus(), nil, boards.svcRepos...)
	require.NoError(t, err)

	group, err := wireGitSync(t.Context(), cfg, boards, svc, nil, events.NewBus())
	require.NoError(t, err)
	require.NotNil(t, group)

	assert.Eventually(t, func() bool {
		return strings.HasPrefix(wireGitOut(t, dir, "ls-remote", "origin", "HEAD"), local)
	}, 10*time.Second, 100*time.Millisecond, "recovered commit must reach the remote")
}

// TestBuildBoards_SharedRepoLeavesLeftovers pins the shared exclusion: the
// shared sync cycle commits leftovers itself after clearing a stale merge,
// so startup must not touch a shared repo's dirty tree.
func TestBuildBoards_SharedRepoLeavesLeftovers(t *testing.T) {
	dir := wireGitSyncUpstream(t)
	wireDirtyBoardYAML(t, dir)

	entry := wireEntry("team", dir)
	entry.Shared = true
	entry.GitRemoteURL = "https://example.com/org/boards.git"
	entry.GitAutoPull, entry.GitAutoPush = true, true

	wireGitSyncTest(t, entry)

	assert.Equal(t, "initial", wireGitOut(t, dir, "log", "-1", "--format=%s"))
	assert.Contains(t, wireGitOut(t, dir, "status", "--porcelain"), ".board.yaml")
}
