package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type twoRepos struct {
	svc       *CardService
	one, two  *BoardsRepo
	clk       *clock.FakeClock
	composite *storage.Composite
	bus       *events.Bus
}

func repoProject(name, prefix string) *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       name,
		Prefix:     prefix,
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

// repoFixture is one boards repo on disk with one project, before it is
// wired into a service.
type repoFixture struct {
	name  string
	dir   string
	git   *gitops.Manager
	store *storage.FilesystemStore
}

func newRepoFixture(t *testing.T, name, project, prefix string) *repoFixture {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, project, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(dir, project), repoProject(project, prefix)))

	git, err := gitops.NewManager(dir, "", name, gitopsTestProvider(t))
	require.NoError(t, err)

	// A repo without a root commit has no HEAD, and the git health check
	// reads the current branch off it.
	require.NoError(t, git.CommitAll(context.Background(), "init"))

	store, err := storage.NewFilesystemStore(dir)
	require.NoError(t, err)

	return &repoFixture{name: name, dir: dir, git: git, store: store}
}

// newTwoRepoService wires repo "one" (project alpha) and repo "two"
// (project beta), both private, on one composite and one fake clock.
func newTwoRepoService(t *testing.T) (*twoRepos, func()) {
	t.Helper()

	fOne := newRepoFixture(t, "one", "alpha", "ALPHA")
	fTwo := newRepoFixture(t, "two", "beta", "BETA")

	composite, err := storage.NewComposite(
		storage.NamedStore{Name: "one", Store: fOne.store},
		storage.NamedStore{Name: "two", Store: fTwo.store},
	)
	require.NoError(t, err)

	clk := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	bus := events.NewBus()

	build := func(f *repoFixture) *BoardsRepo {
		view, err := composite.View(f.name)
		require.NoError(t, err)

		return &BoardsRepo{
			Name:          f.name,
			Store:         view,
			Git:           f.git,
			Dir:           f.dir,
			GitAutoCommit: true,
			Lock:          lock.NewManagerWithClock(view, 30*time.Minute, clk),
			Queue:         gitops.NewCommitQueue(f.git, 0),
		}
	}

	one, two := build(fOne), build(fTwo)

	svc, err := NewCardServiceRepos(composite, bus, nil, one, two)
	require.NoError(t, err)

	cleanup := func() {
		_ = one.Queue.Close(context.Background())
		_ = two.Queue.Close(context.Background())
	}

	return &twoRepos{svc: svc, one: one, two: two, clk: clk, composite: composite, bus: bus}, cleanup
}

func TestRepoOf_RoutesByProject(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	assert.Same(t, tr.one, tr.svc.repoOf("alpha"))
	assert.Same(t, tr.two, tr.svc.repoOf("beta"))
	assert.Same(t, tr.one, tr.svc.repoOf("nope"), "an unknown project resolves to the first repo so the store error is the one reported")
	assert.Equal(t, []*BoardsRepo{tr.one, tr.two}, tr.svc.Repos())
}

func TestRepoNamed(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	r, err := tr.svc.repoNamed("")
	require.NoError(t, err)
	assert.Same(t, tr.one, r, "empty means the first configured repo")

	r, err = tr.svc.repoNamed("two")
	require.NoError(t, err)
	assert.Same(t, tr.two, r)

	_, err = tr.svc.repoNamed("three")
	require.ErrorIs(t, err, ErrUnknownBoardsRepo)
	require.ErrorIs(t, err, storage.ErrInvalidInput)
	assert.ErrorContains(t, err, "three")
}

