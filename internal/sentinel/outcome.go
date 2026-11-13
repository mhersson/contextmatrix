package sentinel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// Agent is the agent ID Sentinel writes the board under.
const Agent = "system:sentinel"

// activityAction is the action of every activity entry Sentinel writes.
const activityAction = "sentinel"

// maxActivityText is the card service's limit on an activity message; a
// diagnosis in a park reason can be far longer, and the tab shows it whole.
const maxActivityText = 2000

// activityText is the one activity entry a heal writes; a parked heal's names
// why it needs a human.
func activityText(h Heal) string {
	text := "Sentinel left this for a human: " + h.Reason
	if len(text) <= maxActivityText {
		return text
	}

	const ellipsis = "..."

	return strings.ToValidUTF8(text[:maxActivityText-len(ellipsis)], "") + ellipsis
}

// park leaves the heal to a human: it saves stage parked with reason, then
// writes the heal's one activity entry.
func (c *Controller) park(ctx context.Context, h *Heal, reason string) error {
	if err := c.saveStage(ctx, h, StageParked, reason); err != nil {
		return fmt.Errorf("park the heal: %w", err)
	}

	return c.logActivity(ctx, h)
}

// parkUnreported parks a heal whose run kind never reported and charges the
// dispatch's ceiling. The failure is saved with the stage, so a late report
// matches no dispatch and is never charged on top.
func (c *Controller) parkUnreported(ctx context.Context, h *Heal, reason string) error {
	h.Dispatch.Failure = reason

	if err := c.saveStage(ctx, h, StageParked, reason); err != nil {
		return fmt.Errorf("park the heal: %w", err)
	}

	var chargeErr error
	if err := c.cfg.Store.Charge(ctx, h.ID, h.Dispatch.Mode, h.Dispatch.CeilingUSD, h.UpdatedAt); err != nil {
		chargeErr = fmt.Errorf("charge the %s ceiling: %w", h.Dispatch.Kind, err)
	}

	return errors.Join(chargeErr, c.logActivity(ctx, h))
}

// closeHeal ends the heal without an activity entry: the card was closed or
// someone else took it over, which the card already shows.
func (c *Controller) closeHeal(ctx context.Context, h *Heal, reason string) error {
	if err := c.saveStage(ctx, h, StageClosed, reason); err != nil {
		return fmt.Errorf("close the heal: %w", err)
	}

	return nil
}

// saveStage moves the heal to stage with reason.
func (c *Controller) saveStage(ctx context.Context, h *Heal, stage, reason string) error {
	h.Stage, h.Reason, h.UpdatedAt = stage, reason, c.cfg.Clock.Now()

	return c.cfg.Store.SaveHeal(ctx, h)
}

func (c *Controller) logActivity(ctx context.Context, h *Heal) error {
	entry := board.ActivityEntry{Agent: Agent, Timestamp: c.cfg.Clock.Now(), Action: activityAction, Message: activityText(*h)}
	if _, err := c.cfg.Cards.AddSystemLogEntry(ctx, h.Project, h.CardID, entry); err != nil {
		return fmt.Errorf("write the activity entry: %w", err)
	}

	return nil
}
