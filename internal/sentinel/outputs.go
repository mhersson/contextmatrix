package sentinel

import (
	"context"
	"fmt"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// projectRouting is what routing a project's findings reads once, then keeps
// current as the pass moves findings on.
type projectRouting struct {
	config       *board.ProjectConfig
	cards        []*board.Card
	guidance     []state.Finding
	createdToday int
}

// routeFindings moves every queued finding on: a code finding becomes a
// friction card, a guidance finding waits for the guidance PR, and duplicates
// and findings holding a secret are kept out. A finding is written only when
// its state or reason changes. A project that cannot be read leaves its
// findings queued.
func (c *Controller) routeFindings(ctx context.Context, now time.Time) error {
	queued, err := c.cfg.Store.QueuedFindings(ctx)
	if err != nil {
		return fmt.Errorf("list queued findings: %w", err)
	}

	var secrets []string
	if c.cfg.Secrets != nil {
		secrets = c.cfg.Secrets()
	}

	projects := map[string]*projectRouting{}
	log := ctxlog.Logger(ctx)

	for _, f := range queued {
		pr, ok := projects[f.Project]
		if !ok {
			pr, err = c.readRouting(ctx, f.Project, now)
			if err != nil {
				log.Warn("sentinel: reading a project's friction routing failed", "project", f.Project, "error", err)
			}

			projects[f.Project] = pr
		}

		if pr == nil {
			continue
		}

		if err := c.routeFinding(ctx, f, pr, secrets, now); err != nil {
			log.Warn("sentinel: routing a finding failed", "finding", f.ID, "error", err)
		}
	}

	return nil
}

// readRouting reads a project's routing inputs; nil when the project cannot be read.
func (c *Controller) readRouting(ctx context.Context, project string, now time.Time) (*projectRouting, error) {
	cfg, err := c.cfg.Cards.GetProject(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}

	cards, err := c.cfg.Cards.ListCards(ctx, project, storage.CardFilter{Label: frictionLabel})
	if err != nil {
		return nil, fmt.Errorf("list friction cards: %w", err)
	}

	guidance, err := c.cfg.Store.ProjectGuidance(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("list guidance: %w", err)
	}

	today, err := c.cfg.Store.CardedSince(ctx, project, dayStart(now))
	if err != nil {
		return nil, fmt.Errorf("count today's friction cards: %w", err)
	}

	return &projectRouting{config: cfg, cards: cards, guidance: guidance, createdToday: today}, nil
}

// routeFinding applies one finding's decision and folds it into the project's
// routing, so a later finding of the pass sees it.
func (c *Controller) routeFinding(ctx context.Context, f state.Finding, pr *projectRouting, secrets []string, now time.Time) error {
	var (
		newState, reason, output string
		created                  *board.Card
	)

	switch f.Kind {
	case protocol.FindingGuidance:
		newState, reason = routeGuidance(f, pr.guidance, secrets)
	case protocol.FindingCode:
		newState, reason = routeCode(f, pr.cards, secrets, pr.createdToday)

		if newState == state.FindingCarded {
			var err error

			if created, err = c.cfg.Cards.CreateCard(ctx, f.Project, frictionCardInput(f, pr.config)); err != nil {
				return fmt.Errorf("create friction card: %w", err)
			}

			output = created.ID
		}
	default:
		return nil
	}

	if newState == f.State && reason == f.Reason {
		return nil
	}

	if err := c.cfg.Store.SetFindingOutcome(ctx, f.ID, newState, reason, output, 0, now); err != nil {
		return fmt.Errorf("set finding outcome: %w", err)
	}

	switch {
	case created != nil:
		pr.cards = append(pr.cards, created)
		pr.createdToday++
	case newState == state.FindingPending:
		f.State = newState
		pr.guidance = append(pr.guidance, f)
	}

	return nil
}
