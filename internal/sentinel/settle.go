package sentinel

import (
	"context"
	"errors"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// settle ends the rerun's heal v holds at s's stage, healed or rerun_failed,
// and charges what the rerun cost on its channel; a healed heal then forwards
// its fix while healing is on. The stage is saved first: a save refused as
// stale charges and forwards nothing, so the next pass's settle does both once.
func (c *Controller) settle(ctx context.Context, v *healView, s step) error {
	h := &v.Heal

	if err := c.saveStage(ctx, h, s.Stage, s.Reason); err != nil {
		return fmt.Errorf("settle the heal: %w", err)
	}

	var chargeErr error
	if err := c.cfg.Store.Charge(ctx, h.ID, v.Run.Mode, rerunCharge(*h, v.Card), h.UpdatedAt); err != nil {
		chargeErr = fmt.Errorf("charge the rerun: %w", err)
	}

	if s.Stage != state.StageHealed || !v.Healing {
		return chargeErr
	}

	return errors.Join(chargeErr, c.forward(ctx, h))
}

// rerunCharge is what a settled rerun cost: the card's cost now minus at
// relaunch, never negative.
func rerunCharge(h state.Heal, card *board.Card) float64 {
	return max(cardCost(card)-h.CostBaseUSD, 0)
}
