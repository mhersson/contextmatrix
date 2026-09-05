package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func wireProject(t *testing.T, dir, name, prefix string) {
	t.Helper()

	projectDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, &board.ProjectConfig{
		Name: name, Prefix: prefix, NextID: 1,
		States: []string{"todo", "in_progress", "done", "stalled", "not_planned"}, Types: []string{"task"}, Priorities: []string{"low"},
		Transitions: map[string][]string{
			"todo": {"in_progress"}, "in_progress": {"done", "todo"}, "done": {"todo"},
			"stalled": {"todo", "in_progress"}, "not_planned": {"todo"},
		},
	}))
}

func wireEntry(name, dir string) config.BoardsConfig {
	return config.BoardsConfig{Name: name, Dir: dir, GitAutoCommit: true, GitPullInterval: "60s", LeaseInterval: "5m", LeaseTimeout: "1h"}
}

func wireProvider(t *testing.T) githubauth.TokenGenerator {
	t.Helper()

	p, err := githubauth.NewPATProvider("x")
	require.NoError(t, err)

	return p
}

func TestBuildBoards_TwoPrivateRepos(t *testing.T) {
	one := t.TempDir()
	two := t.TempDir()
	wireProject(t, one, "alpha", "ALPHA")
	wireProject(t, two, "beta", "BETA")

	cfg := &config.Config{Boards: config.Boards{wireEntry("one", one), wireEntry("two", two)}}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	defer func() {
		for _, q := range boards.queues() {
			_ = q.Close(t.Context())
		}
	}()

	assert.Equal(t, []string{"one", "two"}, boards.composite.RepoNames())
	require.Len(t, boards.svcRepos, 2)
	assert.Equal(t, "one", boards.svcRepos[0].Name)
	assert.Empty(t, boards.svcRepos[0].Instance)
	assert.NotNil(t, boards.svcRepos[0].Lock)
	assert.NotNil(t, boards.svcRepos[1].Queue)
	assert.NotNil(t, boards.playbooks)
	assert.Empty(t, boards.playbooksDisabledBy)
	require.Len(t, boards.pbRepos, 2)
	assert.Equal(t, "two", boards.pbRepos[1].Name)

	repo, ok := boards.composite.RepoOf("beta")
	require.True(t, ok)
	assert.Equal(t, "two", repo)
}

func TestBuildBoards_DuplicateProjectIsFatal(t *testing.T) {
	one := t.TempDir()
	two := t.TempDir()
	wireProject(t, one, "alpha", "ALPHA")
	wireProject(t, two, "alpha", "ALPHA")

	cfg := &config.Config{Boards: config.Boards{wireEntry("one", one), wireEntry("two", two)}}

	_, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.ErrorContains(t, err, `project "alpha"`)
	require.ErrorContains(t, err, "one")
	require.ErrorContains(t, err, "two")
}

func TestBuildBoards_SharedWithoutOriginIsFatal(t *testing.T) {
	dir := t.TempDir()
	wireProject(t, dir, "alpha", "ALPHA")

	entry := wireEntry("team", dir)
	entry.Shared = true
	entry.GitRemoteURL = "https://example.com/org/boards.git"
	entry.GitAutoPull, entry.GitAutoPush = true, true

	cfg := &config.Config{Boards: config.Boards{entry}, Instance: config.InstanceConfig{ID: "lap-a"}}

	_, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.ErrorContains(t, err, "origin")
	require.ErrorContains(t, err, "team")
}

func TestBuildBoards_PlaybooksDisabledByAProjectNamedPlaybooks(t *testing.T) {
	one := t.TempDir()
	two := t.TempDir()
	wireProject(t, one, "alpha", "ALPHA")
	wireProject(t, two, "playbooks", "PB")

	cfg := &config.Config{Boards: config.Boards{wireEntry("one", one), wireEntry("two", two)}}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	defer func() {
		for _, q := range boards.queues() {
			_ = q.Close(t.Context())
		}
	}()

	assert.Nil(t, boards.playbooks)
	assert.Equal(t, "two", boards.playbooksDisabledBy)
}

