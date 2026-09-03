package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sharedTestProject() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name: "test-project", Prefix: "TEST", NextID: 1,
		States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"review", "done", "todo", "not_planned"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

// newSharedService is a CardService on a shared board: instance lap-a, a
// fake clock shared with the lock manager, lease interval 5m, lease timeout
// 1h, pull interval 1m. heartbeat is the stall timeout.
func newSharedService(t *testing.T, heartbeat time.Duration) (*CardService, *clock.FakeClock, func()) {
	t.Helper()

	boardsDir := filepath.Join(t.TempDir(), "boards")
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, sharedTestProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	fake := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	lockMgr := lock.NewManagerWithClock(store, heartbeat, fake)
	lockMgr.SetShared("lap-a", 5*time.Minute, time.Hour)

	svc := NewCardService(store, gitMgr, lockMgr, events.NewBus(), boardsDir, nil, true, false)
	svc.SetSharedRepo(true)
	svc.SetLease("lap-a", time.Hour, time.Minute)

	queue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(queue)

	return svc, fake, func() { _ = queue.Close(context.Background()) }
}

// writeForeignClaim marks a card as held by agent-<id> through instance
// lap-b, straight through the store, as a pull would leave it.
func writeForeignClaim(t *testing.T, svc *CardService, id string, hb time.Time, epoch int) *board.Card {
	t.Helper()

	c, err := svc.store.GetCard(context.Background(), "test-project", id)
	require.NoError(t, err)

	c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.LastHeartbeat, c.ClaimEpoch = "agent-"+id, "lap-b", &hb, &hb, epoch
	c.State, c.WorkerStatus = board.StateInProgress, "running"
	require.NoError(t, svc.store.UpdateCard(context.Background(), "test-project", c))

	return c
}

func createShared(t *testing.T, svc *CardService) *board.Card {
	t.Helper()

	c, err := svc.CreateCard(context.Background(), "test-project", CreateCardInput{Title: "t", Type: "task", Priority: "medium"})
	require.NoError(t, err)

	return c
}

func TestSharedClaimLifecycle_TupleAndEpochs(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)

	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)
	assert.Equal(t, "lap-a", claimed.ClaimedVia)
	assert.Equal(t, 1, claimed.ClaimEpoch)
	require.NotNil(t, claimed.ClaimedAt)
	assert.Equal(t, fake.Now(), *claimed.ClaimedAt)

	_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateInProgress)
	require.NoError(t, err)

	done, err := svc.TransitionTo(ctx, "test-project", card.ID, board.StateDone)
	require.NoError(t, err)
	assert.Equal(t, "a", done.AssignedAgent, "done keeps the claim for the release that follows")
	assert.Equal(t, 2, done.ClaimEpoch, "the terminal transition bumps the epoch")

	released, err := svc.ReleaseCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)
	assert.Empty(t, released.AssignedAgent)
	assert.Empty(t, released.ClaimedVia)
	assert.Nil(t, released.ClaimedAt)
	assert.Equal(t, 3, released.ClaimEpoch)
}

func TestSharedNotPlanned_ClearsTupleAndBumps(t *testing.T) {
	svc, _, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	cancelled, err := svc.TransitionTo(ctx, "test-project", card.ID, board.StateNotPlanned)
	require.NoError(t, err)
	assert.Empty(t, cancelled.AssignedAgent)
	assert.Empty(t, cancelled.ClaimedVia)
	assert.Equal(t, 2, cancelled.ClaimEpoch)

	read, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Nil(t, read.LastHeartbeat, "a cancelled card carries no live beat")
}

func TestSharedHeartbeat_RenewsTheFileOncePerLeaseInterval(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	claimMsg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)

	fake.Advance(time.Minute)

	beat, err := svc.HeartbeatCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)
	assert.Equal(t, fake.Now(), *beat.LastHeartbeat, "the ack reports the live beat")

	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, claimMsg, msg, "no commit within the lease interval")

	onDisk, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, fake.Now().Add(-time.Minute), *onDisk.LastHeartbeat, "the file still carries the claim time")

	fake.Advance(5 * time.Minute)

	_, err = svc.HeartbeatCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	msg, err = svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "heartbeat")

	read, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, fake.Now(), *read.LastHeartbeat)
}

