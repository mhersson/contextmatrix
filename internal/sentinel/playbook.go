package sentinel

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// activePlaybook reads the playbook the card's active lock names; nil when
// playbooks are off or no active run holds the card.
func (c *Controller) activePlaybook(ctx context.Context, card *board.Card) (*service.PlaybookDetail, error) {
	if c.cfg.Playbooks == nil || !card.PlaybookLock.Active() {
		return nil, nil //nolint:nilnil // no active playbook run holds the card
	}

	pb, err := c.cfg.Playbooks.Get(ctx, card.PlaybookLock.ID)
	if err != nil {
		return nil, fmt.Errorf("read playbook %s: %w", card.PlaybookLock.ID, err)
	}

	return pb, nil
}

// todoCards reads the project's todo cards by ID.
func (c *Controller) todoCards(ctx context.Context, project string) (map[string]*board.Card, error) {
	cards, err := c.cfg.Cards.ListCards(ctx, project, storage.CardFilter{State: board.StateTodo})
	if err != nil {
		return nil, fmt.Errorf("list todo cards of %s: %w", project, err)
	}

	byID := make(map[string]*board.Card, len(cards))
	for _, card := range cards {
		byID[card.ID] = card
	}

	return byID, nil
}

// siblings are the cards of pb's active run on instance that have not started:
// card entries in card's project that are incomplete, in todo, vetted, with no
// worker status, and not card itself, in entry order. todo holds the
// candidates by ID.
func siblings(pb *service.PlaybookDetail, instance string, card cardRef, todo map[string]*board.Card) []*board.Card {
	if pb == nil || !pb.Runnable || !pb.Run.Active() || pb.Run.Instance != instance {
		return nil
	}

	var out []*board.Card

	for _, e := range pb.Entries {
		c := todo[e.Card]
		if e.Type != board.EntryTypeCard || e.Project != card.project || e.Complete || e.Card == card.id ||
			c == nil || c.State != board.StateTodo || c.NeedsVetting() || c.WorkerStatus != "" {
			continue
		}

		out = append(out, c)
	}

	return out
}
