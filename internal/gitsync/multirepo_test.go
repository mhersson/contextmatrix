package gitsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// multiNode is one instance serving a shared clone ("team", project
// test-project) next to a private repo ("private", project private-project)
// through one composite, one service, two lock managers on one clock and
// one playbook service. The private repo is listed first, so a name it
// owns wins over the same name arriving in the shared repo.
type multiNode struct {
	id        string
	svc       *service.CardService
	pb        *service.PlaybookService
	composite *storage.Composite
	group     *Group
	syncer    *Syncer
	team      *service.BoardsRepo
	private   *service.BoardsRepo
	teamDir   string
	privDir   string
	upstream  string
	clk       *clock.FakeClock
	bus       *events.Bus
}

func privateProjectConfig() *board.ProjectConfig {
	cfg := testProjectConfig()
	cfg.Name, cfg.Prefix = "private-project", "PRIV"

	return cfg
}

func setupMultiPair(t *testing.T) (a, b *multiNode) {
	t.Helper()

	_, _, upstream := setupSharedPair(t)

	return newMultiNode(t, upstream, "lap-a"), newMultiNode(t, upstream, "lap-b")
}

func newMultiNode(t *testing.T, upstream, instance string) *multiNode {
	t.Helper()

	teamDir := filepath.Join(t.TempDir(), instance+"-team")
	run(t, "", "git", "clone", upstream, teamDir)

	privDir := filepath.Join(t.TempDir(), instance+"-private")
	require.NoError(t, os.MkdirAll(filepath.Join(privDir, "private-project", "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(privDir, "private-project"), privateProjectConfig()))

	teamGit, err := gitops.NewManager(teamDir, "", instance+"-team", gitopsTestProvider(t))
	require.NoError(t, err)
	teamGit.SetAuthor("ContextMatrix", "contextmatrix@"+instance)

	privGit, err := gitops.NewManager(privDir, "", instance+"-private", gitopsTestProvider(t))
	require.NoError(t, err)

	teamStore, err := storage.NewFilesystemStore(teamDir)
	require.NoError(t, err)

	privStore, err := storage.NewFilesystemStore(privDir)
	require.NoError(t, err)

	composite, err := storage.NewComposite(
		storage.NamedStore{Name: "private", Store: privStore},
		storage.NamedStore{Name: "team", Store: teamStore},
	)
	require.NoError(t, err)

	bus := events.NewBus()
	clk := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))

	teamView, err := composite.View("team")
	require.NoError(t, err)

	privView, err := composite.View("private")
	require.NoError(t, err)

	teamLock := lock.NewManagerWithClock(teamView, 30*time.Minute, clk)
	teamLock.SetShared(instance, 5*time.Minute, time.Hour)

	teamQueue := gitops.NewCommitQueue(teamGit, 0)
	privQueue := gitops.NewCommitQueue(privGit, 0)

	t.Cleanup(func() {
		_ = teamQueue.Close(context.Background())
		_ = privQueue.Close(context.Background())
	})

	team := &service.BoardsRepo{
		Name: "team", Store: teamView, Git: teamGit, Dir: teamDir, GitAutoCommit: true, Shared: true,
		Lock: teamLock, Queue: teamQueue, Instance: instance, LeaseTimeout: time.Hour, PullInterval: time.Minute,
	}
	private := &service.BoardsRepo{
		Name: "private", Store: privView, Git: privGit, Dir: privDir, GitAutoCommit: true,
		Lock: lock.NewManagerWithClock(privView, 30*time.Minute, clk), Queue: privQueue,
	}

	svc, err := service.NewCardServiceRepos(composite, bus, nil, private, team)
	require.NoError(t, err)

	syncer := NewSyncer(teamGit, teamView, svc, bus, teamDir, true, true, time.Minute,
		WithRepo("team"), WithShared(instance), WithSyncTimeout(5*time.Second), WithLeaseInterval(5*time.Minute))
	require.NotNil(t, syncer)

	runner := func(ctx context.Context, trigger string, m service.SyncMutation) (service.SyncOutcome, error) {
		r, err := syncer.SyncedMutation(ctx, trigger, m)

		return service.SyncOutcome{BodyRan: r.BodyRan, Pushed: r.Pushed, Resolutions: r.Resolutions}, err
	}
	require.NoError(t, svc.SetSyncRunnerFor("team", runner))

	pbTeam, err := storage.NewFilesystemPlaybookStore(teamDir)
	require.NoError(t, err)

	pbPriv, err := storage.NewFilesystemPlaybookStore(privDir)
	require.NoError(t, err)

	pbc, err := storage.NewPlaybookComposite(
		storage.NamedPlaybookStore{Name: "private", Store: pbPriv},
		storage.NamedPlaybookStore{Name: "team", Store: pbTeam},
	)
	require.NoError(t, err)

	pb, err := service.NewPlaybookServiceRepos(pbc, composite, bus, clk,
		&service.PlaybookRepo{Name: "private", Queue: privQueue, GitAutoCommit: true},
		&service.PlaybookRepo{Name: "team", Queue: teamQueue, GitAutoCommit: true},
	)
	require.NoError(t, err)
	require.NoError(t, pb.SetSyncRunnerFor("team", runner, service.DirectCommitter(teamGit)))
	syncer.SetPlaybooks(pb.ForRepo("team"))
	svc.SetPlaybookLister(pbc)

	// Matches production wiring (cmd/contextmatrix/wire_gitsync.go): only
	// the repo with a remote gets the hook; private has none.
	require.NoError(t, svc.SetOnCommitFor("team", syncer.NotifyCommit))
	require.NoError(t, pb.SetOnCommitFor("team", syncer.NotifyCommit))

	group := NewGroup(composite.Hidden, GroupEntry{Name: "private"}, GroupEntry{Name: "team", Syncer: syncer})

	return &multiNode{
		id: instance, svc: svc, pb: pb, composite: composite, group: group, syncer: syncer,
		team: team, private: private, teamDir: teamDir, privDir: privDir, upstream: upstream, clk: clk, bus: bus,
	}
}

func (n *multiNode) sync(t *testing.T) SyncReport {
	t.Helper()

	r, err := n.syncer.Synced(context.Background(), "test", nil)
	require.NoError(t, err)

	return r
}

func (n *multiNode) create(t *testing.T, project, title string) *board.Card {
	t.Helper()

	c, err := n.svc.CreateCard(context.Background(), project, service.CreateCardInput{Title: title, Type: "task", Priority: "medium"})
	require.NoError(t, err)

	return c
}

func (n *multiNode) card(t *testing.T, project, id string) *board.Card {
	t.Helper()

	c, err := n.composite.GetCard(context.Background(), project, id)
	require.NoError(t, err)

	return c
}

func TestMultiRepo_PrivateClaimKeepsAgentIDOwnershipAndNeverPushes(t *testing.T) {
	a, b := setupMultiPair(t)
	ctx := context.Background()

	priv := a.create(t, "private-project", "mine")
	assert.Equal(t, "PRIV-001", priv.ID)

	claimed, err := a.svc.ClaimCard(ctx, "private-project", priv.ID, "agent-1")
	require.NoError(t, err)
	assert.Empty(t, claimed.ClaimedVia)
	assert.Equal(t, 0, claimed.ClaimEpoch)

	a.sync(t)
	b.sync(t)

	_, err = b.composite.GetCard(ctx, "private-project", "PRIV-001")
	require.Error(t, err, "b's private repo is its own; a's private card never crosses the remote")

	msg, err := a.private.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "claimed")
}

