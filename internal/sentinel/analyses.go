package sentinel

import (
	"context"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// queueAnalyses stores an analysis row for every settled run that has none, so
// a relaunch, which replaces the card's run row, cannot lose the analysis.
func (c *Controller) queueAnalyses(ctx context.Context, now time.Time) error {
	runs, err := c.cfg.Store.UnqueuedRuns(ctx)
	if err != nil {
		return fmt.Errorf("list unqueued runs: %w", err)
	}

	for _, run := range runs {
		if err := c.queueRun(ctx, now, run); err != nil {
			ctxlog.Logger(ctx).Warn("sentinel: run not queued for analysis",
				"project", run.Project, "card_id", run.CardID, "error", err)
		}
	}

	return nil
}

func (c *Controller) queueRun(ctx context.Context, now time.Time, run state.Run) error {
	card, err := c.cfg.Cards.GetCard(ctx, run.Project, run.CardID)

	switch {
	case gone(err):
		card = nil
	case err != nil:
		return fmt.Errorf("read card: %w", err)
	}

	switch queueStep(run, card, now) {
	case queueSkip:
		if err := c.cfg.Store.MarkAnalysisQueued(ctx, run.Project, run.CardID, run.StartedAt); err != nil {
			return fmt.Errorf("mark analysis queued: %w", err)
		}
	case queueRow:
		row := state.Heal{
			Kind: state.HealKindAnalysis, Project: run.Project, CardID: run.CardID, RunStarted: run.StartedAt,
			RunID: run.RunID, Cause: run.StopCause, Stage: state.StageAnalyse, CreatedAt: now, UpdatedAt: now,
		}
		if err := c.cfg.Store.QueueAnalysis(ctx, row); err != nil {
			return fmt.Errorf("queue analysis: %w", err)
		}
	}

	return nil
}
