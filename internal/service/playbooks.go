package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// playbooksDirName is the playbook file directory, duplicated here
	// because storage's own playbooksDirName is unexported in its package.
	playbooksDirName = "playbooks"

	// playbooksCommitPartition is the commit queue's per-project worker key
	// for playbook commits, keeping them serialized independently of any
	// board project's own commit stream.
	playbooksCommitPartition = "playbooks"
)

// Sentinel errors for playbook entry operations.
var (
	ErrPlaybookEntryNotFound = errors.New("playbook entry not found")
	ErrDuplicateCardEntry    = errors.New("duplicate card entry")
	ErrInvalidPlaybookEntry  = errors.New("invalid playbook entry")
)

// PlaybookStore is the persistence interface PlaybookService depends on.
// Implemented by storage.FilesystemPlaybookStore.
type PlaybookStore interface {
	List(ctx context.Context) ([]*board.Playbook, error)
	Get(ctx context.Context, id string) (*board.Playbook, error)
	Create(ctx context.Context, p *board.Playbook) error
	Save(ctx context.Context, p *board.Playbook) error
	Delete(ctx context.Context, id string) error
	ReloadIndex(ctx context.Context) error
}

// PlaybookEntryInput is one entry as submitted by an API/MCP caller.
type PlaybookEntryInput struct {
	Type    string
	Project string
	Card    string
	Text    string
	Note    string
}

// CreatePlaybookInput is the payload for creating a new playbook.
type CreatePlaybookInput struct {
	Title       string
	Description string
	AgentID     string
	Entries     []PlaybookEntryInput
}

// UpdatePlaybookInput patches a playbook's metadata. Nil fields are left
// unchanged.
type UpdatePlaybookInput struct {
	Title       *string
	Description *string
}

// UpdateEntryInput patches one playbook entry. Nil fields are left
// unchanged. Done and Text apply only to manual entries; Note applies to
// both types; Position moves the entry to that final index in the array.
type UpdateEntryInput struct {
	Done     *bool
	Note     *string
	Text     *string
	Position *int
}

// PlaybookSummary is the list-view projection of a playbook: enough to
// render the board's playbook list page without fetching every entry's
// full detail.
type PlaybookSummary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Complete int    `json:"complete"`
	Total    int    `json:"total"`
	// Segments is the per-entry status in playbook order:
	// "complete" | "active" | "missing" | "pending". The list page's
	// segmented meter renders real entry states, never a counts-derived
	// approximation (an entry is "active" when its card state is
	// in_progress).
	Segments []string `json:"segments"`
	// Projects is the count of distinct projects referenced by card
	// entries (the list row shows "N entries · M projects").
	Projects int       `json:"projects"`
	Updated  time.Time `json:"updated_at"`
}

// PlaybookEntryDetail is one entry enriched with the current state of the
// card it references (if any).
type PlaybookEntryDetail struct {
	board.PlaybookEntry

	CardTitle         string `json:"card_title,omitempty"`
	CardState         string `json:"card_state,omitempty"`
	CardAssignedAgent string `json:"card_assigned_agent,omitempty"`
	Missing           bool   `json:"missing,omitempty"`
	Complete          bool   `json:"complete"`
}

// PlaybookDetail is the full playbook view: metadata plus every entry
// resolved against the card store.
type PlaybookDetail struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	Description string                `json:"description,omitempty"`
	CreatedBy   string                `json:"created_by,omitempty"`
	Created     time.Time             `json:"created_at"`
	Updated     time.Time             `json:"updated_at"`
	Complete    int                   `json:"complete"`
	Total       int                   `json:"total"`
	Entries     []PlaybookEntryDetail `json:"entries"`
}

// PlaybookService orchestrates playbook CRUD, mirroring CardService's
// store/git/events/clock composition but scoped to the global playbooks
// partition rather than a per-project board.
type PlaybookService struct {
	store PlaybookStore
	cards storage.Store
	bus   *events.Bus
	clk   clock.Clock

	gitAutoCommit bool
	queue         *gitops.CommitQueue
	onCommit      func()

	// writeMu serializes all playbook mutations (the store's RWMutex
	// protects the index, not a read-modify-write cycle). Unlike
	// CardService.LockWrites, this never touches the commit queue - see
	// LockWrites for the ordering constraint this implies for the syncer.
	writeMu sync.Mutex
}

