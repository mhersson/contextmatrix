package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// maxSelections bounds one import request.
	maxSelections = 200
	// writeTimeout bounds the writes of one issue, and the playbook create.
	// A shutdown waits for the unit in flight, so it must fit the shutdown
	// drain budget; local writes take milliseconds.
	writeTimeout = 10 * time.Second
)

// writeContext detaches a unit of writes from ctx: a shutdown stops the import
// between issues, never inside one, where a cancelled store write or a dropped
// commit would leave a card on disk uncommitted.
func writeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
}

// prepare validates the request and reads the epic tree: everything that can
// fail before the first write, so a Jira error or 429 leaves nothing behind.
func (s *Service) prepare(ctx context.Context, epicKey string, req ImportRequest) (*tree, error) {
	if len(req.Selections) == 0 {
		return nil, fmt.Errorf("%w: select at least one issue", ErrInvalidRequest)
	}

	if len(req.Selections) > maxSelections {
		return nil, fmt.Errorf("%w: at most %d issues per import", ErrInvalidRequest, maxSelections)
	}

	t, err := s.fetchTree(ctx, epicKey)
	if err != nil {
		return nil, err
	}

	d, err := s.playbooks.FindBySource(ctx, jira.SourceSystem, t.epic.Key)

	switch {
	case err == nil:
		if d.Run.Active() {
			return nil, errPlaybookRunning(d)
		}
	case !errors.Is(err, storage.ErrPlaybookNotFound):
		return nil, fmt.Errorf("find playbook for %s: %w", t.epic.Key, err)
	}

	return t, nil
}

func errPlaybookRunning(d *service.PlaybookDetail) error {
	return fmt.Errorf("%w: stop the run of playbook %q or wait for it to finish, then import again", ErrPlaybookRunning, d.Title)
}

// importTree creates one card per selected issue, sub-tasks before their
// parent, and adds each card to the epic's playbook as soon as it exists, so
// a stop midway leaves a consistent partial import that a re-run completes.
func (s *Service) importTree(
	ctx context.Context, t *tree, req ImportRequest, agentID string, progress func(Result),
) (*ImportResponse, error) {
	unlock := s.lockEpic(t.epic.Key)
	defer unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	idx, err := s.loadImported(ctx)
	if err != nil {
		return nil, err
	}

	pb, err := s.resolvePlaybook(ctx, t.epic, req.Playbook, agentID)
	if err != nil {
		return nil, err
	}

	run := &importRun{
		svc: s, req: req, agentID: agentID, tree: t, index: idx, progress: progress,
		playbookID: pb.ID, entries: entryRefs(pb), cards: map[string]cardRef{},
	}

	if req.IncludeEpicContext {
		run.epicSection = epicSection(t.epic, s.browseURL(t.epic.Key))
	}

	results, err := run.execute(ctx)

	return &ImportResponse{
		Playbook: PlaybookRef{ID: pb.ID, Title: pb.Title, BoardsRepo: pb.BoardsRepo},
		Results:  results,
	}, err
}

// resolvePlaybook reuses the epic's playbook from any boards repo, ignoring
// the submitted title and repo, or creates one sourced to the epic. A run
// that went active since prepare refuses the import before any card exists.
func (s *Service) resolvePlaybook(
	ctx context.Context, epic jira.Issue, req PlaybookRequest, agentID string,
) (*service.PlaybookDetail, error) {
	d, err := s.playbooks.FindBySource(ctx, jira.SourceSystem, epic.Key)
	if err == nil {
		if d.Run.Active() {
			return nil, errPlaybookRunning(d)
		}

		return d, nil
	}

	if !errors.Is(err, storage.ErrPlaybookNotFound) {
		return nil, fmt.Errorf("find playbook for %s: %w", epic.Key, err)
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = epic.Key + " " + epic.Summary
	}

	wctx, cancel := writeContext(ctx)
	defer cancel()

	d, err = s.playbooks.Create(wctx, service.CreatePlaybookInput{
		Title: title, Description: req.Description, AgentID: agentID, BoardsRepo: req.BoardsRepo,
		Source: &board.Source{System: jira.SourceSystem, ExternalID: epic.Key, ExternalURL: s.browseURL(epic.Key)},
	})
	if err != nil {
		return nil, fmt.Errorf("create playbook for %s: %w", epic.Key, err)
	}

	return d, nil
}

