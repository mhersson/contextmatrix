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
// (maybeTransitionParent), leaving the parent in_progress + UNCLAIMED — exactly
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
// never covers it — the parent carries no claim — so without this sweep it is
// stuck forever.
func TestProcessAbandonedParents_ReapsStuckParent(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	parent, _ := setupParentWithSubtask(t, svc, board.StateDone)

	// The run died: parent sits in_progress + unclaimed with no active subtask.
	// Advance well past the stall timeout so it counts as abandoned.
	fake.Advance(10 * svc.lock.Timeout())
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
	fake.Advance(10 * svc.lock.Timeout())
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
// validateStalledCardFn call from markCardStalled — without the gate, the
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

	// Card must NOT have been persisted with state=stalled — the validator
	// rejection short-circuits UpdateCard. The pre-stall claim must remain.
	got, err := svc.store.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.NotEqual(t, board.StateStalled, got.State, "validator rejection must gate persist")
	assert.Equal(t, "claimed-agent", got.AssignedAgent, "claim must remain when stall is blocked")
	assert.NotNil(t, got.LastHeartbeat, "heartbeat must remain when stall is blocked")
}
