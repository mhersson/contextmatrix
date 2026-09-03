package gitsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedNode is one ContextMatrix instance: its own clone of the upstream
// board repository plus the store, service and shared syncer wired on top.
type sharedNode struct {
	syncer *Syncer
	store  *storage.FilesystemStore
	svc    *service.CardService
	git    *gitops.Manager
	bus    *events.Bus
	dir    string
}

// setupSharedPair builds one bare upstream and two independent clones, each
// with its own store, service and shared syncer. Cards created through
// svcA are visible to svcB only after both have run Synced.
func setupSharedPair(t *testing.T) (a, b *sharedNode, upstream string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}

	upstream = filepath.Join(t.TempDir(), "upstream.git")
	run(t, "", "git", "init", "--bare", "-b", "main", upstream)

	seed := filepath.Join(t.TempDir(), "seed")
	run(t, "", "git", "clone", upstream, seed)
	run(t, seed, "git", "config", "user.email", "t@t")
	run(t, seed, "git", "config", "user.name", "t")

	require.NoError(t, os.MkdirAll(filepath.Join(seed, "test-project", "tasks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(seed, "test-project", "tasks", ".gitkeep"), nil, 0o644))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(seed, "test-project"), testProjectConfig()))

	run(t, seed, "git", "add", "-A")
	run(t, seed, "git", "commit", "-m", "initial")
	run(t, seed, "git", "push", "origin", "HEAD:main")

	return newSharedNode(t, upstream, "lap-a"), newSharedNode(t, upstream, "lap-b"), upstream
}

func testProjectConfig() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

func newSharedNode(t *testing.T, upstream, instance string) *sharedNode {
	t.Helper()

	dir := filepath.Join(t.TempDir(), instance)
	run(t, "", "git", "clone", upstream, dir)

	gitMgr, err := gitops.NewManager(dir, "", instance, gitopsTestProvider(t))
	require.NoError(t, err)
	gitMgr.SetAuthor("ContextMatrix", "contextmatrix@"+instance)

	store, err := storage.NewFilesystemStore(dir)
	require.NoError(t, err)

	bus := events.NewBus()
	svc := service.NewCardService(store, gitMgr, lock.NewManager(store, 30*time.Minute), bus, dir, nil, true, false)
	svc.SetSharedRepo(true)

	queue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(queue)
	t.Cleanup(func() { _ = queue.Close(context.Background()) })

	syncer := NewSyncer(gitMgr, store, svc, bus, dir, true, true, time.Minute,
		WithShared(instance), WithSyncTimeout(5*time.Second))
	require.NotNil(t, syncer)

	return &sharedNode{syncer: syncer, store: store, svc: svc, git: gitMgr, bus: bus, dir: dir}
}

func (n *sharedNode) create(t *testing.T, title string) *board.Card {
	t.Helper()

	c, err := n.svc.CreateCard(context.Background(), "test-project",
		service.CreateCardInput{Title: title, Type: "task", Priority: "medium"})
	require.NoError(t, err)

	return c
}

func (n *sharedNode) sync(t *testing.T) SyncReport {
	t.Helper()

	r, err := n.syncer.Synced(context.Background(), "test", nil)
	require.NoError(t, err)

	return r
}

func TestSynced_PushesAndPullsWithoutConflict(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "from a")

	ra := a.sync(t)
	assert.True(t, ra.Pushed)

	rb := b.sync(t)
	assert.True(t, rb.ChangesPulled)

	cards, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, "from a", cards[0].Title)

	st := b.syncer.Status()
	assert.True(t, st.Shared)
	require.NotNil(t, st.RemoteReachable)
	assert.True(t, *st.RemoteReachable)
	assert.Equal(t, 0, st.UnpushedCommits)
}

func TestSynced_DirtyTreeIsCommittedNotStashed(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	// Hand edit on a, left uncommitted.
	p := filepath.Join(a.dir, "test-project", "tasks", c.ID+".md")

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(data, []byte("hand edit\n")...), 0o644))

	a.sync(t)

	out := run(t, a.dir, "git", "log", "--oneline", "-1")
	assert.Contains(t, out, "external edit")

	clean, _, err := a.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)

	b.sync(t)

	got, err := b.store.GetCard(context.Background(), "test-project", c.ID)
	require.NoError(t, err)
	assert.Contains(t, got.Body, "hand edit")
}

func TestSynced_NonFastForwardRetriesThenPushes(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "a1")
	a.sync(t)
	b.sync(t)

	a.create(t, "a2") // a is ahead of the remote after this push
	a.sync(t)

	// b edits a different file (its own copy of the first card) while behind,
	// so the merge is clean and the first push is rejected as non-fast-forward.
	first, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, first, 1)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", first[0].ID,
		service.UpdateCardInput{Title: first[0].Title, Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	r := b.sync(t)
	assert.True(t, r.Pushed)
	assert.True(t, r.ChangesPulled)
	assert.Empty(t, r.Resolutions)

	a.sync(t)

	ca, err := a.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	cb, err := b.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)

	assert.Len(t, ca, 2)
	assert.Len(t, cb, 2)

	edited, err := a.store.GetCard(context.Background(), "test-project", first[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "high", edited.Priority)
}

func TestSynced_RemoteUnreachable(t *testing.T) {
	a, _, upstream := setupSharedPair(t)

	require.NoError(t, os.RemoveAll(upstream))

	_, err := a.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	st := a.syncer.Status()
	require.NotNil(t, st.RemoteReachable)
	assert.False(t, *st.RemoteReachable)
	assert.NotEmpty(t, st.LastRemoteError)
}

func TestSynced_ConflictAbortsCleanlyWithoutResolver(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	a.sync(t)
	b.sync(t)

	_, err := a.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "A", Type: "task", State: "todo", Priority: "high"})
	require.NoError(t, err)

	a.sync(t)

	_, err = b.svc.UpdateCard(context.Background(), "test-project", c.ID,
		service.UpdateCardInput{Title: "B", Type: "task", State: "todo", Priority: "low"})
	require.NoError(t, err)

	// The sentinel proves the merge really conflicted and reached the hook,
	// rather than the cycle failing somewhere earlier.
	errNoResolver := errors.New("no resolver yet")
	b.syncer.resolveHook = func(context.Context, string, []string) ([]boardmerge.Resolution, error) {
		return nil, errNoResolver
	}

	_, err = b.syncer.Synced(context.Background(), "test", nil)
	require.ErrorIs(t, err, errNoResolver)
	assert.False(t, b.git.MergeInProgress())

	clean, _, err := b.git.IsClean(context.Background())
	require.NoError(t, err)
	assert.True(t, clean)
}
