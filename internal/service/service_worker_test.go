package service

import (
	"cmp"
	"context"
	"testing"
	"time"

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

// TestFailLostWorker pins the guard: only a queued or running worker on a card
// unwritten since the cutoff, and not claimed through another instance, fails.
func TestFailLostWorker(t *testing.T) {
	const message = "worker lost in test"

	claimedHere := func(status string) func(*testing.T, *CardService, string) {
		return func(t *testing.T, svc *CardService, id string) {
			t.Helper()

			_, err := svc.ClaimCard(context.Background(), "test-project", id, "agent-1")
			require.NoError(t, err)
			_, err = svc.UpdateWorkerStatus(context.Background(), "test-project", id, status, "seed")
			require.NoError(t, err)
		}
	}

	tests := []struct {
		name   string
		seed   func(*testing.T, *CardService, string)
		cutoff time.Duration // offset from the seeded card's Updated
		wrote  bool
	}{
		{"queued, unwritten since cutoff", claimedHere("queued"), time.Minute, true},
		{"running, unwritten since cutoff", claimedHere("running"), time.Minute, true},
		{"written after cutoff", claimedHere("queued"), -time.Minute, false},
		{"completed worker", func(t *testing.T, svc *CardService, id string) {
			t.Helper()

			_, err := svc.UpdateWorkerStatus(context.Background(), "test-project", id, "completed", "seed")
			require.NoError(t, err)
		}, time.Minute, false},
		{"claimed through another instance", func(t *testing.T, svc *CardService, id string) {
			t.Helper()

			writeForeignClaim(t, svc, id, svc.clk.Now(), 1)
		}, time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, cleanup := newSharedService(t, 30*time.Minute)
			defer cleanup()

			ctx := context.Background()
			card := createShared(t, svc)
			tt.seed(t, svc, card.ID)

			before, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)

			wrote, err := svc.FailLostWorker(ctx, "test-project", card.ID, before.Updated.Add(tt.cutoff), message)
			require.NoError(t, err)
			assert.Equal(t, tt.wrote, wrote)

			after, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)

			if !tt.wrote {
				assert.Equal(t, before, after, "a refused write changes nothing")

				return
			}

			assert.Equal(t, "failed", after.WorkerStatus)
			assert.Empty(t, after.AssignedAgent, "the claim held here is cleared")
			require.NotEmpty(t, after.ActivityLog)

			last := after.ActivityLog[len(after.ActivityLog)-1]
			assert.Equal(t, "worker_status", last.Action)
			assert.Equal(t, message, last.Message)
		})
	}
}

// TestLiveWorkerCards returns exactly the queued and running cards of every project.
func TestLiveWorkerCards(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	other, err := svc.CreateProject(ctx, validCreateProjectInput())
	require.NoError(t, err)

	seed := func(project, status string) string {
		card, err := svc.CreateCard(ctx, project, CreateCardInput{Title: "worker " + status, Type: "task", Priority: "medium"})
		require.NoError(t, err)

		if status != "" {
			_, err = svc.UpdateWorkerStatus(ctx, project, card.ID, status, "seed")
			require.NoError(t, err)
		}

		return project + "/" + card.ID
	}

	want := []string{
		seed(testProjectName, "queued"),
		seed(testProjectName, "running"),
		seed(other.Name, "running"),
	}

	seed(testProjectName, "")
	seed(testProjectName, "failed")
	seed(other.Name, "parked")
	seed(other.Name, "killed")

	cards, err := svc.LiveWorkerCards(ctx)
	require.NoError(t, err)

	got := make([]string, 0, len(cards))
	for _, c := range cards {
		got = append(got, c.Project+"/"+c.ID)
	}

	assert.ElementsMatch(t, want, got)
}
