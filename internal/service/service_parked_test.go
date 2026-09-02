package service

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

func TestReportParked(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "parked card", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	t.Run("rejects a non-owning agent", func(t *testing.T) {
		_, err := svc.ReportParked(ctx, "test-project", card.ID, "intruder", "review parked: attempts cap exhausted without approval")
		assert.ErrorIs(t, err, lock.ErrAgentMismatch)
	})

	t.Run("sets worker_status parked and logs the reason", func(t *testing.T) {
		got, err := svc.ReportParked(ctx, "test-project", card.ID, "agent-1", "review parked: attempts cap exhausted without approval")
		require.NoError(t, err)
		assert.Equal(t, "parked", got.WorkerStatus)

		last := got.ActivityLog[len(got.ActivityLog)-1]
		assert.Equal(t, "parked", last.Action)
		assert.Equal(t, "review parked: attempts cap exhausted without approval", last.Message)
	})
}

func TestReportParked_SurvivesCompletedCallback(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "parked card", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)
	_, err = svc.ReportParked(ctx, "test-project", card.ID, "agent-1", "review parked: no reviewer model is selectable")
	require.NoError(t, err)

	parkedBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "review_parked", "normal"))

	// The container exits 0 after parking; serve posts a plain completed
	// callback. That must not erase the park, but still ends the claim.
	got, err := svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "completed", "")
	require.NoError(t, err)
	assert.Equal(t, "parked", got.WorkerStatus)
	assert.Empty(t, got.AssignedAgent)

	assert.InDelta(t, parkedBase+1,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "review_parked", "normal")), 1e-9,
		"the completed callback after a park is the run's end")

	// A re-trigger replaces the park like any other stale terminal status.
	got, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "queued", "task queued for worker")
	require.NoError(t, err)
	assert.Equal(t, "queued", got.WorkerStatus)
}
