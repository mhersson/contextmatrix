package sentinel

import (
	"context"
	"fmt"
)

// park leaves the heal to a human, with reason.
func (c *Controller) park(ctx context.Context, h *Heal, reason string) error {
	h.Stage, h.Reason, h.UpdatedAt = StageParked, reason, c.cfg.Clock.Now()

	if err := c.cfg.Store.SaveHeal(ctx, h); err != nil {
		return fmt.Errorf("park the heal: %w", err)
	}

	return nil
}
