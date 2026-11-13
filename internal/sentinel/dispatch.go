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
func (c *Controller) dispatchInvestigate(ctx context.Context, h *Heal, ceiling float64) error {
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
func (c *Controller) investigateInput(ctx context.Context, h *Heal) (protocol.InvestigateInput, []cardFile, []cardFile, error) {
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

	in := protocol.InvestigateInput{
		RunID: h.RunID, StopCause: investigateStopCause(h.Cause), Card: string(file),
		Verify: protocolVerify(board.ResolveVerify(card.Verify, project.Verify)),
	}

	return in, subFiles, sibFiles, nil
}

// unstartedSiblings reads the playbook run holding card and the project's todo
// cards; a card outside an active playbook run has no siblings.
func (c *Controller) unstartedSiblings(ctx context.Context, card *board.Card) ([]*board.Card, error) {
	if c.cfg.Playbooks == nil || !card.PlaybookLock.Active() {
		return nil, nil
	}

	pb, err := c.cfg.Playbooks.Get(ctx, card.PlaybookLock.ID)
	if err != nil {
		return nil, fmt.Errorf("read playbook %s: %w", card.PlaybookLock.ID, err)
	}

	todo, err := c.cfg.Cards.ListCards(ctx, card.Project, storage.CardFilter{State: board.StateTodo})
	if err != nil {
		return nil, fmt.Errorf("list todo cards of %s: %w", card.Project, err)
	}

	return siblings(pb, c.cfg.Instance, card, todo), nil
}

// dispatchCheck sends the check run for the fix h's investigation proposed,
// with its verify resolved as the card will store it.
func (c *Controller) dispatchCheck(ctx context.Context, h *Heal) error {
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
// starts is always on record. A send the agent refuses parks the heal; any
// other failed send clears the dispatch for a later pass to retry. Nothing is
// recorded or sent for a while after the agent said it has no run kinds.
func (c *Controller) dispatch(ctx context.Context, h *Heal, p protocol.RunKindPayload, mode llmchannel.Mode) error {
	now := c.cfg.Clock.Now()
	if c.noRunKinds(now) {
		return nil
	}

	h.Dispatch = Dispatch{Kind: p.Kind, Mode: string(mode), CeilingUSD: p.MaxCostUSD, At: &now}
	h.UpdatedAt = now

	if err := c.cfg.Store.SaveHeal(ctx, h); err != nil {
		return fmt.Errorf("record the %s dispatch: %w", p.Kind, err)
	}

	sendErr := c.cfg.Backend.RunKind(ctx, p)
	if sendErr == nil {
		return nil
	}

	log := ctxlog.Logger(ctx).With("project", h.Project, "card_id", h.CardID, "kind", p.Kind)
	h.Dispatch = Dispatch{}

	if errors.Is(sendErr, backend.ErrRunKindRejected) {
		log.Warn("sentinel: the agent refused a run kind", "error", sendErr)

		return c.park(ctx, h, fmt.Sprintf("the agent refused the %s run", p.Kind))
	}

	if errors.Is(sendErr, backend.ErrRunKindsUnavailable) {
		c.markNoRunKinds(now)
	}

	// ErrHealChanged here means the agent reported anyway; the report stays.
	if err := c.cfg.Store.SaveHeal(ctx, h); err != nil {
		log.Warn("sentinel: failed dispatch not cleared", "error", err)
	}

	return fmt.Errorf("send the %s run: %w", p.Kind, sendErr)
}

func (c *Controller) cardAndProject(ctx context.Context, h *Heal) (*board.Card, *board.ProjectConfig, error) {
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

func (c *Controller) base(ctx context.Context, h *Heal, model bool) (protocol.RunKindPayload, llmchannel.Mode, error) {
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
