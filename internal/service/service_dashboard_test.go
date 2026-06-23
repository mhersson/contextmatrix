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
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopChatRunner is a minimal chat.Backend for tests that do not exercise
// the runner path (cold opens, message sends, log streaming).
type noopChatRunner struct{}

func (noopChatRunner) StartChat(_ context.Context, _ chat.StartChatOpts) (string, error) {
	return "noop-container", nil
}

func (noopChatRunner) EndChat(_ context.Context, _ string) error { return nil }

func (noopChatRunner) SendChatMessage(_ context.Context, _, _, _ string) error { return nil }

func (noopChatRunner) StreamLogs(_ context.Context, _ string, _ func(chat.LogEntry)) error {
	return nil
}

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
			board.StateInProgress: {board.StateDone, board.StateTodo, board.StateStalled},
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

// TestGetDashboard_ModelCosts_SameModelTwoBucketsCountsCardOnce verifies that a
// card with two breakdown buckets on the SAME model (different agents) counts
// once in ModelCost.CardCount — matching the legacy once-per-card semantics —
// while tokens and cost still sum across both buckets.
func TestGetDashboard_ModelCosts_SameModelTwoBucketsCountsCardOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	card, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "two agents one model", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	refreshed, err := svc.GetCard(ctx, project, card.ID)
	require.NoError(t, err)

	refreshed.TokenUsage = &board.TokenUsage{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     300,
		CompletionTokens: 150,
		EstimatedCostUSD: 0.30,
	}
	refreshed.UsageBreakdown = []board.UsageBucket{
		{
			Agent:            "cmx-agent-a",
			Model:            "claude-sonnet-4-6",
			PromptTokens:     100,
			CompletionTokens: 50,
			CostUSD:          0.10,
			CostSource:       "estimated",
		},
		{
			Agent:            "cmx-agent-b",
			Model:            "claude-sonnet-4-6",
			PromptTokens:     200,
			CompletionTokens: 100,
			CostUSD:          0.20,
			CostSource:       "estimated",
		},
	}
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	byModel := map[string]ModelCost{}
	for _, mc := range data.ModelCosts {
		byModel[mc.Model] = mc
	}

	sonnet, ok := byModel["claude-sonnet-4-6"]
	require.True(t, ok, "expected sonnet row")
	assert.Equal(t, 1, sonnet.CardCount, "one card must count once even with two buckets on the model")
	assert.Equal(t, int64(300), sonnet.PromptTokens)
	assert.Equal(t, int64(150), sonnet.CompletionTokens)
	assert.InDelta(t, 0.30, sonnet.EstimatedCostUSD, 1e-9)

	// Each agent still gets its own row with CardCount 1.
	byAgent := map[string]AgentCost{}
	for _, ac := range data.AgentCosts {
		byAgent[ac.AgentID] = ac
	}

	assert.Equal(t, 1, byAgent["cmx-agent-a"].CardCount)
	assert.Equal(t, 1, byAgent["cmx-agent-b"].CardCount)
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

	// Set parent via direct storage update before transitioning to stalled,
	// so the parent field is in place. The state transition itself goes through
	// PatchCard (in_progress → stalled is now in the test project's transitions).
	s2card, err := svc.GetCard(ctx, project, sub2.ID)
	require.NoError(t, err)

	s2card.Parent = parent1.ID
	require.NoError(t, svc.store.UpdateCard(ctx, project, s2card))

	s2stalled := board.StateStalled
	_, err = svc.PatchCard(ctx, project, sub2.ID, PatchCardInput{State: &s2stalled})
	require.NoError(t, err)

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