func TestNewCardServiceRepos_Rejects(t *testing.T) {
	f := newRepoFixture(t, "one", "alpha", "ALPHA")
	clkA := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	clkB := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	bus := events.NewBus()

	tests := []struct {
		name    string
		repos   []*BoardsRepo
		wantErr string
	}{
		{"none", nil, "at least one"},
		{"unnamed", []*BoardsRepo{{Lock: lock.NewManagerWithClock(f.store, time.Minute, clkA)}}, "needs a name"},
		{"no lock", []*BoardsRepo{{Name: "a"}}, "no lock manager"},
		{"duplicate", []*BoardsRepo{
			{Name: "a", Lock: lock.NewManagerWithClock(f.store, time.Minute, clkA)},
			{Name: "a", Lock: lock.NewManagerWithClock(f.store, time.Minute, clkA)},
		}, "duplicate"},
		{"clocks differ", []*BoardsRepo{
			{Name: "a", Lock: lock.NewManagerWithClock(f.store, time.Minute, clkA)},
			{Name: "b", Lock: lock.NewManagerWithClock(f.store, time.Minute, clkB)},
		}, "different clock"},
		{"timeouts differ", []*BoardsRepo{
			{Name: "a", Lock: lock.NewManagerWithClock(f.store, time.Minute, clkA)},
			{Name: "b", Lock: lock.NewManagerWithClock(f.store, 2*time.Minute, clkA)},
		}, "different heartbeat timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCardServiceRepos(f.store, bus, nil, tt.repos...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewCardService_IsAOneRepoServiceNamedBoards(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	repos := svc.Repos()
	require.Len(t, repos, 1)
	assert.Equal(t, DefaultRepoName, repos[0].Name)
	assert.NotNil(t, repos[0].Queue, "SetCommitQueue lands on the one repo")

	svc.SetSharedRepo(true)
	svc.SetLease("lap-a", time.Hour, time.Minute)
	assert.True(t, repos[0].Shared)
	assert.Equal(t, "lap-a", repos[0].Instance)
	assert.Equal(t, time.Hour, repos[0].LeaseTimeout)
	assert.Equal(t, time.Minute, repos[0].PullInterval)
}

func TestLockWrites_QuiescesOnlyThatRepoQueue(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	ctx := context.Background()
	job := func(project string) gitops.CommitJob {
		return gitops.CommitJob{Project: project, Kind: gitops.CommitKindFile, Path: project + "/.board.yaml", Message: "m", Ctx: ctx}
	}

	tr.svc.LockWrites("one")

	twoDone := tr.two.Queue.Enqueue(job("beta"))
	select {
	case err := <-twoDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("repo two's queue must keep running while repo one is locked")
	}

	oneDone := tr.one.Queue.Enqueue(job("alpha"))
	select {
	case <-oneDone:
		t.Fatal("repo one's queue must stay paused while its writes are locked")
	case <-time.After(300 * time.Millisecond):
	}

	tr.svc.UnlockWrites("one")

	select {
	case err := <-oneDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("repo one's queue must resume after unlock")
	}
}

func TestHealthCheck_ReportsEveryRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	results := tr.svc.HealthCheck(context.Background())

	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Name)
		assert.True(t, r.OK, r.Name)
	}

	assert.Equal(t, []string{"store", "git", "git:two", "session_log"}, names)
}

func TestBoardsRepo_PushVerifiedAndSharedClaims(t *testing.T) {
	run := SyncRunner(func(context.Context, string, SyncMutation) (SyncOutcome, error) {
		return SyncOutcome{}, nil
	})

	tests := []struct {
		name         string
		repo         *BoardsRepo
		pushVerified bool
		sharedClaims bool
	}{
		{"private", &BoardsRepo{}, false, false},
		{"shared without a runner", &BoardsRepo{Shared: true, Instance: "lap-a"}, false, true},
		{"runner without shared", &BoardsRepo{runner: run}, false, false},
		{"shared with a runner", &BoardsRepo{Shared: true, Instance: "lap-a", runner: run}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.pushVerified, tt.repo.pushVerified())
			assert.Equal(t, tt.sharedClaims, tt.repo.sharedClaims())
		})
	}
}

func TestBoardsRepo_RecentlySynced(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	r := &BoardsRepo{PullInterval: time.Minute}

	assert.False(t, r.recentlySynced(now), "never synced")

	r.markSynced(now)
	assert.True(t, r.recentlySynced(now.Add(2*time.Minute)), "twice the pull interval still counts")
	assert.False(t, r.recentlySynced(now.Add(2*time.Minute+time.Second)))
}

func TestBoardsRepo_NotifyCommit(t *testing.T) {
	r := &BoardsRepo{}
	r.notifyCommit() // no hook is a no-op

	calls := 0
	r.onCommit = func() { calls++ }

	r.notifyCommit()
	assert.Equal(t, 1, calls)
}

