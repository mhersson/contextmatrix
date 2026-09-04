package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
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
