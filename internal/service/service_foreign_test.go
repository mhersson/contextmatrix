package service

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForeignStall_PushVerifiedAfterTheLeaseTimeout(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	runner := &fakeRunner{svc: svc}
	svc.SetSyncRunner(runner.run)

	ctx := context.Background()
	card := createShared(t, svc)
	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)
	require.NoError(t, svc.ObserveLeases(ctx))
	svc.SyncSucceeded(ctx)

	runner.calls = 0

	require.NoError(t, svc.SweepStalled(ctx))
	assert.Zero(t, runner.calls, "a live foreign lease is left alone")

	fake.Advance(61 * time.Minute)
	svc.SyncSucceeded(ctx) // a fresh pull is a precondition

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	require.NoError(t, svc.SweepStalled(ctx))
	assert.Equal(t, 1, runner.calls)
	assert.Equal(t, "foreign stall", runner.lastTrigger)

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.AssignedAgent)
	assert.Empty(t, got.ClaimedVia)
	assert.Equal(t, 2, got.ClaimEpoch)
	assert.Equal(t, "failed", got.WorkerStatus)

	select {
	case e := <-ch:
		assert.Equal(t, events.CardStalled, e.Type)
		assert.Equal(t, "lap-b", e.Data["claimed_via"])
	case <-time.After(time.Second):
		t.Fatal("no card.stalled event")
	}
}

func TestForeignStall_RequiresARecentPull(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	runner := &fakeRunner{svc: svc}
	svc.SetSyncRunner(runner.run)

	ctx := context.Background()
	card := createShared(t, svc)
	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)
	require.NoError(t, svc.ObserveLeases(ctx))

	runner.calls = 0

	fake.Advance(61 * time.Minute) // no SyncSucceeded since: the local view is stale

	require.NoError(t, svc.SweepStalled(ctx))
	assert.Zero(t, runner.calls)

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "lap-b", got.ClaimedVia)
}

func TestForeignStall_RenewedLeaseInsideTheCycleCancelsIt(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	runner := &fakeRunner{svc: svc}
	svc.SetSyncRunner(runner.run)

	ctx := context.Background()
	card := createShared(t, svc)
	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)
	require.NoError(t, svc.ObserveLeases(ctx))

	fake.Advance(61 * time.Minute)
	svc.SyncSucceeded(ctx)

	// The merge inside the cycle brought a renewal: the stall must not fire.
	renewed := fake.Now()
	c, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	c.LastHeartbeat = &renewed
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", c))
	require.NoError(t, svc.ObserveLeases(ctx))

	require.NoError(t, svc.SweepStalled(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State)
	assert.Equal(t, 1, got.ClaimEpoch)
}

func TestForeignStall_UndoneWhenThePushNeverLands(t *testing.T) {
	svc, fake, cleanup := newSharedService(t, 30*time.Minute)
	defer cleanup()

	runner := &fakeRunner{svc: svc, failAfter: true}
	svc.SetSyncRunner(runner.run)

	ctx := context.Background()

	// The verified create would fail with failAfter, so the card goes in
	// through the store.
	card := &board.Card{
		ID: "TEST-001", Title: "t", Project: "test-project", Type: "task", State: "todo",
		Priority: "medium", Created: fake.Now(), Updated: fake.Now(),
	}
	require.NoError(t, svc.store.CreateCard(ctx, "test-project", card))

	writeForeignClaim(t, svc, card.ID, fake.Now(), 1)
	require.NoError(t, svc.ObserveLeases(ctx))
	fake.Advance(61 * time.Minute)
	svc.SyncSucceeded(ctx)

	require.NoError(t, svc.SweepStalled(ctx), "a failed foreign stall is logged, not returned")
	assert.Equal(t, 1, runner.undoCalls)

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State)
	assert.Equal(t, "lap-b", got.ClaimedVia)
	assert.Equal(t, 1, got.ClaimEpoch)
}