// TestGetDashboard_CostSeries30d_5dAgo verifies a card updated 5 days ago
// contributes to Last30d and lands at series30d index 24 (29 - 5).
func TestGetDashboard_CostSeries30d_5dAgo(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	cardID := createCardWithUsage(ctx, t, svc, project, "recent", "claude-sonnet-4-6", 100, 50, 1.50)

	refreshed, err := svc.GetCard(ctx, project, cardID)
	require.NoError(t, err)

	refreshed.Updated = now.Add(-5 * 24 * time.Hour)
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 1.50, data.TotalCostUSDLast30d, 1e-9, "should contribute to last30d")
	assert.InDelta(t, 0.0, data.TotalCostUSDPrior30d, 1e-9, "should not contribute to prior30d")
	require.Len(t, data.CostSeries30d, 30, "series must be 30 elements")
	assert.InDelta(t, 1.50, data.CostSeries30d[24], 1e-9, "index 24 = 29-5 days ago")
	// All other buckets must be zero.
	for i, v := range data.CostSeries30d {
		if i != 24 {
			assert.InDelta(t, 0.0, v, 1e-9, "bucket %d should be zero", i)
		}
	}
}

// TestGetDashboard_CostSeries30d_35dAgo verifies a card updated 35 days ago
// contributes only to Prior30d.
func TestGetDashboard_CostSeries30d_35dAgo(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	cardID := createCardWithUsage(ctx, t, svc, project, "prior", "claude-sonnet-4-6", 100, 50, 2.00)

	refreshed, err := svc.GetCard(ctx, project, cardID)
	require.NoError(t, err)

	refreshed.Updated = now.Add(-35 * 24 * time.Hour)
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, data.TotalCostUSDLast30d, 1e-9, "should not be in last30d")
	assert.InDelta(t, 2.00, data.TotalCostUSDPrior30d, 1e-9, "should be in prior30d")
	require.Len(t, data.CostSeries30d, 30)

	for i, v := range data.CostSeries30d {
		assert.InDelta(t, 0.0, v, 1e-9, "series bucket %d should be zero for 35d-old card", i)
	}
}

// TestGetDashboard_CostSeries30d_65dAgo verifies a card updated 65 days ago
// is excluded from all three accumulators.
func TestGetDashboard_CostSeries30d_65dAgo(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	cardID := createCardWithUsage(ctx, t, svc, project, "old", "claude-sonnet-4-6", 100, 50, 3.00)

	refreshed, err := svc.GetCard(ctx, project, cardID)
	require.NoError(t, err)

	refreshed.Updated = now.Add(-65 * 24 * time.Hour)
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, data.TotalCostUSDLast30d, 1e-9)
	assert.InDelta(t, 0.0, data.TotalCostUSDPrior30d, 1e-9)
	require.Len(t, data.CostSeries30d, 30)

	for i, v := range data.CostSeries30d {
		assert.InDelta(t, 0.0, v, 1e-9, "bucket %d should be zero for 65d-old card", i)
	}
}

// TestGetDashboard_CostSeries30d_NilTokenUsage verifies cards with nil
// TokenUsage are entirely ignored.
func TestGetDashboard_CostSeries30d_NilTokenUsage(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Create a card without usage (nil TokenUsage by default).
	_, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title: "no usage card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, data.TotalCostUSDLast30d, 1e-9)
	assert.InDelta(t, 0.0, data.TotalCostUSDPrior30d, 1e-9)
	require.Len(t, data.CostSeries30d, 30)

	for _, v := range data.CostSeries30d {
		assert.InDelta(t, 0.0, v, 1e-9)
	}
}

// TestGetDashboard_CostSeries30d_SeriesBoundary verifies a card updated
// exactly at dayStarts[0] (oldest bucket start) lands at series[0].
func TestGetDashboard_CostSeries30d_SeriesBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// dayStarts[0] = todayStart - 29*24h
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dayStart0 := todayStart.Add(-29 * 24 * time.Hour)

	cardID := createCardWithUsage(ctx, t, svc, project, "boundary", "claude-sonnet-4-6", 100, 50, 0.75)

	refreshed, err := svc.GetCard(ctx, project, cardID)
	require.NoError(t, err)

	refreshed.Updated = dayStart0 // exactly at the start of the oldest bucket
	require.NoError(t, svc.store.UpdateCard(ctx, project, refreshed))

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	require.Len(t, data.CostSeries30d, 30)
	assert.InDelta(t, 0.75, data.CostSeries30d[0], 1e-9, "card at dayStarts[0] must land at index 0")
	assert.InDelta(t, 0.75, data.TotalCostUSDLast30d, 1e-9, "should be in last30d")
	assert.InDelta(t, 0.0, data.TotalCostUSDPrior30d, 1e-9, "should not be in prior30d")
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

