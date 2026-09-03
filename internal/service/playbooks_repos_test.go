package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// RepoPlaybooks is handed to the gitsync syncer, whose playbookSync
// interface is unexported. Pin the shape it must keep.
var _ interface {
	LockWrites()
	UnlockWrites()
	Reload(ctx context.Context) error
} = (*RepoPlaybooks)(nil)

// newTwoRepoPlaybooksWith is newTwoRepoPlaybooks with the two PlaybookRepo
// bundles exposed, for tests that swap a repo's commit queue.
func newTwoRepoPlaybooksWith(t *testing.T) (
	*PlaybookService, *twoRepos, *storage.PlaybookComposite, *PlaybookRepo, *PlaybookRepo, func(),
) {
	t.Helper()

	tr, cleanup := newTwoRepoService(t)

	pbOne, err := storage.NewFilesystemPlaybookStore(tr.one.Dir)
	require.NoError(t, err)

	pbTwo, err := storage.NewFilesystemPlaybookStore(tr.two.Dir)
	require.NoError(t, err)

	pbc, err := storage.NewPlaybookComposite(
		storage.NamedPlaybookStore{Name: "one", Store: pbOne},
		storage.NamedPlaybookStore{Name: "two", Store: pbTwo},
	)
	require.NoError(t, err)

	repoOne := &PlaybookRepo{Name: "one", Queue: tr.one.Queue, GitAutoCommit: true}
	repoTwo := &PlaybookRepo{Name: "two", Queue: tr.two.Queue, GitAutoCommit: true}

	pb, err := NewPlaybookServiceRepos(pbc, tr.composite, tr.bus, tr.clk, repoOne, repoTwo)
	require.NoError(t, err)

	return pb, tr, pbc, repoOne, repoTwo, cleanup
}

func newTwoRepoPlaybooks(t *testing.T) (*PlaybookService, *twoRepos, *storage.PlaybookComposite, func()) {
	t.Helper()

	pb, tr, pbc, _, _, cleanup := newTwoRepoPlaybooksWith(t)

	return pb, tr, pbc, cleanup
}

func TestPlaybooks_CreateTargetsRepoAndIDsStayUniqueAcrossRepos(t *testing.T) {
	pb, tr, _, cleanup := newTwoRepoPlaybooks(t)
	defer cleanup()

	ctx := context.Background()

	first, err := pb.Create(ctx, CreatePlaybookInput{Title: "Alpha", AgentID: "human:a", BoardsRepo: "one"})
	require.NoError(t, err)
	assert.Equal(t, "alpha", first.ID)
	assert.Equal(t, "one", first.BoardsRepo)
	assert.FileExists(t, filepath.Join(tr.one.Dir, "playbooks", "alpha.yaml"))

	msg, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "playbook(alpha): created", strings.TrimSpace(msg))

	second, err := pb.Create(ctx, CreatePlaybookInput{Title: "Alpha", AgentID: "human:a", BoardsRepo: "two"})
	require.NoError(t, err)
	assert.Equal(t, "alpha-2", second.ID, "IDs are unique across repos")
	assert.Equal(t, "two", second.BoardsRepo)
	assert.FileExists(t, filepath.Join(tr.two.Dir, "playbooks", "alpha-2.yaml"))
	assert.NoFileExists(t, filepath.Join(tr.one.Dir, "playbooks", "alpha-2.yaml"))

	third, err := pb.Create(ctx, CreatePlaybookInput{Title: "Default", AgentID: "human:a"})
	require.NoError(t, err)
	assert.Equal(t, "one", third.BoardsRepo, "empty boards_repo means the first configured repo")

	_, err = pb.Create(ctx, CreatePlaybookInput{Title: "Nope", AgentID: "human:a", BoardsRepo: "three"})
	require.ErrorIs(t, err, ErrUnknownBoardsRepo)

	list, err := pb.List(ctx)
	require.NoError(t, err)

	repos := map[string]string{}

	for _, s := range list {
		repos[s.ID] = s.BoardsRepo
	}

	assert.Equal(t, map[string]string{"alpha": "one", "alpha-2": "two", "default": "one"}, repos)
}