// NewPlaybookService creates a new PlaybookService. A nil clk defaults to
// clock.Real().
func NewPlaybookService(store PlaybookStore, cards storage.Store, bus *events.Bus, clk clock.Clock, gitAutoCommit bool) *PlaybookService {
	if clk == nil {
		clk = clock.Real()
	}

	return &PlaybookService{
		store:         store,
		cards:         cards,
		bus:           bus,
		clk:           clk,
		gitAutoCommit: gitAutoCommit,
	}
}

// SetCommitQueue registers a commit queue. Passing nil disables git commits
// (mutations still apply to the store; see enqueueCommit).
func (s *PlaybookService) SetCommitQueue(q *gitops.CommitQueue) {
	s.queue = q
}

// SetOnCommit registers a callback invoked after each successful git commit.
func (s *PlaybookService) SetOnCommit(fn func()) {
	s.onCommit = fn
}

func (s *PlaybookService) notifyCommit() {
	if s.onCommit != nil {
		s.onCommit()
	}
}

// LockWrites acquires the write mutex, preventing all playbook mutations.
// Exposed for the gitsync layer (Task 6).
//
// Unlike CardService.LockWrites, this never pauses the commit queue - it is
// a plain mutex lock. Ordering constraint owed to the caller: the syncer
// must acquire this lock BEFORE CardService.LockWrites pauses the shared
// commit queue. mutate awaits its own commit while holding writeMu; if that
// commit's job landed in an already-paused queue, writeMu would stay held
// until the queue resumes, and resume only happens after the sync returns -
// a deadlock. Cards avoid this only because their own mutex is acquired
// before the pause.
func (s *PlaybookService) LockWrites() {
	s.writeMu.Lock()
}

// UnlockWrites releases the write mutex. Paired with LockWrites.
func (s *PlaybookService) UnlockWrites() {
	s.writeMu.Unlock()
}

// Reload rebuilds the store's in-memory index from disk. Used by the syncer
// (Task 6) after a git pull brings new/changed playbook files.
func (s *PlaybookService) Reload(ctx context.Context) error {
	return s.store.ReloadIndex(ctx)
}

// Create builds a new playbook from input, validating and resolving every
// entry before the first store write so a bad entry never leaves a partial
// playbook behind.
func (s *PlaybookService) Create(ctx context.Context, input CreatePlaybookInput) (*PlaybookDetail, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := s.clk.Now().UTC().Truncate(time.Second)

	p := &board.Playbook{
		Title:       strings.TrimSpace(input.Title),
		Description: input.Description,
		CreatedBy:   input.AgentID,
		Created:     now,
		Updated:     now,
		NextEntryID: 1,
	}

	for i, in := range input.Entries {
		e, err := s.buildEntry(ctx, p, in)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}

		p.Entries = append(p.Entries, *e)
	}

	base := board.SlugifyPlaybookTitle(p.Title)
	p.ID = base

	if err := p.Validate(); err != nil {
		return nil, err
	}

	id := base
	for n := 2; ; n++ {
		p.ID = id

		err := s.store.Create(ctx, p)
		if err == nil {
			break
		}

		if !errors.Is(err, storage.ErrPlaybookExists) {
			return nil, fmt.Errorf("create playbook: %w", err)
		}

		id = fmt.Sprintf("%s-%d", base, n)
	}

	if err := s.enqueueCommit(ctx, p.ID, "created"); err != nil {
		if rbErr := s.store.Delete(ctx, p.ID); rbErr != nil {
			ctxlog.Logger(ctx).Error("playbook rollback after commit failure failed", "playbook", p.ID, "error", rbErr)

			return nil, errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
		}

		return nil, err
	}

	s.publish(events.PlaybookCreated, p.ID, input.AgentID, now)

	return s.resolve(ctx, p)
}

