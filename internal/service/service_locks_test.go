package service

import (
	"context"
	"errors"
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

// newStalledTestService builds a CardService wired with a fake clock and a
// 1ms heartbeat timeout so processStalled can be triggered deterministically
// by advancing the clock. The default testProject config lists "stalled" in
// States, so the standard validator accepts a properly cleared stalled card.
func newStalledTestService(t *testing.T) (*CardService, *clock.FakeClock, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	bus := events.NewBus()
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lockMgr := lock.NewManagerWithClock(store, 1*time.Millisecond, fake)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	commitQueue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(commitQueue)

	cleanup := func() {
		_ = commitQueue.Close(context.Background())
	}

	return svc, fake, cleanup
}

// TestMarkCardStalled_RejectsInvariantViolation pins the card-level invariant
// that a stalled card has no assigned_agent. The stall path is system-managed
// (it bypasses the per-project transition map), so a future regression that
// forgets to clear assigned_agent must be caught by card-level validation,
// not transition validation. The test wires a custom validateStalledCardFn
// that enforces this invariant and asserts the production code honours it.
func TestMarkCardStalled_RejectsInvariantViolation(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Override the stall-path validator with a stricter stub that requires
	// assigned_agent=="" iff state==stalled. The production code already
	// clears assigned_agent in markCardStalled, so this validator must
	// accept the post-mutation card under the current implementation. A
	// future regression that drops the field-clear would persist a card
	// the stub rejects, and this test would fail.
	svc.validateStalledCardFn = func(_ *board.ProjectConfig, c *board.Card) error {
		if c.State == board.StateStalled && c.AssignedAgent != "" {
			return errors.New("stalled card must have no assigned_agent")
		}

		return nil
	}

	// Create and claim a card so it is eligible for stalling.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Stall", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "stale-agent")
	require.NoError(t, err)

	// Advance past the 1 ms stall cutoff so the lock manager flags the card.
	fake.Advance(10 * time.Millisecond)

	require.NoError(t, svc.processStalled(ctx), "valid stall must succeed under stub validator")

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Empty(t, got.AssignedAgent)
	assert.Nil(t, got.LastHeartbeat)
}

// setupParentWithSubtask creates a parent card plus one subtask, then drives
// the subtask through the real transition path. Moving the subtask to
// in_progress auto-transitions the parent todo→in_progress
// (maybeTransitionParent), leaving the parent in_progress + UNCLAIMED - exactly
// the state a live run leaves a parent in while its subtasks execute, and the
// state FindStalled can never reach because it only scans claimed cards. When
// subtaskState is not in_progress the subtask is then transitioned on to that
// (terminal) state.
func setupParentWithSubtask(t *testing.T, svc *CardService, subtaskState string) (parent, sub *board.Card) {
	t.Helper()

	ctx := context.Background()

	var err error

	parent, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Parent", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	sub, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Sub", Type: "task", Priority: "medium", Parent: parent.ID,
	})
	require.NoError(t, err)

	// First move to in_progress drags the parent along via maybeTransitionParent.
	_, err = svc.TransitionTo(ctx, "test-project", sub.ID, board.StateInProgress)
	require.NoError(t, err)

	if subtaskState != board.StateInProgress {
		_, err = svc.TransitionTo(ctx, "test-project", sub.ID, subtaskState)
		require.NoError(t, err)
	}

	p, err := svc.store.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	require.Equal(t, board.StateInProgress, p.State, "parent must be in_progress after the first subtask claim")
	require.Empty(t, p.AssignedAgent, "a parent is never itself claimed")

	return parent, sub
}

// TestProcessAbandonedParents_ReapsStuckParent pins the janitor's core job: a
// parent left in_progress + unclaimed after its whole run died (no active
// subtask, untouched past the stall timeout) is reaped to stalled. FindStalled
// never covers it - the parent carries no claim - so without this sweep it is
// stuck forever.
func TestProcessAbandonedParents_ReapsStuckParent(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	parent, _ := setupParentWithSubtask(t, svc, board.StateDone)

	// The run died: parent sits in_progress + unclaimed with no active subtask.
	// Advance well past the stall timeout so it counts as abandoned.
	fake.Advance(10 * svc.HeartbeatTimeout())
	require.NoError(t, svc.processAbandonedParents(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State, "abandoned parent is reaped to stalled")
	assert.Empty(t, got.AssignedAgent)

	// Idempotent: a second sweep leaves the already-stalled parent untouched.
	require.NoError(t, svc.processAbandonedParents(ctx))

	got2, err := svc.store.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got2.State)
}

