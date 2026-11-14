package sentinel

import (
	"context"
	"errors"
	"fmt"
	"slices"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

// forward applies a healed heal's fix to the siblings its investigation named
// that have still not started in the card's active playbook run on this
// instance. A target the forward checks refuse, or that changed or was claimed
// since it was read, is skipped.
func (c *Controller) forward(ctx context.Context, h *state.Heal) error {
	fix := healFix(*h)
	if fix == nil || len(h.Investigation.ForwardTo) == 0 {
		return nil
	}

	card, project, err := c.cardAndProject(ctx, h)
	if err != nil {
		return err
	}

	pb, err := c.activePlaybook(ctx, card)
	if err != nil || pb == nil {
		return err
	}

	todo, err := c.todoCards(ctx, h.Project)
	if err != nil {
		return err
	}

	var errs []error

	for _, id := range forwardTargets(*h, pb, todo, c.cfg.Instance) {
		errs = append(errs, c.forwardTo(ctx, h, todo[id], project.Verify, forwardFix(*fix)))
	}

	return errors.Join(errs...)
}

// forwardTargets returns the investigation's ForwardTo IDs that were sent as
// siblings and are still not started in the same active playbook run.
func forwardTargets(h state.Heal, pb *service.PlaybookDetail, cards map[string]*board.Card, instance string) []string {
	if h.Investigation == nil {
		return nil
	}

	var ids []string

	for _, sib := range siblings(pb, instance, cardRef{h.Project, h.CardID}, cards) {
		if slices.Contains(h.Investigation.ForwardTo, sib.ID) && slices.Contains(h.SentSiblings, sib.ID) {
			ids = append(ids, sib.ID)
		}
	}

	return ids
}

// forwardFix strips what is card-specific (retired subtasks).
func forwardFix(fix protocol.CardFix) protocol.CardFix {
	fix.RetireSubtasks = nil

	return fix
}

// forwardTo vets fix against target and its project's verify under the healed
// heal's check, writes it guarded on target, then records the forward row and
// the target's one activity entry.
func (c *Controller) forwardTo(ctx context.Context, h *state.Heal, target *board.Card, verify *board.VerifyConfig, fix protocol.CardFix) error {
	log := ctxlog.Logger(ctx).With("project", target.Project, "card_id", target.ID, "source_card", h.CardID)

	if violations := vetForward(fixContext{Card: target, Project: verify, Fix: fix, Check: h.Check}); len(violations) > 0 {
		log.Info("sentinel: fix not forwarded", "violations", violations)

		return nil
	}

	// Taken before the write, so every run that can carry the fix starts after it.
	now := c.cfg.Clock.Now()

	_, change, err := c.writeFix(ctx, target, fix)

	switch {
	case err != nil && refusal(err) == refusedByChange:
		log.Info("sentinel: fix not forwarded", "error", err)

		return nil
	case err != nil:
		return fmt.Errorf("forward the fix to %s: %w", target.ID, err)
	}

	row := state.Heal{
		Kind: state.HealKindForward, Project: target.Project, CardID: target.ID, Stage: state.StageForwarded,
		SourceCard: h.CardID, Change: &change, CreatedAt: now, UpdatedAt: now,
	}

	if row.ID, err = c.cfg.Store.OpenHeal(ctx, row); err != nil {
		return fmt.Errorf("record the fix forwarded to %s: %w", target.ID, err)
	}

	return c.logActivity(ctx, &row, "Sentinel applied a fix from "+h.CardID)
}

// onTrial reads the card's forward rows still on trial that were forwarded
// before run started, newest first.
func (c *Controller) onTrial(ctx context.Context, run state.Run) ([]state.Heal, error) {
	rows, err := c.cfg.Store.CardHeals(ctx, run.Project, run.CardID)
	if err != nil {
		return nil, fmt.Errorf("read the card's heals: %w", err)
	}

	return slices.DeleteFunc(rows, func(h state.Heal) bool { return !forwardedBefore(&h, run.StartedAt) }), nil
}

// forwardOutcome is the stage a settled run of the card moves its forward rows
// on trial to: passed when the run stopped done or the card is done, failed
// when the run's cause is forward_fix_failed, and "" otherwise, which leaves
// the fix on trial.
func forwardOutcome(key string, card *board.Card) string {
	switch {
	case key == protocol.StopCauseDone || card.State == board.StateDone:
		return state.StageForwardPassed
	case key == CauseForwardFixFailed:
		return state.StageForwardFailed
	}

	return ""
}

// judge moves each forward row on trial to stage; "" leaves them on trial.
func (c *Controller) judge(ctx context.Context, trial []state.Heal, stage string) error {
	if stage == "" {
		return nil
	}

	var errs []error

	for i := range trial {
		if err := c.saveStage(ctx, &trial[i], stage, ""); err != nil {
			errs = append(errs, fmt.Errorf("judge the fix forwarded from %s: %w", trial[i].SourceCard, err))
		}
	}

	return errors.Join(errs...)
}
