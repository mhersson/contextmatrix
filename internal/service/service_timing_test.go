package service

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/lock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCardTiming_ClaimsAndReleases(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	c, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "Timing", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	assert.Nil(t, c.WorkedSeconds, "never claimed")

	_, err = svc.ClaimCard(ctx, c.Project, c.ID, "worker-a")
	require.NoError(t, err)
	// The holder's re-claim is liveness, not a second interval.
	_, err = svc.ClaimCard(ctx, c.Project, c.ID, "worker-a")
	require.NoError(t, err)

	c, err = svc.GetCard(ctx, c.Project, c.ID)
	require.NoError(t, err)
	require.Len(t, c.WorkIntervals, 1)
	require.NotNil(t, c.WorkingSince)
	assert.NotNil(t, c.WorkedSeconds)
	assert.Nil(t, c.UnclaimedSince)

	_, err = svc.ReleaseCard(ctx, c.Project, c.ID, "worker-a")
	require.NoError(t, err)

	c, err = svc.GetCard(ctx, c.Project, c.ID)
	require.NoError(t, err)
	require.Len(t, c.WorkIntervals, 1)
	require.NotNil(t, c.WorkIntervals[0].To)
	assert.Nil(t, c.WorkingSince)
	assert.NotNil(t, c.UnclaimedSince)

	// A terminal worker status ends the next claim too.
	_, err = svc.ClaimCard(ctx, c.Project, c.ID, "worker-b")
	require.NoError(t, err)
	_, err = svc.UpdateWorkerStatus(ctx, c.Project, c.ID, "completed", "")
	require.NoError(t, err)

	c, err = svc.GetCard(ctx, c.Project, c.ID)
	require.NoError(t, err)
	require.Len(t, c.WorkIntervals, 2)
	assert.NotNil(t, c.WorkIntervals[1].To)
	assert.Empty(t, c.AssignedAgent)
}

func TestCardTiming_EveryWayAClaimEndsIsRecorded(t *testing.T) {
	tests := []struct {
		name string
		end  func(t *testing.T, svc *CardService, c *board.Card) error
	}{
		{
			name: "force release",
			end: func(t *testing.T, svc *CardService, c *board.Card) error {
				t.Helper()
				_, err := svc.ForceReleaseCard(t.Context(), c.Project, c.ID, "human:tester")

				return err
			},
		},
		{
			name: "not planned",
			end: func(t *testing.T, svc *CardService, c *board.Card) error {
				t.Helper()

				state := board.StateNotPlanned
				_, err := svc.PatchCard(t.Context(), c.Project, c.ID, PatchCardInput{State: &state, AgentID: "worker"})

				return err
			},
		},
		{
			name: "stall",
			end: func(t *testing.T, svc *CardService, c *board.Card) error {
				t.Helper()

				old := svc.clk.Now().Add(-time.Hour)
				c.LastHeartbeat = &old
				require.NoError(t, svc.store.UpdateCard(t.Context(), c.Project, c))

				return svc.markCardStalled(t.Context(), lock.StalledCard{Project: c.Project, Card: c})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			ctx := t.Context()

			c, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "Release path", Type: "task", Priority: "medium"})
			require.NoError(t, err)
			c, err = svc.ClaimCard(ctx, c.Project, c.ID, "worker")
			require.NoError(t, err)

			require.NoError(t, tt.end(t, svc, c))

			c, err = svc.GetCard(ctx, c.Project, c.ID)
			require.NoError(t, err)
			assert.Empty(t, c.AssignedAgent)
			require.Len(t, c.WorkIntervals, 1)
			assert.Equal(t, "worker", c.WorkIntervals[0].Agent)
			assert.NotNil(t, c.WorkIntervals[0].To)
			assert.NotNil(t, c.WorkedSeconds)
		})
	}
}

func TestUndoClaimWrite_DropsTheClaimEntry(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	snapshot, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "Rollback", Type: "task", Priority: "medium"})
	require.NoError(t, err)
	written, err := svc.ClaimCard(ctx, snapshot.Project, snapshot.ID, "worker")
	require.NoError(t, err)

	require.NoError(t, svc.undoClaimWrite(ctx, snapshot.Project, snapshot.ID, written, snapshot, "claim undone"))

	got, err := svc.GetCard(ctx, snapshot.Project, snapshot.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AssignedAgent)
	assert.Nil(t, got.WorkedSeconds)
	assert.Empty(t, got.WorkIntervals)

	for _, e := range got.ActivityLog {
		assert.NotEqual(t, board.ClaimedAction, e.Action)
	}
}
