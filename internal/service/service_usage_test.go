package service

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPriceTokensHelper verifies the cache-tier multiplier arithmetic for the
// package-level PriceTokens helper.
func TestPriceTokensHelper(t *testing.T) {
	t.Parallel()

	rate := ModelRate{Prompt: 0.000003, Completion: 0.000015}

	tests := []struct {
		name        string
		prompt      int64
		cacheRead   int64
		cacheCreate int64
		completion  int64
		wantApprox  float64
	}{
		{
			name:       "prompt only",
			prompt:     1000,
			wantApprox: 1000 * 0.000003,
		},
		{
			name:       "completion only",
			completion: 500,
			wantApprox: 500 * 0.000015,
		},
		{
			name:      "cache read discount",
			cacheRead: 1000,
			// cache_read is billed at 0.10× the prompt rate
			wantApprox: 1000 * 0.000003 * 0.10,
		},
		{
			name:        "cache creation surcharge",
			cacheCreate: 1000,
			// cache_creation is billed at 1.25× the prompt rate
			wantApprox: 1000 * 0.000003 * 1.25,
		},
		{
			name:        "all tiers combined",
			prompt:      1000,
			cacheRead:   2000,
			cacheCreate: 500,
			completion:  300,
			wantApprox: 1000*0.000003 +
				2000*0.000003*0.10 +
				500*0.000003*1.25 +
				300*0.000015,
		},
		{
			name:       "zero tokens",
			wantApprox: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PriceTokens(rate, tc.prompt, tc.cacheRead, tc.cacheCreate, tc.completion)
			assert.InDelta(t, tc.wantApprox, got, 1e-12)
		})
	}
}

// TestCardServicePriceTokens verifies that the CardService.PriceTokens method
// returns (cost, true) for a known model and (0, false) for an unknown one.
func TestCardServicePriceTokens(t *testing.T) {
	t.Parallel()

	svc, _, cleanup := setupTest(t)
	defer cleanup()

	// Inject a known model into tokenCosts so we can assert correct delegation.
	svc.tokenCosts = map[string]ModelRate{
		"test-model": {Prompt: 0.000003, Completion: 0.000015},
	}

	t.Run("known model returns cost and true", func(t *testing.T) {
		t.Parallel()

		cost, ok := svc.PriceTokens("test-model", 1000, 0, 0, 500)
		require.True(t, ok)

		want := PriceTokens(ModelRate{Prompt: 0.000003, Completion: 0.000015}, 1000, 0, 0, 500)
		assert.InDelta(t, want, cost, 1e-12)
	})

	t.Run("unknown model returns zero and false", func(t *testing.T) {
		t.Parallel()

		cost, ok := svc.PriceTokens("not-a-real-model", 1000, 0, 0, 500)
		assert.False(t, ok)
		assert.InDelta(t, 0.0, cost, 1e-12)
	})
}

// byModelOf returns a map from model name to UsageBucket for easy lookup in
// tests. It assumes all buckets belong to a single agent — with multiple
// agents reporting the same model, later buckets would overwrite earlier ones.
func byModelOf(card *board.Card) map[string]board.UsageBucket {
	m := make(map[string]board.UsageBucket, len(card.UsageBreakdown))
	for _, b := range card.UsageBreakdown {
		m[b.Model] = b
	}

	return m
}

// bucketCostSum returns the sum of CostUSD across all UsageBreakdown buckets.
func bucketCostSum(card *board.Card) float64 {
	var total float64
	for _, b := range card.UsageBreakdown {
		total += b.CostUSD
	}

	return total
}