func TestMultiRepo_SharedCreateReachesThePeer(t *testing.T) {
	a, b := setupMultiPair(t)

	created := a.create(t, "test-project", "shared")
	b.sync(t)

	got := b.card(t, "test-project", created.ID)
	assert.Equal(t, "shared", got.Title)

	statuses := b.group.Statuses()
	require.Len(t, statuses, 2)
	assert.Equal(t, "private", statuses[0].Repo)
	assert.False(t, statuses[0].Enabled)
	assert.Equal(t, "team", statuses[1].Repo)
	assert.True(t, statuses[1].Shared)
	assert.NotNil(t, statuses[1].LastSyncTime)
}

func TestMultiRepo_ForeignStallNeedsARecentPullOfThatRepo(t *testing.T) {
	a, b := setupMultiPair(t)
	ctx := context.Background()

	created := a.create(t, "test-project", "held by b")
	b.sync(t)

	_, err := b.svc.ClaimCard(ctx, "test-project", created.ID, "agent-b")
	require.NoError(t, err)

	a.sync(t)
	require.True(t, a.card(t, "test-project", created.ID).ClaimedElsewhere("lap-a"))

	a.clk.Advance(2 * time.Hour)

	// The private repo never syncs; marking it synced says nothing about team.
	a.svc.SyncSucceeded(ctx, "private")
	require.NoError(t, a.svc.SweepStalled(ctx))
	assert.Equal(t, "agent-b", a.card(t, "test-project", created.ID).AssignedAgent)

	a.sync(t)
	require.NoError(t, a.svc.SweepStalled(ctx))
	got := a.card(t, "test-project", created.ID)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.AssignedAgent)

	b.sync(t)
	assert.Equal(t, board.StateStalled, b.card(t, "test-project", created.ID).State)
}

