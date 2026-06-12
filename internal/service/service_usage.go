package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ModelRate defines per-token cost rates for a model.
type ModelRate struct {
	Prompt     float64
	Completion float64
}

const (
	cacheReadMultiplier     = 0.10
	cacheCreationMultiplier = 1.25
)

// PriceTokens computes the estimated cost in USD for a single usage delta using
// the per-tier multipliers for cached tokens.
func PriceTokens(rate ModelRate, prompt, cacheRead, cacheCreation, completion int64) float64 {
	return float64(prompt)*rate.Prompt +
		float64(cacheRead)*rate.Prompt*cacheReadMultiplier +
		float64(cacheCreation)*rate.Prompt*cacheCreationMultiplier +
		float64(completion)*rate.Completion
}

// PriceTokens looks up the model in the service's cost map and, when found,
// delegates to the package-level PriceTokens helper. Returns (cost, true) when
// the model is known and (0, false) when it is not. Chat and other callers use
// this via the chat.Pricer interface.
func (s *CardService) PriceTokens(model string, prompt, cacheRead, cacheCreation, completion int64) (float64, bool) {
	rate, ok := s.tokenCosts[model]
	if !ok {
		return 0, false
	}

	return PriceTokens(rate, prompt, cacheRead, cacheCreation, completion), true
}

// ReportUsageInput contains the fields for reporting token usage on a card.
type ReportUsageInput struct {
	AgentID             string
	Model               string
	PromptTokens        int64
	CompletionTokens    int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	// ActualCostUSD is the authoritative provider-reported cost for this delta.
	// When set it bypasses the rate-table estimate for this call. The bucket
	// records cost_source "actual"; EstimatedCostUSD is incremented by this
	// value rather than the rate-table result.
	ActualCostUSD *float64
}