// TestProcessAbandonedParents_SkipsParentWithActiveSubtask pins guard 3: a
// parent whose subtask is still being worked (claimed / in_progress) must never
// be reaped, even when the parent itself is old enough to trip the recency
// guard. This is the "merely between subtask claims" case the janitor must not
// disturb.
func TestProcessAbandonedParents_SkipsParentWithActiveSubtask(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	parent, sub := setupParentWithSubtask(t, svc, board.StateInProgress)

	// An agent is actively working the subtask.
	_, err := svc.ClaimCard(ctx, "test-project", sub.ID, "worker-agent")
	require.NoError(t, err)

	// Parent is old enough that only the active-subtask guard can save it.
	fake.Advance(10 * svc.HeartbeatTimeout())
	require.NoError(t, svc.processAbandonedParents(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State, "parent with an active subtask must not be reaped")
}

// TestProcessAbandonedParents_SkipsRecentlyUpdatedParent pins guard 4: a parent
// touched within the stall timeout is not abandoned yet, even with no active
// subtask, so the janitor must leave it alone.
func TestProcessAbandonedParents_SkipsRecentlyUpdatedParent(t *testing.T) {
	svc, _, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	parent, _ := setupParentWithSubtask(t, svc, board.StateDone)

	// Subtask is terminal (guard 3 would allow a reap) but the clock is NOT
	// advanced, so the parent stays inside the stall window and only the
	// recency guard prevents the reap.
	require.NoError(t, svc.processAbandonedParents(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateInProgress, got.State, "recently-touched parent must not be reaped")
}

// TestMarkCardStalled_NormalizesWorkerStatus pins the fix for the Run Now
// 409 bug: a card stalled mid-run keeps worker_status at "running" (or
// "queued"), which makes runCard treat it as a live worker and reject every
// future Run Now with ErrCodeWorkerConflict until a manual Stop. A stalled
// worker is presumed dead, so markCardStalled must normalize it to the
// terminal "failed" status the failed-callback path would have set.
func TestMarkCardStalled_NormalizesWorkerStatus(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Stall Running", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "dead-agent")
	require.NoError(t, err)

	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "running", "")
	require.NoError(t, err)

	fake.Advance(10 * time.Millisecond)
	require.NoError(t, svc.processStalled(ctx))

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, board.StateStalled, got.State)
	assert.Equal(t, "failed", got.WorkerStatus,
		"a stalled worker is presumed dead; leaving queued/running blocks every future Run Now with a 409")
}

// TestMarkCardStalled_PersistGatedByValidator drives the validator-rejection
// branch directly: a stub that always returns an error must short-circuit
// the persist, leaving the card in its pre-stall state with the claim
// intact. This guards against a future regression that removes the
// validateStalledCardFn call from markCardStalled - without the gate, the
// card would be persisted as stalled despite the validator's veto.
func TestMarkCardStalled_PersistGatedByValidator(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	rejected := errors.New("validator says no")
	svc.validateStalledCardFn = func(*board.ProjectConfig, *board.Card) error {
		return rejected
	}

	// Create and claim the card so markCardStalled sees a valid candidate.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Veto Stall", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "claimed-agent")
	require.NoError(t, err)

	// Advance past the 1ms stall cutoff so the lock manager would otherwise
	// flag this card.
	fake.Advance(10 * time.Millisecond)

	// Call markCardStalled directly so we observe the returned error rather
	// than the swallow-and-log behaviour of processStalled.
	err = svc.markCardStalled(ctx, lock.StalledCard{Project: "test-project", Card: claimed})
	require.ErrorIs(t, err, rejected)

	// Card must NOT have been persisted with state=stalled - the validator
	// rejection short-circuits UpdateCard. The pre-stall claim must remain.
	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.NotEqual(t, board.StateStalled, got.State, "validator rejection must gate persist")
	assert.Equal(t, "claimed-agent", got.AssignedAgent, "claim must remain when stall is blocked")
	assert.NotNil(t, got.LastHeartbeat, "heartbeat must remain when stall is blocked")
}

func TestForceReleaseCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Crashed Worker", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Simulate a hard-crashed worker: status stuck at running.
	seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	seeded.WorkerStatus = "running"
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	released, err := svc.ForceReleaseCard(ctx, "test-project", card.ID, "human:alice")
	require.NoError(t, err)

	assert.Empty(t, released.AssignedAgent)
	assert.Nil(t, released.LastHeartbeat)
	assert.Equal(t, seeded.State, released.State, "card state must be untouched")
	assert.Equal(t, "failed", released.WorkerStatus, "stuck running status must normalize to failed")

	require.NotEmpty(t, released.ActivityLog)
	last := released.ActivityLog[len(released.ActivityLog)-1]
	assert.Equal(t, "force_released", last.Action)
	assert.Equal(t, "human:alice", last.Agent)
	assert.Contains(t, last.Message, "agent-1")

	select {
	case event := <-ch:
		assert.Equal(t, events.CardReleased, event.Type)
		assert.Equal(t, "human:alice", event.Agent)
		assert.Equal(t, "agent-1", event.Data["previous_agent"])
		assert.Equal(t, true, event.Data["forced"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardReleased event")
	}
}

func TestForceReleaseCard_TerminalWorkerStatusUntouched(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Already Failed", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	seeded.WorkerStatus = "failed"
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

	released, err := svc.ForceReleaseCard(ctx, "test-project", card.ID, "human:alice")
	require.NoError(t, err)

	assert.Equal(t, "failed", released.WorkerStatus)
}

func TestForceReleaseCard_NotClaimed(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Unclaimed", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ForceReleaseCard(ctx, "test-project", card.ID, "human:alice")
	require.Error(t, err)
	assert.ErrorIs(t, err, lock.ErrNotClaimed)
}

func TestForceReleaseCard_NonHumanCaller(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Guarded", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	_, err = svc.ForceReleaseCard(ctx, "test-project", card.ID, "claude-1")
	require.ErrorIs(t, err, ErrForceReleaseRequiresHuman)

	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", got.AssignedAgent, "claim must survive a rejected force-release")
}

// claimCardFixture creates and claims a card, then advances the fake clock so
// any subsequent mutation's timestamp is distinguishable from the claim-time
// heartbeat. Returns the card and the claim-time heartbeat (T0).
func claimCardFixture(t *testing.T, svc *CardService, fake *clock.FakeClock, agent string) (*board.Card, time.Time) {
	t.Helper()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Heartbeat Fixture", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, agent)
	require.NoError(t, err)

	t0 := fake.Now()
	fake.Advance(time.Hour)

	return card, t0
}

// TestMutationsRefreshHeartbeat pins the mutation-as-heartbeat guarantee: any
// owner-attributed write to a claimed card refreshes LastHeartbeat, so an
// agent actively mutating a card is never stalled purely for lack of an
// explicit heartbeat call. System commits (empty commit agent) and edits by
// someone other than the card's owner must never bump it - a bump on those
// paths would keep a dead agent's claim alive or let a bystander edit extend
// it.
func TestMutationsRefreshHeartbeat(t *testing.T) {
	ctx := context.Background()

	t.Run("owner PatchCard bumps", func(t *testing.T) {
		svc, fake, cleanup := newStalledTestService(t)
		defer cleanup()

		card, t0 := claimCardFixture(t, svc, fake, "owner-agent")

		title := "Patched by owner"
		patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID: "owner-agent",
			Title:   &title,
		})
		require.NoError(t, err)
		require.NotNil(t, patched.LastHeartbeat)
		assert.Equal(t, fake.Now(), *patched.LastHeartbeat)
		assert.True(t, patched.LastHeartbeat.After(t0))
	})

	t.Run("mismatched commitAgentID does not bump", func(t *testing.T) {
		svc, fake, cleanup := newStalledTestService(t)
		defer cleanup()

		card, t0 := claimCardFixture(t, svc, fake, "owner-agent")

		// PatchCard/AddLogEntry/ReportUsage each reject a caller whose agent ID
		// does not match AssignedAgent before any mutation is attempted, so a
		// real non-owner call never reaches the heartbeat-stamping site. This
		// exercises applyCardMutation's own guard directly (same package) as
		// defense in depth for that invariant.
		noop := func(c *board.Card, _ *board.ProjectConfig) error {
			c.Title = "mutated on behalf of a different agent"

			return nil
		}

		mutated, err := svc.applyCardMutation(ctx, "test-project", card.ID, noop, mutationOpts{
			commitAgentID: "someone-else",
			commitAction:  "updated",
		})
		require.NoError(t, err)
		require.NotNil(t, mutated.LastHeartbeat)
		assert.Equal(t, t0, *mutated.LastHeartbeat, "a commitAgentID that doesn't match AssignedAgent must not bump")
	})

	t.Run("owner AddLogEntry bumps", func(t *testing.T) {
		svc, fake, cleanup := newStalledTestService(t)
		defer cleanup()

		card, t0 := claimCardFixture(t, svc, fake, "owner-agent")

		updated, err := svc.AddLogEntry(ctx, "test-project", card.ID, board.ActivityEntry{
			Agent:  "owner-agent",
			Action: "status_update",
		})
		require.NoError(t, err)
		require.NotNil(t, updated.LastHeartbeat)
		assert.Equal(t, fake.Now(), *updated.LastHeartbeat)
		assert.True(t, updated.LastHeartbeat.After(t0))
	})

	t.Run("owner ReportUsage bumps", func(t *testing.T) {
		svc, fake, cleanup := newStalledTestService(t)
		defer cleanup()

		card, t0 := claimCardFixture(t, svc, fake, "owner-agent")

		updated, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
			AgentID:      "owner-agent",
			PromptTokens: 10,
		})
		require.NoError(t, err)
		require.NotNil(t, updated.LastHeartbeat)
		assert.Equal(t, fake.Now(), *updated.LastHeartbeat)
		assert.True(t, updated.LastHeartbeat.After(t0))
	})

	t.Run("system mutation does not bump", func(t *testing.T) {
		svc, fake, cleanup := newStalledTestService(t)
		defer cleanup()

		card, t0 := claimCardFixture(t, svc, fake, "owner-agent")

		updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title: "System edit", Type: card.Type, State: card.State, Priority: card.Priority,
		})
		require.NoError(t, err)
		require.NotNil(t, updated.LastHeartbeat)
		assert.Equal(t, t0, *updated.LastHeartbeat, "a system commit (no commit agent) must not extend a claim")
	})

	t.Run("unclaimed card stays nil", func(t *testing.T) {
		svc, _, cleanup := newStalledTestService(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Unclaimed", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		title := "Edited"
		patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			AgentID: "some-agent",
			Title:   &title,
		})
		require.NoError(t, err)
		assert.Nil(t, patched.LastHeartbeat)
	})
}

