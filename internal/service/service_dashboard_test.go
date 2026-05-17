package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDashboard_ShippedWindowCounts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// 3 cards shipped in last 7d, 5 shipped in the prior 7d (8-14d ago).
	backdateDone(t, ctx, svc, project, "a1", now.Add(-2*24*time.Hour))
	backdateDone(t, ctx, svc, project, "a2", now.Add(-5*24*time.Hour))
	backdateDone(t, ctx, svc, project, "a3", now.Add(-6*24*time.Hour))
	for i := 4; i <= 8; i++ {
		backdateDone(t, ctx, svc, project, fmt.Sprintf("b%d", i), now.Add(-9*24*time.Hour))
	}

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)
	assert.Equal(t, 3, data.CardsCompletedLast7d)
	assert.Equal(t, 5, data.CardsCompletedPrior7d)
}

// setupDashboardServiceAt creates a CardService with a FakeClock pinned to
// now, plus a single test project. The returned cleanup closes the commit queue.
func setupDashboardServiceAt(t *testing.T, now time.Time) (*CardService, string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	project := "dash-project"
	projectDir := filepath.Join(boardsDir, project)
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

	cfg := &board.ProjectConfig{
		Name:       project,
		Prefix:     "D",
		NextID:     1,
		States:     []string{board.StateTodo, board.StateInProgress, board.StateDone, board.StateStalled, board.StateNotPlanned},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			board.StateTodo:       {board.StateInProgress, board.StateNotPlanned},
			board.StateInProgress: {board.StateDone, board.StateTodo},
			board.StateDone:       {board.StateTodo},
			board.StateStalled:    {board.StateTodo, board.StateInProgress},
			board.StateNotPlanned: {board.StateTodo},
		},
	}
	require.NoError(t, board.SaveProjectConfig(projectDir, cfg))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	p, err := githubauth.NewPATProvider("test-token")
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", p)
	require.NoError(t, err)

	fake := clock.Fake(now)
	bus := events.NewBus()
	lockMgr := lock.NewManagerWithClock(store, 30*time.Minute, fake)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	commitQueue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(commitQueue)

	cleanup := func() {
		_ = commitQueue.Close(context.Background())
	}

	return svc, project, cleanup
}

// backdateDone creates a card, transitions it to done, then rewrites its
// Updated timestamp to at via direct storage update.
func backdateDone(t *testing.T, ctx context.Context, svc *CardService, project, title string, at time.Time) {
	t.Helper()

	card, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: title, Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	inProgress := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	done := board.StateDone
	_, err = svc.PatchCard(ctx, project, card.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	refreshed, err := svc.GetCard(ctx, project, card.ID)
	require.NoError(t, err)

	refreshed.Updated = at
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))
}
