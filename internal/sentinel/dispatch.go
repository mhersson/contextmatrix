package sentinel

import (
	"context"
	"errors"
	"fmt"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// RunKindBase fills a run-kind payload's common fields for a card: repo, base
// branch, worker image, a contents-read git token, and for model kinds the
// model, selection and endpoint of the effective channel, which it returns.
type RunKindBase func(ctx context.Context, project, cardID string, model bool) (protocol.RunKindPayload, llmchannel.Mode, error)

// noRunKindsHold is how long nothing is dispatched after the agent answered
// that it has no run kinds.
const noRunKindsHold = 15 * time.Minute

const reasonNoRunKinds = "the agent has no run kinds"

// dispatchInvestigate sends the investigate run for h's stopped run, capped at
// ceiling USD. A card whose file alone is over the body cap parks the heal.
func (c *Controller) dispatchInvestigate(ctx context.Context, h *state.Heal, ceiling float64) error {
	in, subtasks, siblings, err := c.investigateInput(ctx, h)
	if err != nil {
		return err
	}

	base, mode, err := c.base(ctx, h, true)
	if err != nil {
		return err
	}

	base.MaxCostUSD = ceiling

	p, sentSubtasks, sentSiblings, ok := investigatePayload(base, in, subtasks, siblings)
	if !ok {
		return c.park(ctx, h, "the card file is too large to send for investigation (over 1 MiB)")
	}

	h.SentSubtasks, h.SentSiblings = sentSubtasks, sentSiblings

	return c.dispatch(ctx, h, p, mode)
}

// investigateInput reads what an investigation is given: the card file and
// its resolved verify, and the files of its subtasks and unstarted siblings.
func (c *Controller) investigateInput(ctx context.Context, h *state.Heal) (protocol.InvestigateInput, []cardFile, []cardFile, error) {
	var none protocol.InvestigateInput

	card, project, err := c.cardAndProject(ctx, h)
	if err != nil {
		return none, nil, nil, err
	}

	subtasks, err := c.cfg.Cards.ListCards(ctx, h.Project, storage.CardFilter{Parent: card.ID})
	if err != nil {
		return none, nil, nil, fmt.Errorf("list subtasks of %s: %w", card.ID, err)
	}

	sibs, err := c.unstartedSiblings(ctx, card)
	if err != nil {
		return none, nil, nil, err
	}

	file, err := board.SerializeCard(card)
	if err != nil {
		return none, nil, nil, fmt.Errorf("render card %s: %w", card.ID, err)
	}

	subFiles, err := cardFiles(subtasks)
	if err != nil {
		return none, nil, nil, err
	}

	sibFiles, err := cardFiles(sibs)
	if err != nil {
		return none, nil, nil, err
	}

	in := protocol.InvestigateInput{RunID: h.RunID, StopCause: investigateStopCause(h.Cause), Card: string(file)}

	// Resolved as the fix checks resolve it, so the investigation sees the
	// verify a fix is judged against.
	if v := resolved(card.Verify, project.Verify); !v.IsZero() {
		in.Verify = protocolVerify(&v)
	}

	return in, subFiles, sibFiles, nil
}

// unstartedSiblings reads the playbook run holding card and the project's todo
// cards; a card outside an active playbook run has no siblings.
func (c *Controller) unstartedSiblings(ctx context.Context, card *board.Card) ([]*board.Card, error) {
	pb, err := c.activePlaybook(ctx, card)
	if err != nil || pb == nil {
		return nil, err
	}

	todo, err := c.todoCards(ctx, card.Project)
	if err != nil {
		return nil, err
	}

	return siblings(pb, c.cfg.Instance, cardRef{card.Project, card.ID}, todo), nil
}

// dispatchCheck sends the check run for the fix h's investigation proposed,
// with its verify resolved as the card will store it.
func (c *Controller) dispatchCheck(ctx context.Context, h *state.Heal) error {
	if h.Investigation == nil || h.Investigation.Fix == nil {
		return fmt.Errorf("check %s: the heal holds no fix", h.CardID)
	}

	fix := h.Investigation.Fix

	card, project, err := c.cardAndProject(ctx, h)
	if err != nil {
		return err
	}

	base, mode, err := c.base(ctx, h, false)
	if err != nil {
		return err
	}

	before := resolved(card.Verify, project.Verify)

	var after *board.VerifyConfig

	if fix.Verify != nil {
		v := resolvedFix(fix.Verify, project.Verify)
		after = &v
	}

	return c.dispatch(ctx, h, checkPayload(base, &before, after, fix.Section), mode)
}

// dispatch records the dispatch on the heal, then sends it, so a run the agent
// starts is always on record. Only a definite answer that started nothing
// clears it: a refusal parks the heal, busy or no run kinds leaves it for a
// later pass. A send of unknown outcome stays in flight until the run reports
// or the settle grace parks it. Nothing is recorded or sent for a while after
// the agent said it has no run kinds.
func (c *Controller) dispatch(ctx context.Context, h *state.Heal, p protocol.RunKindPayload, mode llmchannel.Mode) error {
	now := c.cfg.Clock.Now()
	if c.noRunKinds(now) {
		return nil
	}

	h.Dispatch = state.Dispatch{Kind: p.Kind, Mode: string(mode), CeilingUSD: p.MaxCostUSD, At: &now}
	h.UpdatedAt = now

	if err := c.saveHeal(ctx, h); err != nil {
		return fmt.Errorf("record the %s dispatch: %w", p.Kind, err)
	}

	sendErr := c.cfg.Backend.RunKind(ctx, p)
	if sendErr == nil {
		return nil
	}

	log := ctxlog.Logger(ctx).With("project", h.Project, "card_id", h.CardID, "kind", p.Kind)

	switch {
	case errors.Is(sendErr, backend.ErrRunKindRejected):
		log.Warn("sentinel: the agent refused a run kind", "error", sendErr)

		h.Dispatch = state.Dispatch{}

		return c.park(ctx, h, fmt.Sprintf("the agent refused the %s run", p.Kind))
	case errors.Is(sendErr, backend.ErrRunKindsUnavailable):
		c.markNoRunKinds(now)
	case !errors.Is(sendErr, backend.ErrRunKindBusy):
		// The agent may have admitted the run, so clearing it could lose its
		// spend and send a second one.
		log.Warn("sentinel: run kind send failed; it stays in flight", "error", sendErr)

		return nil
	}

	h.Dispatch = state.Dispatch{}

	// ErrHealChanged here means the agent reported anyway; the report stays.
	if err := c.saveHeal(ctx, h); err != nil {
		log.Warn("sentinel: failed dispatch not cleared", "error", err)
	}

	return fmt.Errorf("send the %s run: %w", p.Kind, sendErr)
}

func (c *Controller) cardAndProject(ctx context.Context, h *state.Heal) (*board.Card, *board.ProjectConfig, error) {
	card, err := c.cfg.Cards.GetCard(ctx, h.Project, h.CardID)
	if err != nil {
		return nil, nil, fmt.Errorf("read card %s: %w", h.CardID, err)
	}

	project, err := c.cfg.Cards.GetProject(ctx, h.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("read project %s: %w", h.Project, err)
	}

	return card, project, nil
}

func (c *Controller) base(ctx context.Context, h *state.Heal, model bool) (protocol.RunKindPayload, llmchannel.Mode, error) {
	c.mu.Lock()
	b := c.runKindBase
	c.mu.Unlock()

	if b == nil {
		return protocol.RunKindPayload{}, "", errors.New("no run-kind payload base is wired")
	}

	p, mode, err := b(ctx, h.Project, h.CardID, model)
	if err != nil {
		return protocol.RunKindPayload{}, "", fmt.Errorf("fill the run-kind payload for %s: %w", h.CardID, err)
	}

	return p, mode, nil
}

func (c *Controller) markNoRunKinds(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.noRunKindsUntil = now.Add(noRunKindsHold)
}

func (c *Controller) noRunKinds(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return now.Before(c.noRunKindsUntil)
}