type importRun struct {
	svc         *Service
	req         ImportRequest
	agentID     string
	tree        *tree
	index       *importedIndex
	playbookID  string
	epicSection string
	// cards maps an issue key to its card, created in this run or earlier.
	cards map[string]cardRef
	// entries mirrors the playbook's entries after the last write.
	entries []entryRef
	// progress, when set, sees each result as it lands.
	progress func(Result)
}

func (r *importRun) record(results []Result, res Result) []Result {
	if r.progress != nil {
		r.progress(res)
	}

	return append(results, res)
}

// execute stops between two issues once ctx ends: the issue in flight
// finishes, the rest wait for a re-run.
func (r *importRun) execute(ctx context.Context) ([]Result, error) {
	selected := map[string]Selection{}
	for _, sel := range r.req.Selections {
		if _, dup := selected[sel.Key]; !dup {
			selected[sel.Key] = sel
		}
	}

	var results []Result

	for _, it := range r.tree.ordered() {
		sel, ok := selected[it.issue.Key]
		if !ok {
			continue
		}

		if err := ctx.Err(); err != nil {
			return results, err
		}

		delete(selected, it.issue.Key)
		results = r.record(results, r.importIssue(ctx, it, sel))
	}

	rest := make([]string, 0, len(selected))
	for k := range selected {
		rest = append(rest, k)
	}

	slices.Sort(rest)

	for _, k := range rest {
		results = r.record(results, Result{Key: k, Status: StatusFailed, Reason: "not a child of " + r.tree.epic.Key})
	}

	return results, nil
}

func (r *importRun) importIssue(ctx context.Context, it treeItem, sel Selection) Result {
	ctx, cancel := writeContext(ctx)
	defer cancel()

	key := it.issue.Key

	if existing := r.index.find(it.issue); existing != nil {
		ref := cardRef{project: existing.Project, card: existing.ID}
		r.cards[key] = ref

		res := Result{Key: key, Status: StatusSkipped, Project: ref.project, Card: ref.card}
		if err := r.place(ctx, ref, it.parentKey); err != nil {
			res.Status = StatusFailed
			res.Reason = "already imported as " + ref.card + " but not added to the playbook: " + r.reason(ctx, key, err)
		}

		return res
	}

	if sel.Project == "" {
		return Result{Key: key, Status: StatusFailed, Reason: "no project chosen"}
	}

	cfg, err := r.svc.cards.GetProject(ctx, sel.Project)
	if err != nil {
		return Result{Key: key, Status: StatusFailed, Project: sel.Project, Reason: r.reason(ctx, key, err)}
	}

	input := r.svc.cardInput(it.issue, cfg, r.epicSection, r.req.Vetted, r.agentID)
	input.DependsOn = r.dependencies(it.issue, sel.Project)

	card, err := r.svc.cards.CreateCard(ctx, sel.Project, input)
	if err != nil {
		return Result{Key: key, Status: StatusFailed, Project: sel.Project, Reason: r.reason(ctx, key, err)}
	}

	ref := cardRef{project: card.Project, card: card.ID}
	r.cards[key] = ref

	res := Result{Key: key, Status: StatusCreated, Project: ref.project, Card: ref.card}
	if err := r.place(ctx, ref, it.parentKey); err != nil {
		res.Status = StatusFailed
		res.Reason = "created " + ref.card + " but could not add it to the playbook; run the import again: " + r.reason(ctx, key, err)
	}

	return res
}

// dependencies are the issue's sub-tasks that live in the same CM project,
// created in this run or imported earlier. Cross-project ones are order only.
func (r *importRun) dependencies(issue jira.Issue, project string) []string {
	var deps []string

	for _, st := range r.tree.subtasks[issue.Key] {
		if ref, ok := r.lookup(st.Key); ok && ref.project == project {
			deps = append(deps, ref.card)
		}
	}

	return deps
}