// List returns the list-view summary of every playbook.
func (s *PlaybookService) List(ctx context.Context) ([]PlaybookSummary, error) {
	playbooks, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list playbooks: %w", err)
	}

	summaries := make([]PlaybookSummary, 0, len(playbooks))

	for _, p := range playbooks {
		summary, err := s.summarize(ctx, p)
		if err != nil {
			return nil, err
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// Get returns the full detail view of one playbook.
func (s *PlaybookService) Get(ctx context.Context, id string) (*PlaybookDetail, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get playbook: %w", err)
	}

	return s.resolve(ctx, p)
}

// UpdateMeta patches a playbook's title and/or description. The id is
// immutable - a title edit never re-slugs it.
func (s *PlaybookService) UpdateMeta(ctx context.Context, id string, input UpdatePlaybookInput, agentID string) (*PlaybookDetail, error) {
	return s.mutate(ctx, id, "meta updated", agentID, func(p *board.Playbook) error {
		if input.Title != nil {
			title := strings.TrimSpace(*input.Title)
			if title == "" {
				return fmt.Errorf("%w: title is required", board.ErrInvalidPlaybook)
			}

			p.Title = title
		}

		if input.Description != nil {
			p.Description = *input.Description
		}

		return nil
	})
}

// Delete removes a playbook. On commit failure the deleted playbook is
// restored from its pre-delete snapshot, best-effort.
func (s *PlaybookService) Delete(ctx context.Context, id, agentID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	snapshot, err := s.store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get playbook: %w", err)
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete playbook: %w", err)
	}

	now := s.clk.Now().UTC().Truncate(time.Second)

	if err := s.enqueueCommit(ctx, id, "deleted"); err != nil {
		if rbErr := s.store.Create(ctx, snapshot); rbErr != nil {
			ctxlog.Logger(ctx).Error("playbook rollback after commit failure failed", "playbook", id, "error", rbErr)

			return errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
		}

		return err
	}

	s.publish(events.PlaybookDeleted, id, agentID, now)

	return nil
}

// mutate runs a read-modify-write cycle on one playbook under writeMu:
// load, apply fn, bump Updated, validate, save, commit, publish, resolve.
// On commit failure the pre-mutation snapshot is restored best-effort.
func (s *PlaybookService) mutate(ctx context.Context, id, action, agentID string, fn func(p *board.Playbook) error) (*PlaybookDetail, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	snapshot, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get playbook: %w", err)
	}

	p, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get playbook: %w", err)
	}

	if err := fn(p); err != nil {
		return nil, err
	}

	now := s.clk.Now().UTC().Truncate(time.Second)
	p.Updated = now

	if err := p.Validate(); err != nil {
		return nil, err
	}

	if err := s.store.Save(ctx, p); err != nil {
		return nil, fmt.Errorf("save playbook: %w", err)
	}

	if err := s.enqueueCommit(ctx, id, action); err != nil {
		if rbErr := s.store.Save(ctx, snapshot); rbErr != nil {
			ctxlog.Logger(ctx).Error("playbook rollback after commit failure failed", "playbook", id, "error", rbErr)

			return nil, errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
		}

		return nil, err
	}

	s.publish(events.PlaybookUpdated, id, agentID, now)

	return s.resolve(ctx, p)
}

// enqueueCommit enqueues a playbook-file commit and awaits its result.
// gitAutoCommit == false or no queue configured skips the commit entirely
// (still treated as success by the caller). FilesShell handles create,
// update, and delete uniformly and is immune to stale go-git state.
func (s *PlaybookService) enqueueCommit(ctx context.Context, id, action string) error {
	if !s.gitAutoCommit || s.queue == nil {
		return nil
	}

	done := s.queue.Enqueue(gitops.CommitJob{
		Project: playbooksCommitPartition,
		Kind:    gitops.CommitKindFilesShell,
		Paths:   []string{playbooksDirName + "/" + id + ".yaml"},
		Message: fmt.Sprintf("playbook(%s): %s", id, action),
		// Shell commits advance HEAD outside go-git's cached state; without
		// this, later go-git card commits can act on stale state (same as
		// flushDeferredCommit's FilesShell job).
		ReloadAfter: true,
		Ctx:         ctx,
	})
	if err := <-done; err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	s.notifyCommit()

	return nil
}

// publish sends a playbook event on the bus. Project is always empty -
// playbook events are global and must pass every SSE project filter.
func (s *PlaybookService) publish(t events.EventType, id, agentID string, ts time.Time) {
	s.bus.Publish(events.Event{
		Type: t, Project: "", Agent: agentID, Timestamp: ts,
		Data: map[string]any{"id": id},
	})
}

