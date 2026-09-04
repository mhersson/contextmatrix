package service

import (
	"context"
	"errors"
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

type sharedPrivate struct {
	svc           *CardService
	team, private *BoardsRepo
	runner        *fakeRunner
	clk           *clock.FakeClock
	composite     *storage.Composite
}

// newSharedAndPrivateService wires repo "team" (project alpha, shared via
// instance lap-a, fake runner) next to repo "private" (project beta, no
// instance) on one composite and one fake clock.
func newSharedAndPrivateService(t *testing.T) (*sharedPrivate, func()) {
	t.Helper()

	fTeam := newRepoFixture(t, "team", "alpha", "ALPHA")
	fPriv := newRepoFixture(t, "private", "beta", "BETA")

	composite, err := storage.NewComposite(
		storage.NamedStore{Name: "team", Store: fTeam.store},
		storage.NamedStore{Name: "private", Store: fPriv.store},
	)
	require.NoError(t, err)

	clk := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))

	teamView, err := composite.View("team")
	require.NoError(t, err)

	privView, err := composite.View("private")
	require.NoError(t, err)

	teamLock := lock.NewManagerWithClock(teamView, 30*time.Minute, clk)
	teamLock.SetShared("lap-a", 5*time.Minute, time.Hour)

	team := &BoardsRepo{
		Name: "team", Store: teamView, Git: fTeam.git, Dir: fTeam.dir, GitAutoCommit: true, Shared: true,
		Lock: teamLock, Queue: gitops.NewCommitQueue(fTeam.git, 0),
		Instance: "lap-a", LeaseTimeout: time.Hour, PullInterval: time.Minute,
	}
	private := &BoardsRepo{
		Name: "private", Store: privView, Git: fPriv.git, Dir: fPriv.dir, GitAutoCommit: true,
		Lock: lock.NewManagerWithClock(privView, 30*time.Minute, clk), Queue: gitops.NewCommitQueue(fPriv.git, 0),
	}

	svc, err := NewCardServiceRepos(composite, events.NewBus(), nil, team, private)
	require.NoError(t, err)

	runner := &fakeRunner{svc: svc, repo: "team"}
	require.NoError(t, svc.SetSyncRunnerFor("team", runner.run))

	cleanup := func() {
		_ = team.Queue.Close(context.Background())
		_ = private.Queue.Close(context.Background())
	}

	return &sharedPrivate{svc: svc, team: team, private: private, runner: runner, clk: clk, composite: composite}, cleanup
}

func TestPrivateRepoNextToShared_ClaimWritesNoOwnershipFields(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	priv, err := sp.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "p", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	assert.Equal(t, 0, sp.runner.calls, "a private create is a local write")

	claimed, err := sp.svc.ClaimCard(ctx, "beta", priv.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "agent-1", claimed.AssignedAgent)
	assert.Empty(t, claimed.ClaimedVia)
	assert.Nil(t, claimed.ClaimedAt)
	assert.Equal(t, 0, claimed.ClaimEpoch)
	assert.Equal(t, 0, sp.runner.calls, "a private claim is a local write")

	again, err := sp.svc.ClaimCard(ctx, "beta", priv.ID, "agent-1")
	require.NoError(t, err, "same agent re-claim is a refresh on a private repo")
	assert.Equal(t, 0, again.ClaimEpoch)

	team, err := sp.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	assert.Equal(t, 1, sp.runner.calls)

	claimed, err = sp.svc.ClaimCard(ctx, "alpha", team.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "lap-a", claimed.ClaimedVia)
	assert.NotNil(t, claimed.ClaimedAt)
	assert.Equal(t, 1, claimed.ClaimEpoch)
	assert.Equal(t, 2, sp.runner.calls)
}

func TestPrivateRepoNextToShared_OwnershipIgnoresClaimedVia(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()
	now := sp.clk.Now()

	write := func(project, id string) *board.Card {
		c, err := sp.svc.CreateCard(ctx, project, CreateCardInput{Title: id, Type: "task", Priority: "medium"})
		require.NoError(t, err)

		c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.ClaimEpoch, c.LastHeartbeat = "agent-1", "lap-b", &now, 1, &now
		require.NoError(t, sp.composite.UpdateCard(ctx, project, c))

		got, err := sp.composite.GetCard(ctx, project, c.ID)
		require.NoError(t, err)

		return got
	}

	priv := write("beta", "p")
	assert.True(t, sp.svc.OwnsClaim(priv, "agent-1"), "a private repo compares the agent ID only")
	assert.False(t, sp.svc.ClaimedElsewhere(priv))

	team := write("alpha", "t")
	assert.False(t, sp.svc.OwnsClaim(team, "agent-1"), "a shared repo needs the instance to match")
	assert.True(t, sp.svc.ClaimedElsewhere(team))
}