// stubChatCostSummarizer is a test double that returns fixed values.
type stubChatCostSummarizer struct {
	last30d   float64
	prior30d  float64
	series30d []float64
	err       error
	calls     int
}

func (s *stubChatCostSummarizer) GetChatCostSummary(_ context.Context) (float64, float64, []float64, error) {
	s.calls++

	return s.last30d, s.prior30d, s.series30d, s.err
}

func TestGetDashboard_ChatCostSummarizer_PopulatesFields(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	stub := &stubChatCostSummarizer{
		last30d:   12.50,
		prior30d:  8.00,
		series30d: make([]float64, 30),
	}
	stub.series30d[29] = 12.50

	svc.SetChatCostSummarizer(stub)

	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 12.50, data.ChatCostUSDLast30d, 1e-9, "ChatCostUSDLast30d should match stub")
	assert.InDelta(t, 8.00, data.ChatCostUSDPrior30d, 1e-9, "ChatCostUSDPrior30d should match stub")
	require.Len(t, data.ChatCostSeries30d, 30)
	assert.InDelta(t, 12.50, data.ChatCostSeries30d[29], 1e-9, "ChatCostSeries30d[29] should match stub")
	assert.Equal(t, 1, stub.calls, "GetChatCostSummary must be called exactly once")
}

func TestGetDashboard_ChatCostSummarizer_ErrorFallsBackToZero(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	stub := &stubChatCostSummarizer{
		err: fmt.Errorf("injected chat cost error"),
	}
	svc.SetChatCostSummarizer(stub)

	// GetDashboard must succeed despite the summarizer error.
	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err, "GetDashboard must not propagate chat cost error")

	// Chat cost fields must be zero.
	assert.InDelta(t, 0.0, data.ChatCostUSDLast30d, 1e-9, "ChatCostUSDLast30d must be zero on error")
	assert.InDelta(t, 0.0, data.ChatCostUSDPrior30d, 1e-9, "ChatCostUSDPrior30d must be zero on error")
	assert.Nil(t, data.ChatCostSeries30d, "ChatCostSeries30d must be nil on error")
}

func TestGetDashboard_NilChatCostSummarizer_ReturnsZero(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// No SetChatCostSummarizer call — chatCostSummarizer is nil.
	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, data.ChatCostUSDLast30d, 1e-9)
	assert.InDelta(t, 0.0, data.ChatCostUSDPrior30d, 1e-9)
	assert.Nil(t, data.ChatCostSeries30d)
}

// concurrentSafeSummarizer is a minimal ChatCostSummarizer that is safe for
// concurrent use. It returns fixed zero values and has no mutable fields —
// used only to exercise the atomic load/store path without a racy counter.
type concurrentSafeSummarizer struct {
	last30d   float64
	prior30d  float64
	series30d []float64
}

func (c *concurrentSafeSummarizer) GetChatCostSummary(_ context.Context) (float64, float64, []float64, error) {
	// Return copies so callers cannot race on the backing array.
	return c.last30d, c.prior30d, append([]float64(nil), c.series30d...), nil
}

// TestSetChatCostSummarizer_ConcurrentReadWrite exercises SetChatCostSummarizer
// and GetDashboard concurrently to confirm there is no data race under -race.
// The test does not assert specific values — correctness is secondary to the
// absence of a race-detector report.
func TestSetChatCostSummarizer_ConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Use a concurrency-safe stub so the race detector reports only real
	// production-code races, not test-double counter increments.
	stub := &concurrentSafeSummarizer{
		last30d:   5.00,
		prior30d:  3.00,
		series30d: make([]float64, 30),
	}

	const goroutines = 20

	ready := make(chan struct{})
	done := make(chan struct{})

	// Writers: repeatedly call SetChatCostSummarizer.
	for range goroutines / 2 {
		go func() {
			<-ready

			for {
				select {
				case <-done:
					return
				default:
					svc.SetChatCostSummarizer(stub)
				}
			}
		}()
	}

	// Readers: repeatedly call GetDashboard.
	for range goroutines / 2 {
		go func() {
			<-ready

			for {
				select {
				case <-done:
					return
				default:
					_, _ = svc.GetDashboard(ctx, project)
				}
			}
		}()
	}

	close(ready)
	time.Sleep(50 * time.Millisecond)
	close(done)
}

