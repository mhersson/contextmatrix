package sentinel

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
)

// settle ends a rerun's heal at stage, healed or rerun_failed, and charges
// what the rerun cost on its channel. The stage is saved first: a save refused
// as stale charges nothing, so the next pass's settle charges once.
func (c *Controller) settle(ctx context.Context, h *Heal, card *board.Card, run Run, stage string) error {
	if err := c.saveStage(ctx, h, stage, ""); err != nil {
		return fmt.Errorf("settle the heal: %w", err)
	}

	if err := c.cfg.Store.Charge(ctx, h.ID, run.Mode, rerunCharge(*h, card), h.UpdatedAt); err != nil {
		return fmt.Errorf("charge the rerun: %w", err)
	}

	return nil
}

// rerunCharge is what a settled rerun cost: the card's cost now minus at
// relaunch, never negative.
func rerunCharge(h Heal, card *board.Card) float64 {
	return max(cardCost(card)-h.CostBaseUSD, 0)
}
