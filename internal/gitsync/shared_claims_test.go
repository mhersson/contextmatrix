package gitsync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedClaim_SecondInstanceRefused is the verified claim in the calm
// case: the first claim is on the remote before the second instance tries.
func TestSharedClaim_SecondInstanceRefused(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	agent := "agent-" + c.ID

	a.claim(t, c.ID, agent)
	b.sync(t)

	_, err := b.svc.ClaimCard(context.Background(), "test-project", c.ID, agent)
	require.ErrorIs(t, err, lock.ErrAlreadyClaimed)
	assert.Contains(t, err.Error(), "via lap-a")
}

// TestSharedClaim_DoubleClaimHasOneWinner races two instances for one card.
// The second instance pushes first; the first instance merges, loses the
// double claim on claimed_at, and learns it through claim.lost.
func TestSharedClaim_DoubleClaimHasOneWinner(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	c := a.create(t, "x")
	agent := "agent-" + c.ID

	b.sync(t)

	// b claims first on its own clock; a claims a second later, while b's
	// push is still in flight, and reaches the remote first.
	a.clk.Advance(time.Second)

	b.syncer.prePushHook = func(attempt int) {
		if attempt == 0 {
			a.claim(t, c.ID, agent)
		}
	}

	won := b.claim(t, c.ID, agent)
	assert.Equal(t, "lap-b", won.ClaimedVia)

	ch, unsubscribe := a.bus.Subscribe()
	defer unsubscribe()

	a.sync(t)

	var got []events.Event

	drainEvents(ch, &got)
	assertHasEventType(t, got, events.ClaimLost)

	assert.Equal(t, "lap-b", a.card(t, c.ID).ClaimedVia)
	assert.Equal(t, "lap-b", b.card(t, c.ID).ClaimedVia)

	_, err := a.svc.HeartbeatCard(ctx, "test-project", c.ID, agent)
	require.ErrorIs(t, err, lock.ErrAgentMismatch, "the losing instance's agent is locked out")

	b.heartbeat(t, c.ID, agent)
}

func TestSharedHeartbeat_RenewsTheLeaseOncePerInterval(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	c := a.create(t, "x")
	agent := "agent-" + c.ID
	a.claim(t, c.ID, agent)

	a.clk.Advance(time.Minute)
	a.heartbeat(t, c.ID, agent)
	assert.Contains(t, a.lastCommit(t), "claimed", "a beat inside the lease interval writes nothing")
	assert.False(t, a.sync(t).Pushed)

	a.clk.Advance(5 * time.Minute)
	a.heartbeat(t, c.ID, agent)
	assert.Contains(t, a.lastCommit(t), "heartbeat")
	require.True(t, a.sync(t).Pushed)

	b.sync(t)
	assert.Equal(t, a.clk.Now(), *b.card(t, c.ID).LastHeartbeat, "the peer sees the renewed lease")
}

func TestSharedStall_OwnClaimsOnlyThenLeaseTakeover(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	c := a.create(t, "x")
	agent := "agent-" + c.ID
	a.claim(t, c.ID, agent)
	b.sync(t)

	b.clk.Advance(31 * time.Minute)
	b.sweep(t)
	assert.Equal(t, "lap-a", b.card(t, c.ID).ClaimedVia, "the heartbeat rule never touches a peer's claim")

	b.clk.Advance(31 * time.Minute)
	b.sync(t) // a fresh pull; the lease value is unchanged since it was first seen
	b.sweep(t)

	stalled := b.card(t, c.ID)
	assert.Equal(t, "stalled", stalled.State)
	assert.Empty(t, stalled.AssignedAgent)
	assert.Equal(t, 2, stalled.ClaimEpoch)
	assert.Equal(t, 0, b.syncer.Status().UnpushedCommits, "the stall was push-verified")

	ch, unsubscribe := a.bus.Subscribe()
	defer unsubscribe()

	a.sync(t)

	var got []events.Event

	drainEvents(ch, &got)
	assertHasEventType(t, got, events.ClaimLost)

	_, err := a.svc.HeartbeatCard(ctx, "test-project", c.ID, agent)
	require.Error(t, err)
}

