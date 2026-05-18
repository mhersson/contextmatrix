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
	backdateDone(ctx, t, svc, project, "a1", now.Add(-2*24*time.Hour))
	backdateDone(ctx, t, svc, project, "a2", now.Add(-5*24*time.Hour))
	backdateDone(ctx, t, svc, project, "a3", now.Add(-6*24*time.Hour))

	for i := 4; i <= 8; i++ {
		backdateDone(ctx, t, svc, project, fmt.Sprintf("b%d", i), now.Add(-9*24*time.Hour))
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
	backdateDone(ctx, t, svc, project, "old", now.Add(-2*24*time.Hour).Add(-1*time.Hour))
	backdateDone(ctx, t, svc, project, "today-a", now.Add(-1*time.Hour))
	backdateDone(ctx, t, svc, project, "today-b", now.Add(-30*time.Minute))

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
func backdateDone(ctx context.Context, t *testing.T, svc *CardService, project, title string, at time.Time) {
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

// createCardWithUsage creates a card and writes a TokenUsage block to it via
// direct storage update. The plan-given signature uses agent_id and a
// SaveCard method; the real service exposes CreateCard(ctx, project, input)
// and the storage interface uses UpdateCard for in-place writes.
func createCardWithUsage(
	ctx context.Context, t *testing.T, svc *CardService, project, idHint, model string,
	promptTokens, completionTokens int64, costUSD float64,
) string {
	t.Helper()

	card, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title:    "test card " + idHint,
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	refreshed, err := svc.GetCard(ctx, project, card.ID)
	require.NoError(t, err)

	refreshed.TokenUsage = &board.TokenUsage{
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		EstimatedCostUSD: costUSD,
	}
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	return card.ID
}

func TestGetDashboard_ModelCosts_EmptyBoard(t *testing.T) {
	ctx := context.Background()
	svc, project, cleanup := setupDashboardServiceAt(t, time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC))
	t.Cleanup(cleanup)

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)
	assert.Empty(t, data.ModelCosts)
}

func TestGetDashboard_ModelCosts_BucketsByModel(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Two cards on opus, one on haiku, one with no model (-> "unknown").
	createCardWithUsage(ctx, t, svc, project, "opus-1", "claude-opus-4-7", 100, 50, 1.50)
	createCardWithUsage(ctx, t, svc, project, "opus-2", "claude-opus-4-7", 200, 60, 2.00)
	createCardWithUsage(ctx, t, svc, project, "hai-1", "claude-haiku-4-5", 50, 30, 0.10)
	createCardWithUsage(ctx, t, svc, project, "untagged", "", 10, 5, 0.05)

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	byModel := map[string]ModelCost{}
	for _, mc := range data.ModelCosts {
		byModel[mc.Model] = mc
	}

	opus, ok := byModel["claude-opus-4-7"]
	require.True(t, ok, "expected opus row")
	assert.Equal(t, int64(300), opus.PromptTokens)
	assert.Equal(t, int64(110), opus.CompletionTokens)
	assert.InDelta(t, 3.50, opus.EstimatedCostUSD, 1e-9)
	assert.Equal(t, 2, opus.CardCount)

	haiku, ok := byModel["claude-haiku-4-5"]
	require.True(t, ok, "expected haiku row")
	assert.Equal(t, 1, haiku.CardCount)
	assert.InDelta(t, 0.10, haiku.EstimatedCostUSD, 1e-9)

	unknown, ok := byModel["unknown"]
	require.True(t, ok, "expected unknown bucket for untagged card")
	assert.Equal(t, 1, unknown.CardCount)
	assert.InDelta(t, 0.05, unknown.EstimatedCostUSD, 1e-9)
}

