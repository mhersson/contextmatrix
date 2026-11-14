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

// advanceRevisions steps each project's guidance: it ends an open revision
// that was answered or never reported, and sends one when nextRevision allows.
// Without a guidance base, or while friction is off, no revision is sent, so
// only open ones are stepped.
// It reports whether a revision ended, so a refused one is retried at once.
// A project whose read or step failed waits for the next pass.
func (c *Controller) advanceRevisions(ctx context.Context, r *passReads) (bool, error) {
	open, err := c.cfg.Store.OpenRevisions(ctx)
	if err != nil {
		return false, fmt.Errorf("list open revisions: %w", err)
	}

	prs, err := c.cfg.Store.StandingPRs(ctx)
	if err != nil {
		return false, fmt.Errorf("list standing prs: %w", err)
	}

	projects, err := c.revisionProjects(ctx, open, r.set.Friction)
	if err != nil {
		return false, err
	}

	ended := false

	for _, project := range projects {
		row := rowOf(open, project)

		moved, err := c.stepRevision(ctx, r, project, row, recordOf(prs, project))
		// ErrHealChanged: its report arrived since the pass read it.
		if err != nil && !errors.Is(err, state.ErrHealChanged) {
			ctxlog.Logger(ctx).Warn("sentinel: guidance revision step failed", "project", project, "error", err)
		}

		ended = ended || moved
	}

	return ended, nil
}

// revisionProjects lists the projects with an open revision, then, while a
// revision may be sent, every other project.
func (c *Controller) revisionProjects(ctx context.Context, open []state.Heal, sending bool) ([]string, error) {
	var names []string

	for _, h := range open {
		names = append(names, h.Project)
	}

	if !sending || c.guidance() == nil {
		return names, nil
	}

	projects, err := c.cfg.Cards.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	for _, p := range projects {
		if !slices.Contains(names, p.Name) {
			names = append(names, p.Name)
		}
	}

	return names, nil
}

func (c *Controller) stepRevision(ctx context.Context, r *passReads, project string, open *state.Heal, pr *state.StandingPR) (bool, error) {
	v, err := c.readRevision(ctx, r, project, open, pr)
	if err != nil {
		return false, err
	}

	switch s := nextRevision(v); s.Action {
	case revisionDispatch:
		return false, c.dispatchRevision(ctx, r, v)
	case revisionEnd:
		err := c.endRevision(ctx, open, s)

		return err == nil, err
	}

	return false, nil
}

// readRevision builds a project's view. While friction is off, or once the
// open revision was sent, only that revision's own rules decide, so nothing
// more is read; with no pending finding nothing is sent either.
func (c *Controller) readRevision(ctx context.Context, r *passReads, project string, open *state.Heal, pr *state.StandingPR) (revisionView, error) {
	v := revisionView{Project: project, PR: pr, Open: open, On: r.set.Friction, Now: r.now}

	if open != nil {
		v.Listed = inFlight(open.Dispatch) && r.listed(*open)
	}

	if !v.On || (open != nil && open.Dispatch.At != nil) {
		return v, nil
	}

	var err error
	if v.Pending, err = c.cfg.Store.PendingFindings(ctx, project); err != nil {
		return v, fmt.Errorf("list pending findings: %w", err)
	}

	if len(v.Pending) == 0 {
		return v, nil
	}

	if v.Proposed, err = c.cfg.Store.ProposedFindings(ctx, project); err != nil {
		return v, fmt.Errorf("list proposed findings: %w", err)
	}

	var since time.Time
	if pr != nil {
		since = pr.PushedAt
	}

	if v.LastRefused, err = c.cfg.Store.LastRefusedRevision(ctx, project, since); err != nil {
		return v, fmt.Errorf("read last refused revision: %w", err)
	}

	if v.LastFailed, err = c.cfg.Store.LastFailedRevision(ctx, project); err != nil {
		return v, fmt.Errorf("read last failed revision: %w", err)
	}

	if v.LastRevised, err = c.cfg.Store.LastRevisedRevision(ctx, project); err != nil {
		return v, fmt.Errorf("read last revised revision: %w", err)
	}

	c.readHealth(ctx, r)
	v.Room = roomFor(r.health)

	return v, nil
}