func TestForceReleaseCard_TerminalStateStrayClaim(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Done With Stray Claim", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Seed the anomaly directly: terminal state with a lingering claim.
	seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	seeded.State = board.StateDone
	seeded.AssignedAgent = "agent-1"
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

	released, err := svc.ForceReleaseCard(ctx, "test-project", card.ID, "human:alice")
	require.NoError(t, err)

	assert.Empty(t, released.AssignedAgent)
	assert.Equal(t, board.StateDone, released.State, "terminal state must be untouched")
}

// TestClaimCard_TerminalState pins the invariant the code has always assumed
// but never enforced: no agent goes to work on a finished card. Claiming a
// not_planned card is how a cancelled subtask gets picked up and reimplemented.
func TestClaimCard_TerminalState(t *testing.T) {
	ctx := context.Background()

	t.Run("not_planned card cannot be claimed", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Cancelled by a human", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateNotPlanned)
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.ErrorIs(t, err, ErrCardTerminal)

		reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.Empty(t, reloaded.AssignedAgent, "refused claim must not land on the card")
		assert.Equal(t, board.StateNotPlanned, reloaded.State)
	})

	t.Run("done card cannot be claimed by another agent", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Finished", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateInProgress)
		require.NoError(t, err)
		_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateDone)
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-2")
		require.ErrorIs(t, err, ErrCardTerminal)

		reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.Empty(t, reloaded.AssignedAgent)
	})

	// The holder exemption must key on a claim that actually exists: agent_id
	// is only length-checked, so an empty one would otherwise match the empty
	// assigned_agent of an unclaimed terminal card and walk straight through.
	t.Run("empty agent id cannot claim a terminal card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Cancelled, unclaimed", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateNotPlanned)
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "")
		require.ErrorIs(t, err, ErrCardTerminal)

		reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
		require.NoError(t, err)
		assert.Nil(t, reloaded.LastHeartbeat, "refused claim must not set a heartbeat")
	})

	// An agent keeps its claim through done so ReleaseCard can flush deferred
	// commits (see enforceTerminalStateInvariants). A re-claim by that holder is
	// a heartbeat refresh, not a new agent picking up finished work.
	t.Run("holder may reclaim its own done card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Finished by me", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err)

		_, err = svc.TransitionTo(ctx, "test-project", card.ID, board.StateInProgress)
		require.NoError(t, err)

		done, err := svc.TransitionTo(ctx, "test-project", card.ID, board.StateDone)
		require.NoError(t, err)
		require.Equal(t, "agent-1", done.AssignedAgent, "claim is retained through done")

		reclaimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err)

		assert.Equal(t, "agent-1", reclaimed.AssignedAgent, "the holder keeps the card")
		assert.Equal(t, board.StateDone, reclaimed.State, "a re-claim does not move a done card")
	})
}