func TestPlaybooks_MutationsCommitInTheirRepo(t *testing.T) {
	pb, tr, _, cleanup := newTwoRepoPlaybooks(t)
	defer cleanup()

	ctx := context.Background()

	_, err := pb.Create(ctx, CreatePlaybookInput{Title: "In two", AgentID: "human:a", BoardsRepo: "two"})
	require.NoError(t, err)

	before, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)

	title := "Renamed"
	detail, err := pb.UpdateMeta(ctx, "in-two", UpdatePlaybookInput{Title: &title}, "human:a")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", detail.Title)
	assert.Equal(t, "two", detail.BoardsRepo)

	msg, err := tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "playbook(in-two)")

	unchanged, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, before, unchanged)

	require.NoError(t, pb.Delete(ctx, "in-two", "human:a"))
	assert.NoFileExists(t, filepath.Join(tr.two.Dir, "playbooks", "in-two.yaml"))

	msg, err = tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "playbook(in-two): deleted", strings.TrimSpace(msg),
		"the delete commits in the repo that owned the playbook")

	unchanged, err = tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, before, unchanged, "the other repo is untouched by the delete")
}

func TestPlaybooks_DeleteRollsBackIntoItsOwnRepoWhenTheCommitFails(t *testing.T) {
	pb, tr, pbc, _, repoTwo, cleanup := newTwoRepoPlaybooksWith(t)
	defer cleanup()

	ctx := context.Background()

	_, err := pb.Create(ctx, CreatePlaybookInput{Title: "In two", AgentID: "human:a", BoardsRepo: "two"})
	require.NoError(t, err)

	sentinel := errors.New("commit refused")
	failQueue := gitops.NewCommitQueueWithCommitter(&failingCommitter{err: sentinel}, 0)

	t.Cleanup(func() { _ = failQueue.Close(context.Background()) })

	repoTwo.Queue = failQueue

	require.ErrorIs(t, pb.Delete(ctx, "in-two", "human:a"), sentinel)

	detail, err := pb.Get(ctx, "in-two")
	require.NoError(t, err, "the failed delete is rolled back")
	assert.Equal(t, "two", detail.BoardsRepo, "the restored playbook stays in the repo that owned it")

	repo, ok := pbc.RepoOf("in-two")
	require.True(t, ok)
	assert.Equal(t, "two", repo)

	assert.FileExists(t, filepath.Join(tr.two.Dir, "playbooks", "in-two.yaml"))
	assert.NoFileExists(t, filepath.Join(tr.one.Dir, "playbooks", "in-two.yaml"))
}

func TestPlaybooks_ForRepoReloadsOneRepo(t *testing.T) {
	pb, tr, pbc, cleanup := newTwoRepoPlaybooks(t)
	defer cleanup()

	ctx := context.Background()

	childTwo, err := storage.NewFilesystemPlaybookStore(tr.two.Dir)
	require.NoError(t, err)
	require.NoError(t, childTwo.Create(ctx, boardPlaybook("hand")))

	_, err = pb.Get(ctx, "hand")
	require.Error(t, err, "not indexed until the repo reloads")

	adapter := pb.ForRepo("two")
	adapter.LockWrites()
	require.NoError(t, adapter.Reload(ctx))
	adapter.UnlockWrites()

	detail, err := pb.Get(ctx, "hand")
	require.NoError(t, err)
	assert.Equal(t, "two", detail.BoardsRepo)

	repo, ok := pbc.RepoOf("hand")
	require.True(t, ok)
	assert.Equal(t, "two", repo)
}

func boardPlaybook(id string) *board.Playbook {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	return &board.Playbook{ID: id, Title: id, Created: now, Updated: now, NextEntryID: 1}
}
