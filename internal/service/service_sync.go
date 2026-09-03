package service

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
)

// SyncSucceeded is called by the syncer at the end of every successful
// cycle of repo, when the remote holds every local commit of that repo. The
// leases this instance holds there are confirmed and the repo's pull clock
// restarts. Other repos are untouched: a fresh pull of one repo says nothing
// about another.
func (s *CardService) SyncSucceeded(ctx context.Context, repo string) {
	r, err := s.repoNamed(repo)
	if err != nil {
		ctxlog.Logger(ctx).Warn("sync succeeded for unknown repo", "repo", repo)

		return
	}

	r.markSynced(s.clk.Now())

	if err := r.Lock.ConfirmLeases(ctx); err != nil {
		ctxlog.Logger(ctx).Warn("confirm leases after sync", "repo", r.Name, "error", err)
	}
}

// ObserveLeases is called by the syncer after every index reload of repo so
// that repo's lease table reflects what the pull brought in.
func (s *CardService) ObserveLeases(ctx context.Context, repo string) error {
	r, err := s.repoNamed(repo)
	if err != nil {
		return err
	}

	if err := r.Lock.ObserveLeases(ctx); err != nil {
		return fmt.Errorf("observe leases: %w", err)
	}

	return nil
}

// NoteClaimLost is called by the syncer for each card a pull moved out of
// this instance's hands at a higher epoch. The live beat goes, the run is
// recorded as ended, and claim.lost tells the backend integration to stop
// the local container.
func (s *CardService) NoteClaimLost(ctx context.Context, project, id, previousAgent, newVia string, epoch int) {
	s.repoOf(project).Lock.ClearBeat(project, id)

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

// fenced refuses a write on a claim this instance holds but the remote has
// not confirmed for lease_timeout.
func (s *CardService) fenced(card *board.Card) error {
	if s.repoOf(card.Project).Lock.Fenced(card) {
		return fmt.Errorf("card %s: %w", card.ID, ErrClaimFenced)
	}

	return nil
}
