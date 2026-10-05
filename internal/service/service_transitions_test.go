package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestStateChange_RerunClearsStaleWorkerFailure(t *testing.T) {
	tests := []struct {
		name       string
		from       string
		to         string
		status     string
		viaPatch   bool
		wantStatus string
	}{
		{name: "failed card moved back to todo", from: "in_progress", to: "todo", status: "failed", viaPatch: true, wantStatus: ""},
		{name: "killed card moved back to todo", from: "in_progress", to: "todo", status: "killed", viaPatch: true, wantStatus: ""},
		{name: "stalled failed card resumed", from: "stalled", to: "in_progress", status: "failed", wantStatus: ""},
		{name: "stalled failed card reset to todo", from: "stalled", to: "todo", status: "failed", wantStatus: ""},
		{name: "failed card moved to done keeps the failure", from: "in_progress", to: "done", status: "failed", viaPatch: true, wantStatus: "failed"},
		{name: "live worker survives a move to todo", from: "in_progress", to: "todo", status: "running", viaPatch: true, wantStatus: "running"},
		{name: "parked card survives a move to todo", from: "in_progress", to: "todo", status: "parked", viaPatch: true, wantStatus: "parked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, cleanup := setupTest(t)
			defer cleanup()

			ctx := context.Background()

			card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
				Title: "Rerun", Type: "task", Priority: "medium",
			})
			require.NoError(t, err)

			seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)

			seeded.State = tt.from
			seeded.WorkerStatus = tt.status
			require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

			var got *board.Card
			if tt.viaPatch {
				got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &tt.to})
			} else {
				got, err = svc.TransitionTo(ctx, "test-project", card.ID, tt.to)
			}

			require.NoError(t, err)
			assert.Equal(t, tt.to, got.State)
			assert.Equal(t, tt.wantStatus, got.WorkerStatus)

			stored, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, stored.WorkerStatus)
		})
	}
}
