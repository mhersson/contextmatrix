package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldCards holds the holdAt-th CreateCard until release is closed, and
// counts the creates started.
type heldCards struct {
	Cards

	mu      sync.Mutex
	calls   int
	holdAt  int
	reached chan struct{}
	release chan struct{}
}

func holdCreate(inner Cards, holdAt int) *heldCards {
	return &heldCards{Cards: inner, holdAt: holdAt, reached: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldCards) CreateCard(ctx context.Context, project string, in service.CreateCardInput) (*board.Card, error) {
	h.mu.Lock()
	h.calls++
	n := h.calls
	h.mu.Unlock()

	if n == h.holdAt {
		close(h.reached)
		<-h.release
	}

	return h.Cards.CreateCard(ctx, project, in)
}

func (h *heldCards) started() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.calls
}

func (h *heldCards) waitHeld(t *testing.T) {
	t.Helper()

	select {
	case <-h.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the held create was never reached")
	}
}

// scriptedPlaybooks answers every FindBySource after the first, which is
// prepare's, from afterPrepare, and fails Create with createErr.
type scriptedPlaybooks struct {
	Playbooks

	finds        int
	afterPrepare func() (*service.PlaybookDetail, error)
	createErr    error
}

func (p *scriptedPlaybooks) FindBySource(ctx context.Context, system, externalID string) (*service.PlaybookDetail, error) {
	p.finds++
	if p.finds > 1 && p.afterPrepare != nil {
		return p.afterPrepare()
	}

	return p.Playbooks.FindBySource(ctx, system, externalID)
}

func (p *scriptedPlaybooks) Create(ctx context.Context, in service.CreatePlaybookInput) (*service.PlaybookDetail, error) {
	if p.createErr != nil {
		return nil, p.createErr
	}

	return p.Playbooks.Create(ctx, in)
}

// panicCards panics on every CreateCard.
type panicCards struct {
	Cards

	value string
}

func (p *panicCards) CreateCard(context.Context, string, service.CreateCardInput) (*board.Card, error) {
	panic(p.value)
}

// serviceWith builds another service over the env's Jira and bus, started on
// its own context, which the returned cancel ends the way the app context
// does at shutdown.
func (e *importEnv) serviceWith(t *testing.T, cards Cards, pbs Playbooks) (*Service, context.CancelFunc) {
	t.Helper()

	svc := New(e.jira, cards, pbs, Config{SiteURL: testSite, Bus: e.bus, Clock: e.clk})
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	t.Cleanup(func() {
		cancel()

		_ = svc.Shutdown(context.Background())
	})

	return svc, cancel
}

func waitDone(t *testing.T, svc *Service, id string) *ImportState {
	t.Helper()

	var st *ImportState

	require.Eventually(t, func() bool {
		var ok bool

		st, ok = svc.Lookup(id)

		return ok && st.Status != ImportRunning
	}, 5*time.Second, 10*time.Millisecond)

	return st
}

// doneEvent waits for the import's jira_import.done event.
func doneEvent(t *testing.T, ch <-chan events.Event, id string) events.Event {
	t.Helper()

	timeout := time.After(5 * time.Second)

	for {
		select {
		case ev := <-ch:
			if ev.Type == events.JiraImportDone && ev.Data["import_id"] == id {
				return ev
			}
		case <-timeout:
			t.Fatal("no jira_import.done event")
		}
	}
}

func twoIssues() ImportRequest {
	return ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}}}
}

func threeIssues() ImportRequest {
	return ImportRequest{Selections: []Selection{
		{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}, {Key: "PROJ-4", Project: "api"},
	}}
}

func TestBegin_ProgressThenDone(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	ch, unsubscribe := e.bus.Subscribe()
	defer unsubscribe()

	id, err := e.svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
	require.NoError(t, err)

	var got []events.Event

	timeout := time.After(5 * time.Second)

	for done := false; !done; {
		select {
		case ev := <-ch:
			if ev.Type != events.JiraImportProgress && ev.Type != events.JiraImportDone {
				continue // card and playbook events interleave
			}

			require.Equal(t, id, ev.Data["import_id"])
			assert.Empty(t, ev.Project, "a global event reaches every SSE client")
			got = append(got, ev)
			done = ev.Type == events.JiraImportDone
		case <-timeout:
			t.Fatal("no jira_import.done event")
		}
	}

	require.Len(t, got, 3)
	assert.Equal(t, events.JiraImportProgress, got[0].Type)
	assert.Equal(t, "PROJ-3", got[0].Data["key"], "the sub-task lands first")
	assert.Equal(t, events.JiraImportProgress, got[1].Type)
	assert.Equal(t, "PROJ-2", got[1].Data["key"])
	assert.Equal(t, StatusCreated, got[1].Data["status"])
	assert.NotEmpty(t, got[1].Data["card_id"])
	assert.Equal(t, events.JiraImportDone, got[2].Type)
	assert.Equal(t, ImportDone, got[2].Data["status"])
	assert.NotEmpty(t, got[2].Data["playbook_id"])
	assert.Equal(t, "human:alice", got[2].Agent)
}