// ProjectUsage contains aggregated token usage across all cards in a project.
type ProjectUsage struct {
	PromptTokens        int64   `json:"prompt_tokens"`
	CompletionTokens    int64   `json:"completion_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	CardCount           int     `json:"card_count"`
}

// RecalculateCostsResult summarises the outcome of a cost recalculation pass.
//
// TotalCostRecalculated accumulates each updated card's full new cost: for
// breakdown cards that is the complete bucket sum (including untouched actual
// buckets), not the re-pricing delta — consistent with the legacy whole-card
// semantics. Cards that were not written contribute nothing.
type RecalculateCostsResult struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

// ReportUsage increments token usage counters on a card and recalculates cost.
//
// Zero-token calls (PromptTokens=0 and CompletionTokens=0) are intentionally
// written and emit an event. This makes a heartbeat+report_usage pair at idle
// a useful health signal even when no new tokens are consumed — removing this
// write would silently drop that observability.
//
// Multiple calls with different model names cause the stored TokenUsage.Model
// to be overwritten with the most recently reported model. Cost arithmetic is
// unaffected: each delta is priced using the model passed in that call. The
// overwrite ensures that RecalculateCosts — which uses TokenUsage.Model as a
// fallback — always applies the most recent model, which is the correct default
// for the typical single-primary-model-per-card agent pattern.
func (s *CardService) ReportUsage(ctx context.Context, project, id string, input ReportUsageInput) (*board.Card, error) {
	id = strings.ToUpper(id)

	s.writeMu.Lock()

	unlocked := false

	defer func() {
		if !unlocked {
			s.writeMu.Unlock()
		}
	}()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != "" && card.AssignedAgent != input.AgentID {
		return nil, fmt.Errorf("agent authorization: %w", lock.ErrAgentMismatch)
	}

	// Snapshot for rollback on commit failure.
	snapshot, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card snapshot: %w", err)
	}

	if card.TokenUsage == nil {
		card.TokenUsage = &board.TokenUsage{}
	}

	// Store the model name when provided
	if input.Model != "" {
		card.TokenUsage.Model = input.Model
	}

	card.TokenUsage.PromptTokens += input.PromptTokens
	card.TokenUsage.CompletionTokens += input.CompletionTokens
	card.TokenUsage.CacheReadTokens += input.CacheReadTokens
	card.TokenUsage.CacheCreationTokens += input.CacheCreationTokens

	// Calculate cost delta for this report and add to running total.
	// When ActualCostUSD is set it takes precedence over the rate-table estimate.
	// Warn when a model name is provided, no actual cost is given, and the model
	// is not in the rate table.
	var deltaCost float64

	costSource := "estimated"

	if input.ActualCostUSD != nil {
		deltaCost = *input.ActualCostUSD
		costSource = "actual"
	} else if input.Model != "" {
		if rate, ok := s.tokenCosts[input.Model]; ok {
			deltaCost = PriceTokens(rate, input.PromptTokens, input.CacheReadTokens, input.CacheCreationTokens, input.CompletionTokens)
		} else {
			ctxlog.Logger(ctx).Warn("unknown model in cost map, cost not calculated",
				"model", input.Model,
				"card_id", id,
			)
			// Bump the observability counter so ops can alert on unknown models
			// via Prometheus (e.g. contextmatrix_report_usage_unknown_model_total).
			metrics.ReportUsageUnknownModelTotal.WithLabelValues(input.Model).Inc()
		}
	}

	card.TokenUsage.EstimatedCostUSD += deltaCost

	upsertUsageBucket(card, input, deltaCost, costSource)

	card.Updated = s.clk.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Enqueue the commit under writeMu, then release writeMu before awaiting
	// so a slow commit does not stall other concurrent writers. Per-project
	// worker ordering still guarantees in-enqueue-order landing.
	commitDone, notify := s.enqueueCardCommit(ctx, project, id, input.AgentID, "usage reported")

	s.writeMu.Unlock()

	unlocked = true

	if err := s.awaitCommit(commitDone, notify); err != nil {
		s.writeMu.Lock()
		rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
		s.writeMu.Unlock()

		return nil, rollbackErr
	}

	// If the card has no active agent, flush immediately — there is no
	// subsequent ReleaseCard call to flush deferred paths (e.g. report_usage
	// called after complete_task). Re-acquire writeMu because
	// flushDeferredCommit mutates deferredPaths.
	if card.AssignedAgent == "" {
		s.writeMu.Lock()
		flushErr := s.flushDeferredCommit(ctx, id, input.AgentID)
		s.writeMu.Unlock()

		if flushErr != nil {
			ctxlog.Logger(ctx).Error("flush deferred commit on post-release usage report", "card_id", id, "error", flushErr)
		}
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUsageReported,
		Project:   project,
		CardID:    id,
		Agent:     input.AgentID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"prompt_tokens":         input.PromptTokens,
			"completion_tokens":     input.CompletionTokens,
			"cache_read_tokens":     input.CacheReadTokens,
			"cache_creation_tokens": input.CacheCreationTokens,
			"model":                 input.Model,
		},
	})

	s.enrichDependenciesMet(ctx, card)

	return card, nil
}

// AggregateUsage returns total token usage across all cards in a project.
func (s *CardService) AggregateUsage(ctx context.Context, project string) (*ProjectUsage, error) {
	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	usage := &ProjectUsage{}

	for _, card := range cards {
		if card.TokenUsage != nil {
			usage.PromptTokens += card.TokenUsage.PromptTokens
			usage.CompletionTokens += card.TokenUsage.CompletionTokens
			usage.CacheReadTokens += card.TokenUsage.CacheReadTokens
			usage.CacheCreationTokens += card.TokenUsage.CacheCreationTokens
			usage.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
			usage.CardCount++
		}
	}

	return usage, nil
}

// RecalculateCosts recomputes estimated costs for cards.
//
// Cards with UsageBreakdown: every bucket with CostSource "estimated" is
// re-priced from the current rate table — including buckets that already have
// a non-zero cost (stale prices are corrected). Actual-cost buckets are never
// modified; that is what the cost_source flag exists for. The model fallback
// chain per bucket is bucket model → card's stored model → defaultModel; a
// bucket whose model resolves to no rate is left unchanged. EstimatedCostUSD
// is set to the bucket sum. The card is written only when at least one bucket
// price actually changed.
//
// Legacy cards (no breakdown): fill-missing-only, verbatim pre-breakdown
// behavior — only cards with non-zero tokens and a zero EstimatedCostUSD are
// updated; cards that already have a cost are not modified.
func (s *CardService) RecalculateCosts(ctx context.Context, project, defaultModel string) (*RecalculateCostsResult, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	result := &RecalculateCostsResult{}

	var (
		updatedPaths []string
		// snapshots preserves the pre-mutation state of every card we
		// wrote so a failed batch commit can restore cache + disk.
		snapshots []*board.Card
	)

	for _, card := range cards {
		if card.TokenUsage == nil {
			continue
		}

		var (
			cost    float64
			changed bool
		)

		if len(card.UsageBreakdown) > 0 {
			// Breakdown path: re-price every estimated bucket from the
			// current rate table; never touch actual buckets.
			var bucketSum float64

			for i := range card.UsageBreakdown {
				b := &card.UsageBreakdown[i]
				bucketSum += b.CostUSD

				if b.CostSource != "estimated" {
					continue
				}

				// Fallback chain mirrors the legacy path: bucket model →
				// card's stored model → defaultModel parameter.
				model := b.Model
				if model == "" {
					model = card.TokenUsage.Model
				}

				if model == "" {
					model = defaultModel
				}

				rate, ok := s.tokenCosts[model]
				if !ok {
					ctxlog.Logger(ctx).Warn("recalculate_costs: model not in cost map, skipping bucket",
						"model", model,
						"card_id", card.ID,
					)

					continue
				}

				repriced := PriceTokens(rate, b.PromptTokens, b.CacheReadTokens, b.CacheCreationTokens, b.CompletionTokens)
				if repriced != b.CostUSD {
					bucketSum += repriced - b.CostUSD
					b.CostUSD = repriced
					changed = true
				}
			}

			if !changed {
				continue
			}

			cost = bucketSum
			card.TokenUsage.EstimatedCostUSD = cost
		} else {
			// Legacy path: only process cards with tokens but no cost yet.
			if card.TokenUsage.PromptTokens == 0 &&
				card.TokenUsage.CompletionTokens == 0 &&
				card.TokenUsage.CacheReadTokens == 0 &&
				card.TokenUsage.CacheCreationTokens == 0 {
				continue
			}

			if card.TokenUsage.EstimatedCostUSD != 0 {
				continue // already has a cost — don't double-count
			}

			model := card.TokenUsage.Model
			if model == "" {
				model = defaultModel
			}

			rate, ok := s.tokenCosts[model]
			if !ok {
				ctxlog.Logger(ctx).Warn("recalculate_costs: model not in cost map, skipping card",
					"model", model,
					"card_id", card.ID,
				)

				continue
			}

			cost = PriceTokens(rate, card.TokenUsage.PromptTokens, card.TokenUsage.CacheReadTokens, card.TokenUsage.CacheCreationTokens, card.TokenUsage.CompletionTokens)
			card.TokenUsage.EstimatedCostUSD = cost
			changed = true
		}

		// Persist the effective model name so future recalculations are idempotent.
		if card.TokenUsage.Model == "" && defaultModel != "" {
			card.TokenUsage.Model = defaultModel
		}

		// Snapshot before mutating. store.GetCard returns a deep copy
		// independent of the one we are about to write back.
		snapshot, err := s.store.GetCard(ctx, project, card.ID)
		if err != nil {
			return nil, fmt.Errorf("get card snapshot %s: %w", card.ID, err)
		}

		card.Updated = s.clk.Now()

		if err := s.store.UpdateCard(ctx, project, card); err != nil {
			return nil, fmt.Errorf("update card %s: %w", card.ID, err)
		}

		snapshots = append(snapshots, snapshot)
		updatedPaths = append(updatedPaths, s.cardPath(project, card.ID))
		result.CardsUpdated++
		result.TotalCostRecalculated += cost
	}

	// Batch-commit all recalculated cards in a single git commit.
	if s.gitAutoCommit && len(updatedPaths) > 0 {
		msg := fmt.Sprintf("[contextmatrix] %s: recalculated costs for %d cards", project, result.CardsUpdated)
		if err := s.git.CommitFiles(ctx, updatedPaths, msg); err != nil {
			// Batch commit failed: roll each mutated card back to its
			// pre-mutation snapshot so cache + disk stay consistent
			// with git. A partial-rollback failure leaves the cache
			// inconsistent and is reported alongside the commit error.
			var rollbackErrs []error

			for _, snap := range snapshots {
				if rbErr := s.store.UpdateCard(ctx, project, snap); rbErr != nil {
					ctxlog.Logger(ctx).Error("recalculate_costs rollback failed",
						"project", project,
						"card_id", snap.ID,
						"committed", false,
						"rollback_failed", true,
						"commit_error", err,
						"rollback_error", rbErr,
					)
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: %w", snap.ID, rbErr))
				}
			}

			if len(rollbackErrs) > 0 {
				return nil, errors.Join(
					append([]error{fmt.Errorf("git commit recalculated costs (rollback failed, state inconsistent): %w", err)}, rollbackErrs...)...,
				)
			}

			return nil, fmt.Errorf("git commit recalculated costs: %w", err)
		}

		s.notifyCommit()
	}

	return result, nil
}

// upsertUsageBucket merges one report into the card's (agent, model) bucket.
// A bucket that has ever received an actual-cost report stays "actual" —
// mixed-source sums are still real spend, and the flag's job is to protect
// the bucket from rate-table recalculation.
func upsertUsageBucket(card *board.Card, in ReportUsageInput, cost float64, source string) {
	for i := range card.UsageBreakdown {
		b := &card.UsageBreakdown[i]
		if b.Agent == in.AgentID && b.Model == in.Model {
			b.PromptTokens += in.PromptTokens
			b.CompletionTokens += in.CompletionTokens
			b.CacheReadTokens += in.CacheReadTokens
			b.CacheCreationTokens += in.CacheCreationTokens
			b.CostUSD += cost

			if source == "actual" {
				b.CostSource = "actual"
			}

			return
		}
	}

	card.UsageBreakdown = append(card.UsageBreakdown, board.UsageBucket{
		Agent:               in.AgentID,
		Model:               in.Model,
		PromptTokens:        in.PromptTokens,
		CompletionTokens:    in.CompletionTokens,
		CacheReadTokens:     in.CacheReadTokens,
		CacheCreationTokens: in.CacheCreationTokens,
		CostUSD:             cost,
		CostSource:          source,
	})
}
