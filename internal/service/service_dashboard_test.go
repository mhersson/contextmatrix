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

func TestGetDashboard_InFlightSparkline_FromStateChanged(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Card A: created 6 days ago, transitioned to in_progress 5 days ago,
	// still in_progress today. Should count for in_flight on days -5..0.
	cardA, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "active long", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	refreshA, err := svc.GetCard(ctx, project, cardA.ID)
	require.NoError(t, err)
	refreshA.Created = now.Add(-6 * 24 * time.Hour)
	refreshA.State = board.StateInProgress
	refreshA.Updated = now.Add(-5 * 24 * time.Hour)
	refreshA.ActivityLog = []board.ActivityEntry{
		{
			Agent:     "human:test",
			Timestamp: now.Add(-5 * 24 * time.Hour),
			Action:    stateChangedAction,
			Message:   "todo -> in_progress",
		},
	}
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshA))

	// Card B: created 3 days ago, transitioned to in_progress 2 days ago,
	// then to done 1 day ago. Should count for in_flight only on day -2.
	cardB, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "shipped quick", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	refreshB, err := svc.GetCard(ctx, project, cardB.ID)
	require.NoError(t, err)
	refreshB.Created = now.Add(-3 * 24 * time.Hour)
	refreshB.State = board.StateDone
	refreshB.Updated = now.Add(-1 * 24 * time.Hour)
	refreshB.ActivityLog = []board.ActivityEntry{
		{
			Agent:     "human:test",
			Timestamp: now.Add(-2 * 24 * time.Hour),
			Action:    stateChangedAction,
			Message:   "todo -> in_progress",
		},
		{
			Agent:     "human:test",
			Timestamp: now.Add(-1 * 24 * time.Hour),
			Action:    stateChangedAction,
			Message:   "in_progress -> done",
		},
	}
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshB))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	// Sample indexes: 0 = day-7, ..., 7 = today.
	// Card A was in_progress on days -5..0 (indexes 2..7).
	// Card B was in_progress only on day -2 (index 5).
	for i := 0; i <= 1; i++ {
		assert.Equal(t, 0, data.MetricSeries.InFlight[i], "in_flight[%d]", i)
	}
	for i := 2; i <= 4; i++ {
		assert.Equal(t, 1, data.MetricSeries.InFlight[i], "in_flight[%d]", i)
	}
	assert.Equal(t, 2, data.MetricSeries.InFlight[5], "in_flight[5] (both cards in_progress)")
	for i := 6; i <= 7; i++ {
		assert.Equal(t, 1, data.MetricSeries.InFlight[i], "in_flight[%d]", i)
	}
}

func TestGetDashboard_ShippedSparkline(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Place 1 card on day-2 (i.e. 2 days ago) and 2 cards on today.
	// Using 11:00 keeps the timestamps inside the day buckets regardless
	// of small drift from now (12:00).
	backdateDone(t, ctx, svc, project, "old", now.Add(-2*24*time.Hour).Add(-1*time.Hour))
	backdateDone(t, ctx, svc, project, "today-a", now.Add(-1*time.Hour))
	backdateDone(t, ctx, svc, project, "today-b", now.Add(-30*time.Minute))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	require.Len(t, data.MetricSeries.Shipped, MetricSeriesDays)
	// today is the last slot
	assert.Equal(t, 2, data.MetricSeries.Shipped[MetricSeriesDays-1])
	// 2 days ago is the third-to-last slot
	assert.Equal(t, 1, data.MetricSeries.Shipped[MetricSeriesDays-3])
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
