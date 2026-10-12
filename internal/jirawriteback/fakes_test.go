package jirawriteback

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func st(name, cat string) jira.Status { return jira.Status{Name: name, CategoryKey: cat} }

func tr(id, to, cat string, requiredFields ...string) jira.Transition {
	t := jira.Transition{ID: id, Name: to, To: st(to, cat), Fields: map[string]jira.TransitionField{}}
	for _, f := range requiredFields {
		t.Fields[f] = jira.TransitionField{Required: true}
	}

	return t
}

// fakeJira is a one-issue Jira whose workflow moves the issue on Transition.
// Whatever key is asked for resolves to that one issue.
type fakeJira struct {
	mu               sync.Mutex
	issue            jira.Issue
	workflow         map[string][]jira.Transition // from status name -> available transitions
	posts            []string
	fields           []map[string]any
	transitionsCalls int
	issueErr         error
	myselfErr        error
	postErrOnce      error           // returned by the next Transition, then cleared
	onPostErr        func(*fakeJira) // runs when postErrOnce fires (simulates a racing writer)
}

func newFakeJira(status jira.Status, workflow map[string][]jira.Transition) *fakeJira {
	return &fakeJira{
		issue:    jira.Issue{ID: "10001", Key: "PROJ-1", ProjectKey: "PROJ", IssueType: jira.IssueType{Name: "Task"}, Status: status},
		workflow: workflow,
	}
}

func (f *fakeJira) Myself(context.Context) (jira.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return jira.Account{DisplayName: "bot"}, f.myselfErr
}

func (f *fakeJira) Issue(_ context.Context, key string) (jira.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := f.issue
	out.Key = key

	return out, f.issueErr
}

func (f *fakeJira) Transitions(context.Context, string) ([]jira.Transition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.transitionsCalls++

	return f.workflow[f.issue.Status.Name], nil
}

func (f *fakeJira) Transition(_ context.Context, _, id string, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.posts = append(f.posts, id)
	f.fields = append(f.fields, fields)

	if err := f.postErrOnce; err != nil {
		f.postErrOnce = nil
		if f.onPostErr != nil {
			f.onPostErr(f)
		}

		return err
	}

	for _, t := range f.workflow[f.issue.Status.Name] {
		if t.ID == id {
			f.issue.Status = t.To

			return nil
		}
	}

	return &jira.StatusError{StatusCode: 400, Message: "transition not valid for this issue"}
}

func (f *fakeJira) status() jira.Status {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.issue.Status
}

func (f *fakeJira) postedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.posts...)
}

// simpleWorkflow: To Do <-> In Progress -> Done (resolution required).
func simpleWorkflow() map[string][]jira.Transition {
	return map[string][]jira.Transition{
		"To Do":       {tr("21", "In Progress", "indeterminate"), tr("31", "Done", "done", "resolution")},
		"In Progress": {tr("31", "Done", "done", "resolution"), tr("11", "To Do", "new")},
		"Done":        {tr("11", "To Do", "new")},
	}
}

// reviewWorkflow has no shortcut: To Do -> In Progress -> In Review -> Done.
func reviewWorkflow() map[string][]jira.Transition {
	return map[string][]jira.Transition{
		"To Do":       {tr("21", "In Progress", "indeterminate")},
		"In Progress": {tr("41", "In Review", "indeterminate"), tr("11", "To Do", "new")},
		"In Review":   {tr("31", "Done", "done")},
	}
}

// fakeCards is an in-memory card store keyed "project/ID".
type fakeCards struct {
	mu     sync.Mutex
	cards  map[string]*board.Card
	logs   []loggedLine
	logErr error
}

type loggedLine struct {
	project, id string
	entry       board.ActivityEntry
}

func newFakeCards(cards map[string]*board.Card) *fakeCards {
	return &fakeCards{cards: cards}
}

func (f *fakeCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.cards[project+"/"+id]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	cp := *c

	return &cp, nil
}

func (f *fakeCards) ListProjects(context.Context) ([]board.ProjectConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	seen := map[string]bool{}

	var out []board.ProjectConfig

	for k := range f.cards {
		p, _, _ := strings.Cut(k, "/")
		if !seen[p] {
			seen[p] = true
			out = append(out, board.ProjectConfig{Name: p})
		}
	}

	return out, nil
}

func (f *fakeCards) ListCards(_ context.Context, project string, _ storage.CardFilter) ([]*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []*board.Card

	for k, c := range f.cards {
		if strings.HasPrefix(k, project+"/") {
			cp := *c
			out = append(out, &cp)
		}
	}

	return out, nil
}

func (f *fakeCards) loggedLines() []loggedLine {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]loggedLine(nil), f.logs...)
}

func (f *fakeCards) AddSystemLogEntry(_ context.Context, project, id string, e board.ActivityEntry) (*board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.logErr != nil {
		return nil, f.logErr
	}

	c, ok := f.cards[project+"/"+id]
	if !ok {
		return nil, storage.ErrCardNotFound
	}

	f.logs = append(f.logs, loggedLine{project: project, id: id, entry: e})

	return c, nil
}

const testSite = "https://acme.atlassian.net"

func jiraCard(id, key, state string, updated time.Time) *board.Card {
	return &board.Card{
		ID: id, State: state, Updated: updated,
		Source: &board.Source{System: jira.SourceSystem, ExternalID: key, ExternalURL: testSite + "/browse/" + key},
	}
}

var testStart = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

func newTestWriteback(t *testing.T, cards *fakeCards, fj *fakeJira) (*Writeback, *opsqlite.Store, *clock.FakeClock) {
	t.Helper()

	store, err := opsqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	clk := clock.Fake(testStart)

	w, err := New(events.NewBus(), cards, store, fj, Options{
		SiteURL: testSite,
		Clock:   clk,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	return w, store, clk
}

func rowState(t *testing.T, store *opsqlite.Store, project, card, kind string) (opsqlite.JiraWriteback, bool) {
	t.Helper()

	row, ok, err := store.GetJiraWriteback(context.Background(), project, card, kind)
	require.NoError(t, err)

	return row, ok
}