func TestMultiRepo_PlaybookIDsAreUniqueAcrossRepos(t *testing.T) {
	a, _ := setupMultiPair(t)
	ctx := context.Background()

	first, err := a.pb.Create(ctx, service.CreatePlaybookInput{Title: "Alpha", AgentID: "human:a", BoardsRepo: "private"})
	require.NoError(t, err)
	assert.Equal(t, "alpha", first.ID)
	assert.Equal(t, "private", first.BoardsRepo)

	second, err := a.pb.Create(ctx, service.CreatePlaybookInput{Title: "Alpha", AgentID: "human:a", BoardsRepo: "team"})
	require.NoError(t, err)
	assert.Equal(t, "alpha-2", second.ID)
	assert.Equal(t, "team", second.BoardsRepo)
	assert.FileExists(t, filepath.Join(a.teamDir, "playbooks", "alpha-2.yaml"))
	assert.NoFileExists(t, filepath.Join(a.privDir, "playbooks", "alpha-2.yaml"))
}

func TestMultiRepo_DuplicateProjectViaPullIsHiddenAndReported(t *testing.T) {
	a, _ := setupMultiPair(t)

	// A peer pushes a shared project named like a's private one. No peer
	// service can mint it: project names are unique across repos, and every
	// node holds private-project in its own private repo, so a create in the
	// team repo is rejected before it reaches disk. Pushing straight to the
	// upstream is the only way this name reaches a's team clone, and it is
	// the case the composite has to survive: a duplicate arriving on a pull.
	origin := strings.TrimSpace(run(t, a.teamDir, "git", "remote", "get-url", "origin"))
	seed := filepath.Join(t.TempDir(), "dup-seed")
	run(t, "", "git", "clone", origin, seed)
	run(t, seed, "git", "config", "user.email", "t@t")
	run(t, seed, "git", "config", "user.name", "t")

	dup := privateProjectConfig()
	dup.Prefix = "DUP"

	require.NoError(t, os.MkdirAll(filepath.Join(seed, "private-project", "tasks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(seed, "private-project", "tasks", ".gitkeep"), nil, 0o644))
	require.NoError(t, board.SaveProjectConfig(filepath.Join(seed, "private-project"), dup))

	run(t, seed, "git", "add", "-A")
	run(t, seed, "git", "commit", "-m", "duplicate project")
	run(t, seed, "git", "push", "origin", "HEAD:main")

	a.sync(t)

	repo, ok := a.composite.RepoOf("private-project")
	require.True(t, ok)
	assert.Equal(t, "private", repo, "the earlier configured repo keeps the name")

	hidden := a.composite.Hidden()
	require.Len(t, hidden, 1)
	assert.Equal(t, storage.HiddenProject{Name: "private-project", Repo: "team", VisibleIn: "private"}, hidden[0])

	statuses := a.group.Statuses()
	assert.Empty(t, statuses[0].HiddenProjects)
	assert.Equal(t, []string{"private-project"}, statuses[1].HiddenProjects)

	card := a.create(t, "private-project", "still private")
	assert.Equal(t, "PRIV-001", card.ID, "writes route to the visible copy")
	assert.FileExists(t, filepath.Join(a.privDir, "private-project", "tasks", "PRIV-001.md"))
	assert.NoFileExists(t, filepath.Join(a.teamDir, "private-project", "tasks", "PRIV-001.md"))
}

func TestMultiRepo_StallSweepCoversBothRepos(t *testing.T) {
	a, _ := setupMultiPair(t)
	ctx := context.Background()

	priv := a.create(t, "private-project", "p")
	team := a.create(t, "test-project", "t")

	_, err := a.svc.ClaimCard(ctx, "private-project", priv.ID, "agent-p")
	require.NoError(t, err)

	_, err = a.svc.ClaimCard(ctx, "test-project", team.ID, "agent-t")
	require.NoError(t, err)

	a.clk.Advance(31 * time.Minute)
	require.NoError(t, a.svc.SweepStalled(ctx))

	assert.Equal(t, board.StateStalled, a.card(t, "private-project", priv.ID).State)
	assert.Equal(t, board.StateStalled, a.card(t, "test-project", team.ID).State)
}

// TestMultiRepo_NoDeadlockAcrossReposAndPlaybooks runs a shared cycle, a
// verified shared create, private card creates and playbook creates in both
// repos at once. Lock order is playbooks, writeMu, then the one repo's
// queue, so every goroutine finishes.
func TestMultiRepo_NoDeadlockAcrossReposAndPlaybooks(t *testing.T) {
	a, _ := setupMultiPair(t)
	ctx := context.Background()

	var wg sync.WaitGroup

	errs := make(chan error, 32)
	start := make(chan struct{})

	spawn := func(fn func() error) {
		wg.Go(func() {
			<-start

			if err := fn(); err != nil {
				errs <- err
			}
		})
	}

	for range 3 {
		spawn(func() error {
			_, err := a.syncer.Synced(ctx, "tick", nil)

			return err
		})
		spawn(func() error {
			_, err := a.svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "shared", Type: "task", Priority: "medium"})

			return err
		})
		spawn(func() error {
			_, err := a.svc.CreateCard(ctx, "private-project", service.CreateCardInput{Title: "private", Type: "task", Priority: "medium"})

			return err
		})
		spawn(func() error {
			_, err := a.pb.Create(ctx, service.CreatePlaybookInput{Title: "In private", AgentID: "human:a", BoardsRepo: "private"})

			return err
		})
		spawn(func() error {
			_, err := a.pb.Create(ctx, service.CreatePlaybookInput{Title: "In team", AgentID: "human:a", BoardsRepo: "team"})

			return err
		})
	}

	close(start)

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("deadlock: not every goroutine finished")
	}

	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	for _, r := range []*service.BoardsRepo{a.private, a.team} {
		dirty, err := r.Git.HasUncommittedChanges()
		require.NoError(t, err)
		assert.False(t, dirty, r.Name)
	}

	list, err := a.pb.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 6)
}

// TestMultiRepo_OnCommitNotifiesOnlyTheTeamSyncer checks the on-commit hook
// wiring: a team commit notifies the team syncer, a private commit (repo
// without a remote, so no hook in production) does not, and nothing from the
// private commit reaches the upstream. The harness never calls syncer.Start,
// so nothing consumes pushCh, and notifyCommit runs synchronously inside the
// mutation path before CreateCard returns - the channel state is settled
// deterministically after each create.
func TestMultiRepo_OnCommitNotifiesOnlyTheTeamSyncer(t *testing.T) {
	a, b := setupMultiPair(t)

	a.create(t, "test-project", "shared")
	require.Len(t, a.syncer.pushCh, 1, "team commit notified the team syncer")

	a.sync(t)
	before := strings.TrimSpace(run(t, a.upstream, "git", "log", "--oneline"))
	require.NotEmpty(t, before)

	<-a.syncer.pushCh

	a.create(t, "private-project", "mine")
	assert.Empty(t, a.syncer.pushCh, "private commit must not notify the team syncer")

	a.sync(t)
	b.sync(t)

	after := strings.TrimSpace(run(t, a.upstream, "git", "log", "--oneline"))
	assert.Equal(t, before, after, "private commit must not push to the upstream")
}