// buildEntry validates one input entry against the playbook and the card
// store, assigns the next entry ID, and returns it. Caller holds writeMu.
func (s *PlaybookService) buildEntry(ctx context.Context, p *board.Playbook, in PlaybookEntryInput) (*board.PlaybookEntry, error) {
	e := board.PlaybookEntry{Type: in.Type, Note: in.Note}

	switch in.Type {
	case board.EntryTypeCard:
		if in.Project == "" || in.Card == "" {
			return nil, fmt.Errorf("%w: card entries need project and card", ErrInvalidPlaybookEntry)
		}

		if p.HasCardEntry(in.Project, in.Card) {
			return nil, fmt.Errorf("%w: %s/%s", ErrDuplicateCardEntry, in.Project, in.Card)
		}

		if _, err := s.cards.GetCard(ctx, in.Project, in.Card); err != nil {
			if errors.Is(err, storage.ErrCardNotFound) || errors.Is(err, storage.ErrProjectNotFound) {
				return nil, fmt.Errorf("%w: card %s/%s not found", ErrInvalidPlaybookEntry, in.Project, in.Card)
			}

			return nil, fmt.Errorf("check card %s/%s: %w", in.Project, in.Card, err)
		}

		e.Project, e.Card = in.Project, in.Card
	case board.EntryTypeManual:
		if strings.TrimSpace(in.Text) == "" {
			return nil, fmt.Errorf("%w: manual entries need text", ErrInvalidPlaybookEntry)
		}

		e.Text = in.Text
	default:
		return nil, fmt.Errorf("%w: unknown entry type %q", ErrInvalidPlaybookEntry, in.Type)
	}

	e.ID = fmt.Sprintf("e%d", p.NextEntryID)
	p.NextEntryID++

	return &e, nil
}

// AddEntry appends one new entry to the playbook.
func (s *PlaybookService) AddEntry(ctx context.Context, id string, in PlaybookEntryInput, agentID string) (*PlaybookDetail, error) {
	return s.mutate(ctx, id, "add entry", agentID, func(p *board.Playbook) error {
		e, err := s.buildEntry(ctx, p, in)
		if err != nil {
			return err
		}

		p.Entries = append(p.Entries, *e)

		return nil
	})
}

// RemoveEntry deletes one entry from the playbook. The entry's ID is never
// reused - NextEntryID only ever increments.
func (s *PlaybookService) RemoveEntry(ctx context.Context, id, entryID, agentID string) (*PlaybookDetail, error) {
	return s.mutate(ctx, id, "remove entry "+entryID, agentID, func(p *board.Playbook) error {
		i := p.FindEntry(entryID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrPlaybookEntryNotFound, entryID)
		}

		p.Entries = slices.Delete(p.Entries, i, i+1)

		return nil
	})
}

// UpdateEntry patches one entry's done/note/text/position. Done and text
// apply only to manual entries; checking done stamps DoneBy/DoneAt from the
// caller and clock, unchecking clears both, and re-checking restamps.
// Position is the entry's final index after removal from the array:
// negative is rejected, beyond-end clamps to the end.
func (s *PlaybookService) UpdateEntry(ctx context.Context, id, entryID string, in UpdateEntryInput, agentID string) (*PlaybookDetail, error) {
	return s.mutate(ctx, id, "update entry "+entryID, agentID, func(p *board.Playbook) error {
		i := p.FindEntry(entryID)
		if i < 0 {
			return fmt.Errorf("%w: %s", ErrPlaybookEntryNotFound, entryID)
		}

		e := &p.Entries[i]

		if (in.Done != nil || in.Text != nil) && e.Type != board.EntryTypeManual {
			return fmt.Errorf("%w: done and text apply only to manual entries", ErrInvalidPlaybookEntry)
		}

		if in.Text != nil {
			if strings.TrimSpace(*in.Text) == "" {
				return fmt.Errorf("%w: text must not be empty", ErrInvalidPlaybookEntry)
			}

			e.Text = *in.Text
		}

		if in.Done != nil {
			if *in.Done {
				e.Done = true
				e.DoneBy = agentID
				at := s.clk.Now().UTC().Truncate(time.Second)
				e.DoneAt = &at
			} else {
				e.Done = false
				e.DoneBy = ""
				e.DoneAt = nil
			}
		}

		if in.Note != nil {
			e.Note = *in.Note
		}

		if in.Position != nil {
			pos := *in.Position
			if pos < 0 {
				return fmt.Errorf("%w: position must not be negative", ErrInvalidPlaybookEntry)
			}

			moved := p.Entries[i]
			p.Entries = slices.Delete(p.Entries, i, i+1)

			if pos > len(p.Entries) {
				pos = len(p.Entries)
			}

			p.Entries = slices.Insert(p.Entries, pos, moved)
		}

		return nil
	})
}

