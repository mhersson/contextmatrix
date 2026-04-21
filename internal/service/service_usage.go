package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ModelCost defines per-token cost rates for a model.
type ModelCost struct {
	Prompt     float64
	Completion float64
}

// ReportUsageInput contains the fields for reporting token usage on a card.
type ReportUsageInput struct {
	AgentID          string
	Model            string
	PromptTokens     int64
	CompletionTokens int64
}

// ProjectUsage contains aggregated token usage across all cards in a project.
type ProjectUsage struct {
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CardCount        int     `json:"card_count"`
}

// RecalculateCostsResult summarises the outcome of a cost recalculation pass.
type RecalculateCostsResult struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

// ReportUsage increments token usage counters on a card and recalculates cost.
func (s *CardService) ReportUsage(ctx context.Context, project, id string, input ReportUsageInput) (*board.Card, error) {
	id = strings.ToUpper(id)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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

	// Calculate cost delta for this report and add to running total.
	// Warn when a model name is provided but not found in the cost map.
	if input.Model != "" {
		if rate, ok := s.tokenCosts[input.Model]; ok {
			deltaCost := float64(input.PromptTokens)*rate.Prompt + float64(input.CompletionTokens)*rate.Completion
			card.TokenUsage.EstimatedCostUSD += deltaCost
		} else {
			ctxlog.Logger(ctx).Warn("unknown model in cost map, cost not calculated",
				"model", input.Model,
				"card_id", id,
			)
		}
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer). On failure roll back to snapshot so cache +
	// disk stay consistent with git.
	if err := s.commitCardChange(ctx, project, id, input.AgentID, "usage reported"); err != nil {
		return nil, s.rollbackCardOnCommitFailure(ctx, project, snapshot, err)
	}

	// If the card has no active agent, flush immediately — there is no
	// subsequent ReleaseCard call to flush deferred paths (e.g. report_usage
	// called after complete_task).
	if card.AssignedAgent == "" {
		if err := s.flushDeferredCommit(ctx, id, input.AgentID); err != nil {
			ctxlog.Logger(ctx).Error("flush deferred commit on post-release usage report", "card_id", id, "error", err)
		}
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUsageReported,
		Project:   project,
		CardID:    id,
		Agent:     input.AgentID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"prompt_tokens":     input.PromptTokens,
			"completion_tokens": input.CompletionTokens,
			"model":             input.Model,
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
			usage.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
			usage.CardCount++
		}
	}

	return usage, nil
}

// RecalculateCosts recomputes estimated costs for cards that have non-zero token
// counts but a zero estimated cost (e.g. because the model was not provided when
// usage was first reported). Only cards that match this condition are updated;
// cards that already have a non-zero estimated cost are left untouched.
//
// defaultModel is used when card.TokenUsage.Model is empty.  If neither the
// card's stored model nor defaultModel is in the cost map the card is skipped.
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

		if card.TokenUsage.PromptTokens == 0 && card.TokenUsage.CompletionTokens == 0 {
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

		// Snapshot before mutating. store.GetCard returns a deep copy
		// independent of the one we are about to write back.
		snapshot, err := s.store.GetCard(ctx, project, card.ID)
		if err != nil {
			return nil, fmt.Errorf("get card snapshot %s: %w", card.ID, err)
		}

		cost := float64(card.TokenUsage.PromptTokens)*rate.Prompt +
			float64(card.TokenUsage.CompletionTokens)*rate.Completion

		card.TokenUsage.EstimatedCostUSD = cost
		// Persist the effective model name so future recalculations are idempotent.
		if card.TokenUsage.Model == "" && model != "" {
			card.TokenUsage.Model = model
		}

		card.Updated = time.Now()

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