func TestBuildBoards_SharedRepoGetsAnImageIndexPrivateDoesNot(t *testing.T) {
	shared := wireGitSyncUpstream(t)
	private := t.TempDir()
	wireProject(t, private, "beta", "BETA")

	// One image already on disk in the shared clone, so the startup reload
	// has something to find.
	imagesDir := filepath.Join(shared, "alpha", "images")
	require.NoError(t, os.MkdirAll(imagesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(imagesDir, "aabbccddeeff0011.png"), []byte("x"), 0o644))

	sharedEntry := wireEntry("team", shared)
	sharedEntry.Shared = true
	sharedEntry.GitAutoPull, sharedEntry.GitAutoPush = true, true

	cfg := &config.Config{
		Boards:   config.Boards{wireEntry("private", private), sharedEntry},
		Instance: config.InstanceConfig{ID: "lap-a"},
	}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	defer func() {
		for _, q := range boards.queues() {
			_ = q.Close(t.Context())
		}
	}()

	assert.Nil(t, boards.repos[0].images, "a private repo has no image index")
	require.NotNil(t, boards.repos[1].images)
	assert.Equal(t, "team", boards.repos[1].images.Name())
	assert.True(t, boards.repos[1].images.Has("alpha", "aabbccddeeff0011"), "the index is loaded from disk at startup")

	idxs := boards.imageIndexes()
	require.Len(t, idxs, 1)
	assert.Same(t, boards.repos[1].images, idxs[0])
}

// wireDirtyRepo builds a boards repo with one committed card, then rewrites
// the card on disk without committing - the footprint a previous process
// leaves when it parked a mutation in a deferred batch that never flushed.
func wireDirtyRepo(t *testing.T) (string, *gitops.Manager) {
	t.Helper()

	dir := t.TempDir()
	wireProject(t, dir, "alpha", "ALPHA")

	ctx := t.Context()

	git, err := gitops.NewManager(dir, "", "x", wireProvider(t))
	require.NoError(t, err)

	store, err := storage.NewFilesystemStore(dir)
	require.NoError(t, err)

	card := &board.Card{ID: "ALPHA-1", Project: "alpha", Title: "one", Type: "task", State: "todo", Priority: "low"}
	require.NoError(t, store.CreateCard(ctx, "alpha", card))
	require.NoError(t, git.CommitAll(ctx, "seed"))

	card.Title = "renamed but never committed"
	require.NoError(t, store.UpdateCard(ctx, "alpha", card))

	clean, _, err := git.IsClean(ctx)
	require.NoError(t, err)
	require.False(t, clean, "precondition: the rewrite must leave the tree dirty")

	return dir, git
}

func TestBuildBoards_CommitsLeftoverChangesAtStartup(t *testing.T) {
	dir, git := wireDirtyRepo(t)

	cfg := &config.Config{Boards: config.Boards{wireEntry("one", dir)}}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	defer func() {
		for _, q := range boards.queues() {
			_ = q.Close(t.Context())
		}
	}()

	clean, dirty, err := git.IsClean(t.Context())
	require.NoError(t, err)
	assert.True(t, clean, "startup must commit leftovers: %v", dirty)

	msg, err := git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "[contextmatrix] recover uncommitted changes", strings.TrimSpace(msg))

	committed := wireGitOut(t, dir, "show", "HEAD:alpha/tasks/ALPHA-1.md")
	assert.Contains(t, committed, "renamed but never committed", "the swept commit must carry the edit")
}

func TestBuildBoards_LeavesLeftoversWhenAutoCommitOff(t *testing.T) {
	dir, git := wireDirtyRepo(t)

	entry := wireEntry("one", dir)
	entry.GitAutoCommit = false
	cfg := &config.Config{Boards: config.Boards{entry}}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	defer func() {
		for _, q := range boards.queues() {
			_ = q.Close(t.Context())
		}
	}()

	clean, _, err := git.IsClean(t.Context())
	require.NoError(t, err)
	assert.False(t, clean, "no auto-commit means the tree is not ours to commit")

	msg, err := git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "seed", strings.TrimSpace(msg))
}