// TestReportUsageBreakdown verifies that report_usage accumulates per-(agent, model)
// buckets, respects actual_cost_usd when provided, and keeps EstimatedCostUSD
// equal to the bucket sum.
func TestReportUsageBreakdown(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Breakdown test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// First call: estimated (model in cost map, no actual cost).
	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-x",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     100,
		CompletionTokens: 50,
	})
	require.NoError(t, err)

	// Second call: actual cost provided, model also in cost map — actual wins.
	cost := 0.42
	got, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-x",
		Model:            "openai/gpt-5.5",
		PromptTokens:     10,
		CompletionTokens: 5,
		ActualCostUSD:    &cost,
	})
	require.NoError(t, err)

	// Two distinct models → two buckets.
	require.Len(t, got.UsageBreakdown, 2)

	byModel := byModelOf(got)
	assert.Equal(t, "estimated", byModel["claude-sonnet-4-6"].CostSource)
	assert.Equal(t, "actual", byModel["openai/gpt-5.5"].CostSource)
	assert.InDelta(t, 0.42, byModel["openai/gpt-5.5"].CostUSD, 1e-9)
	assert.Equal(t, "cmx-agent-x", byModel["openai/gpt-5.5"].Agent)

	// Cumulative cost equals bucket sum.
	assert.InDelta(t, bucketCostSum(got), got.TokenUsage.EstimatedCostUSD, 1e-9)

	// Same (agent, model) again: merged into existing bucket, not appended.
	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-x",
		Model:            "openai/gpt-5.5",
		PromptTokens:     1,
		CompletionTokens: 1,
		ActualCostUSD:    &cost,
	})
	require.NoError(t, err)

	got, err = svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	require.Len(t, got.UsageBreakdown, 2, "same (agent, model) must merge, not append")
	assert.Equal(t, int64(11), byModelOf(got)["openai/gpt-5.5"].PromptTokens)

	// Cumulative TokenUsage cost still equals bucket sum after merge.
	assert.InDelta(t, bucketCostSum(got), got.TokenUsage.EstimatedCostUSD, 1e-9)
}

// TestReportUsageBreakdownStickyActual verifies that a bucket which starts as
// "estimated" flips to "actual" on an actual-cost report and stays "actual"
// on a subsequent estimated report — protecting real spend from rate-table
// recalculation.
func TestReportUsageBreakdownStickyActual(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Sticky actual test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Estimated report first: bucket starts as "estimated".
	got, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-z",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     100,
		CompletionTokens: 50,
	})
	require.NoError(t, err)
	require.Len(t, got.UsageBreakdown, 1)
	assert.Equal(t, "estimated", got.UsageBreakdown[0].CostSource)

	// Actual-cost report on the same (agent, model): flips to "actual".
	cost := 0.10
	got, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-z",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     10,
		CompletionTokens: 5,
		ActualCostUSD:    &cost,
	})
	require.NoError(t, err)
	require.Len(t, got.UsageBreakdown, 1)
	assert.Equal(t, "actual", got.UsageBreakdown[0].CostSource)

	// Subsequent estimated report: stays "actual", tokens and cost still merge.
	got, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-z",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     20,
		CompletionTokens: 10,
	})
	require.NoError(t, err)
	require.Len(t, got.UsageBreakdown, 1)
	assert.Equal(t, "actual", got.UsageBreakdown[0].CostSource,
		"bucket must stay actual once any actual-cost report has landed")
	assert.Equal(t, int64(130), got.UsageBreakdown[0].PromptTokens)
	assert.InDelta(t, bucketCostSum(got), got.TokenUsage.EstimatedCostUSD, 1e-9)
}

// TestReportUsageBreakdownActualUnknownModel verifies that an actual-cost report
// for a model absent from tokenCosts still records the cost in the bucket.
func TestReportUsageBreakdownActualUnknownModel(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Unknown model actual cost",
		Type:     "task",
		Priority: "low",
	})
	require.NoError(t, err)

	cost := 1.23
	got, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-y",
		Model:            "some/brand-new-model",
		PromptTokens:     500,
		CompletionTokens: 200,
		ActualCostUSD:    &cost,
	})
	require.NoError(t, err)

	require.Len(t, got.UsageBreakdown, 1)
	bucket := got.UsageBreakdown[0]
	assert.Equal(t, "actual", bucket.CostSource)
	assert.InDelta(t, 1.23, bucket.CostUSD, 1e-9)
	assert.InDelta(t, 1.23, got.TokenUsage.EstimatedCostUSD, 1e-9)
}

