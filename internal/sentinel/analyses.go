package sentinel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

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
		if err := c.queueAnalysis(ctx, row); err != nil {
			return fmt.Errorf("queue analysis: %w", err)
		}
	}

	return nil
}

// advanceAnalyses steps every open analysis, newest first, on the budget and
// worker slots the heals left, and returns how many are in flight after the
// pass. An analysis whose read or step failed waits for the next pass.
func (c *Controller) advanceAnalyses(ctx context.Context, r *passReads) (analysing int) {
	for i := range r.analyses {
		row := &r.analyses[i]

		// ErrHealChanged: its report arrived since the pass read it.
		if err := c.stepAnalysis(ctx, r, row); err != nil && !errors.Is(err, state.ErrHealChanged) {
			ctxlog.Logger(ctx).Warn("sentinel: analysis step failed",
				"project", row.Project, "card_id", row.CardID, "heal_id", row.ID, "error", err)
		}

		if inFlight(row.Dispatch) {
			analysing++
		}
	}

	return analysing
}

func (c *Controller) stepAnalysis(ctx context.Context, r *passReads, row *state.Heal) error {
	v, err := c.readAnalysis(ctx, r, *row)
	if err != nil {
		return err
	}

	switch s := nextAnalysis(v); s.Action {
	case analysisDispatch:
		err := c.dispatchAnalyse(ctx, row, r.set)
		r.sent(*row)

		return err
	case analysisEnd:
		return c.endAnalysis(ctx, row, s)
	}

	return nil
}

// readAnalysis builds row's view: the gate, the worker slots and the holds as
// the pass has left them, then the card, the analysed run's heal and the
// card's run row, read fresh.
func (c *Controller) readAnalysis(ctx context.Context, r *passReads, row state.Heal) (analysisView, error) {
	v := analysisView{
		Row: row, On: r.set.Analyses(), Healing: r.set.Healing, Now: r.now,
		GateOpen: analysisGate(r.mode, r.set, r.spent, r.windows, r.now),
		Room:     roomFor(r.health),
		Listed:   inFlight(row.Dispatch) && r.listed(row),
		// Also held while the agent holds the kind, so no payload is built.
		Held: slices.Contains(r.held(row), protocol.RunKindAnalyse) ||
			c.noRunKinds(r.now) || c.kindHeld(protocol.RunKindAnalyse, r.now),
	}

	card, err := c.cfg.Cards.GetCard(ctx, row.Project, row.CardID)

	switch {
	case gone(err):
		return v, nil
	case err != nil:
		return v, fmt.Errorf("read card: %w", err)
	}

	v.Card = card

	if v.Heal, err = c.cfg.Store.RunHeal(ctx, row.Project, row.CardID, row.RunStarted); err != nil {
		return v, fmt.Errorf("read the run's heal: %w", err)
	}

	if v.Run, err = c.cfg.Store.CardRunRow(ctx, row.Project, row.CardID); err != nil {
		return v, fmt.Errorf("read run row: %w", err)
	}

	return v, nil
}

// endAnalysis ends h as s says. A dispatch that never reported is marked
// failed in the same save, so a late report is never charged on top of the
// ceiling charged here.
func (c *Controller) endAnalysis(ctx context.Context, h *state.Heal, s analysisStep) error {
	if s.ChargeCeiling {
		h.Dispatch.Failure = s.Reason
	}

	if err := c.saveStage(ctx, h, s.Stage, s.Reason); err != nil {
		return fmt.Errorf("end the analysis: %w", err)
	}

	if !s.ChargeCeiling {
		return nil
	}

	return c.chargeCeiling(ctx, h, h.UpdatedAt)
}
