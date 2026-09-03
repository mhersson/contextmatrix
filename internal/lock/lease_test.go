package lock

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSharedManager(t *testing.T) (*Manager, storage.Store, *clock.FakeClock) {
	t.Helper()

	store, _ := setupTestStore(t)
	fake := clock.Fake(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	mgr := NewManagerWithClock(store, 30*time.Minute, fake)
	mgr.SetShared("lap-a", 5*time.Minute, time.Hour)

	return mgr, store, fake
}

// foreignCard writes a card another instance holds, with the given lease
// value, straight into the store.
func foreignCard(t *testing.T, store storage.Store, id string, hb time.Time, epoch int) *board.Card {
	t.Helper()

	c := &board.Card{
		ID: id, Title: id, Project: "test-project", Type: "task", State: "in_progress", Priority: "medium",
		AssignedAgent: "agent-" + id, ClaimedVia: "lap-b", ClaimedAt: &hb, ClaimEpoch: epoch,
		LastHeartbeat: &hb, Created: hb, Updated: hb,
	}
	require.NoError(t, store.CreateCard(context.Background(), "test-project", c))

	return c
}

func TestClaim_Shared(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(t *testing.T, mgr *Manager, store storage.Store, fake *clock.FakeClock)
		agent   string
		wantErr error
		check   func(t *testing.T, c *board.Card, fake *clock.FakeClock)
	}{
		{"fresh claim sets the tuple", func(t *testing.T, _ *Manager, store storage.Store, fake *clock.FakeClock) {
			createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())
		}, "a", nil, func(t *testing.T, c *board.Card, fake *clock.FakeClock) {
			assert.Equal(t, "lap-a", c.ClaimedVia)
			assert.Equal(t, 1, c.ClaimEpoch)
			require.NotNil(t, c.ClaimedAt)
			assert.Equal(t, fake.Now(), *c.ClaimedAt)
		}},
		{"refresh by the holder keeps the epoch", func(t *testing.T, mgr *Manager, store storage.Store, fake *clock.FakeClock) {
			createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())

			c, err := mgr.Claim(ctx, "test-project", "TEST-001", "a")
			require.NoError(t, err)
			require.NoError(t, store.UpdateCard(ctx, "test-project", c))
		}, "a", nil, func(t *testing.T, c *board.Card, _ *clock.FakeClock) {
			assert.Equal(t, 1, c.ClaimEpoch)
		}},
		{"other agent on this instance refused", func(t *testing.T, _ *Manager, store storage.Store, fake *clock.FakeClock) {
			createTestCardAt(t, store, "test-project", "TEST-001", "a", fake.Now())
		}, "b", ErrAlreadyClaimed, nil},
		{"same agent via another instance refused while the lease is live", func(t *testing.T, _ *Manager, store storage.Store, fake *clock.FakeClock) {
			foreignCard(t, store, "TEST-001", fake.Now(), 2)
		}, "agent-TEST-001", ErrAlreadyClaimed, nil},
		{"expired foreign lease is taken over with a higher epoch", func(t *testing.T, mgr *Manager, store storage.Store, fake *clock.FakeClock) {
			foreignCard(t, store, "TEST-001", fake.Now(), 2)
			require.NoError(t, mgr.ObserveLeases(ctx))
			fake.Advance(61 * time.Minute)
		}, "agent-TEST-001", nil, func(t *testing.T, c *board.Card, _ *clock.FakeClock) {
			assert.Equal(t, "lap-a", c.ClaimedVia)
			assert.Equal(t, 3, c.ClaimEpoch)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, store, fake := newSharedManager(t)
			tt.setup(t, mgr, store, fake)

			c, err := mgr.Claim(ctx, "test-project", "TEST-001", tt.agent)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			tt.check(t, c, fake)
		})
	}
}

func TestClaim_SharedForeignRefusalNamesTheInstance(t *testing.T) {
	mgr, store, fake := newSharedManager(t)
	foreignCard(t, store, "TEST-001", fake.Now(), 1)

	_, err := mgr.Claim(context.Background(), "test-project", "TEST-001", "agent-TEST-001")
	require.ErrorIs(t, err, ErrAlreadyClaimed)
	assert.Contains(t, err.Error(), "lap-b")
}

func TestRelease_SharedClearsTupleAndBumpsEpoch(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())

	c, err := mgr.Claim(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateCard(ctx, "test-project", c))

	released, err := mgr.Release(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	assert.Empty(t, released.AssignedAgent)
	assert.Empty(t, released.ClaimedVia)
	assert.Nil(t, released.ClaimedAt)
	assert.Equal(t, 2, released.ClaimEpoch)
	assert.Nil(t, mgr.LastBeat(released), "the live beat is dropped with the claim")
}

func TestRelease_SharedRefusesForeignHolder(t *testing.T) {
	mgr, store, fake := newSharedManager(t)
	foreignCard(t, store, "TEST-001", fake.Now(), 1)

	_, err := mgr.Release(context.Background(), "test-project", "TEST-001", "agent-TEST-001")
	require.ErrorIs(t, err, ErrAgentMismatch)
}