// dispatchRevision reads the project's PR from GitHub, as every revision must,
// decides again on what it shows, drops the pending findings no section can
// hold, then sends the guidance_pr run of the findings that fit the section on
// the project's open revision row. A payload that cannot be filled, such as a
// token GitHub refuses to mint, ends the row failed with why. Nothing is read
// or sent while the agent holds the kind.
func (c *Controller) dispatchRevision(ctx context.Context, r *passReads, v revisionView) error {
	base := c.guidance()
	if base == nil || c.noRunKinds(r.now) || c.kindHeld(protocol.RunKindGuidancePR, r.now) {
		return nil
	}

	v, err := c.refreshPR(ctx, v)
	if err != nil {
		return err
	}

	s := nextRevision(v)
	if s.Action != revisionDispatch {
		return nil
	}

	prefixes, err := c.projectPrefixes(ctx)
	if err != nil {
		return err
	}

	proposed, pending := published(v.Proposed, prefixes), published(v.Pending, prefixes)

	if err := c.dropTooLong(ctx, tooLong(pending, maxSectionBytes)); err != nil {
		return err
	}

	section, ids := packSection(proposed, pending, maxSectionBytes)
	if len(ids) == 0 {
		return nil
	}

	row, err := c.cfg.Store.OpenRevision(ctx, v.Project, r.now)
	if err != nil {
		return fmt.Errorf("open revision: %w", err)
	}

	row.SentFindings, row.Allowed, row.Reason = ids, s.Allowed, ""

	p, err := base(ctx, v.Project)
	if err != nil {
		return c.failUnsent(ctx, &row, err)
	}

	in := protocol.GuidancePRInput{
		ExpectHead: s.ExpectHead, Section: section, Allowed: s.Allowed,
		Title: prTitle(v.Project), Body: prBody(withIDs(slices.Concat(proposed, pending), ids)),
	}

	err = c.dispatch(ctx, &row, guidancePayload(p, in), "")
	r.sent(row)

	return err
}

// refreshPR reads v's PR from GitHub and then the proposed findings, which a
// merge or close settles.
func (c *Controller) refreshPR(ctx context.Context, v revisionView) (revisionView, error) {
	if v.PR == nil {
		return v, nil
	}

	fresh, err := c.readStandingPR(ctx, *v.PR, v.Now, true)
	if err != nil {
		return v, fmt.Errorf("read the standing pr: %w", err)
	}

	v.PR = &fresh

	if v.Proposed, err = c.cfg.Store.ProposedFindings(ctx, v.Project); err != nil {
		return v, fmt.Errorf("list proposed findings: %w", err)
	}

	return v, nil
}

// failUnsent ends row failed with why its payload could not be filled, so the
// back-off after a failure holds the next try; its findings stay pending.
func (c *Controller) failUnsent(ctx context.Context, row *state.Heal, cause error) error {
	if err := c.saveStage(ctx, row, state.StageRevisionFailed, "not sent: "+cause.Error()); err != nil {
		return errors.Join(cause, fmt.Errorf("end the unsent revision: %w", err))
	}

	return fmt.Errorf("fill the guidance_pr payload: %w", cause)
}

// dropTooLong drops each pending finding ids names as too long for a section.
func (c *Controller) dropTooLong(ctx context.Context, ids []int64) error {
	now := c.cfg.Clock.Now()

	for _, id := range ids {
		if err := c.cfg.Store.SetFindingOutcome(ctx, id, state.FindingDropped, reasonTooLong, "", 0, now); err != nil {
			return fmt.Errorf("drop finding %d: %w", id, err)
		}
	}

	return nil
}

// endRevision does what the open revision's outcome asks, then ends it as s
// says. A dispatch that never reported is marked failed in the same save, so a
// late report matches nothing.
func (c *Controller) endRevision(ctx context.Context, row *state.Heal, s revisionStep) error {
	var err error

	switch s.Stage {
	case state.StageRevised:
		err = c.recordRevised(ctx, *row)
	case state.StageRevisionStale:
		err = c.forgetRead(ctx, row.Project)
	case state.StageRevisionRefused:
		err = c.traceRefusal(ctx, row)
	}

	if err != nil {
		return err
	}

	if s.Unreported {
		row.Dispatch.Failure = s.Reason
	}

	if err := c.saveStage(ctx, row, s.Stage, s.Reason); err != nil {
		return fmt.Errorf("end the revision: %w", err)
	}

	return nil
}

