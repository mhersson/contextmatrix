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

// SetCatalogRateLookup installs a catalog-backed rate fallback used when a
// model is not in the static token_costs override map.
func (s *CardService) SetCatalogRateLookup(fn func(model string) (ModelRate, bool)) {
	s.catalogRate = fn
}

// SetModelValidator wires catalog-backed model-pin validation. fn reports
// whether a slug is in the served model set and must fail open (return true)
// when the catalog is unavailable. Nil disables pin validation.
func (s *CardService) SetModelValidator(fn func(ctx context.Context, slug string) bool) {
	s.modelValidator = fn
}

// rateFor resolves a model's per-token rate: the static token_costs override
// wins first (e.g. cache-aware rates), else the catalog fallback, else false.
// Used by every cost path so ReportUsage, RecalculateCosts, and the PriceTokens
// method all price identically.
func (s *CardService) rateFor(model string) (ModelRate, bool) {
	if rate, ok := s.tokenCosts[model]; ok {
		return rate, true
	}

	if s.catalogRate != nil {
		return s.catalogRate(model)
	}

	return ModelRate{}, false
}

// PriceTokens looks up the model via rateFor (static token_costs first, then
// catalog fallback) and delegates to the package-level PriceTokens helper.
// Returns (cost, true) when the model is known and (0, false) when it is not.
// Chat and other callers use this via the chat.Pricer interface.
func (s *CardService) PriceTokens(model string, prompt, cacheRead, cacheCreation, completion int64) (float64, bool) {
	rate, ok := s.rateFor(model)
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
	// Phase, Step, and DurationMS are Prometheus-only attribution hints from
	// the agent; they are never persisted on the card. Phase falls back to the
	// card's current phase when empty. DurationMS is the wall time of the
	// harness step; values <= 0 are ignored.
	Phase      string
	Step       string
	DurationMS int64
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
// buckets), not the re-pricing delta - consistent with the legacy whole-card
// semantics. Cards that were not written contribute nothing.
type RecalculateCostsResult struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

// ReportUsage increments token usage counters on a card and recalculates cost.
//
// Zero-token calls (PromptTokens=0 and CompletionTokens=0) are intentionally
// written and emit an event. This makes a heartbeat+report_usage pair at idle
// a useful health signal even when no new tokens are consumed - removing this
// write would silently drop that observability.
//
// Multiple calls with different model names cause the stored TokenUsage.Model
// to be overwritten with the most recently reported model. Cost arithmetic is
// unaffected: each delta is priced using the model passed in that call. The
// overwrite ensures that RecalculateCosts - which uses TokenUsage.Model as a
// fallback - always applies the most recent model, which is the correct default
// for the typical single-primary-model-per-card agent pattern.
func (s *CardService) ReportUsage(ctx context.Context, project, id string, input ReportUsageInput) (*board.Card, error) {
	id = strings.ToUpper(id)

	// Resolve the model rate BEFORE taking writeMu: the catalog fallback can
	// block on network I/O (stale-cache refresh), and holding the global card
	// write lock across that stalls every claim/heartbeat/transition. Nothing
	// the lookup reads is guarded by writeMu - tokenCosts is immutable after
	// construction and catalogRate is wired once at startup.
	var (
		rate   ModelRate
		rateOK bool
	)

	if input.ActualCostUSD == nil && input.Model != "" {
		rate, rateOK = s.rateFor(input.Model)
	}

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

	// Migration bucket: when a legacy card (cumulative spend or tokens, no
	// buckets) starts bucketing, seed a bucket carrying the pre-existing
	// cumulative so the bucket sum stays equal to the cumulative cost. Without
	// this the dashboard - which switches to the breakdown path on
	// len(UsageBreakdown) > 0 - would attribute only the new delta and silently
	// drop the legacy spend. Tokens-but-zero-cost cards (the old fill-missing
	// population) are seeded too so token rollups stay complete and
	// RecalculateCosts can later price the migrated bucket from the rate table.
	// Seeded before the model-store and increments below so it reflects the
	// legacy model and pre-existing totals only. Agent is the card's
	// AssignedAgent (may be empty, mapping to the dashboard's "unassigned"
	// rollup, matching the legacy attribution path).
	hasLegacyUsage := card.TokenUsage.EstimatedCostUSD > 0 ||
		card.TokenUsage.PromptTokens > 0 ||
		card.TokenUsage.CompletionTokens > 0 ||
		card.TokenUsage.CacheReadTokens > 0 ||
		card.TokenUsage.CacheCreationTokens > 0

	if len(card.UsageBreakdown) == 0 && hasLegacyUsage {
		card.UsageBreakdown = append(card.UsageBreakdown, board.UsageBucket{
			Agent:               card.AssignedAgent,
			Model:               card.TokenUsage.Model,
			PromptTokens:        card.TokenUsage.PromptTokens,
			CompletionTokens:    card.TokenUsage.CompletionTokens,
			CacheReadTokens:     card.TokenUsage.CacheReadTokens,
			CacheCreationTokens: card.TokenUsage.CacheCreationTokens,
			CostUSD:             card.TokenUsage.EstimatedCostUSD,
			CostSource:          "estimated",
		})
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
		if rateOK {
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

	// If the card has no active agent, flush immediately - there is no
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

	s.emitUsageMetrics(ctx, project, card, input, deltaCost, costSource)

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
// re-priced from the current rate table - including buckets that already have
// a non-zero cost (stale prices are corrected). Actual-cost buckets are never
// modified; that is what the cost_source flag exists for. The model fallback
// chain per bucket is bucket model → card's stored model → defaultModel; a
// bucket whose model resolves to no rate is left unchanged. EstimatedCostUSD
// is set to the bucket sum. The card is written only when at least one bucket
// price actually changed.
//
// Legacy cards (no breakdown): fill-missing-only, verbatim pre-breakdown
// behavior - only cards with non-zero tokens and a zero EstimatedCostUSD are
// updated; cards that already have a cost are not modified.
func (s *CardService) RecalculateCosts(ctx context.Context, project, defaultModel string) (*RecalculateCostsResult, error) {
	// Pre-resolve every rate this pass could need BEFORE taking writeMu: the
	// catalog fallback can block on network I/O and must never run under the
	// global card write lock. The lock-free ListCards pass walks the same
	// model fallback chains as the locked pass below; resolveRate memoizes,
	// so a model that only appears mid-pass (concurrent ReportUsage between
	// the two listings) costs at most one bounded lookup under the lock -
	// bounded by the catalog Builder's failure backoff. Memoization also
	// means each model is priced from a single rate snapshot for the whole
	// pass.
	type rateResult struct {
		rate ModelRate
		ok   bool
	}

	resolved := make(map[string]rateResult)

	resolveRate := func(model string) (ModelRate, bool) {
		r, seen := resolved[model]
		if !seen {
			r.rate, r.ok = s.rateFor(model)
			resolved[model] = r
		}

		return r.rate, r.ok
	}

	prewarm, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	for _, card := range prewarm {
		if card.TokenUsage == nil {
			continue
		}

		if len(card.UsageBreakdown) > 0 {
			for _, b := range card.UsageBreakdown {
				if b.CostSource != "estimated" {
					continue
				}

				resolveRate(usageModel(b.Model, card.TokenUsage.Model, defaultModel))
			}

			continue
		}

		resolveRate(usageModel("", card.TokenUsage.Model, defaultModel))
	}

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
				model := usageModel(b.Model, card.TokenUsage.Model, defaultModel)

				rate, ok := resolveRate(model)
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
				continue // already has a cost - don't double-count
			}

			model := usageModel("", card.TokenUsage.Model, defaultModel)

			rate, ok := resolveRate(model)
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

// usageModel resolves the cost-recalculation model fallback chain:
// bucket model → card's stored model → defaultModel.
func usageModel(bucketModel, cardModel, defaultModel string) string {
	if bucketModel != "" {
		return bucketModel
	}

	if cardModel != "" {
		return cardModel
	}

	return defaultModel
}

// upsertUsageBucket merges one report into the card's (agent, model) bucket.
// A bucket that has ever received an actual-cost report stays "actual" -
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

// emitUsageMetrics records the Prometheus side of a usage report. Called
// after the commit landed so failed reports emit nothing. Model-less reports
// (heartbeat health pattern) are skipped entirely - there is no spend or
// model call to attribute.
func (s *CardService) emitUsageMetrics(ctx context.Context, project string, card *board.Card, input ReportUsageInput, deltaCost float64, costSource string) {
	if input.Model == "" {
		return
	}

	phase := input.Phase
	if phase == "" {
		phase = card.Phase
	}

	phase = metrics.NormalizePhase(phase)
	mode := s.runModeLabel(ctx, project, card)

	metrics.LLMCostUSDTotal.WithLabelValues(project, input.Model, phase, mode, costSource).Add(deltaCost)
	metrics.LLMCallsTotal.WithLabelValues(project, input.Model, phase, mode).Inc()

	kinds := []struct {
		kind   string
		tokens int64
	}{
		{"prompt", input.PromptTokens},
		{"completion", input.CompletionTokens},
		{"cache_read", input.CacheReadTokens},
		{"cache_creation", input.CacheCreationTokens},
	}
	for _, k := range kinds {
		if k.tokens > 0 {
			metrics.LLMTokensTotal.WithLabelValues(project, input.Model, phase, k.kind).Add(float64(k.tokens))
		}
	}

	if input.DurationMS > 0 {
		metrics.LLMStepDuration.WithLabelValues(input.Model, phase, metrics.NormalizeStep(input.Step)).
			Observe(float64(input.DurationMS) / 1000.0)
	}
}

// runModeLabel derives the bounded run_mode label from the card's frontmatter,
// falling back to the parent card for subtasks: execute-phase usage lands on
// subtask cards, which never carry the run-mode fields themselves.
func (s *CardService) runModeLabel(ctx context.Context, project string, card *board.Card) string {
	if mode, ok := runModeOf(card); ok {
		return mode
	}

	if card.Parent == "" {
		return "normal"
	}

	parent, err := s.store.GetCard(ctx, project, card.Parent)
	if err != nil {
		return "normal"
	}

	if mode, ok := runModeOf(parent); ok {
		return mode
	}

	return "normal"
}

func runModeOf(card *board.Card) (string, bool) {
	switch {
	case card.BestOfN >= 2:
		return "best_of_n", true
	case card.MobParticipants >= 2:
		return "mob", true
	default:
		return "", false
	}
}

// enrichSubtaskCost sets SubtaskCostUSD to the summed EstimatedCostUSD of the
// card's direct subtasks. Assigns rather than accumulates so a value carried
// in from the store cache is overwritten. Best-effort: a list failure leaves
// the field as-is rather than failing the read - the rollup is a decoration
// on an otherwise intact card.
func (s *CardService) enrichSubtaskCost(ctx context.Context, card *board.Card) {
	if card.Type == board.SubtaskType {
		return
	}

	subs, err := s.store.ListCards(ctx, card.Project, storage.CardFilter{Parent: card.ID})
	if err != nil {
		ctxlog.Logger(ctx).Warn("enrich subtask cost", "card_id", card.ID, "error", err)

		return
	}

	var sum float64

	for _, sub := range subs {
		if sub.TokenUsage != nil {
			sum += sub.TokenUsage.EstimatedCostUSD
		}
	}

	card.SubtaskCostUSD = sum
}