func TestGetDashboard_ParentOnlyCounters(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Parent1: in_progress (no parent).
	parent1, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "parent1", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	p1 := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, parent1.ID, PatchCardInput{State: &p1})
	require.NoError(t, err)

	// Parent2: done, updated 3 days ago (counts in CardsCompletedLast7d*).
	parent2, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "parent2", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	p2ip := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, parent2.ID, PatchCardInput{State: &p2ip})
	require.NoError(t, err)

	p2done := board.StateDone
	_, err = svc.PatchCard(ctx, project, parent2.ID, PatchCardInput{State: &p2done})
	require.NoError(t, err)

	p2card, err := svc.GetCard(ctx, project, parent2.ID)
	require.NoError(t, err)

	p2card.Updated = now.Add(-3 * 24 * time.Hour)
	require.NoError(t, svc.store.UpdateCard(ctx, project, p2card))

	// Subtask1: in_progress (has parent = parent1).
	sub1, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "subtask1", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	s1ip := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, sub1.ID, PatchCardInput{State: &s1ip})
	require.NoError(t, err)

	s1card, err := svc.GetCard(ctx, project, sub1.ID)
	require.NoError(t, err)

	s1card.Parent = parent1.ID
	require.NoError(t, svc.store.UpdateCard(ctx, project, s1card))

	// Subtask2: stalled (has parent = parent1).
	sub2, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "subtask2", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	s2ip := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, sub2.ID, PatchCardInput{State: &s2ip})
	require.NoError(t, err)

	s2card, err := svc.GetCard(ctx, project, sub2.ID)
	require.NoError(t, err)

	s2card.Parent = parent1.ID
	s2card.State = board.StateStalled
	// Add a state_changed entry so sparkline reconstruction sees stalled at today's end.
	// Timestamp must be after the PatchCard in_progress entry (which used now = 12:00).
	s2card.ActivityLog = append(s2card.ActivityLog, board.ActivityEntry{
		Agent:     "system",
		Timestamp: now.Add(1 * time.Hour),
		Action:    stateChangedAction,
		Message:   "in_progress -> stalled",
	})
	require.NoError(t, svc.store.UpdateCard(ctx, project, s2card))

	// Subtask3: done today (has parent = parent1, Updated = today).
	sub3, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "subtask3-today", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	s3ip := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, sub3.ID, PatchCardInput{State: &s3ip})
	require.NoError(t, err)

	s3done := board.StateDone
	_, err = svc.PatchCard(ctx, project, sub3.ID, PatchCardInput{State: &s3done})
	require.NoError(t, err)

	s3card, err := svc.GetCard(ctx, project, sub3.ID)
	require.NoError(t, err)

	s3card.Parent = parent1.ID
	s3card.Updated = todayStart.Add(1 * time.Hour) // today
	require.NoError(t, svc.store.UpdateCard(ctx, project, s3card))

	// Subtask4: done 10 days ago (has parent = parent2; counts in CardsCompletedPrior7d but NOT *Parents).
	sub4, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "subtask4-old", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	s4ip := board.StateInProgress
	_, err = svc.PatchCard(ctx, project, sub4.ID, PatchCardInput{State: &s4ip})
	require.NoError(t, err)

	s4done := board.StateDone
	_, err = svc.PatchCard(ctx, project, sub4.ID, PatchCardInput{State: &s4done})
	require.NoError(t, err)

	s4card, err := svc.GetCard(ctx, project, sub4.ID)
	require.NoError(t, err)

	s4card.Parent = parent2.ID
	s4card.Updated = now.Add(-10 * 24 * time.Hour)
	require.NoError(t, svc.store.UpdateCard(ctx, project, s4card))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	// StateCounts includes all cards; StateCountsParents only top-level cards.
	assert.Equal(t, 2, data.StateCounts[board.StateInProgress], "in_progress total")
	assert.Equal(t, 1, data.StateCountsParents[board.StateInProgress], "in_progress parents only")
	assert.Equal(t, 1, data.StateCounts[board.StateStalled], "stalled total")
	assert.Equal(t, 0, data.StateCountsParents[board.StateStalled], "stalled parents only")
	assert.Equal(t, 3, data.StateCounts[board.StateDone], "done total")
	assert.Equal(t, 1, data.StateCountsParents[board.StateDone], "done parents only")

	// Completed counters.
	assert.Equal(t, 1, data.CardsCompletedToday, "completed today total (sub3)")
	assert.Equal(t, 0, data.CardsCompletedTodayParents, "completed today parents (sub3 is a subtask)")
	assert.Equal(t, 2, data.CardsCompletedLast7d, "completed last7d total (sub3 + parent2)")
	assert.Equal(t, 1, data.CardsCompletedLast7dParents, "completed last7d parents (parent2)")
	assert.Equal(t, 1, data.CardsCompletedPrior7d, "completed prior7d total (sub4)")
	assert.Equal(t, 0, data.CardsCompletedPrior7dParents, "completed prior7d parents (sub4 is a subtask)")

	// Sparkline: last slot (index MetricSeriesDays-1) is today.
	lastIdx := MetricSeriesDays - 1
	assert.Equal(t, 2, data.MetricSeries.InFlight[lastIdx], "in_flight today (parent1 + sub1)")
	assert.Equal(t, 1, data.MetricSeries.InFlightParents[lastIdx], "in_flight_parents today (parent1 only)")
	assert.Equal(t, 1, data.MetricSeries.Stalled[lastIdx], "stalled today (sub2)")
	assert.Equal(t, 0, data.MetricSeries.StalledParents[lastIdx], "stalled_parents today (sub2 is subtask)")
	assert.Equal(t, 1, data.MetricSeries.Shipped[lastIdx], "shipped today (sub3)")
	assert.Equal(t, 0, data.MetricSeries.ShippedParents[lastIdx], "shipped_parents today (sub3 is subtask)")
	// ActiveAgents unchanged.
	assert.GreaterOrEqual(t, data.MetricSeries.ActiveAgents[lastIdx], 0)
}

func TestExtractStateChanges_Empty(t *testing.T) {
	card := &board.Card{State: board.StateTodo}
	changes, baseline := extractStateChanges(card)

	assert.Nil(t, changes)
	assert.Empty(t, baseline)
}

