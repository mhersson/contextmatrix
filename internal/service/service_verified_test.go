package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner stands in for the syncer: it takes the write locks like Synced
// does, runs Apply, lets the test simulate what the merge or the push did,
// and runs Undo when told the push never landed.
type fakeRunner struct {
	svc         *CardService
	repo        string // empty means the first configured boards repo
	calls       int
	failBefore  int                       // cycles that fail before Apply runs
	failAfter   bool                      // the push never lands: Undo runs, the error is returned
	afterApply  func(ctx context.Context) // simulates the merge that follows Apply
	resolutions []boardmerge.Resolution
	undoCalls   int
	lastTrigger string
}

func (f *fakeRunner) run(ctx context.Context, trigger string, m SyncMutation) (SyncOutcome, error) {
	f.calls++
	f.lastTrigger = trigger

	if f.calls <= f.failBefore {
		return SyncOutcome{}, errors.New("fetch: remote unreachable")
	}

	f.svc.LockWrites(f.repo)
	defer f.svc.UnlockWrites(f.repo)

	out := SyncOutcome{BodyRan: true, Resolutions: f.resolutions}

	if err := m.Apply(ctx); err != nil {
		return out, err
	}

	if f.afterApply != nil {
		f.afterApply(ctx)
	}

	if f.failAfter {
		if m.Undo != nil {
			f.undoCalls++

			if err := m.Undo(ctx); err != nil {
				return out, err
			}
		}

		return out, errors.New("push rejected 5 times")
	}

	out.Pushed = true

	return out, nil
}

func newVerifiedService(t *testing.T) (*CardService, *fakeRunner, func()) {
	t.Helper()

	svc, _, cleanup := newSharedService(t, 30*time.Minute)
	runner := &fakeRunner{svc: svc}
	svc.SetSyncRunner(runner.run)

	return svc, runner, cleanup
}

func TestCreateCardVerified_RunsInsideTheCycle(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	card := createShared(t, svc)
	assert.Equal(t, 1, runner.calls)
	assert.Equal(t, "create card", runner.lastTrigger)

	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID)

	dirty, err := svc.git.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, dirty)
}

func TestCreateCardVerified_MapsARemintedID(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	// The merge kept the remote's TEST-001 and re-minted ours as TEST-002.
	runner.afterApply = func(ctx context.Context) {
		src := filepath.Join(svc.boardsDir, "test-project", "tasks", "TEST-001.md")
		dst := filepath.Join(svc.boardsDir, "test-project", "tasks", "TEST-002.md")

		data, err := os.ReadFile(src)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(dst, []byte(strings.Replace(string(data), "id: TEST-001", "id: TEST-002", 1)), 0o644))
		require.NoError(t, os.WriteFile(src, []byte(strings.Replace(string(data), "title: t", "title: remote", 1)), 0o644))
		require.NoError(t, svc.reloadStoreIndex(ctx))
	}
	runner.resolutions = []boardmerge.Resolution{{
		Path: "test-project/tasks/TEST-001.md", CardID: "TEST-001", Rule: boardmerge.RuleAddAddRemint,
		OldID: "TEST-001", NewID: "TEST-002",
	}}

	card := createShared(t, svc)
	assert.Equal(t, "TEST-002", card.ID)
	assert.Equal(t, "t", card.Title)
}

func TestCreateCardVerified_RetriesWhenTheCycleFailsBeforeApply(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	runner.failBefore = 2

	card := createShared(t, svc)
	assert.Equal(t, 3, runner.calls)
	assert.Equal(t, "TEST-001", card.ID)
}

func TestCreateCardVerified_RemoteUnreachableAfterThreeAttempts(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	runner.failBefore = 10

	_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.ErrorIs(t, err, ErrRemoteUnreachable)
	assert.Equal(t, 3, runner.calls)

	cards, err := svc.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Empty(t, cards)
}

func TestCreateCardVerified_UndoneWhenThePushNeverLands(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	runner.failAfter = true

	_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.ErrorIs(t, err, ErrRemoteUnreachable)
	assert.Equal(t, 1, runner.calls, "a write that ran is not retried blindly")
	assert.Equal(t, 1, runner.undoCalls)

	cards, err := svc.store.ListCards(context.Background(), "test-project", storage.CardFilter{})
	require.NoError(t, err)
	assert.Empty(t, cards)

	dirty, err := svc.git.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, dirty)

	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "undone")
}