func TestHeartbeat_SharedPersistsOnlyPastTheLeaseInterval(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())

	claimed, err := mgr.Claim(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateCard(ctx, "test-project", claimed))

	fake.Advance(time.Minute)

	c, persist, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	assert.False(t, persist, "one minute in, the file lease is fresh enough")
	assert.Equal(t, *claimed.LastHeartbeat, *c.LastHeartbeat, "the file value is untouched")
	assert.Equal(t, fake.Now(), *mgr.LastBeat(c), "the live beat moved")

	fake.Advance(5 * time.Minute)

	c, persist, err = mgr.Heartbeat(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	assert.True(t, persist)
	assert.Equal(t, fake.Now(), *c.LastHeartbeat)
}

func TestFindStalled_SharedSkipsForeignAndUsesLiveBeat(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	old := fake.Now().Add(-2 * time.Hour)

	foreignCard(t, store, "TEST-001", old, 1) // foreign: never ours to stall

	own := createTestCardAt(t, store, "test-project", "TEST-002", "", fake.Now())
	c, err := mgr.Claim(ctx, "test-project", own.ID, "a")
	require.NoError(t, err)

	c.LastHeartbeat = &old // stale on file
	require.NoError(t, store.UpdateCard(ctx, "test-project", c))

	_, _, err = mgr.Heartbeat(ctx, "test-project", own.ID, "a") // live beat now
	require.NoError(t, err)

	stale := createTestCardAt(t, store, "test-project", "TEST-003", "", fake.Now())
	c3, err := mgr.Claim(ctx, "test-project", stale.ID, "b")
	require.NoError(t, err)

	c3.LastHeartbeat = &old
	require.NoError(t, store.UpdateCard(ctx, "test-project", c3))
	mgr.ClearBeat("test-project", stale.ID)

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)
	require.Len(t, stalled, 1)
	assert.Equal(t, "TEST-003", stalled[0].Card.ID)
}

func TestLastBeat_OnlyOverlaysOwnClaims(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())

	claimed, err := mgr.Claim(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateCard(ctx, "test-project", claimed))

	fake.Advance(time.Minute)

	held, _, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	require.Equal(t, fake.Now(), *mgr.LastBeat(held), "the holder sees its own live beat")

	// A merge handed the card to a peer: our beat says nothing about the
	// lease the board now shows, and neither does it after a release.
	taken := *held
	taken.ClaimedVia, taken.ClaimEpoch = "lap-b", held.ClaimEpoch+1
	assert.Equal(t, held.LastHeartbeat, mgr.LastBeat(&taken), "a foreign claim keeps the file lease")

	released := *held
	released.AssignedAgent, released.ClaimedVia = "", ""
	assert.Equal(t, held.LastHeartbeat, mgr.LastBeat(&released), "an unclaimed card keeps the file lease")

	legacy := *held
	legacy.ClaimedVia = ""
	assert.Equal(t, held.LastHeartbeat, mgr.LastBeat(&legacy), "a claim from before shared boards keeps the file lease")
}

func TestForeignLeaseExpired(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	c := foreignCard(t, store, "TEST-001", fake.Now(), 1)

	assert.False(t, mgr.ForeignLeaseExpired(c), "never observed")

	require.NoError(t, mgr.ObserveLeases(ctx))
	fake.Advance(59 * time.Minute)
	assert.False(t, mgr.ForeignLeaseExpired(c))

	fake.Advance(2 * time.Minute)
	assert.True(t, mgr.ForeignLeaseExpired(c))

	// A renewed lease restarts the clock.
	renewed := fake.Now()
	c.LastHeartbeat = &renewed
	require.NoError(t, store.UpdateCard(ctx, "test-project", c))
	require.NoError(t, mgr.ObserveLeases(ctx))
	assert.False(t, mgr.ForeignLeaseExpired(c))

	// A different epoch is a different claim.
	c.ClaimEpoch = 2
	assert.False(t, mgr.ForeignLeaseExpired(c))
}

func TestFenced(t *testing.T) {
	ctx := context.Background()
	mgr, store, fake := newSharedManager(t)
	createTestCardAt(t, store, "test-project", "TEST-001", "", fake.Now())

	c, err := mgr.Claim(ctx, "test-project", "TEST-001", "a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateCard(ctx, "test-project", c))
	assert.False(t, mgr.Fenced(c), "a fresh claim is confirmed by the claim itself")

	fake.Advance(61 * time.Minute)
	assert.True(t, mgr.Fenced(c))

	require.NoError(t, mgr.ConfirmLeases(ctx))
	assert.False(t, mgr.Fenced(c))

	legacy := &board.Card{Project: "test-project", ID: "TEST-009", AssignedAgent: "x"}
	assert.False(t, mgr.Fenced(legacy), "a legacy claim has no lease to fence")
}

func TestPrivateBoardWritesNoClaimFields(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	createTestCard(t, store, "test-project", "TEST-001", "")

	c, err := mgr.Claim(context.Background(), "test-project", "TEST-001", "a")
	require.NoError(t, err)
	assert.Empty(t, c.ClaimedVia)
	assert.Nil(t, c.ClaimedAt)
	assert.Zero(t, c.ClaimEpoch)
}
