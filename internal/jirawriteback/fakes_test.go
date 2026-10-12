package jirawriteback

import (
	"context"
	"sync"

	"github.com/mhersson/contextmatrix/internal/jira"
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
