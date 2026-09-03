package service

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// processForeignStalls stalls cards other instances hold whose pushed lease
// has stayed unchanged for lease_timeout on this instance's clock. It only
// runs on a fresh local view, and each stall is decided again against the
// merged card inside a sync cycle, so a renewal that lands in the same cycle
// cancels it. Errors are logged so one card never blocks the sweep.
func (s *CardService) processForeignStalls(ctx context.Context) {
	if !s.recentlySynced() {
		return
	}

	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		ctxlog.Logger(ctx).Error("foreign stall scan: list projects", "error", err)

		return
	}

	for _, proj := range projects {
		cards, err := s.store.ListCards(ctx, proj.Name, storage.CardFilter{})
		if err != nil {
			ctxlog.Logger(ctx).Error("foreign stall scan: list cards", "project", proj.Name, "error", err)

			continue
		}

		for _, card := range cards {
			if !s.foreignStallCandidate(card) {
				continue
			}

			if err := s.stallForeignVerified(ctx, proj.Name, card.ID); err != nil {
				ctxlog.Logger(ctx).Error("foreign stall", "project", proj.Name, "card_id", card.ID, "error", err)
			}
		}
	}
}

func (s *CardService) foreignStallCandidate(card *board.Card) bool {
	return card.ClaimedElsewhere(s.instance) &&
		!board.IsTerminalState(card.State) && card.State != board.StateStalled &&
		s.lock.ForeignLeaseExpired(card)
}

// stallForeignVerified stalls one card inside a sync cycle. The apply step
// re-reads the merged card and re-checks the lease, so nothing happens when
// the pull brought a renewal or a release; the undo restores the peer's
// tuple when the push never lands.
func (s *CardService) stallForeignVerified(ctx context.Context, project, id string) error {
	var (
		snapshot, written *board.Card
		prevAgent, via    string
	)

	_, err := s.runVerified(ctx, s.repoOf(project), "foreign stall",
		func(ctx context.Context) error {
			card, err := s.store.GetCard(ctx, project, id)
			if err != nil {
				return fmt.Errorf("get card: %w", err)
			}

			if !s.foreignStallCandidate(card) {
				return nil // the merge changed its mind
			}

			snap, err := s.store.GetCard(ctx, project, id)
			if err != nil {
				return fmt.Errorf("get card snapshot: %w", err)
			}

			prevAgent, via = card.AssignedAgent, card.ClaimedVia
			previousState := card.State

			card.State = board.StateStalled
			card.ClearClaim()
			card.ClaimEpoch++

			if card.WorkerStatus == "queued" || card.WorkerStatus == "running" {
				card.WorkerStatus = "failed"
			}

			card.Updated = s.clk.Now()
			appendStateChangeLog(card, previousState, board.StateStalled, "", card.Updated)
			card.ActivityLog = board.TrimActivityLog(append(card.ActivityLog, board.ActivityEntry{
				Agent: "system", Action: "stalled", Timestamp: card.Updated,
				Message: fmt.Sprintf("lease held by %s via %s expired (instance %s)", prevAgent, via, s.instance),
			}))

			cfg, err := s.getConfig(ctx, project)
			if err != nil {
				return fmt.Errorf("get project config: %w", err)
			}

			if err := s.validateStalledCardFn(cfg, card); err != nil {
				return fmt.Errorf("validate stalled card: %w", err)
			}

			if err := s.store.UpdateCard(ctx, project, card); err != nil {
				return fmt.Errorf("update card: %w", err)
			}

			if err := s.commitNow(ctx, []string{s.cardPath(project, id)}, commitMessage("", id, "stalled (lease expired)")); err != nil {
				return s.rollbackCardOnCommitFailure(ctx, project, snap, err)
			}

			snapshot, written = snap, card

			return nil
		},
		func(ctx context.Context) error {
			return s.undoClaimWrite(ctx, project, id, written, snapshot, "foreign stall undone: remote unreachable")
		})
	if err != nil {
		return err
	}

	if written == nil {
		return nil
	}

	metrics.StallCardsMarked.Inc()
	s.observeRunEnd(project, written, "stalled")

	s.bus.Publish(events.Event{
		Type:      events.CardStalled,
		Project:   project,
		CardID:    id,
		Timestamp: written.Updated,
		Data:      map[string]any{"previous_agent": prevAgent, "claimed_via": via},
	})

	ctxlog.Logger(ctx).Info("foreign card marked stalled",
		"project", project, "card_id", id, "previous_agent", prevAgent, "claimed_via", via)

	return nil
}
