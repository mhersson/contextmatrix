package service

import (
	"cmp"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProjectName = "test-project"

func TestBackendAuthor_DefaultsToNeutralBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	// No SetTaskBackendName call → unset → defaults to "backend".
	assert.Equal(t, "backend", svc.backendAuthor())
}

func TestBackendAuthor_ReflectsConfiguredBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	svc.SetTaskBackendName("agent")
	assert.Equal(t, "agent", svc.backendAuthor())
}

func TestUpdateWorkerStatus_AttributesMessageToActiveBackend(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	svc.SetTaskBackendName("agent")

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	_, err = svc.UpdateWorkerStatus(context.Background(), testProjectName, card.ID, "running", "container started")
	require.NoError(t, err)

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.NotEmpty(t, got.ActivityLog)
	last := got.ActivityLog[len(got.ActivityLog)-1]
	assert.Equal(t, "worker_status", last.Action)
	assert.Equal(t, "agent", last.Agent)
}

// TestMarkQueued_RefusesALiveWorker pins the compare-and-set: queued is
// written only over an idle worker, so two launches cannot both queue a card.
func TestMarkQueued_RefusesALiveWorker(t *testing.T) {
	tests := []struct {
		status  string
		refused bool
	}{
		{"", false},
		{"failed", false},
		{"killed", false},
		{"parked", false},
		{"queued", true},
		{"running", true},
	}

	for _, tt := range tests {
		t.Run("worker "+cmp.Or(tt.status, "idle"), func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			ctx := context.Background()

			card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "queue target", Type: "task", Priority: "medium"})
			require.NoError(t, err)

			if tt.status != "" {
				_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, tt.status, "seed")
				require.NoError(t, err)
			}

			got, err := svc.MarkQueued(ctx, "test-project", card.ID, "task queued for worker")
			if tt.refused {
				require.ErrorIs(t, err, ErrWorkerActive)

				stored, getErr := svc.GetCard(ctx, "test-project", card.ID)
				require.NoError(t, getErr)
				assert.Equal(t, tt.status, stored.WorkerStatus, "a refused write changes nothing")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "queued", got.WorkerStatus)
		})
	}
}