func TestExtractStateChanges_IgnoresNonStateChanged(t *testing.T) {
	card := &board.Card{
		State: board.StateTodo,
		ActivityLog: []board.ActivityEntry{
			{Action: "claimed", Message: "by alice"},
			{Action: "released", Message: "stalled"},
			{Action: "progress", Message: "step 1"},
		},
	}
	changes, baseline := extractStateChanges(card)

	assert.Nil(t, changes)
	assert.Empty(t, baseline)
}

func TestExtractStateChanges_SortsAscendingAndSetsBaseline(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	card := &board.Card{
		State: board.StateReview,
		ActivityLog: []board.ActivityEntry{
			// Out-of-order entries to verify sort.
			{Action: stateChangedAction, Timestamp: t0.Add(2 * time.Hour), Message: "in_progress -> review"},
			{Action: stateChangedAction, Timestamp: t0, Message: "todo -> in_progress"},
			{Action: stateChangedAction, Timestamp: t0.Add(time.Hour), Message: "in_progress -> stalled"},
		},
	}
	changes, baseline := extractStateChanges(card)

	require.Len(t, changes, 3)
	assert.Equal(t, "todo", baseline)
	// Sorted ascending.
	assert.True(t, changes[0].ts.Equal(t0))
	assert.True(t, changes[1].ts.Equal(t0.Add(time.Hour)))
	assert.True(t, changes[2].ts.Equal(t0.Add(2*time.Hour)))
	assert.Equal(t, "in_progress", changes[0].to)
	assert.Equal(t, "stalled", changes[1].to)
	assert.Equal(t, "review", changes[2].to)
}

func TestExtractStateChanges_SkipsMalformedMessages(t *testing.T) {
	card := &board.Card{
		ActivityLog: []board.ActivityEntry{
			{Action: stateChangedAction, Message: "no arrow here"},
			{Action: stateChangedAction, Message: "todo -> in_progress"},
		},
	}
	changes, baseline := extractStateChanges(card)

	require.Len(t, changes, 1)
	assert.Equal(t, "todo", baseline)
}

func TestStateAtTimeFromChanges_NoEntriesFallsBackToCardState(t *testing.T) {
	card := &board.Card{State: board.StateInProgress}

	got := stateAtTimeFromChanges(card, nil, "", time.Now())

	assert.Equal(t, board.StateInProgress, got)
}

func TestStateAtTimeFromChanges_ReturnsBaselineWhenQueryBeforeAllEntries(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	changes := []stateChange{
		{ts: t0, from: "todo", to: "in_progress"},
		{ts: t0.Add(time.Hour), from: "in_progress", to: "done"},
	}

	got := stateAtTimeFromChanges(&board.Card{}, changes, "todo", t0.Add(-time.Hour))

	assert.Equal(t, "todo", got)
}

func TestStateAtTimeFromChanges_ReturnsLatestAtOrBeforeT(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	changes := []stateChange{
		{ts: t0, from: "todo", to: "in_progress"},
		{ts: t0.Add(time.Hour), from: "in_progress", to: "stalled"},
		{ts: t0.Add(2 * time.Hour), from: "stalled", to: "in_progress"},
	}

	// Exactly at second entry -> that entry.
	assert.Equal(t, "stalled", stateAtTimeFromChanges(&board.Card{}, changes, "todo", t0.Add(time.Hour)))
	// Between second and third -> still stalled.
	assert.Equal(t, "stalled", stateAtTimeFromChanges(&board.Card{}, changes, "todo", t0.Add(90*time.Minute)))
	// At/after third -> in_progress.
	assert.Equal(t, "in_progress", stateAtTimeFromChanges(&board.Card{}, changes, "todo", t0.Add(2*time.Hour)))
	// Far past -> last entry.
	assert.Equal(t, "in_progress", stateAtTimeFromChanges(&board.Card{}, changes, "todo", t0.Add(24*time.Hour)))
}

func TestStateAtTimeFromChanges_IdenticalTimestampPicksLastInsertedDeterministically(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	// Two changes at the same timestamp — extractStateChanges uses stable
	// sort so insertion order is preserved. After sort, both remain in
	// their original relative order, and sort.Search picks the index where
	// changes[i].ts > t, so the entry just before that index — the LAST
	// of the two — wins. Verify the result is deterministic across runs.
	card := &board.Card{
		ActivityLog: []board.ActivityEntry{
			{Action: stateChangedAction, Timestamp: t0, Message: "todo -> in_progress"},
			{Action: stateChangedAction, Timestamp: t0, Message: "in_progress -> review"},
		},
	}

	changes, baseline := extractStateChanges(card)
	require.Len(t, changes, 2)
	assert.Equal(t, "todo", baseline)

	got := stateAtTimeFromChanges(card, changes, baseline, t0.Add(time.Minute))
	// Both entries are at-or-before t; the later-inserted one wins.
	assert.Equal(t, "review", got)
}
