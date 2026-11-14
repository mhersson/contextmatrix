package sentinel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// Agent is the agent ID Sentinel writes the board under.
const Agent = "system:sentinel"

// activityAction is the action of every activity entry Sentinel writes.
const activityAction = "sentinel"

// maxActivityText is the card service's limit on an activity message; a
// diagnosis in a park reason can be far longer, and the tab shows it whole.
const maxActivityText = 2000

// activityText is a parked heal's activity entry: why it needs a human.
func activityText(h state.Heal) string {
	return clipActivity("Sentinel left this for a human: " + h.Reason)
}

func clipActivity(text string) string {
	if len(text) <= maxActivityText {
		return text
	}

	const ellipsis = "..."

	return strings.ToValidUTF8(text[:maxActivityText-len(ellipsis)], "") + ellipsis
}

// park leaves the heal to a human: it saves stage parked with reason, then
// writes the heal's one activity entry.
func (c *Controller) park(ctx context.Context, h *state.Heal, reason string) error {
	if err := c.saveStage(ctx, h, state.StageParked, reason); err != nil {
		return fmt.Errorf("park the heal: %w", err)
	}

	return c.logActivity(ctx, h, activityText(*h))
}

// parkUnreported parks a heal whose run kind never reported and charges the
// dispatch's ceiling. The failure is saved with the stage, so a late report
// matches no dispatch and is never charged on top.
func (c *Controller) parkUnreported(ctx context.Context, h *state.Heal, reason string) error {
	h.Dispatch.Failure = reason

	if err := c.saveStage(ctx, h, state.StageParked, reason); err != nil {
		return fmt.Errorf("park the heal: %w", err)
	}

	return errors.Join(c.chargeCeiling(ctx, h, h.UpdatedAt), c.logActivity(ctx, h, activityText(*h)))
}

// failUnreported marks a finished heal's run kind that never reported failed
// and charges its ceiling, as parkUnreported does for an open heal. The heal
// keeps its stage, its updated time and its activity log.
func (c *Controller) failUnreported(ctx context.Context, h *state.Heal) error {
	h.Dispatch.Failure = unreportedReason(h.Dispatch.Kind)

	if err := c.saveHeal(ctx, h); err != nil {
		return fmt.Errorf("fail the unreported %s run: %w", h.Dispatch.Kind, err)
	}

	return c.chargeCeiling(ctx, h, c.cfg.Clock.Now())
}

func (c *Controller) chargeCeiling(ctx context.Context, h *state.Heal, at time.Time) error {
	if err := c.cfg.Store.Charge(ctx, h.ID, h.Dispatch.Mode, h.Dispatch.CeilingUSD, at); err != nil {
		return fmt.Errorf("charge the %s ceiling: %w", h.Dispatch.Kind, err)
	}

	return nil
}

// closeHeal ends the heal without an activity entry: the card was closed or
// someone else took it over, which the card already shows.
func (c *Controller) closeHeal(ctx context.Context, h *state.Heal, reason string) error {
	if err := c.saveStage(ctx, h, state.StageClosed, reason); err != nil {
		return fmt.Errorf("close the heal: %w", err)
	}

	return nil
}

// saveStage moves the heal to stage with reason.
func (c *Controller) saveStage(ctx context.Context, h *state.Heal, stage, reason string) error {
	h.Stage, h.Reason, h.UpdatedAt = stage, reason, c.cfg.Clock.Now()

	return c.saveHeal(ctx, h)
}

// saveHeal and openHeal write a heal row and announce the card's change:
// the board marks and the attention list read heal stages.
func (c *Controller) saveHeal(ctx context.Context, h *state.Heal) error {
	if err := c.cfg.Store.SaveHeal(ctx, h); err != nil {
		return err
	}

	c.healChanged(h.Project, h.CardID)

	return nil
}

func (c *Controller) openHeal(ctx context.Context, h state.Heal) (int64, error) {
	id, err := c.cfg.Store.OpenHeal(ctx, h)
	if err != nil {
		return 0, err
	}

	c.healChanged(h.Project, h.CardID)

	return id, nil
}

// queueAnalysis, openRevision, setFindingOutcome and putStandingPR write the
// other rows a Sentinel view reads and announce them as saveHeal does: an
// analysis its card, a revision or the standing PR the project, a finding its card.
func (c *Controller) queueAnalysis(ctx context.Context, h state.Heal) error {
	if err := c.cfg.Store.QueueAnalysis(ctx, h); err != nil {
		return err
	}

	c.healChanged(h.Project, h.CardID)

	return nil
}

func (c *Controller) openRevision(ctx context.Context, project string, at time.Time) (state.Heal, error) {
	h, err := c.cfg.Store.OpenRevision(ctx, project, at)
	if err != nil {
		return state.Heal{}, err
	}

	c.healChanged(project, "")

	return h, nil
}

func (c *Controller) setFindingOutcome(ctx context.Context, f state.Finding, st, reason, output string, revisionID int64, at time.Time) error {
	if err := c.cfg.Store.SetFindingOutcome(ctx, f.ID, st, reason, output, revisionID, at); err != nil {
		return err
	}

	c.healChanged(f.Project, f.CardID)

	return nil
}

func (c *Controller) putStandingPR(ctx context.Context, rec state.StandingPR) error {
	if err := c.cfg.Store.PutStandingPR(ctx, rec); err != nil {
		return err
	}

	c.healChanged(rec.Project, "")

	return nil
}

func (c *Controller) healChanged(project, cardID string) {
	c.cfg.Bus.Publish(events.Event{
		Type: events.SentinelChanged, Project: project, CardID: cardID, Timestamp: c.cfg.Clock.Now().UTC(),
	})
}

func (c *Controller) logActivity(ctx context.Context, h *state.Heal, text string) error {
	entry := board.ActivityEntry{Agent: Agent, Timestamp: c.cfg.Clock.Now(), Action: activityAction, Message: text}
	if _, err := c.cfg.Cards.AddSystemLogEntry(ctx, h.Project, h.CardID, entry); err != nil {
		return fmt.Errorf("write the activity entry: %w", err)
	}

	return nil
}