// recordRevised stores the PR a revision pushed or found unchanged, the push
// time only when it pushed, and moves every finding it sent to proposed.
func (c *Controller) recordRevised(ctx context.Context, row state.Heal) error {
	res, now := row.Revision, c.cfg.Clock.Now()

	rec := state.StandingPR{Project: row.Project}

	old, err := c.cfg.Store.StandingPR(ctx, row.Project)
	if err != nil {
		return fmt.Errorf("read standing pr: %w", err)
	}

	if old != nil {
		rec = *old
	}

	rec.URL, rec.HeadSHA, rec.File, rec.State = res.PRURL, res.HeadSHA, res.File, state.PROpen
	if res.Outcome == protocol.GuidancePushed {
		rec.PushedAt = now
	}

	if err := c.cfg.Store.PutStandingPR(ctx, rec); err != nil {
		return fmt.Errorf("save standing pr: %w", err)
	}

	for _, id := range row.SentFindings {
		if err := c.cfg.Store.SetFindingOutcome(ctx, id, state.FindingProposed, "", res.PRURL, row.ID, now); err != nil {
			return fmt.Errorf("propose finding %d: %w", id, err)
		}
	}

	return nil
}

// forgetRead clears the project's last read and its state, so the next pass
// reads GitHub, a PR recorded merged or closed included.
func (c *Controller) forgetRead(ctx context.Context, project string) error {
	rec, err := c.cfg.Store.StandingPR(ctx, project)
	if err != nil {
		return fmt.Errorf("read standing pr: %w", err)
	}

	if rec == nil {
		return nil
	}

	rec.State, rec.ReadAt = "", time.Time{}

	if err := c.cfg.Store.PutStandingPR(ctx, *rec); err != nil {
		return fmt.Errorf("save standing pr: %w", err)
	}

	return nil
}

// traceRefusal drops each sent finding that introduces an entry the text
// check refused, and adds every entry traced to a source card to the row's
// allowed list, keeping the ones it was sent with. Every sent finding counts
// as a holder whatever its state now, so a retried end decides the same; only
// pending and proposed ones are written.
func (c *Controller) traceRefusal(ctx context.Context, row *state.Heal) error {
	sent, err := c.cfg.Store.Findings(ctx, row.SentFindings)
	if err != nil {
		return fmt.Errorf("read sent findings: %w", err)
	}

	prefixes, err := c.projectPrefixes(ctx)
	if err != nil {
		return err
	}

	sources, err := c.sourceTexts(ctx, sent)
	if err != nil {
		return err
	}

	allowed, dropped := attributeAbsent(row.Allowed, row.Revision.AbsentFromRepo, published(sent, prefixes), sources)
	now := c.cfg.Clock.Now()

	for _, f := range sent {
		reason, ok := dropped[f.ID]
		if !ok || (f.State != state.FindingPending && f.State != state.FindingProposed) {
			continue
		}

		if err := c.cfg.Store.SetFindingOutcome(ctx, f.ID, state.FindingDropped, reason, f.Output, row.ID, now); err != nil {
			return fmt.Errorf("drop finding %d: %w", f.ID, err)
		}
	}

	row.Allowed = allowed

	return nil
}

// sourceTexts reads the human-written text of each finding's source card, by
// finding ID; a deleted card has none.
func (c *Controller) sourceTexts(ctx context.Context, fs []state.Finding) (map[int64]string, error) {
	texts := make(map[int64]string, len(fs))
	byCard := map[cardRef]string{}

	for _, f := range fs {
		ref := cardRef{f.Project, f.CardID}

		text, ok := byCard[ref]
		if !ok {
			card, err := c.cfg.Cards.GetCard(ctx, f.Project, f.CardID)

			switch {
			case err == nil:
				text = humanText(card)
			case !gone(err):
				return nil, fmt.Errorf("read source card %s: %w", f.CardID, err)
			}

			byCard[ref] = text
		}

		texts[f.ID] = text
	}

	return texts, nil
}

// guidance is the wired guidance_pr payload base; nil sends no revision.
func (c *Controller) guidance() GuidanceBase {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.guidanceBase
}

func rowOf(rows []state.Heal, project string) *state.Heal {
	for i := range rows {
		if rows[i].Project == project {
			return &rows[i]
		}
	}

	return nil
}

func recordOf(prs []state.StandingPR, project string) *state.StandingPR {
	for i := range prs {
		if prs[i].Project == project {
			return &prs[i]
		}
	}

	return nil
}
