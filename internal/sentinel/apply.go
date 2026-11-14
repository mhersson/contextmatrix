package sentinel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

// apply writes the heal's fix and reruns the card. A heal that cannot rerun
// parks before anything is written, and a refused write or retire parks it; a
// card changed since the pass read it leaves the heal at apply for the next
// pass to decide.
func (c *Controller) apply(ctx context.Context, h *state.Heal, card *board.Card, run state.Run, subtasks []*board.Card) error {
	plan, err := c.planRelaunch(ctx, card, run)
	if err != nil {
		return err
	}

	if plan.route == routeNone {
		return c.park(ctx, h, plan.reason)
	}

	written, change, err := c.applyFix(ctx, h, card, subtasks)
	h.Change = &change

	if re, ok := errors.AsType[*refusedError](err); ok {
		return c.park(ctx, h, re.Error())
	}

	if err != nil {
		return err
	}

	return c.relaunch(ctx, h, written, run, plan)
}

// freshRerun reports whether a fix restarts the rerun from planning: it
// changes card's own section or retires subtasks while none of subtasks has
// started, as a fresh run plans again from base and would strand started work.
// checkRetire already refuses a retire once a subtask has started.
func freshRerun(card *board.Card, fix protocol.CardFix, subtasks []*board.Card) bool {
	return (sectionChanges(card, fix.Section) || len(fix.RetireSubtasks) > 0) && !slices.ContainsFunc(subtasks, started)
}

// started reports whether a subtask has left todo for anything but not_planned.
func started(sub *board.Card) bool {
	return sub.State != board.StateTodo && sub.State != board.StateNotPlanned
}

// sectionChanges reports whether writing section changes card's own section;
// whitespace at either end does not count.
func sectionChanges(card *board.Card, section string) bool {
	return section != "" && strings.TrimSpace(section) != strings.TrimSpace(sentinelSection(card))
}

// applyFix writes the heal's fix in one write guarded on the card the pass
// read, retires its subtasks, then refreshes the run's snapshot so the edit
// check reads Sentinel's write as no edit. It returns the card as written, so
// the rerun's guarded move reads Sentinel's own write as no change. A heal
// holding no fix writes nothing.
func (c *Controller) applyFix(ctx context.Context, h *state.Heal, card *board.Card, subtasks []*board.Card) (*board.Card, state.Change, error) {
	fix := healFix(*h)
	if fix == nil {
		return card, state.Change{}, nil
	}

	written, change, err := c.writeFix(ctx, card, *fix, subtasks)
	if err != nil {
		return nil, state.Change{}, err
	}

	for _, id := range fix.RetireSubtasks {
		if err := c.retire(ctx, card.Project, id); err != nil {
			return nil, change, err
		}

		change.Retired = append(change.Retired, id)
	}

	if err := c.cfg.Store.RefreshEditSnapshot(ctx, card.Project, card.ID, board.TakeEditSnapshot(written)); err != nil {
		return nil, change, fmt.Errorf("refresh the launch snapshot: %w", err)
	}

	return written, change, nil
}

// writeFix writes the fix's section and verify in one write guarded on card,
// clearing the phase when the rerun is fresh by card's subtasks, and returns
// the written card and what changed. A card changed since it was read comes
// back as is, for the next pass to decide again; any other error is a refusal,
// so the same write is not retried every pass.
func (c *Controller) writeFix(ctx context.Context, card *board.Card, fix protocol.CardFix, subtasks []*board.Card) (*board.Card, state.Change, error) {
	fresh := freshRerun(card, fix, subtasks)

	written, err := c.cfg.Cards.PatchCard(ctx, card.Project, card.ID, fixPatch(card, fix, fresh))

	switch {
	case errors.Is(err, service.ErrCardChanged):
		return nil, state.Change{}, fmt.Errorf("write the fix: %w", err)
	case err != nil:
		return nil, state.Change{}, refused(ctx, "cannot write the fix", err)
	}

	return written, state.Change{
		SectionBefore: sentinelSection(card), SectionAfter: sentinelSection(written),
		VerifyBefore: card.Verify, VerifyAfter: written.Verify, Fresh: fresh,
	}, nil
}

// fixPatch is the fix as one write guarded on card, its section only when it
// changes; a fresh rerun also clears the saved phase.
func fixPatch(card *board.Card, fix protocol.CardFix, fresh bool) service.PatchCardInput {
	updated := card.Updated
	in := service.PatchCardInput{AgentID: Agent, ExpectUpdated: &updated}

	if sectionChanges(card, fix.Section) {
		in.UpsertSection = &service.SectionPatch{Heading: SectionHeading, Content: fix.Section}
	}

	if fix.Verify != nil {
		in.Verify = boardVerify(fix.Verify)
	}

	if fresh {
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