// resolve turns a stored playbook into its API-facing detail: each card
// entry is checked against the card store (missing/complete/title/state/
// agent), each manual entry's completion is its own Done flag. Missing
// entries count in Total but never in Complete. Any card-store error other
// than not-found propagates rather than being masked as missing (findCard
// precedent).
func (s *PlaybookService) resolve(ctx context.Context, p *board.Playbook) (*PlaybookDetail, error) {
	detail := &PlaybookDetail{
		ID:          p.ID,
		Title:       p.Title,
		Description: p.Description,
		CreatedBy:   p.CreatedBy,
		Created:     p.Created,
		Updated:     p.Updated,
		Total:       len(p.Entries),
		Entries:     make([]PlaybookEntryDetail, len(p.Entries)),
	}

	for i := range p.Entries {
		e := p.Entries[i]
		ed := PlaybookEntryDetail{PlaybookEntry: e}

		switch e.Type {
		case board.EntryTypeCard:
			card, err := s.cards.GetCard(ctx, e.Project, e.Card)

			switch {
			case err == nil:
				ed.CardTitle = card.Title
				ed.CardState = card.State
				ed.CardAssignedAgent = card.AssignedAgent
				ed.Complete = board.IsTerminalState(card.State)
			case errors.Is(err, storage.ErrCardNotFound), errors.Is(err, storage.ErrProjectNotFound):
				ed.Missing = true
			default:
				return nil, fmt.Errorf("resolve entry %s: %w", e.ID, err)
			}
		case board.EntryTypeManual:
			ed.Complete = e.Done
		}

		if ed.Complete {
			detail.Complete++
		}

		detail.Entries[i] = ed
	}

	return detail, nil
}

// summarize derives the list-view projection of a playbook by resolving it
// (the same mapping resolve uses for the full detail view) and reducing the
// resolved entries to segments/counts via SummarizeDetail. Shared by List
// and the MCP layer so both surfaces render identical progress.
func (s *PlaybookService) summarize(ctx context.Context, p *board.Playbook) (PlaybookSummary, error) {
	detail, err := s.resolve(ctx, p)
	if err != nil {
		return PlaybookSummary{}, err
	}

	return SummarizeDetail(detail), nil
}

// SummarizeDetail reduces a resolved detail to its list-view projection:
// one status segment per entry ("complete" | "active" | "missing" |
// "pending") plus the count of distinct projects referenced by card
// entries. Exported so callers that already hold a *PlaybookDetail (e.g.
// the MCP layer's mutation responses) can derive the same slim summary
// without a second resolve against the card store.
func SummarizeDetail(d *PlaybookDetail) PlaybookSummary {
	segments := make([]string, len(d.Entries))
	projects := make(map[string]struct{}, len(d.Entries))

	for i, ed := range d.Entries {
		switch {
		case ed.Complete:
			segments[i] = "complete"
		case ed.Missing:
			segments[i] = "missing"
		case ed.Type == board.EntryTypeCard && ed.CardState == board.StateInProgress:
			segments[i] = "active"
		default:
			segments[i] = "pending"
		}

		if ed.Type == board.EntryTypeCard {
			projects[ed.Project] = struct{}{}
		}
	}

	return PlaybookSummary{
		ID:       d.ID,
		Title:    d.Title,
		Complete: d.Complete,
		Total:    d.Total,
		Segments: segments,
		Projects: len(projects),
		Updated:  d.Updated,
	}
}