// TestRecalculateCostsSkipsActualBuckets verifies that RecalculateCosts re-prices
// estimated buckets from the rate table but leaves actual buckets byte-identical.
// The cumulative EstimatedCostUSD must equal the bucket sum after recalculation.
func TestRecalculateCostsSkipsActualBuckets(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Recalc bucket test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Seed the card directly: bucket A estimated (model in rate table, cost 0),
	// bucket B actual (cost 0.42). The estimated bucket at CostUSD=0 is what
	// gets re-priced — the breakdown path processes the card regardless of the
	// cumulative EstimatedCostUSD.
	refreshed, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	refreshed.TokenUsage = &board.TokenUsage{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
		EstimatedCostUSD: 0,
	}
	refreshed.UsageBreakdown = []board.UsageBucket{
		{
			Agent:            "cmx-agent-x",
			Model:            "claude-sonnet-4-6",
			PromptTokens:     1000,
			CompletionTokens: 500,
			CostUSD:          0,
			CostSource:       "estimated",
		},
		{
			Agent:            "cmx-agent-x",
			Model:            "openai/gpt-5.5",
			PromptTokens:     100,
			CompletionTokens: 50,
			CostUSD:          0.42,
			CostSource:       "actual",
		},
	}
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", refreshed))

	result, err := svc.RecalculateCosts(ctx, "test-project", "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, 1, result.CardsUpdated)

	after, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	// Bucket A (estimated) must be re-priced from the rate table.
	// claude-sonnet-4-6: Prompt=0.000003, Completion=0.000015
	// 1000*0.000003 + 500*0.000015 = 0.003 + 0.0075 = 0.0105
	wantA := PriceTokens(
		ModelRate{Prompt: 0.000003, Completion: 0.000015},
		1000, 0, 0, 500,
	)

	var bucketA, bucketB board.UsageBucket

	for _, b := range after.UsageBreakdown {
		switch b.CostSource {
		case "estimated":
			bucketA = b
		case "actual":
			bucketB = b
		}
	}

	assert.InDelta(t, wantA, bucketA.CostUSD, 1e-9, "estimated bucket must be re-priced")
	// Bucket B (actual) must remain byte-identical.
	assert.InDelta(t, 0.42, bucketB.CostUSD, 1e-9, "actual bucket must not be modified")

	// Cumulative EstimatedCostUSD must equal the bucket sum.
	wantTotal := bucketA.CostUSD + bucketB.CostUSD
	assert.InDelta(t, wantTotal, after.TokenUsage.EstimatedCostUSD, 1e-9,
		"EstimatedCostUSD must equal bucket sum after recalculation")
}

// TestRecalculateCostsRepricesStaleEstimatedBuckets verifies that an estimated
// bucket with a NON-zero cost is re-priced when the rate table gives a
// different price (e.g. rates were corrected after the usage was reported).
// The actual bucket stays untouched and the cumulative cost equals the bucket
// sum. This also exercises the breakdown path on a card whose cumulative
// EstimatedCostUSD is already non-zero — the legacy "skip costed cards" gate
// must not apply to breakdown cards.
func TestRecalculateCostsRepricesStaleEstimatedBuckets(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Stale estimate test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Seed directly: bucket A estimated with a stale non-zero price, bucket B
	// actual with 0.42. Cumulative cost is non-zero.
	refreshed, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	refreshed.TokenUsage = &board.TokenUsage{
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
		EstimatedCostUSD: 0.999 + 0.42,
	}
	refreshed.UsageBreakdown = []board.UsageBucket{
		{
			Agent:            "cmx-agent-x",
			Model:            "claude-sonnet-4-6",
			PromptTokens:     1000,
			CompletionTokens: 500,
			CostUSD:          0.999, // stale price from an outdated rate table
			CostSource:       "estimated",
		},
		{
			Agent:            "cmx-agent-x",
			Model:            "openai/gpt-5.5",
			PromptTokens:     100,
			CompletionTokens: 50,
			CostUSD:          0.42,
			CostSource:       "actual",
		},
	}
	require.NoError(t, svc.store.UpdateCard(ctx, "test-project", refreshed))

	result, err := svc.RecalculateCosts(ctx, "test-project", "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, 1, result.CardsUpdated)

	after, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	// claude-sonnet-4-6: 1000*0.000003 + 500*0.000015 = 0.0105
	wantA := PriceTokens(
		ModelRate{Prompt: 0.000003, Completion: 0.000015},
		1000, 0, 0, 500,
	)

	var bucketA, bucketB board.UsageBucket

	for _, b := range after.UsageBreakdown {
		switch b.CostSource {
		case "estimated":
			bucketA = b
		case "actual":
			bucketB = b
		}
	}

	assert.InDelta(t, wantA, bucketA.CostUSD, 1e-9,
		"stale estimated bucket must be re-priced from the current rate table")
	assert.InDelta(t, 0.42, bucketB.CostUSD, 1e-9, "actual bucket must not be modified")
	assert.InDelta(t, bucketA.CostUSD+bucketB.CostUSD, after.TokenUsage.EstimatedCostUSD, 1e-9,
		"EstimatedCostUSD must equal bucket sum after recalculation")
}

// TestCostByAgentSurvivesRelease verifies that aggregateCostsByAgentModel reads
// from UsageBreakdown rows rather than card.AssignedAgent, so costs are attributed
// correctly even after the agent is released and AssignedAgent is cleared.
func TestCostByAgentSurvivesRelease(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	svc, project, cleanup := setupDashboardServiceAt(t, now)
	t.Cleanup(cleanup)

	// Inject cost rates so ReportUsage can price the estimated calls.
	svc.tokenCosts = map[string]ModelRate{
		"claude-sonnet-4-6": {Prompt: 0.000003, Completion: 0.000015},
	}

	// Create a card and claim it.
	card, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title:    "Release attribution test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, project, card.ID, "cmx-agent-x")
	require.NoError(t, err)

	// Report usage on two models for the same agent.
	cost := 0.25
	_, err = svc.ReportUsage(ctx, project, card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-x",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
		ActualCostUSD:    &cost,
	})
	require.NoError(t, err)

	cost2 := 0.17
	_, err = svc.ReportUsage(ctx, project, card.ID, ReportUsageInput{
		AgentID:          "cmx-agent-x",
		Model:            "openai/gpt-5.5",
		PromptTokens:     200,
		CompletionTokens: 100,
		ActualCostUSD:    &cost2,
	})
	require.NoError(t, err)

	// Release the card: AssignedAgent is cleared.
	_, err = svc.ReleaseCard(ctx, project, card.ID, "cmx-agent-x")
	require.NoError(t, err)

	// Verify AssignedAgent is empty (this is the precondition for the bug).
	released, err := svc.GetCard(ctx, project, card.ID)
	require.NoError(t, err)
	assert.Empty(t, released.AssignedAgent, "AssignedAgent must be cleared after release")
	require.Len(t, released.UsageBreakdown, 2, "breakdown rows must survive release")

	// GetDashboard exercises aggregateCostsByAgentModel.
	data, err := svc.GetDashboard(ctx, project)
	require.NoError(t, err)

	// Build a lookup map: agent_id → AgentCost.
	byAgent := map[string]AgentCost{}
	for _, ac := range data.AgentCosts {
		byAgent[ac.AgentID] = ac
	}

	// The cost must appear under "cmx-agent-x", not under "unassigned".
	agentRow, ok := byAgent["cmx-agent-x"]
	require.True(t, ok, "cmx-agent-x must appear in AgentCosts even after release")
	assert.InDelta(t, 0.42, agentRow.EstimatedCostUSD, 1e-9,
		"total cost (0.25 + 0.17) must be attributed to cmx-agent-x")

	_, hasUnassigned := byAgent["unassigned"]
	assert.False(t, hasUnassigned,
		"no cost should land in the unassigned bucket when breakdown rows are present")
}