func TestInstanceFor_IsPerRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	tr.one.Instance = "lap-a"

	assert.Equal(t, "lap-a", tr.svc.instanceFor("alpha"))
	assert.Empty(t, tr.svc.instanceFor("beta"), "a private repo beside a shared one keeps agent-ID ownership")
}

func TestSaveNewProject_CreatesInTheNamedRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, tr.svc.saveNewProject(ctx, tr.two, repoProject("gamma", "GAMMA")))

	assert.Same(t, tr.two, tr.svc.repoOf("gamma"), "the new project routes to the repo it was created in")

	owner, ok := tr.composite.RepoOf("gamma")
	require.True(t, ok)
	assert.Equal(t, "two", owner)
}

func TestTwoRepos_CardCommitsLandInTheirOwnRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	ctx := context.Background()

	a, err := tr.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "in one", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	b, err := tr.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "in two", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	assert.Equal(t, "ALPHA-001", a.ID)
	assert.Equal(t, "BETA-001", b.ID)

	msgOne, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msgOne, "ALPHA-001")

	msgTwo, err := tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msgTwo, "BETA-001")

	for _, r := range tr.svc.Repos() {
		dirty, err := r.Git.HasUncommittedChanges()
		require.NoError(t, err)
		assert.False(t, dirty, r.Name)
	}

	assert.FileExists(t, filepath.Join(tr.two.Dir, "beta", "tasks", "BETA-001.md"))
	assert.NoFileExists(t, filepath.Join(tr.one.Dir, "beta", "tasks", "BETA-001.md"))
}

func TestTwoRepos_DeferredFlushCommitsInTheCardsRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	tr.two.GitDeferredCommit = true
	ctx := context.Background()

	_, err := tr.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "untouched", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	before, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)

	card, err := tr.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "deferred", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	_, err = tr.svc.ClaimCard(ctx, "beta", card.ID, "agent-1")
	require.NoError(t, err)

	_, err = tr.svc.AddLogEntry(ctx, "beta", card.ID, board.ActivityEntry{Agent: "agent-1", Action: "progress", Message: "working"})
	require.NoError(t, err)

	_, err = tr.svc.ReleaseCard(ctx, "beta", card.ID, "agent-1")
	require.NoError(t, err)

	after, err := tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, after, "deferred commit")

	unchanged, err := tr.one.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, before, unchanged, "repo one saw none of repo two's deferred work")
}

func TestTwoRepos_ProjectCreateTargetsTheNamedRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	ctx := context.Background()
	input := CreateProjectInput{
		Name: "gamma", Prefix: "GAMMA", BoardsRepo: "two",
		States: repoProject("gamma", "GAMMA").States, Types: []string{"task"}, Priorities: []string{"low"},
		Transitions: repoProject("gamma", "GAMMA").Transitions,
	}

	cfg, err := tr.svc.CreateProject(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "two", cfg.BoardsRepo)
	assert.DirExists(t, filepath.Join(tr.two.Dir, "gamma", "tasks"))
	assert.NoDirExists(t, filepath.Join(tr.one.Dir, "gamma"))
	assert.Same(t, tr.two, tr.svc.repoOf("gamma"))

	msg, err := tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "project created")

	input.Name, input.Prefix, input.BoardsRepo = "delta", "DELTA", "three"
	_, err = tr.svc.CreateProject(ctx, input)
	require.ErrorIs(t, err, ErrUnknownBoardsRepo)

	input.Name, input.Prefix, input.BoardsRepo = "eps", "EPS", ""
	cfg, err = tr.svc.CreateProject(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "one", cfg.BoardsRepo, "empty boards_repo means the first configured repo")

	input.Name, input.Prefix, input.BoardsRepo = "alpha", "ALPHA", "two"
	_, err = tr.svc.CreateProject(ctx, input)
	require.ErrorIs(t, err, storage.ErrProjectExists, "names are unique across repos")
}

func TestTwoRepos_DeleteProjectCommitsInItsRepo(t *testing.T) {
	tr, cleanup := newTwoRepoService(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, tr.svc.DeleteProject(ctx, "beta"))
	assert.NoDirExists(t, filepath.Join(tr.two.Dir, "beta"))

	msg, err := tr.two.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "project deleted")

	_, ok := tr.composite.RepoOf("beta")
	assert.False(t, ok)
}