func TestClaimCardVerified_ConflictAfterTheMerge(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	runner.calls = 0

	// The merge handed the double claim to lap-b.
	runner.afterApply = func(ctx context.Context) {
		c, err := svc.store.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)

		c.ClaimedVia = "lap-b"
		require.NoError(t, svc.store.UpdateCard(ctx, "test-project", c))
	}

	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-"+card.ID)
	require.ErrorIs(t, err, lock.ErrAlreadyClaimed)
	assert.Contains(t, err.Error(), "lap-b")
}

func TestClaimCardVerified_ApplyErrorIsReturnedAsIs(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	runner.calls, runner.undoCalls = 0, 0

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "b")
	require.ErrorIs(t, err, lock.ErrAlreadyClaimed)
	assert.Equal(t, 1, runner.calls)
	assert.Equal(t, 0, runner.undoCalls)
}

func TestClaimCardVerified_UndoneWhenThePushNeverLands(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	runner.failAfter = true

	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AssignedAgent)
	assert.Empty(t, got.ClaimedVia)
	assert.Zero(t, got.ClaimEpoch, "the undo restores the pre-claim epoch so a peer's claim outranks it")
}

func TestForceReleaseVerified_RunsInsideTheCycle(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	runner.calls = 0

	got, err := svc.ForceReleaseCard(ctx, "test-project", card.ID, "human:op")
	require.NoError(t, err)
	assert.Equal(t, 1, runner.calls)
	assert.Empty(t, got.AssignedAgent)
	assert.Equal(t, 2, got.ClaimEpoch)
}

func TestProjectMutationsVerified(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	ctx := context.Background()

	cfg, err := svc.CreateProject(ctx, CreateProjectInput{
		Name: "beta", Prefix: "BETA",
		States: []string{"todo", "done", "stalled", "not_planned"}, Types: []string{"task"}, Priorities: []string{"low"},
		Transitions: map[string][]string{"todo": {"done"}, "stalled": {"todo"}, "not_planned": {"todo"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "beta", cfg.Name)
	assert.Equal(t, 1, runner.calls)

	_, err = svc.UpdateProject(ctx, "beta", UpdateProjectInput{
		Repo:   "https://example.com/r",
		States: cfg.States, Types: cfg.Types, Priorities: cfg.Priorities, Transitions: cfg.Transitions,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, runner.calls)

	require.NoError(t, svc.DeleteProject(ctx, "beta"))
	assert.Equal(t, 3, runner.calls)

	_, err = svc.store.GetProject(ctx, "beta")
	require.ErrorIs(t, err, storage.ErrProjectNotFound)
}

func TestProjectCreateVerified_UndoneWhenThePushNeverLands(t *testing.T) {
	svc, runner, cleanup := newVerifiedService(t)
	defer cleanup()

	runner.failAfter = true

	_, err := svc.CreateProject(context.Background(), CreateProjectInput{
		Name: "beta", Prefix: "BETA",
		States: []string{"todo", "done", "stalled", "not_planned"}, Types: []string{"task"}, Priorities: []string{"low"},
		Transitions: map[string][]string{"todo": {"done"}, "stalled": {"todo"}, "not_planned": {"todo"}},
	})
	require.ErrorIs(t, err, ErrRemoteUnreachable)

	_, err = svc.store.GetProject(context.Background(), "beta")
	require.ErrorIs(t, err, storage.ErrProjectNotFound)

	dirty, err := svc.git.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, dirty)
}

func TestRollbackCreate_NeverRewindsAPeersNextID(t *testing.T) {
	svc, _, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc) // TEST-001, next_id 2

	cfg, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	cfg.NextID = 7 // a pull advanced it
	require.NoError(t, svc.store.SaveProject(ctx, cfg))

	for _, e := range svc.rollbackCreate(ctx, "test-project", card) {
		require.NoError(t, e)
	}

	after, err := svc.store.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, 7, after.NextID)

	_, err = svc.store.GetCard(ctx, "test-project", card.ID)
	require.ErrorIs(t, err, storage.ErrCardNotFound)
}

func TestPrivateBoardNeverUsesTheRunner(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	runner := &fakeRunner{svc: svc}
	svc.SetSyncRunner(runner.run)

	_, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	assert.Zero(t, runner.calls, "a private board commits locally, whatever is wired")
}