func TestSyncSucceeded_ScopesToOneRepo(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := sp.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "foreign", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	now := sp.clk.Now()
	card.State, card.AssignedAgent, card.ClaimedVia, card.ClaimedAt, card.ClaimEpoch, card.LastHeartbeat = board.StateInProgress, "agent-x", "lap-b", &now, 1, &now
	require.NoError(t, sp.composite.UpdateCard(ctx, "alpha", card))
	require.NoError(t, sp.svc.ObserveLeases(ctx, "team"))

	sp.clk.Advance(2 * time.Hour)
	callsBefore := sp.runner.calls

	require.NoError(t, sp.svc.SweepStalled(ctx))
	got, err := sp.composite.GetCard(ctx, "alpha", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State, "no recent pull of team: the lease is not judged")
	assert.Equal(t, callsBefore, sp.runner.calls)

	sp.svc.SyncSucceeded(ctx, "private")
	require.NoError(t, sp.svc.SweepStalled(ctx))
	got, err = sp.composite.GetCard(ctx, "alpha", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State, "a pull of another repo does not make team's view fresh")

	sp.svc.SyncSucceeded(ctx, "team")
	require.NoError(t, sp.svc.SweepStalled(ctx))
	got, err = sp.composite.GetCard(ctx, "alpha", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.AssignedAgent)
	assert.Equal(t, 2, got.ClaimEpoch)
	assert.Equal(t, callsBefore+1, sp.runner.calls, "the foreign stall is push-verified")
}

func TestStallSweep_CoversEveryRepo(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	priv, err := sp.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "p", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = sp.svc.ClaimCard(ctx, "beta", priv.ID, "agent-p")
	require.NoError(t, err)

	team, err := sp.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = sp.svc.ClaimCard(ctx, "alpha", team.ID, "agent-t")
	require.NoError(t, err)

	sp.clk.Advance(31 * time.Minute)
	require.NoError(t, sp.svc.SweepStalled(ctx))

	for _, tc := range []struct{ project, id string }{{"beta", priv.ID}, {"alpha", team.ID}} {
		got, err := sp.composite.GetCard(ctx, tc.project, tc.id)
		require.NoError(t, err)
		assert.Equal(t, board.StateStalled, got.State, tc.project)
		assert.Empty(t, got.AssignedAgent, tc.project)
	}

	got, err := sp.composite.GetCard(ctx, "beta", priv.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, got.ClaimEpoch, "a private stall never bumps the epoch")
}

// stallScanFailStore fronts a repo's storage.Store view but makes ListProjects
// fail with a synthetic error, so that repo's lock manager scan fails without
// touching the filesystem. FilesystemStore.ListProjects serves an in-memory
// index, so removing the repo directory does not produce a scan error, and
// Composite.ListProjects fails wholesale - the failure must be scoped to the
// one repo's lock manager.
type stallScanFailStore struct {
	storage.Store
	err error
}

func (f *stallScanFailStore) ListProjects(context.Context) ([]board.ProjectConfig, error) {
	return nil, f.err
}

func TestStallSweep_ContinuesPastFailingRepoScan(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	priv, err := sp.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "p", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = sp.svc.ClaimCard(ctx, "beta", priv.ID, "agent-p")
	require.NoError(t, err)

	team, err := sp.svc.CreateCard(ctx, "alpha", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = sp.svc.ClaimCard(ctx, "alpha", team.ID, "agent-t")
	require.NoError(t, err)

	// The team repo's scan fails; the private repo's beta card must still be
	// swept. The swapped manager drops the fixture's SetShared("lap-a", ...)
	// lease state - intentionally not re-applied, because FindStalled errors
	// on the very first store call and never reads the shared-lease config.
	// The team card's stall is skipped along with the scan, so alpha stays
	// untouched.
	sp.team.Lock = lock.NewManagerWithClock(
		&stallScanFailStore{Store: sp.team.Store, err: errors.New("boom")},
		30*time.Minute, sp.clk)

	sp.clk.Advance(31 * time.Minute)

	require.NoError(t, sp.svc.SweepStalled(ctx), "one repo's scan failure must not fail the sweep")

	got, err := sp.composite.GetCard(ctx, "beta", priv.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State, "the clean repo still stalls its card")
	assert.Empty(t, got.AssignedAgent)
	assert.Equal(t, 0, got.ClaimEpoch, "a private stall never bumps the epoch")

	teamGot, err := sp.composite.GetCard(ctx, "alpha", team.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateTodo, teamGot.State, "alpha stays untouched when its repo's scan fails")
	assert.Equal(t, "agent-t", teamGot.AssignedAgent)
}

func TestStallSweep_AllReposFailingReturnsError(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	priv, err := sp.svc.CreateCard(ctx, "beta", CreateCardInput{Title: "p", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	_, err = sp.svc.ClaimCard(ctx, "beta", priv.ID, "agent-p")
	require.NoError(t, err)

	sp.team.Lock = lock.NewManagerWithClock(
		&stallScanFailStore{Store: sp.team.Store, err: errors.New("boom")},
		30*time.Minute, sp.clk)
	sp.private.Lock = lock.NewManagerWithClock(
		&stallScanFailStore{Store: sp.private.Store, err: errors.New("boom")},
		30*time.Minute, sp.clk)

	sp.clk.Advance(31 * time.Minute)

	err = sp.svc.SweepStalled(ctx)
	require.Error(t, err, "every repo failed - the caller logging keeps a signal")
	assert.Contains(t, err.Error(), "find stalled")
	assert.Contains(t, err.Error(), "team")
	assert.Contains(t, err.Error(), "private")

	got, err := sp.composite.GetCard(ctx, "beta", priv.ID)
	require.NoError(t, err)
	assert.NotEqual(t, board.StateStalled, got.State, "nothing was stalled when every scan failed")
}
