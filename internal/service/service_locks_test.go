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