func TestSharedStall_SkipsForeignClaimsAndBumpsOwn(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, time.Millisecond)
	defer cleanup()

	ctx := context.Background()
	own := createShared(t, svc)
	foreign := createShared(t, svc)

	_, err := svc.ClaimCard(ctx, "test-project", own.ID, "a")
	require.NoError(t, err)

	writeForeignClaim(t, svc, foreign.ID, fake.Now().Add(-2*time.Hour), 1)

	fake.Advance(2 * time.Millisecond)
	require.NoError(t, svc.processStalled(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", own.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.ClaimedVia)
	assert.Equal(t, 2, got.ClaimEpoch)

	other, err := svc.store.GetCard(ctx, "test-project", foreign.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, other.State, "a peer's claim is never stalled by the heartbeat rule")
	assert.Equal(t, "lap-b", other.ClaimedVia)
}

func TestSharedStall_SkipsAFencedOwnClaim(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	// Out of contact for longer than the lease timeout: a peer may already
	// have taken the card over, so this instance syncs before it writes a
	// stall of its own.
	fake.Advance(61 * time.Minute)
	require.NoError(t, svc.SweepStalled(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.AssignedAgent, "a fenced claim is left for the next cycle to settle")
	assert.Equal(t, 1, got.ClaimEpoch)

	// Once a cycle has confirmed the lease, the heartbeat rule applies again.
	svc.SyncSucceeded(ctx)
	fake.Advance(31 * time.Minute)
	require.NoError(t, svc.SweepStalled(ctx))

	got, err = svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.AssignedAgent)
	assert.Equal(t, 2, got.ClaimEpoch)
}

func TestSharedWorkerStatus_IgnoresForeignCard(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)

	got, err := svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "failed", "killed by operator")
	require.NoError(t, err)
	assert.Equal(t, "running", got.WorkerStatus)
	assert.Equal(t, "agent-"+card.ID, got.AssignedAgent)
	assert.Equal(t, 1, got.ClaimEpoch)
}

func TestSharedOwnership_SameAgentThroughAnotherInstanceIsRefused(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)
	agent := "agent-" + card.ID

	_, err := svc.AddLogEntry(ctx, "test-project", card.ID, board.ActivityEntry{Agent: agent, Action: "note", Message: "x"})
	require.ErrorIs(t, err, lock.ErrAgentMismatch)

	body := "b"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body, AgentID: agent})
	require.ErrorIs(t, err, lock.ErrAgentMismatch)

	_, err = svc.RecordPush(ctx, "test-project", card.ID, agent, "feature/x", "")
	require.ErrorIs(t, err, lock.ErrAgentMismatch)

	_, err = svc.HeartbeatCard(ctx, "test-project", card.ID, agent)
	require.ErrorIs(t, err, lock.ErrAgentMismatch)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, agent)
	require.ErrorIs(t, err, lock.ErrAgentMismatch)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, agent)
	require.ErrorIs(t, err, lock.ErrAlreadyClaimed)

	assert.False(t, svc.OwnsClaim(card, agent))
}

func TestSharedReads_OverlayLiveBeat(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	fake.Advance(time.Minute)

	_, err = svc.HeartbeatCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, fake.Now(), *listed[0].LastHeartbeat)

	dash, err := svc.GetDashboard(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, dash.ActiveAgents, 1)
	assert.Equal(t, fake.Now(), dash.ActiveAgents[0].LastHeartbeat)
}

func TestSharedFence_BlocksClaimWritesUntilConfirmed(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	fake.Advance(61 * time.Minute)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "a")
	require.ErrorIs(t, err, ErrClaimFenced)
	require.ErrorIs(t, err, lock.ErrAgentMismatch, "agents see the lost-claim error they already handle")

	_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateInProgress)
	require.ErrorIs(t, err, ErrClaimFenced)

	state := board.StateInProgress
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &state, AgentID: "a"})
	require.ErrorIs(t, err, ErrClaimFenced)

	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.ErrorIs(t, err, ErrClaimFenced)

	_, err = svc.HeartbeatCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err, "heartbeats are how a fenced instance recovers, so they pass")

	svc.SyncSucceeded(ctx)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)
}

func TestSharedFence_NeverOnPrivateOrUnclaimed(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)

	fake.Advance(2 * time.Hour)

	_, err := svc.TransitionTo(ctx, "test-project", card.ID, board.StateInProgress)
	require.NoError(t, err, "an unclaimed card has no lease")
}

func TestNoteClaimLost_PublishesAndDropsTheBeat(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	ctx := context.Background()
	card := createShared(t, svc)
	_, err := svc.ClaimCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	fake.Advance(time.Minute)

	_, err = svc.HeartbeatCard(ctx, "test-project", card.ID, "a")
	require.NoError(t, err)

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	svc.NoteClaimLost(ctx, "test-project", card.ID, "a", "lap-b", 2)

	select {
	case e := <-ch:
		assert.Equal(t, events.ClaimLost, e.Type)
		assert.Equal(t, card.ID, e.CardID)
		assert.Equal(t, "a", e.Data["previous_agent"])
		assert.Equal(t, "lap-b", e.Data["claimed_via"])
		assert.Equal(t, 2, e.Data["claim_epoch"])
	case <-time.After(time.Second):
		t.Fatal("no claim.lost event")
	}

	onDisk, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	read, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, *onDisk.LastHeartbeat, *read.LastHeartbeat, "the live beat is gone with the claim")
}

func TestRecentlySynced(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	assert.False(t, svc.recentlySynced(), "never synced")

	svc.SyncSucceeded(context.Background())
	assert.True(t, svc.recentlySynced())

	fake.Advance(3 * time.Minute)
	assert.False(t, svc.recentlySynced(), "twice the pull interval has passed")
}
