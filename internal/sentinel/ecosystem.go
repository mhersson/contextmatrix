package sentinel

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// routeHeldFindings moves every held ecosystem finding on: into a friction
// card of the upstream project in its boards repo when there is one, else
// into an issue draft. A finding is written only when its state or reason
// changes, and one whose write fails is decided again on the next pass.
func (c *Controller) routeHeldFindings(ctx context.Context, now time.Time) error {
	held, err := c.cfg.Store.HeldFindings(ctx)
	if err != nil {
		return fmt.Errorf("list held findings: %w", err)
	}

	if len(held) == 0 {
		return nil
	}

	projects, err := c.cfg.Cards.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	byUpstream := map[string][]state.Finding{}

	var drafts []state.Finding

	for _, f := range held {
		if up := upstreamProject(f.Project, projects, c.cfg.RepoOf); up != nil {
			byUpstream[up.Name] = append(byUpstream[up.Name], f)
		} else {
			drafts = append(drafts, f)
		}
	}

	for _, up := range slices.Sorted(maps.Keys(byUpstream)) {
		c.cardHeldFindings(ctx, byUpstream[up], up, now)
	}

	if len(drafts) == 0 {
		return nil
	}

	return c.draftHeldFindings(ctx, drafts, projects, now)
}

// cardHeldFindings routes held findings as code findings against the
// upstream project's friction cards. A project that cannot be read leaves
// every finding held.
func (c *Controller) cardHeldFindings(ctx context.Context, held []state.Finding, upstream string, now time.Time) {
	log := ctxlog.Logger(ctx)

	pr, err := c.readRouting(ctx, upstream, now)
	if err != nil {
		log.Warn("sentinel: reading the upstream project's friction routing failed", "project", upstream, "error", err)

		return
	}

	secrets := c.secrets()

	for _, f := range held {
		newState, reason := routeCode(f, pr.cards, secrets, pr.createdToday)

		switch newState {
		case state.FindingCarded:
			err = c.createFrictionCard(ctx, f, pr, now)
		case state.FindingQueued:
			err = c.writeOutcome(ctx, f, state.FindingHeld, reason, "", now)
		default:
			err = c.writeOutcome(ctx, f, newState, reason, "", now)
		}

		if err != nil {
			log.Warn("sentinel: routing a held finding failed", "finding", f.ID, "error", err)
		}
	}
}

// draftHeldFindings routes held findings to issue drafts, built from the
// board and config as they are now.
func (c *Controller) draftHeldFindings(ctx context.Context, held []state.Finding, projects []board.ProjectConfig, now time.Time) error {
	earlier, err := c.cfg.Store.DraftedFindings(ctx)
	if err != nil {
		return fmt.Errorf("list drafted findings: %w", err)
	}

	src := termSources{Projects: projects}
	if c.cfg.Locations != nil {
		src.Locations = c.cfg.Locations()
	}

	if c.cfg.RepoURLs != nil {
		src.RepoURLs = c.cfg.RepoURLs()
	}

	ic := issueContext{terms: issueTerms(src), version: c.cfg.Version}
	for i := range projects {
		ic.prefixes = append(ic.prefixes, projects[i].Prefix)
	}

	secrets := append(c.secrets(), urlSecrets(src)...)
	log := ctxlog.Logger(ctx)

	for _, f := range held {
		newState, reason, link := routeEcosystem(f, earlier, secrets, ic)

		if err := c.writeOutcome(ctx, f, newState, reason, link, now); err != nil {
			log.Warn("sentinel: routing a held finding failed", "finding", f.ID, "error", err)

			continue
		}

		if newState == state.FindingDrafted {
			f.State = newState
			earlier = append(earlier, f)
		}
	}

	return nil
}
