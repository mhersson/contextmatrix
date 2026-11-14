package sentinel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// prReadEvery is the least time between two GitHub reads of one project's PR.
const prReadEvery = 15 * time.Minute

const (
	closedReason    = "the pull request was closed without merging"
	notInFileReason = "no line of it is in the merged file"
)

// readStandingPRs reads each project's standing PR that is due. A failed read
// is logged and keeps the old state, to be read again next pass.
func (c *Controller) readStandingPRs(ctx context.Context, now time.Time) error {
	recs, err := c.cfg.Store.StandingPRs(ctx)
	if err != nil {
		return fmt.Errorf("list standing prs: %w", err)
	}

	for _, rec := range recs {
		if _, err := c.readStandingPR(ctx, rec, now, false); err != nil {
			ctxlog.Logger(ctx).Warn("sentinel: reading a standing PR failed", "project", rec.Project, "error", err)
		}
	}

	return nil
}

// readStandingPR reads rec's PR from GitHub when force is set or the last read
// is old, settles the proposed findings of a merged or closed PR, and saves
// the state and read time. A merged or closed record is final and a missing
// GitHub reads nothing; both come back unchanged.
func (c *Controller) readStandingPR(ctx context.Context, rec state.StandingPR, now time.Time, force bool) (state.StandingPR, error) {
	c.mu.Lock()
	gh := c.github
	c.mu.Unlock()

	final := rec.State == state.PRMerged || rec.State == state.PRClosed
	if gh == nil || final || (!force && now.Sub(rec.ReadAt) < prReadEvery) {
		return rec, nil
	}

	pull, err := gh.PullState(ctx, rec.Project, rec.URL)
	if err != nil {
		return c.keepAttempt(ctx, rec, now, err)
	}

	next := rec
	next.State, next.ReadAt = prState(rec, pull), now

	if next.State == state.PRMerged || next.State == state.PRClosed {
		if err := c.settleFindings(ctx, gh, next, pull, now); err != nil {
			return c.keepAttempt(ctx, rec, now, err)
		}
	}

	if err := c.cfg.Store.PutStandingPR(ctx, next); err != nil {
		return rec, fmt.Errorf("save standing pr: %w", err)
	}

	return next, nil
}

// keepAttempt saves the time of a failed read and keeps the state, so an
// outage is retried at most every prReadEvery; it returns readErr.
func (c *Controller) keepAttempt(ctx context.Context, rec state.StandingPR, now time.Time, readErr error) (state.StandingPR, error) {
	rec.ReadAt = now

	if err := c.cfg.Store.PutStandingPR(ctx, rec); err != nil {
		return rec, errors.Join(readErr, fmt.Errorf("save standing pr: %w", err))
	}

	return rec, readErr
}

// settleFindings moves the project's proposed findings to merged or rejected
// by the state of rec's PR. A failed write leaves the rest proposed, so the
// next read settles them.
func (c *Controller) settleFindings(ctx context.Context, gh GitHub, rec state.StandingPR, pull PullState, now time.Time) error {
	proposed, err := c.cfg.Store.ProposedFindings(ctx, rec.Project)
	if err != nil {
		return fmt.Errorf("list proposed findings: %w", err)
	}

	reason := closedReason

	var merged []int64

	if rec.State == state.PRMerged {
		file, found, err := gh.FileAt(ctx, rec.Project, rec.URL, rec.File, pull.MergeSHA)
		if err != nil {
			return fmt.Errorf("read merged file: %w", err)
		}

		prefixes, err := c.projectPrefixes(ctx)
		if err != nil {
			return err
		}

		published := make([]state.Finding, len(proposed))
		for i, f := range proposed {
			published[i] = f
			published[i].Text = publishedText(f, prefixes)
		}

		merged, _ = mergedFates(file, found, published)
		reason = notInFileReason
	}

	for _, f := range proposed {
		st, why := state.FindingRejected, reason
		if slices.Contains(merged, f.ID) {
			st, why = state.FindingMerged, ""
		}

		if err := c.cfg.Store.SetFindingOutcome(ctx, f.ID, st, why, f.Output, f.RevisionID, now); err != nil {
			return fmt.Errorf("settle finding %d: %w", f.ID, err)
		}
	}

	return nil
}

// projectPrefixes lists every project's card ID prefix.
func (c *Controller) projectPrefixes(ctx context.Context) ([]string, error) {
	projects, err := c.cfg.Cards.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	prefixes := make([]string, 0, len(projects))
	for _, p := range projects {
		if p.Prefix != "" {
			prefixes = append(prefixes, p.Prefix)
		}
	}

	return prefixes, nil
}
