package service

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
)

// SyncSucceeded is called by the syncer at the end of every successful cycle,
// when the remote holds every local commit. The leases this instance holds
// are confirmed and the pull clock restarts.
func (s *CardService) SyncSucceeded(ctx context.Context) {
	s.syncMu.Lock()
	s.lastSync = s.clk.Now()
	s.syncMu.Unlock()

	if err := s.lock.ConfirmLeases(ctx); err != nil {
		ctxlog.Logger(ctx).Warn("confirm leases after sync", "error", err)
	}
}

// ObserveLeases is called by the syncer after every index reload so the lease
// table reflects what the pull brought in.
func (s *CardService) ObserveLeases(ctx context.Context) error {
	if err := s.lock.ObserveLeases(ctx); err != nil {
		return fmt.Errorf("observe leases: %w", err)
	}

	return nil
}

// NoteClaimLost is called by the syncer for each card a pull moved out of
// this instance's hands at a higher epoch. The live beat goes, the run is
// recorded as ended, and claim.lost tells the backend integration to stop
// the local container.
func (s *CardService) NoteClaimLost(ctx context.Context, project, id, previousAgent, newVia string, epoch int) {
	s.lock.ClearBeat(project, id)

	if card, err := s.store.GetCard(ctx, project, id); err == nil {
		s.observeRunEnd(project, card, "claim_lost")
	}

	ctxlog.Logger(ctx).Warn("claim lost to another instance",
		"project", project, "card_id", id, "previous_agent", previousAgent, "claimed_via", newVia, "claim_epoch", epoch)

	s.bus.Publish(events.Event{
		Type:      events.ClaimLost,
		Project:   project,
		CardID:    id,
		Agent:     previousAgent,
		Timestamp: s.clk.Now(),
		Data: map[string]any{
			"previous_agent": previousAgent,
			"claimed_via":    newVia,
			"claim_epoch":    epoch,
			"source":         "sync",
		},
	})
}

// recentlySynced reports whether the last successful cycle is within twice
// the pull interval. Foreign stalls require it: a stale local view must not
// judge a peer's lease.
func (s *CardService) recentlySynced() bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	return !s.lastSync.IsZero() && s.clk.Now().Sub(s.lastSync) <= 2*s.pullInterval
}

// fenced refuses a write on a claim this instance holds but the remote has
// not confirmed for lease_timeout.
func (s *CardService) fenced(card *board.Card) error {
	if s.lock.Fenced(card) {
		return fmt.Errorf("card %s: %w", card.ID, ErrClaimFenced)
	}

	return nil
}
