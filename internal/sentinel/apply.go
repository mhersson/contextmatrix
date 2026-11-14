package sentinel

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// apply writes the heal's fix and reruns the card. A heal that cannot rerun
// parks before anything is written, and so does a subtask the fix cannot
// retire; a card changed since the pass read it leaves the heal at apply for
// the next pass to decide.
func (c *Controller) apply(ctx context.Context, h *Heal, card *board.Card, run Run) error {
	plan, err := c.planRelaunch(ctx, card, run)
	if err != nil {
		return err
	}

	if plan.route == routeNone {
		return c.park(ctx, h, plan.reason)
	}

	change, err := c.applyFix(ctx, h, card)
	h.Change = &change

	if re, ok := errors.AsType[*refusedError](err); ok {
		return c.park(ctx, h, re.Error())
	}

	if err != nil {
		return err
	}

	return c.relaunch(ctx, h, card, run, plan)
}

// freshRerun reports whether a fix restarts the rerun from planning: a
// changed section or retired subtasks.
func freshRerun(fix *protocol.CardFix) bool {
	return fix != nil && (fix.Section != "" || len(fix.RetireSubtasks) > 0)
}

// applyFix writes the heal's fix in one write guarded on the card the pass
// read, retires its subtasks, then refreshes the run's snapshot so the edit
// check reads Sentinel's write as no edit. A heal holding no fix writes nothing.
func (c *Controller) applyFix(ctx context.Context, h *Heal, card *board.Card) (Change, error) {
	fix := healFix(*h)
	if fix == nil {
		return Change{}, nil
	}

	written, err := c.cfg.Cards.PatchCard(ctx, card.Project, card.ID, fixPatch(card, *fix))
	if err != nil {
		return Change{}, fmt.Errorf("write the fix: %w", err)
	}

	change := Change{
		SectionBefore: sentinelSection(card), SectionAfter: sentinelSection(written),
		VerifyBefore: card.Verify, VerifyAfter: written.Verify, Fresh: freshRerun(fix),
	}

	for _, id := range fix.RetireSubtasks {
		if err := c.retire(ctx, card.Project, id); err != nil {
			return change, err
		}

		change.Retired = append(change.Retired, id)
	}

	if err := c.cfg.Store.RefreshEditSnapshot(ctx, card.Project, card.ID, board.TakeEditSnapshot(written)); err != nil {
		return change, fmt.Errorf("refresh the launch snapshot: %w", err)
	}

	return change, nil
}

// fixPatch is the fix as one write guarded on card; a fresh rerun also clears
// the saved phase.
func fixPatch(card *board.Card, fix protocol.CardFix) service.PatchCardInput {
	updated := card.Updated
	in := service.PatchCardInput{AgentID: Agent, ExpectUpdated: &updated}

	if fix.Section != "" {
		in.UpsertSection = &service.SectionPatch{Heading: SectionHeading, Content: fix.Section}
	}

	if v := fix.Verify; v != nil {
		in.Verify = &board.VerifyConfig{Command: v.Command, TimeoutSeconds: v.TimeoutSeconds, Env: v.Env}
	}

	if freshRerun(&fix) {
		in.Phase = new("")
	}

	return in
}

// retire moves a subtask to not_planned as Sentinel, guarded on a fresh read
// so a claim that lands first wins.
func (c *Controller) retire(ctx context.Context, project, id string) error {
	sub, err := c.cfg.Cards.GetCard(ctx, project, id)
	if err == nil {
		_, err = c.cfg.Cards.PatchCard(ctx, project, id, service.PatchCardInput{
			State: new(board.StateNotPlanned), AgentID: Agent, ExpectUpdated: &sub.Updated,
		})
	}

	if err != nil {
		return refused(ctx, "cannot retire "+id, err)
	}

	return nil
}

// sentinelSection is the content of card's own section, "" without one.
func sentinelSection(card *board.Card) string {
	_, sections := board.BodySections(card.Body)

	for _, s := range sections {
		if s.Heading == SectionHeading {
			return s.Content
		}
	}

	return ""
}
