package playbookrun

import (
	"context"
	"fmt"
	"slices"
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

// set changes a stored card under the lock.
func (f *fakeCards) set(project, id string, fn func(*board.Card)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	fn(f.cards[cardKey(project, id)])
}

// queue does to a card what a successful trigger does.
func (f *fakeCards) queue(project, id string) {
	f.set(project, id, func(c *board.Card) {
		c.State = board.StateInProgress
		c.WorkerStatus = "queued"
	})
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
// card is unknown. Every run write is recorded.
type fakePlaybooks struct {
	mu    sync.Mutex
	cards *fakeCards
	pbs   map[string]*board.Playbook
	runs  []board.PlaybookRun

	// failNextSetRun, when set, is returned by the next run write instead
	// of writing, then cleared.
	failNextSetRun error

	// afterSetRun, when set, is called once after the next successful run
	// write, outside the lock, then cleared.
	afterSetRun func()

	// afterGet, when set, is called once by Get after the detail is built
	// and before it is returned, then cleared. Tests use it to hold a pass
	// mid-flight while they mutate the board underneath it.
	afterGet func()

	// gets counts Get calls, so a test can prove an event was filtered
	// without a playbook read.
	gets int
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
		if p.Run != nil {
			run := *p.Run
			copied.Run = &run
		}

		out = append(out, &copied)
	}

	return out, nil
}

// Get snapshots the playbook under the lock and builds the detail from the
// snapshot, so a walker reading a detail never races a test or a run
// writing the same playbook.
func (f *fakePlaybooks) Get(ctx context.Context, id string) (*service.PlaybookDetail, error) {
	f.mu.Lock()
	f.gets++

	stored, ok := f.pbs[id]
	if !ok {
		f.mu.Unlock()

		return nil, storage.ErrPlaybookNotFound
	}

	p := *stored
	p.Entries = append([]board.PlaybookEntry(nil), stored.Entries...)

	if stored.Run != nil {
		run := *stored.Run
		p.Run = &run
	}

	hook := f.afterGet
	f.afterGet = nil
	f.mu.Unlock()

	d := &service.PlaybookDetail{ID: p.ID, Title: p.Title, Runnable: p.Runnable, BaseBranch: p.BaseBranch, Total: len(p.Entries)}
	if p.Runnable {
		d.Branch = p.Branch()
	}

	d.Run = p.Run

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

	if hook != nil {
		hook()
	}

	return d, nil
}

// SetRunIf runs the guard against the stored run block under the same lock
// that applies the write, the way the real service runs it under writeMu.
func (f *fakePlaybooks) SetRunIf(
	ctx context.Context, id string, guard func(current *board.PlaybookRun) error, run *board.PlaybookRun, _ string,
) (*service.PlaybookDetail, error) {
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

	if guard != nil {
		if err := guard(p.Run); err != nil {
			f.mu.Unlock()

			return nil, err
		}
	}

	if run == nil {
		p.Run = nil
	} else {
		copied := *run
		p.Run = &copied
		f.runs = append(f.runs, copied)
	}

	hook := f.afterSetRun
	f.afterSetRun = nil
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

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

func (f *fakePlaybooks) getCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.gets
}

type launchCall struct {
	project, card string
	opts          LaunchOptions
}

type recordingLauncher struct {
	mu    sync.Mutex
	calls []launchCall
	err   error

	// onLaunch, when set, runs after a successful call is recorded and its
	// error is the launch's. Tests with live walkers use it to do to the card
	// what a real trigger does, so a later pass does not relaunch a bare todo card.
	onLaunch func(ctx context.Context, project, card string) error
}

func (l *recordingLauncher) launch(ctx context.Context, project, card string, opts LaunchOptions) error {
	l.mu.Lock()
	l.calls = append(l.calls, launchCall{project: project, card: card, opts: opts})
	err, hook := l.err, l.onLaunch
	l.mu.Unlock()

	if err != nil {
		return err
	}

	// The hook runs outside the lock: a test may block inside it to hold a
	// launch in flight while it reads counts or drives Stop.
	if hook != nil {
		return hook(ctx, project, card)
	}

	return nil
}

func (l *recordingLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.calls)
}

func (l *recordingLauncher) call(i int) launchCall {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.calls[i]
}

type recordingStopper struct {
	mu    sync.Mutex
	calls []string
	err   error

	// onStop, when set, runs after a successful call, outside the lock, so a
	// test can mark the card the way the stop endpoint does.
	onStop func(project, card string)
}

func (s *recordingStopper) stop(_ context.Context, project, card string) error {
	s.mu.Lock()
	s.calls = append(s.calls, card)
	err, hook := s.err, s.onStop
	s.mu.Unlock()

	if err == nil && hook != nil {
		hook(project, card)
	}

	return err
}

func (s *recordingStopper) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.calls)
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