func TestBegin_StateBeforeDuringAfter(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	gate := holdCreate(e.cards, 1)
	svc, _ := e.serviceWith(t, gate, e.pbs)

	_, ok := svc.Lookup("ji-0000000000000000")
	assert.False(t, ok, "before: an unknown id")

	id, err := svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
	require.NoError(t, err)

	gate.waitHeld(t)

	st, ok := svc.Lookup(id)
	require.True(t, ok)
	assert.Equal(t, ImportRunning, st.Status)
	assert.Equal(t, "PROJ-1", st.Epic)
	assert.Equal(t, 2, st.Total)
	assert.Empty(t, st.Results)
	assert.Nil(t, st.FinishedAt)

	close(gate.release)

	st = waitDone(t, svc, id)
	assert.Equal(t, ImportDone, st.Status)
	assert.NotEmpty(t, st.PlaybookID)
	assert.Len(t, st.Results, 2)
	require.NotNil(t, st.FinishedAt)
}

func TestLookup_FinishedImportExpires(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"))

	id, err := e.svc.Begin(context.Background(), "PROJ-1",
		ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
	require.NoError(t, err)
	waitDone(t, e.svc, id)

	e.clk.Advance(59 * time.Minute)

	_, ok := e.svc.Lookup(id)
	assert.True(t, ok, "kept for an hour after it finished")

	e.clk.Advance(2 * time.Minute)

	_, ok = e.svc.Lookup(id)
	assert.False(t, ok, "gone after the hour")
}

func TestBegin_JiraFailureWritesNothing(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.issueErr = jira.ErrRateLimited

	_, err := e.svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
	require.ErrorIs(t, err, jira.ErrRateLimited)

	list, err := e.pbs.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestBegin_FailureShowsOnlyASafePhrase(t *testing.T) {
	const leak = "/srv/boards/playbooks/pb.yaml https://user:s3cret@git.example.com/boards.git"

	unclassified := errors.New("unclassified")

	tests := []struct {
		name  string
		wrap  func(e *importEnv) (Cards, Playbooks)
		class error // the error class whose phrase the import shows
	}{
		{
			name: "finding the playbook fails after prepare",
			wrap: func(e *importEnv) (Cards, Playbooks) {
				return e.cards, &scriptedPlaybooks{Playbooks: e.pbs, afterPrepare: func() (*service.PlaybookDetail, error) {
					return nil, fmt.Errorf("read %s: %w", leak, errors.New("permission denied"))
				}}
			},
			class: unclassified,
		},
		{
			name: "creating the playbook fails",
			wrap: func(e *importEnv) (Cards, Playbooks) {
				return e.cards, &scriptedPlaybooks{Playbooks: e.pbs, createErr: fmt.Errorf("push %s: %w", leak, errors.New("rejected"))}
			},
			class: unclassified,
		},
		{
			name: "the playbook's run starts after prepare",
			wrap: func(e *importEnv) (Cards, Playbooks) {
				return e.cards, &scriptedPlaybooks{Playbooks: e.pbs, afterPrepare: func() (*service.PlaybookDetail, error) {
					return &service.PlaybookDetail{ID: "pb-1", Title: leak, Run: &board.PlaybookRun{Status: board.RunStatusRunning}}, nil
				}}
			},
			class: service.ErrPlaybookRunActive,
		},
		{
			name: "a write panics",
			wrap: func(e *importEnv) (Cards, Playbooks) {
				return &panicCards{Cards: e.cards, value: leak}, e.pbs
			},
			class: unclassified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)
			e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

			cards, pbs := tt.wrap(e)
			svc, _ := e.serviceWith(t, cards, pbs)

			ch, unsubscribe := e.bus.Subscribe()
			defer unsubscribe()

			id, err := svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
			require.NoError(t, err)

			st := waitDone(t, svc, id)
			assert.Equal(t, ImportFailed, st.Status)
			assert.Equal(t, failureReason(tt.class), st.Error)
			assert.NotContains(t, st.Error, "s3cret")
			assert.NotContains(t, st.Error, "/srv/")
			assert.Equal(t, 2, st.Total)
			assert.Empty(t, st.Results)
			assert.Zero(t, e.jiraCards(t, "api"))

			ev := doneEvent(t, ch, id)
			assert.Equal(t, ImportFailed, ev.Data["status"])
			assert.Equal(t, st.Error, ev.Data["error"], "the event carries the same safe phrase")
		})
	}
}

