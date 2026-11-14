package sentinel

import (
	"context"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// considerRuns considers every undecided run whose card has no open heal,
// adding the cards it opens a heal for to open. It reports whether it opened
// one the next pass acts on.
func (c *Controller) considerRuns(ctx context.Context, now time.Time, open map[cardRef]bool) (bool, error) {
	runs, err := c.cfg.Store.UndecidedRuns(ctx)
	if err != nil {
		return false, fmt.Errorf("list undecided runs: %w", err)
	}

	opened := false

	for _, run := range runs {
		ref := cardRef{run.Project, run.CardID}
		if open[ref] {
			continue
		}

		ok, err := c.consider(ctx, now, run)
		if err != nil {
			ctxlog.Logger(ctx).Warn("sentinel: run not considered", "project", run.Project, "card_id", run.CardID, "error", err)

			continue
		}

		if ok {
			open[ref], opened = true, true
		}
	}

	return opened, nil
}

// consider decides a run once it has settled, then judges the fixes forwarded
// to its card that the run tried. It reports whether it opened a heal that is
// still open.
func (c *Controller) consider(ctx context.Context, now time.Time, run Run) (bool, error) {
	card, err := c.cfg.Cards.GetCard(ctx, run.Project, run.CardID)

	switch {
	case gone(err):
		return false, c.leave(ctx, run)
	case err != nil:
		return false, fmt.Errorf("read card: %w", err)
	}

	trial, err := c.onTrial(ctx, run)
	if err != nil {
		return false, err
	}

	var forward *Heal
	if len(trial) > 0 {
		forward = &trial[0]
	}

	key, settled := causeKey(run, card, forward, now)
	if !settled {
		return false, nil
	}

	opened, err := c.decide(ctx, now, run, card, key)
	if err != nil {
		return false, err
	}

	// Judged after deciding: a run a failed write left undecided must still
	// find its fix on trial next pass.
	return opened, c.judge(ctx, trial, forwardOutcome(key, card))
}

// decide opens a heal for a settled run by its cause, parked at once when its
// row says so, or leaves the run alone, marking it decided. It reports whether
// it opened a heal that is still open.
func (c *Controller) decide(ctx context.Context, now time.Time, run Run, card *board.Card, key string) (bool, error) {
	row := responseFor(key)
	if ok, why := eligible(run, card, c.cfg.Cards.ClaimedElsewhere(card)); !ok || row.Response == RespIgnore {
		ctxlog.Logger(ctx).Debug("sentinel: run left alone", "project", run.Project, "card_id", run.CardID, "cause", key, "why", why)

		return false, c.leave(ctx, run)
	}

	h := Heal{
		Kind: HealKindHeal, Project: run.Project, CardID: run.CardID, RunStarted: run.StartedAt,
		RunID: run.RunID, Cause: key, Response: row.Response, CreatedAt: now, UpdatedAt: now,
	}
	h.Stage, h.Reason = openStage(row, run)

	id, err := c.cfg.Store.OpenHeal(ctx, h)
	if err != nil {
		return false, fmt.Errorf("open heal: %w", err)
	}

	h.ID = id

	if h.Stage == StageParked {
		return false, c.logActivity(ctx, &h, activityText(h))
	}

	return true, nil
}

// leave marks the run decided without opening a heal for it.
func (c *Controller) leave(ctx context.Context, run Run) error {
	if err := c.cfg.Store.MarkRunDecided(ctx, run.Project, run.CardID, run.StartedAt); err != nil {
		return fmt.Errorf("mark run decided: %w", err)
	}

	return nil
}