// TestSetChatCostSummarizer_NilDisablesBranch confirms that storing nil via
// SetChatCostSummarizer restores the zero-value fallback behaviour.
func TestSetChatCostSummarizer_NilDisablesBranch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	stub := &stubChatCostSummarizer{last30d: 9.99, prior30d: 4.44, series30d: make([]float64, 30)}
	svc.SetChatCostSummarizer(stub)

	// First call — summarizer is active.
	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)
	assert.InDelta(t, 9.99, data.ChatCostUSDLast30d, 1e-9, "summarizer active: should see stub value")

	// Now disable by passing nil.
	svc.SetChatCostSummarizer(nil)

	data, err = svc.GetDashboard(ctx, project)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, data.ChatCostUSDLast30d, 1e-9, "summarizer nil: should fall back to zero")
	assert.Nil(t, data.ChatCostSeries30d, "summarizer nil: ChatCostSeries30d must be nil")
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

// TestGetChatCostSummary_DeletePreservesCost guards the invariant introduced by
// CTXMAX-604: after Manager.DeleteSession, the deleted session's estimated cost
// must still appear in GetChatCostSummary because DeleteSession archives the
// cost columns into chat_cost_archive and AggregateCost UNIONs both tables.
func TestGetChatCostSummary_DeletePreservesCost(t *testing.T) {
	ctx := context.Background()

	// Open a real SQLite store backed by a temp directory.
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	realStore, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = realStore.Close() })

	// Fix the clock so "now - 1h" is well inside the 30-day window.
	now := time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)
	clk := clock.Fake(now)

	// Build the first manager — used to capture the baseline and delete the session.
	mgr := chat.NewManager(chat.Config{
		Store:   realStore,
		Backend: noopChatRunner{},
		Clock:   clk,
		IdleTTL: time.Hour,
	})

	// Seed a cold session whose last_active falls inside the 30-day window.
	sessID := chat.NewID()
	require.NoError(t, realStore.CreateSession(ctx, chat.Session{
		ID:         sessID,
		Title:      "preserve-cost-test",
		Status:     chat.StatusCold,
		CreatedAt:  now.Add(-1 * time.Hour),
		LastActive: now.Add(-1 * time.Hour),
		CreatedBy:  "human:test",
	}))

	// Increment cost so estimated_cost_usd is non-zero.
	_, _, _, _, _, err = realStore.IncrementSessionCost(ctx, sessID, 100, 50, 0, 0, 1.50, "claude-sonnet-4-6")
	require.NoError(t, err)

	// Capture baseline: the cost must appear before deletion.
	baseline, _, _, err := mgr.GetChatCostSummary(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 1.50, baseline, 1e-9, "baseline last30d must include the seeded session cost")

	// Delete the session — CTXMAX-604 archives cost into chat_cost_archive.
	require.NoError(t, mgr.DeleteSession(ctx, sessID))

	// Build a fresh manager on the same store to bypass the 30s costCache TTL.
	// A zero costCache on the new manager forces a re-query from the store.
	mgr2 := chat.NewManager(chat.Config{
		Store:   realStore,
		Backend: noopChatRunner{},
		Clock:   clk,
		IdleTTL: time.Hour,
	})

	// The cost must survive deletion via the UNION ALL over chat_cost_archive.
	afterDelete, _, _, err := mgr2.GetChatCostSummary(ctx)
	require.NoError(t, err)
	assert.InDelta(t, baseline, afterDelete, 1e-9,
		"deleted session cost must persist via chat_cost_archive UNION branch")
}