func TestBegin_TimeBoundEndsTheImport(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	svc, _ := e.serviceWith(t, e.cards, e.pbs)
	svc.timeout = 0 // the bound has passed before the import starts

	id, err := svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
	require.NoError(t, err)

	st := waitDone(t, svc, id)
	assert.Equal(t, ImportFailed, st.Status, "a timed-out import is not done")
	assert.Equal(t, failureReason(context.DeadlineExceeded), st.Error)
	assert.Equal(t, 2, st.Total, "the issues never reached still count, so they show as not run")
	assert.Empty(t, st.Results)
	assert.Zero(t, e.jiraCards(t, "api"))
}

func TestShutdown_StopsTheImportBetweenTwoIssues(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "A"), story("103", "PROJ-3", "B"), story("104", "PROJ-4", "C"))

	gate := holdCreate(e.cards, 2)
	svc, _ := e.serviceWith(t, gate, e.pbs)

	ch, unsubscribe := e.bus.Subscribe()
	defer unsubscribe()

	id, err := svc.Begin(context.Background(), "PROJ-1", threeIssues(), "human:alice")
	require.NoError(t, err)

	gate.waitHeld(t)

	expired, cancelExpired := context.WithTimeout(context.Background(), 0)
	defer cancelExpired()

	require.ErrorIs(t, svc.Shutdown(expired), context.DeadlineExceeded,
		"the wait is bounded by its context while an issue is in flight")

	close(gate.release)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, svc.Shutdown(ctx))

	st, ok := svc.Lookup(id)
	require.True(t, ok)
	assert.Equal(t, ImportFailed, st.Status, "a stopped import is not done")
	assert.Equal(t, failureReason(context.Canceled), st.Error)
	assert.Equal(t, 3, st.Total, "the issue never reached still counts, so it shows as not run")
	require.Len(t, st.Results, 2)
	assert.Equal(t, 2, gate.started(), "no further issue started")
	assert.Equal(t, 2, e.jiraCards(t, "api"))

	inFlight := st.Results[1]
	assert.Equal(t, StatusCreated, inFlight.Status)
	assert.True(t, e.committed(t, "api/tasks/"+inFlight.Card+".md"), "the issue in flight is committed")

	ev := doneEvent(t, ch, id)
	assert.Equal(t, ImportFailed, ev.Data["status"])
	assert.Equal(t, st.Error, ev.Data["error"])
	assert.Equal(t, st.Results, ev.Data["results"])
}

func TestShutdown_LeavesARerunnablePartialImport(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "A"), story("103", "PROJ-3", "B"), story("104", "PROJ-4", "C"))

	req := threeIssues()

	gate := holdCreate(e.cards, 2)
	svc, shutdown := e.serviceWith(t, gate, e.pbs)

	id, err := svc.Begin(context.Background(), "PROJ-1", req, "human:alice")
	require.NoError(t, err)

	gate.waitHeld(t)
	shutdown()
	close(gate.release)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, svc.Shutdown(ctx))

	st, ok := svc.Lookup(id)
	require.True(t, ok)
	assert.Equal(t, ImportFailed, st.Status)
	assert.NotEmpty(t, st.Error)
	assert.Less(t, len(st.Results), 3, "the import stopped between two issues")

	_, err = svc.Begin(context.Background(), "PROJ-1", req, "human:alice")
	require.ErrorIs(t, err, ErrNotAccepting, "a stopped service takes no new imports")

	// The restarted instance re-runs the same request cleanly.
	resp, err := e.importSync(context.Background(), "PROJ-1", req, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, 3, e.jiraCards(t, "api"), "one card per issue, none duplicated")
	assert.Len(t, e.entryNames(t, resp.Playbook.ID), 3)
}

func TestBegin_RefusesUnlessStarted(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "A"), story("103", "PROJ-3", "B"))

	notStarted := New(e.jira, e.cards, e.pbs, Config{SiteURL: testSite, Bus: e.bus, Clock: e.clk})

	stopped, _ := e.serviceWith(t, e.cards, e.pbs)
	require.NoError(t, stopped.Shutdown(context.Background()))

	for name, svc := range map[string]*Service{"never started": notStarted, "shut down": stopped} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Begin(context.Background(), "PROJ-1", twoIssues(), "human:alice")
			require.ErrorIs(t, err, ErrNotAccepting)
			assert.Zero(t, e.jiraCards(t, "api"), "nothing is written")
		})
	}
}

func TestBegin_TotalCountsEachIssueOnce(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	req := ImportRequest{Selections: []Selection{
		{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}, {Key: "PROJ-2", Project: "web"},
	}}

	id, err := e.svc.Begin(context.Background(), "PROJ-1", req, "human:alice")
	require.NoError(t, err)

	st := waitDone(t, e.svc, id)
	assert.Equal(t, ImportDone, st.Status)
	assert.Equal(t, 2, st.Total)
	assert.Len(t, st.Results, st.Total, "a completed import has one result per issue")
}
