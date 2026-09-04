package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func playbook(id string) *board.Playbook {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	return &board.Playbook{ID: id, Title: id, Created: now, Updated: now, NextEntryID: 1}
}

func twoPlaybookRepos(t *testing.T) (*PlaybookComposite, *FilesystemPlaybookStore, *FilesystemPlaybookStore) {
	t.Helper()

	one, err := NewFilesystemPlaybookStore(t.TempDir())
	require.NoError(t, err)

	two, err := NewFilesystemPlaybookStore(t.TempDir())
	require.NoError(t, err)

	c, err := NewPlaybookComposite(NamedPlaybookStore{Name: "one", Store: one}, NamedPlaybookStore{Name: "two", Store: two})
	require.NoError(t, err)

	return c, one, two
}

func TestPlaybookComposite_ListIgnoresEntriesTheOwnerTableDoesNotKnow(t *testing.T) {
	c, one, _ := twoPlaybookRepos(t)
	ctx := context.Background()

	require.NoError(t, c.CreateIn(ctx, "two", playbook("beta")))

	// "hand" lands in repo "one" (index 0) without the composite indexing
	// it: a missing owner key reads as 0 under the single-value form, so
	// this is the collision the two-value read must reject.
	require.NoError(t, one.Create(ctx, playbook("hand")))

	list, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "beta", list[0].ID)

	_, ok := c.RepoOf("hand")
	assert.False(t, ok)
}

func TestPlaybookComposite_RoutesByIDAndKeepsIDsUnique(t *testing.T) {
	c, one, two := twoPlaybookRepos(t)
	ctx := context.Background()

	require.NoError(t, c.CreateIn(ctx, "one", playbook("alpha")))
	require.NoError(t, c.CreateIn(ctx, "two", playbook("beta")))

	require.ErrorIs(t, c.CreateIn(ctx, "two", playbook("alpha")), ErrPlaybookExists, "an ID owned by another repo is taken")
	require.ErrorIs(t, c.CreateIn(ctx, "three", playbook("gamma")), ErrUnknownRepo)
	require.Error(t, c.Create(ctx, playbook("delta")), "the composite needs a target repo")

	repo, ok := c.RepoOf("beta")
	require.True(t, ok)
	assert.Equal(t, "two", repo)

	list, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].ID)
	assert.Equal(t, "beta", list[1].ID)

	got, err := c.Get(ctx, "beta")
	require.NoError(t, err)

	got.Title = "renamed"
	require.NoError(t, c.Save(ctx, got))

	fromChild, err := two.Get(ctx, "beta")
	require.NoError(t, err)
	assert.Equal(t, "renamed", fromChild.Title)

	_, err = one.Get(ctx, "beta")
	require.ErrorIs(t, err, ErrPlaybookNotFound)

	require.NoError(t, c.Delete(ctx, "beta"))
	_, ok = c.RepoOf("beta")
	assert.False(t, ok)
}

func TestPlaybookComposite_ReloadRepoRegistersAFileThatArrivedOnDisk(t *testing.T) {
	c, _, two := twoPlaybookRepos(t)
	ctx := context.Background()

	require.NoError(t, two.Create(ctx, playbook("hand")))

	_, ok := c.RepoOf("hand")
	assert.False(t, ok)

	require.NoError(t, c.ReloadRepo(ctx, "two"))

	repo, ok := c.RepoOf("hand")
	require.True(t, ok)
	assert.Equal(t, "two", repo)
}

func TestPlaybookComposite_DuplicateIDAcrossReposFirstRepoWins(t *testing.T) {
	c, one, two := twoPlaybookRepos(t)
	ctx := context.Background()

	require.NoError(t, one.Create(ctx, playbook("same")))
	require.NoError(t, two.Create(ctx, playbook("same")))
	require.NoError(t, c.ReloadIndex(ctx))

	list, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	repo, ok := c.RepoOf("same")
	require.True(t, ok)
	assert.Equal(t, "one", repo)
}