func TestSharedTerminal_DoneKeepsClaimReleaseClears(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	c := a.create(t, "x")
	agent := "agent-" + c.ID
	a.claim(t, c.ID, agent)

	_, err := a.svc.TransitionTo(ctx, "test-project", c.ID, "in_progress")
	require.NoError(t, err)
	_, err = a.svc.TransitionTo(ctx, "test-project", c.ID, "done")
	require.NoError(t, err)

	done := a.card(t, c.ID)
	assert.Equal(t, agent, done.AssignedAgent)
	assert.Equal(t, 2, done.ClaimEpoch)

	_, err = a.svc.ReleaseCard(ctx, "test-project", c.ID, agent)
	require.NoError(t, err)

	a.sync(t)
	b.sync(t)

	got := b.card(t, c.ID)
	assert.Equal(t, "done", got.State)
	assert.Empty(t, got.AssignedAgent)
	assert.Equal(t, 3, got.ClaimEpoch)
}

func TestSharedCreate_RemintMapsTheReturnedID(t *testing.T) {
	a, b, _ := setupSharedPair(t)

	a.create(t, "seed")
	b.sync(t)

	b.syncer.prePushHook = func(attempt int) {
		if attempt == 0 {
			a.create(t, "from a") // takes the id b just minted, and reaches the remote first
		}
	}

	fromB := b.create(t, "from b")
	assert.Equal(t, "TEST-003", fromB.ID, "b's card was re-minted behind a's")
	assert.Equal(t, "from b", fromB.Title)
	assert.Equal(t, "from a", b.card(t, "TEST-002").Title)

	a.sync(t)
	assertConverged(t, a, b, 3)
}

func TestSharedCreate_UndoneWhenThePushNeverLands(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	a.create(t, "seed")
	b.sync(t)

	b.syncer.maxAttempts = 1
	b.syncer.retryBackoff = time.Millisecond
	b.syncer.prePushHook = func(int) { a.create(t, "a moved first") }

	_, err := b.svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: "from b", Type: "task", Priority: "medium"})
	require.ErrorIs(t, err, service.ErrRemoteUnreachable)

	assert.Len(t, cardFiles(t, b), 1, "the undone card left no file behind the seed")

	clean, dirty, err := b.git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean, dirty)
	assert.False(t, b.git.MergeInProgress())

	b.syncer.prePushHook = nil
	b.syncer.maxAttempts = defaultMaxAttempts
	b.sync(t)
	assertConverged(t, a, b, 2)
}

// TestSharedLockOrder_CreatePullAndPlaybookConcurrently drives the three
// lock takers at once: verified card creates, periodic cycles, and playbook
// writes (a verified create and a queued entry update). It fails by timing
// out if any ordering deadlocks.
func TestSharedLockOrder_CreatePullAndPlaybookConcurrently(t *testing.T) {
	a, _, _ := setupSharedPair(t)
	ctx := context.Background()

	a.create(t, "seed")
	a.sync(t)

	const rounds = 5

	var wg sync.WaitGroup

	errs := make(chan error, 3*rounds)

	wg.Add(3)

	go func() {
		defer wg.Done()

		for i := range rounds {
			if _, err := a.svc.CreateCard(ctx, "test-project", service.CreateCardInput{Title: fmt.Sprintf("card-%d", i), Type: "task", Priority: "low"}); err != nil {
				errs <- err
			}
		}
	}()

	go func() {
		defer wg.Done()

		for range rounds {
			if _, err := a.syncer.Synced(ctx, "periodic", nil); err != nil {
				errs <- err
			}
		}
	}()

	go func() {
		defer wg.Done()

		for i := range rounds {
			detail, err := a.pb.Create(ctx, service.CreatePlaybookInput{Title: fmt.Sprintf("pb %d", i), AgentID: "human:t"})
			if err != nil {
				errs <- err

				continue
			}

			if _, err := a.pb.AddEntry(ctx, detail.ID, service.PlaybookEntryInput{Type: "manual", Text: "step"}, "human:t"); err != nil {
				errs <- err
			}
		}
	}()

	done := make(chan struct{})

	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("lock order deadlock: create, sync and playbook writers did not all finish")
	}

	close(errs)

	var failures []error

	for err := range errs {
		failures = append(failures, err)
	}

	require.Empty(t, failures)

	assert.Len(t, cardFiles(t, a), rounds+1)

	pbs, err := a.pb.List(ctx)
	require.NoError(t, err)
	assert.Len(t, pbs, rounds)

	clean, dirty, err := a.git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean, dirty)
}

func TestSharedAgentErrors_SameAgentIDOtherInstance(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	c := a.create(t, "x")
	agent := "agent-" + c.ID
	a.claim(t, c.ID, agent)
	b.sync(t)

	_, err := b.svc.AddLogEntry(ctx, "test-project", c.ID, boardEntry(agent))
	require.ErrorIs(t, err, lock.ErrAgentMismatch)
	assert.Contains(t, err.Error(), "agent does not own")
}
