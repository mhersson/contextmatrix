package playbookrun

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func cardKey(project, id string) string { return project + "/" + id }

type fakeCards struct {
	mu          sync.Mutex
	cards       map[string]*board.Card
	elsewhere   map[string]bool
	transitions []string
	forced      []string
}

func newFakeCards() *fakeCards {
	return &fakeCards{cards: map[string]*board.Card{}, elsewhere: map[string]bool{}}
}

func (f *fakeCards) add(c *board.Card) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cards[cardKey(c.Project, c.ID)] = c
}

func (f *fakeCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.cards[cardKey(project, id)]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	copied := *c

	return &copied, nil
}

func (f *fakeCards) ClaimedElsewhere(c *board.Card) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.elsewhere[cardKey(c.Project, c.ID)]
}

func (f *fakeCards) TransitionTo(_ context.Context, project, id, state string) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.cards[cardKey(project, id)]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	f.transitions = append(f.transitions, fmt.Sprintf("%s:%s->%s", id, c.State, state))
	c.State = state
	copied := *c

	return &copied, nil
}

func (f *fakeCards) ForcePlaybookSettings(_ context.Context, project, id, _, branch, _ string) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.cards[cardKey(project, id)]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	c.ApplyPlaybookSettings(branch)

	f.forced = append(f.forced, id)
	copied := *c

	return &copied, nil
}

// fakePlaybooks resolves details the way the service does: card entries
// complete on a terminal state, manual entries on done, missing when the
// card is unknown. Every SetRun is recorded.
type fakePlaybooks struct {
	mu    sync.Mutex
	cards *fakeCards
	pbs   map[string]*board.Playbook
	runs  []board.PlaybookRun

	// failNextSetRun, when set, is returned by the next SetRun call instead
	// of writing, then cleared.
	failNextSetRun error
}

func newFakePlaybooks(cards *fakeCards) *fakePlaybooks {
	return &fakePlaybooks{cards: cards, pbs: map[string]*board.Playbook{}}
}

func (f *fakePlaybooks) add(p *board.Playbook) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pbs[p.ID] = p
}

func (f *fakePlaybooks) List(context.Context) ([]*board.Playbook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]*board.Playbook, 0, len(f.pbs))
	for _, p := range f.pbs {
		copied := *p
		out = append(out, &copied)
	}

	return out, nil
}

func (f *fakePlaybooks) Get(ctx context.Context, id string) (*service.PlaybookDetail, error) {
	f.mu.Lock()
	p, ok := f.pbs[id]
	f.mu.Unlock()

	if !ok {
		return nil, storage.ErrPlaybookNotFound
	}

	d := &service.PlaybookDetail{ID: p.ID, Title: p.Title, Runnable: p.Runnable, BaseBranch: p.BaseBranch, Total: len(p.Entries)}
	if p.Runnable {
		d.Branch = p.Branch()
	}

	if p.Run != nil {
		run := *p.Run
		d.Run = &run
	}

	for _, e := range p.Entries {
		ed := service.PlaybookEntryDetail{PlaybookEntry: e}

		switch e.Type {
		case board.EntryTypeCard:
			c, err := f.cards.GetCard(ctx, e.Project, e.Card)
			if err != nil {
				ed.Missing = true
			} else {
				ed.CardState = c.State
				ed.Complete = board.IsTerminalState(c.State)
			}
		case board.EntryTypeManual:
			ed.Complete = e.Done
		}

		d.Entries = append(d.Entries, ed)
	}

	return d, nil
}

func (f *fakePlaybooks) SetRun(ctx context.Context, id string, run *board.PlaybookRun, _ string) (*service.PlaybookDetail, error) {
	f.mu.Lock()

	if err := f.failNextSetRun; err != nil {
		f.failNextSetRun = nil
		f.mu.Unlock()

		return nil, err
	}

	p, ok := f.pbs[id]
	if !ok {
		f.mu.Unlock()

		return nil, storage.ErrPlaybookNotFound
	}

	if !p.Runnable {
		f.mu.Unlock()

		return nil, service.ErrPlaybookNotRunnable
	}

	if run == nil {
		p.Run = nil
	} else {
		copied := *run
		p.Run = &copied
		f.runs = append(f.runs, copied)
	}
	f.mu.Unlock()

	return f.Get(ctx, id)
}

func (f *fakePlaybooks) lastRun() board.PlaybookRun {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.runs[len(f.runs)-1]
}

func (f *fakePlaybooks) runWrites() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.runs)
}

type launchCall struct {
	project, card string
	opts          LaunchOptions
}

type recordingLauncher struct {
	mu    sync.Mutex
	calls []launchCall
	err   error
}

func (l *recordingLauncher) launch(_ context.Context, project, card string, opts LaunchOptions) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, launchCall{project: project, card: card, opts: opts})

	return l.err
}

func (l *recordingLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.calls)
}

type recordingStopper struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (s *recordingStopper) stop(_ context.Context, _, card string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, card)

	return s.err
}

func todoCard(project, id string) *board.Card {
	return &board.Card{ID: id, Project: project, Title: id, State: board.StateTodo, Created: time.Now(), Updated: time.Now()}
}

func runnablePlaybook(id string, entries ...board.PlaybookEntry) *board.Playbook {
	return &board.Playbook{ID: id, Title: id, Runnable: true, NextEntryID: len(entries) + 1, Entries: entries}
}

func cardEntry(id, project, card string) board.PlaybookEntry {
	return board.PlaybookEntry{ID: id, Type: board.EntryTypeCard, Project: project, Card: card}
}

func manualEntry(id, text string) board.PlaybookEntry {
	return board.PlaybookEntry{ID: id, Type: board.EntryTypeManual, Text: text}
}