func (r *importRun) lookup(key string) (cardRef, bool) {
	if ref, ok := r.cards[key]; ok {
		return ref, true
	}

	is, ok := r.tree.byKey[key]
	if !ok {
		return cardRef{}, false
	}

	c := r.index.find(is)
	if c == nil {
		return cardRef{}, false
	}

	return cardRef{project: c.Project, card: c.ID}, true
}

// place appends the card unless the playbook already has it, then moves a
// sub-task whose parent is already placed to directly before the parent. A
// run that started mid-import refuses reorders, so the card stays at the end.
func (r *importRun) place(ctx context.Context, ref cardRef, parentKey string) error {
	if indexOf(r.entries, ref) >= 0 {
		return nil
	}

	d, err := r.svc.playbooks.AddEntry(ctx, r.playbookID,
		service.PlaybookEntryInput{Type: board.EntryTypeCard, Project: ref.project, Card: ref.card}, r.agentID)
	if errors.Is(err, service.ErrDuplicateCardEntry) {
		return nil
	}

	if err != nil {
		return err
	}

	r.entries = entryRefs(d)

	if parentKey == "" {
		return nil
	}

	parent, ok := r.lookup(parentKey)
	if !ok {
		return nil
	}

	pos, last := indexOf(r.entries, parent), len(r.entries)-1
	if pos < 0 || pos >= last {
		return nil
	}

	if d.Run.Active() {
		r.leftAtEnd(ctx, ref)

		return nil
	}

	d, err = r.svc.playbooks.UpdateEntry(ctx, r.playbookID, r.entries[last].id, service.UpdateEntryInput{Position: &pos}, r.agentID)
	if errors.Is(err, service.ErrPlaybookMoveDuringRun) {
		r.leftAtEnd(ctx, ref)

		return nil
	}

	if err != nil {
		return err
	}

	r.entries = entryRefs(d)

	return nil
}

func (r *importRun) leftAtEnd(ctx context.Context, ref cardRef) {
	ctxlog.Logger(ctx).Info("jira import: playbook run active, sub-task left at the end of the playbook",
		"epic", r.tree.epic.Key, "playbook", r.playbookID, "project", ref.project, "card", ref.card)
}

// reason logs a per-issue failure and returns its safe phrase.
func (r *importRun) reason(ctx context.Context, key string, err error) string {
	ctxlog.Logger(ctx).Warn("jira import: issue failed", "epic", r.tree.epic.Key, "issue", key, "error", err)

	return failureReason(err)
}

// failureReason turns an error into text safe to show and keep: a fixed
// phrase per known class. Store and git errors can carry paths and remote
// URLs, so the raw error stays in the log.
func failureReason(err error) string {
	var ve *board.ValidationError

	switch {
	case errors.As(err, &ve):
		return ve.Message
	case errors.Is(err, context.Canceled):
		return "interrupted by a server shutdown; run the import again"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out; run the import again"
	case errors.Is(err, storage.ErrProjectNotFound):
		return "project not found"
	case errors.Is(err, storage.ErrInvalidInput):
		return "invalid input, such as an unknown boards repository"
	case errors.Is(err, service.ErrFieldTooLong):
		return "a field exceeds its size limit"
	case errors.Is(err, service.ErrRemoteUnreachable):
		return "boards remote unreachable; run the import again"
	case errors.Is(err, service.ErrPlaybookProjectNoRepo):
		return "the project has no GitHub repository, which a runnable playbook needs"
	case errors.Is(err, service.ErrPlaybookCardOwned):
		return "the card belongs to another runnable playbook"
	case errors.Is(err, service.ErrPlaybookCardForce):
		return "could not apply the playbook's settings to the card"
	case errors.Is(err, service.ErrPlaybookRunActive), errors.Is(err, ErrPlaybookRunning):
		return "the playbook's run is active; run the import again once it stops"
	default:
		return "internal error; see the server log"
	}
}
