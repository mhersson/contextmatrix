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

func TestStateChange_ReopenFromDoneClearsPhase(t *testing.T) {
	tests := []struct {
		name      string
		from      string
		to        string
		phase     string
		setPhase  string
		allow     []string
		viaPatch  bool
		wantPhase string
	}{
		{name: "done card reopened to todo", from: "done", to: "todo", phase: "done", viaPatch: true, wantPhase: ""},
		{name: "done card reopened by the transition walker", from: "done", to: "todo", phase: "done", wantPhase: ""},
		{name: "card moved to done mid-run then reopened", from: "done", to: "todo", phase: "review", viaPatch: true, wantPhase: ""},
		{name: "done card reopened to in_progress where the board allows it", from: "done", to: "in_progress", phase: "done", allow: []string{"in_progress"}, wantPhase: ""},
		{name: "card relaunched from review keeps its phase", from: "review", to: "todo", phase: "integrate", wantPhase: "integrate"},
		{name: "failed card moved back to todo keeps its phase", from: "in_progress", to: "todo", phase: "execute", viaPatch: true, wantPhase: "execute"},
		{name: "stalled card reset to todo keeps its phase", from: "stalled", to: "todo", phase: "pr_gates", wantPhase: "pr_gates"},
		{name: "cancelled card reopened keeps its phase", from: "not_planned", to: "todo", phase: "execute", viaPatch: true, wantPhase: "execute"},
		{name: "phase set on a card already done is kept", from: "done", to: "done", phase: "pr_gates", setPhase: "done", viaPatch: true, wantPhase: "done"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, cleanup := setupTestWithReview(t)
			defer cleanup()

			ctx := context.Background()

			card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
				Title: "Reopen", Type: "task", Priority: "medium",
			})
			require.NoError(t, err)

			seeded, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)

			seeded.State = tt.from
			seeded.Phase = tt.phase
			seeded.PRUrl = "https://example.test/pr/1"
			require.NoError(t, svc.store.UpdateCard(ctx, "test-project", seeded))

			// The PR URL goes with the phase: a fresh run must not gate the old PR.
			wantPRUrl := seeded.PRUrl
			if tt.wantPhase == "" {
				wantPRUrl = ""
			}

			if tt.allow != nil {
				cfg, err := svc.store.GetProject(ctx, "test-project")
				require.NoError(t, err)

				cfg.Transitions[tt.from] = append(cfg.Transitions[tt.from], tt.allow...)
				require.NoError(t, svc.store.SaveProject(ctx, cfg))
				svc.ClearCaches()
			}

			var got *board.Card

			switch {
			case tt.setPhase != "":
				got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: &tt.setPhase})
			case tt.viaPatch:
				got, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &tt.to})
			default:
				got, err = svc.TransitionTo(ctx, "test-project", card.ID, tt.to)
			}

			require.NoError(t, err)
			assert.Equal(t, tt.to, got.State)
			assert.Equal(t, tt.wantPhase, got.Phase)
			assert.Equal(t, wantPRUrl, got.PRUrl)

			stored, err := svc.store.GetCard(ctx, "test-project", card.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPhase, stored.Phase)
			assert.Equal(t, wantPRUrl, stored.PRUrl)
		})
	}
}
