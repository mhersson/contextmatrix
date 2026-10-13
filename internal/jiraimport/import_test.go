package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importSync runs an import in the foreground, the way the background job does.
func (e *importEnv) importSync(ctx context.Context, epicKey string, req ImportRequest, agentID string) (*ImportResponse, error) {
	t, err := e.svc.prepare(ctx, epicKey, req)
	if err != nil {
		return nil, err
	}

	return e.svc.importTree(ctx, t, req, agentID, nil)
}

func resultsByKey(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.Key] = r
	}

	return m
}

func (e *importEnv) card(t *testing.T, project, id string) *board.Card {
	t.Helper()

	c, err := e.cards.GetCard(context.Background(), project, id)
	require.NoError(t, err)

	return c
}

func (e *importEnv) entryNames(t *testing.T, id string) []string {
	t.Helper()

	d, err := e.pbs.Get(context.Background(), id)
	require.NoError(t, err)

	var names []string
	for _, en := range d.Entries {
		names = append(names, en.Project+"/"+en.Card)
	}

	return names
}

func (e *importEnv) jiraCards(t *testing.T, project string) int {
	t.Helper()

	cs, err := e.cards.ListCards(context.Background(), project, storage.CardFilter{SourceSystem: jira.SourceSystem})
	require.NoError(t, err)

	return len(cs)
}

func (e *importEnv) makeRunnable(t *testing.T, id string) {
	t.Helper()

	_, err := e.pbs.UpdateMeta(context.Background(), id, service.UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)
}

// setRun gives a runnable playbook a run block with status, as Play, the
// runner or Stop would.
func (e *importEnv) setRun(t *testing.T, id, status string) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	_, err := e.pbs.SetRunIf(context.Background(), id, nil,
		&board.PlaybookRun{Status: status, StartedAt: now, UpdatedAt: now}, "human:alice")
	require.NoError(t, err)
}

// committed reports whether path is in the boards repo's HEAD commit.
func (e *importEnv) committed(t *testing.T, path string) bool {
	t.Helper()

	repo, err := git.PlainOpen(e.dir)
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	_, err = commit.File(path)

	return err == nil
}

// gatedCards holds every CreateCard until release closes, so a test can act
// while an issue's writes are in flight.
type gatedCards struct {
	Cards

	started chan string // receives the issue key of each held create
	release chan struct{}
}

func (g *gatedCards) CreateCard(ctx context.Context, project string, in service.CreateCardInput) (*board.Card, error) {
	g.started <- in.Source.ExternalID

	<-g.release

	return g.Cards.CreateCard(ctx, project, in)
}

// failingCards fails every CreateCard that fail selects.
type failingCards struct {
	Cards

	fail func(service.CreateCardInput) bool
}

func (f *failingCards) CreateCard(ctx context.Context, project string, in service.CreateCardInput) (*board.Card, error) {
	if f.fail(in) {
		return nil, errors.New("injected create failure")
	}

	return f.Cards.CreateCard(ctx, project, in)
}

// failingAdd fails the first AddEntry when failFirst is set.
type failingAdd struct {
	Playbooks

	failFirst bool
}

func (f *failingAdd) AddEntry(ctx context.Context, id string, in service.PlaybookEntryInput, agentID string) (*service.PlaybookDetail, error) {
	if f.failFirst {
		f.failFirst = false

		return nil, errors.New("injected add failure")
	}

	return f.Playbooks.AddEntry(ctx, id, in, agentID)
}

// hookedPlaybooks runs a hook once before the next AddEntry or UpdateEntry,
// and counts UpdateEntry calls.
type hookedPlaybooks struct {
	Playbooks

	beforeAdd    func()
	beforeUpdate func()
	updates      int
}

func (h *hookedPlaybooks) AddEntry(ctx context.Context, id string, in service.PlaybookEntryInput, agentID string) (*service.PlaybookDetail, error) {
	if fn := h.beforeAdd; fn != nil {
		h.beforeAdd = nil

		fn()
	}

	return h.Playbooks.AddEntry(ctx, id, in, agentID)
}

func (h *hookedPlaybooks) UpdateEntry(
	ctx context.Context, id, entryID string, in service.UpdateEntryInput, agentID string,
) (*service.PlaybookDetail, error) {
	h.updates++

	if fn := h.beforeUpdate; fn != nil {
		h.beforeUpdate = nil

		fn()
	}

	return h.Playbooks.UpdateEntry(ctx, id, entryID, in, agentID)
}

func TestImport_ChildrenFirstSameProjectDependencies(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"),
		subtask("103", "PROJ-3", "PROJ-2", "Cart schema"),
		subtask("104", "PROJ-4", "PROJ-2", "Cart UI"))

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}, {Key: "PROJ-4", Project: "web"}},
		Playbook:   PlaybookRequest{Title: "PROJ-1 Checkout revamp", Description: "Checkout revamp"},
	}, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(resp.Results)
	for _, k := range []string{"PROJ-2", "PROJ-3", "PROJ-4"} {
		require.Equal(t, StatusCreated, r[k].Status, k)

		c := e.card(t, r[k].Project, r[k].Card)
		assert.Empty(t, c.Parent, "a Jira child is never a CM subtask")
		assert.NotEqual(t, board.SubtaskType, c.Type)
		assert.False(t, c.Vetted)
		require.NotNil(t, c.Source)
		assert.Equal(t, k, c.Source.ExternalID)
	}

	parent := e.card(t, "api", r["PROJ-2"].Card)
	assert.Equal(t, []string{r["PROJ-3"].Card}, parent.DependsOn, "only the same-project sub-task is a dependency")

	assert.Equal(t, []string{
		"api/" + r["PROJ-3"].Card, "web/" + r["PROJ-4"].Card, "api/" + r["PROJ-2"].Card,
	}, e.entryNames(t, resp.Playbook.ID), "sub-tasks come before their parent")

	d, err := e.pbs.Get(context.Background(), resp.Playbook.ID)
	require.NoError(t, err)
	require.NotNil(t, d.Source)
	assert.Equal(t, "PROJ-1", d.Source.ExternalID)
}

func TestImport_VettedAndEpicContext(t *testing.T) {
	tests := []struct {
		name           string
		vetted         bool
		include        bool
		epicHasText    bool
		wantEpicInBody bool
	}{
		{"defaults", false, true, true, true},
		{"vetted, context off", true, false, true, false},
		{"context on, empty epic", false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)

			if !tt.epicHasText {
				epic := epicIssue()
				epic.Description = nil
				e.jira.add(epic)
			}

			e.jira.add(story("102", "PROJ-2", "Cart API"))

			resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
				Selections:         []Selection{{Key: "PROJ-2", Project: "api"}},
				Vetted:             tt.vetted,
				IncludeEpicContext: tt.include,
			}, "human:alice")
			require.NoError(t, err)

			c := e.card(t, "api", resp.Results[0].Card)
			assert.Equal(t, tt.vetted, c.Vetted)
			assert.Equal(t, tt.wantEpicInBody, strings.Contains(c.Body, "Epic goal"))
			assert.Equal(t, tt.wantEpicInBody, strings.Contains(c.Body, testSite+"/browse/PROJ-1"))
			require.Len(t, c.ActivityLog, 1)
			assert.Equal(t, "human:alice", c.ActivityLog[0].Agent)
		})
	}
}

func TestImport_SkipsAlreadyImported(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Moved"), story("103", "PROJ-3", "By key"), story("105", "PROJ-5", "Key reused"))

	moved := e.seedImported(t, "web", "OLD-9", "102")
	byKey := e.seedImported(t, "api", "PROJ-3", "")
	reused := e.seedImported(t, "api", "PROJ-5", "999")

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}, {Key: "PROJ-5", Project: "api"}},
	}, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(resp.Results)
	assert.Equal(t, Result{Key: "PROJ-2", Status: StatusSkipped, Project: "web", Card: moved.ID}, r["PROJ-2"])
	assert.Equal(t, Result{Key: "PROJ-3", Status: StatusSkipped, Project: "api", Card: byKey.ID}, r["PROJ-3"])
	assert.Equal(t, StatusCreated, r["PROJ-5"].Status, "a key match with another issue id is a different issue")
	assert.NotEqual(t, reused.ID, r["PROJ-5"].Card)

	assert.ElementsMatch(t, []string{"web/" + moved.ID, "api/" + byKey.ID, "api/" + r["PROJ-5"].Card},
		e.entryNames(t, resp.Playbook.ID), "skipped cards are placed in the playbook too")
}

func TestImport_ReimportReusesPlaybookAndPlacesNewChildBeforeParent(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}},
		Playbook:   PlaybookRequest{Title: "PROJ-1 Checkout"},
	}, "human:alice")
	require.NoError(t, err)

	second, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-3", Project: "api"}},
		Playbook:   PlaybookRequest{Title: "Ignored title", BoardsRepo: "ignored"},
	}, "human:alice")
	require.NoError(t, err)

	assert.Equal(t, first.Playbook.ID, second.Playbook.ID, "the epic's playbook is reused; title and repo are ignored")

	parentCard := first.Results[0].Card
	childCard := second.Results[0].Card
	assert.Equal(t, []string{"api/" + childCard, "api/" + parentCard}, e.entryNames(t, first.Playbook.ID))
	assert.Empty(t, e.card(t, "api", parentCard).DependsOn, "re-import never edits an existing card")
}

func TestImport_PartialFailureRerunsCleanly(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{
			{Key: "PROJ-3", Project: "no-such-project"},
			{Key: "PROJ-2", Project: "api"},
			{Key: "PROJ-99", Project: "api"}, // not in the epic's tree
			{Key: "PROJ-1", Project: ""},     // the epic itself is not a child
		},
	}, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(first.Results)
	assert.Equal(t, StatusFailed, r["PROJ-3"].Status)
	assert.NotEmpty(t, r["PROJ-3"].Reason)
	assert.Equal(t, StatusCreated, r["PROJ-2"].Status)
	assert.Equal(t, StatusFailed, r["PROJ-99"].Status)
	assert.Equal(t, StatusFailed, r["PROJ-1"].Status)
	assert.Equal(t, 1, e.jiraCards(t, "api"), "failed rows create nothing")

	second, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-3", Project: "api"}, {Key: "PROJ-2", Project: "api"}},
	}, "human:alice")
	require.NoError(t, err)

	r2 := resultsByKey(second.Results)
	assert.Equal(t, StatusCreated, r2["PROJ-3"].Status)
	assert.Equal(t, StatusSkipped, r2["PROJ-2"].Status)
	assert.Equal(t, 2, e.jiraCards(t, "api"))
	assert.Equal(t, []string{"api/" + r2["PROJ-3"].Card, "api/" + r["PROJ-2"].Card}, e.entryNames(t, first.Playbook.ID))
}

func TestImport_ParentFailsWhenItsSameProjectSubtaskFailed(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	ctx := context.Background()
	req := ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}, {Key: "PROJ-2", Project: "api"}}}

	creates := 0
	e.svc.cards = &failingCards{Cards: e.cards, fail: func(in service.CreateCardInput) bool {
		creates++

		return in.Source.ExternalID == "PROJ-3"
	}}

	first, err := e.importSync(ctx, "PROJ-1", req, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(first.Results)
	assert.Equal(t, StatusFailed, r["PROJ-3"].Status)
	assert.Equal(t, StatusFailed, r["PROJ-2"].Status, "a parent created now would miss the dependency for good")
	assert.Contains(t, r["PROJ-2"].Reason, "PROJ-3")
	assert.Equal(t, 1, creates, "the parent is never attempted")
	assert.Zero(t, e.jiraCards(t, "api"))

	e.svc.cards = e.cards

	second, err := e.importSync(ctx, "PROJ-1", req, "human:alice")
	require.NoError(t, err)

	r2 := resultsByKey(second.Results)
	require.Equal(t, StatusCreated, r2["PROJ-2"].Status)
	assert.Equal(t, []string{r2["PROJ-3"].Card}, e.card(t, "api", r2["PROJ-2"].Card).DependsOn)
}

func TestImport_SubtaskCreatedButNotPlacedKeepsItsParent(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	e.svc.playbooks = &failingAdd{Playbooks: e.pbs, failFirst: true}

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-3", Project: "api"}, {Key: "PROJ-2", Project: "api"}},
	}, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(resp.Results)
	require.Equal(t, StatusFailed, r["PROJ-3"].Status)
	require.NotEmpty(t, r["PROJ-3"].Card, "the card exists; only its playbook entry failed")
	require.Equal(t, StatusCreated, r["PROJ-2"].Status)
	assert.Equal(t, []string{r["PROJ-3"].Card}, e.card(t, "api", r["PROJ-2"].Card).DependsOn)
}

func TestImport_ParentDependsOnASubtaskImportedInAnEarlierRun(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}, "human:alice")
	require.NoError(t, err)

	second, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
	require.NoError(t, err)
	require.Equal(t, StatusCreated, second.Results[0].Status)

	assert.Equal(t, []string{first.Results[0].Card}, e.card(t, "api", second.Results[0].Card).DependsOn)
}

func TestImport_HostileOrStaleSelectionFailsOnlyItsRow(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
		Selections: []Selection{
			{Key: "PROJ-3", Project: ""},                       // in the tree, no project chosen
			{Key: "OTHER-7", Project: "api"},                   // outside the epic's tree
			{Key: "PROJ-2) OR key in (PROJ-9", Project: "api"}, // not a key at all
			{Key: "PROJ-2", Project: "api"},
		},
	}, "human:alice")
	require.NoError(t, err)

	r := resultsByKey(resp.Results)
	require.Len(t, r, 4)
	assert.Equal(t, StatusCreated, r["PROJ-2"].Status)

	for _, k := range []string{"PROJ-3", "OTHER-7", "PROJ-2) OR key in (PROJ-9"} {
		assert.Equal(t, StatusFailed, r[k].Status, k)
		assert.NotEmpty(t, r[k].Reason, k)
		assert.Empty(t, r[k].Card, k)
	}

	assert.Equal(t, 1, e.jiraCards(t, "api"), "only the valid row creates a card")
	assert.Zero(t, e.jiraCards(t, "web"))
	assert.Equal(t, []string{"api/" + r["PROJ-2"].Card}, e.entryNames(t, resp.Playbook.ID))
}

func TestImport_RestoresAMissingPlaybookEntry(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
	require.NoError(t, err)

	d, err := e.pbs.Get(ctx, first.Playbook.ID)
	require.NoError(t, err)
	_, err = e.pbs.RemoveEntry(ctx, d.ID, d.Entries[0].ID, "human:alice")
	require.NoError(t, err)

	p, err := e.svc.Preview(ctx, "PROJ-1")
	require.NoError(t, err)
	require.NotNil(t, p.Children[0].Imported)
	assert.False(t, p.Children[0].Imported.InPlaybook)

	second, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, StatusSkipped, second.Results[0].Status)
	assert.Equal(t, []string{"api/" + first.Results[0].Card}, e.entryNames(t, d.ID))
	assert.Equal(t, 1, e.jiraCards(t, "api"))
}

func TestImport_ConcurrentImportsOfOneEpicSerialise(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	req := ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}}}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := e.importSync(context.Background(), "PROJ-1", req, "human:alice")
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	list, err := e.pbs.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1, "one playbook per epic")
	assert.Equal(t, 2, e.jiraCards(t, "api"), "one card per issue")
}

func TestImport_RequestValidation(t *testing.T) {
	e := newImportEnv(t, nil)

	_, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{}, "human:alice")
	require.ErrorIs(t, err, ErrInvalidRequest)

	many := make([]Selection, maxSelections+1)
	_, err = e.importSync(context.Background(), "PROJ-1", ImportRequest{Selections: many}, "human:alice")
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestImport_ExtendsARunnablePlaybookWithUnvettedCards(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}}, Vetted: true,
	}, "human:alice")
	require.NoError(t, err)

	_, err = e.pbs.UpdateMeta(ctx, first.Playbook.ID, service.UpdatePlaybookInput{Runnable: new(true)}, "human:alice")
	require.NoError(t, err)

	second, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}, "human:alice")
	require.NoError(t, err)
	assert.Equal(t, StatusCreated, second.Results[0].Status, "the runner, not the import, gates unvetted cards")
}

func TestImport_RefusedWhileThePlaybookRunIsActive(t *testing.T) {
	tests := []struct {
		name    string
		status  string // empty: the epic has no playbook yet
		refused bool
	}{
		{"no playbook", "", false},
		{"running", board.RunStatusRunning, true},
		{"waiting", board.RunStatusWaiting, true},
		{"stopped", board.RunStatusStopped, false},
		{"completed", board.RunStatusCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)
			e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

			ctx := context.Background()

			var playbookID string

			if tt.status != "" {
				first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
					Selections: []Selection{{Key: "PROJ-2", Project: "api"}}, Vetted: true,
				}, "human:alice")
				require.NoError(t, err)

				playbookID = first.Playbook.ID
				e.makeRunnable(t, playbookID)
				e.setRun(t, playbookID, tt.status)
			}

			cardsBefore := e.jiraCards(t, "api")

			req := ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}

			_, err := e.svc.prepare(ctx, "PROJ-1", req)
			if tt.refused {
				require.ErrorIs(t, err, ErrPlaybookRunning)

				before := e.entryNames(t, playbookID)
				resp, err := e.importSync(ctx, "PROJ-1", req, "human:alice")
				require.ErrorIs(t, err, ErrPlaybookRunning)
				assert.Nil(t, resp)
				assert.Equal(t, cardsBefore, e.jiraCards(t, "api"), "nothing is written")
				assert.Equal(t, before, e.entryNames(t, playbookID))

				return
			}

			require.NoError(t, err)

			resp, err := e.importSync(ctx, "PROJ-1", req, "human:alice")
			require.NoError(t, err)
			assert.Equal(t, StatusCreated, resp.Results[0].Status)
		})
	}
}

func TestImport_RefusedWhenTheRunStartsAfterPrepare(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}}, Vetted: true,
	}, "human:alice")
	require.NoError(t, err)
	e.makeRunnable(t, first.Playbook.ID)

	req := ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}
	tr, err := e.svc.prepare(ctx, "PROJ-1", req)
	require.NoError(t, err)

	e.setRun(t, first.Playbook.ID, board.RunStatusRunning)

	resp, err := e.svc.importTree(ctx, tr, req, "human:alice", nil)
	require.ErrorIs(t, err, ErrPlaybookRunning)
	assert.Nil(t, resp)
	assert.Equal(t, 1, e.jiraCards(t, "api"), "no card is written")
}

// blindPlaybooks never finds a playbook by source, like an instance whose
// last pull predates a peer's import of the epic.
type blindPlaybooks struct{ Playbooks }

func (blindPlaybooks) FindBySource(context.Context, *board.Source) (*service.PlaybookDetail, error) {
	return nil, storage.ErrPlaybookNotFound
}

func TestImport_RefusedWhenTheCreateReturnsARunningPlaybook(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	ctx := context.Background()

	first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}}, Vetted: true,
	}, "human:alice")
	require.NoError(t, err)
	e.makeRunnable(t, first.Playbook.ID)
	e.setRun(t, first.Playbook.ID, board.RunStatusRunning)

	before := e.entryNames(t, first.Playbook.ID)
	svc, _ := e.serviceWith(t, e.cards, blindPlaybooks{Playbooks: e.pbs})

	req := ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}
	tr, err := svc.prepare(ctx, "PROJ-1", req)
	require.NoError(t, err)

	resp, err := svc.importTree(ctx, tr, req, "human:alice", nil)
	require.ErrorIs(t, err, ErrPlaybookRunning)
	assert.Nil(t, resp)
	assert.Equal(t, 1, e.jiraCards(t, "api"), "no card is written")
	assert.Equal(t, before, e.entryNames(t, first.Playbook.ID))

	list, err := e.pbs.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1, "the create returned the epic's playbook")
}

func TestImport_IgnoresCardsAndPlaybooksFromAnotherSite(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"))

	ctx := context.Background()

	const other = "https://other.atlassian.net"

	foreign, err := e.cards.CreateCard(ctx, "api", service.CreateCardInput{
		Title: "PROJ-2 other site", Type: "task", Priority: "medium",
		Source: &board.Source{System: jira.SourceSystem, ExternalID: "PROJ-2", ExternalURL: other + "/browse/PROJ-2"},
		Custom: map[string]any{jira.CustomIssueID: "102"},
	})
	require.NoError(t, err)

	foreignPlaybook, err := e.pbs.Create(ctx, service.CreatePlaybookInput{
		Title: "PROJ-1 other site", AgentID: "human:alice",
		Source: &board.Source{System: jira.SourceSystem, ExternalID: "PROJ-1", ExternalURL: other + "/browse/PROJ-1"},
	})
	require.NoError(t, err)

	p, err := e.svc.Preview(ctx, "PROJ-1")
	require.NoError(t, err)
	assert.Nil(t, p.Playbook, "a playbook from another site is not this epic's")
	require.Len(t, p.Children, 1)
	assert.Nil(t, p.Children[0].Imported, "a card from another site is not this issue's")

	resp, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
	require.NoError(t, err)
	assert.NotEqual(t, foreignPlaybook.ID, resp.Playbook.ID)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, StatusCreated, resp.Results[0].Status)
	assert.NotEqual(t, foreign.ID, resp.Results[0].Card)
	assert.Equal(t, 2, e.jiraCards(t, "api"))
}

func TestImport_RunStartedMidImportAppendsTheCard(t *testing.T) {
	tests := []struct {
		name        string
		beforeMove  bool // the run starts after AddEntry, before the move
		wantUpdates int
	}{
		{"run active when the card is added: no move attempted", false, 0},
		{"run starts before the move: the refusal is tolerated", true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)
			e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

			ctx := context.Background()

			first, err := e.importSync(ctx, "PROJ-1", ImportRequest{
				Selections: []Selection{{Key: "PROJ-2", Project: "api"}}, Vetted: true,
			}, "human:alice")
			require.NoError(t, err)

			id := first.Playbook.ID
			e.makeRunnable(t, id)

			hook := &hookedPlaybooks{Playbooks: e.pbs}
			start := func() { e.setRun(t, id, board.RunStatusRunning) }

			if tt.beforeMove {
				hook.beforeUpdate = start
			} else {
				hook.beforeAdd = start
			}

			e.svc.playbooks = hook

			second, err := e.importSync(ctx, "PROJ-1", ImportRequest{
				Selections: []Selection{{Key: "PROJ-3", Project: "api"}},
			}, "human:alice")
			require.NoError(t, err)

			require.Len(t, second.Results, 1)
			assert.Equal(t, StatusCreated, second.Results[0].Status)
			assert.Empty(t, second.Results[0].Reason)
			assert.Equal(t, tt.wantUpdates, hook.updates)
			assert.Equal(t, []string{"api/" + first.Results[0].Card, "api/" + second.Results[0].Card},
				e.entryNames(t, id), "the sub-task stays at the end while the run is active")
		})
	}
}

func TestImport_ShutdownFinishesTheIssueInFlight(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "Cart API"), story("103", "PROJ-3", "Cart UI"))

	gate := &gatedCards{Cards: e.svc.cards, started: make(chan string, 2), release: make(chan struct{})}
	e.svc.cards = gate

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}}}
	tr, err := e.svc.prepare(ctx, "PROJ-1", req)
	require.NoError(t, err)

	type outcome struct {
		resp *ImportResponse
		err  error
	}

	var seen []Result

	done := make(chan outcome, 1)

	go func() {
		resp, err := e.svc.importTree(ctx, tr, req, "human:alice", func(r Result) { seen = append(seen, r) })
		done <- outcome{resp, err}
	}()

	require.Equal(t, "PROJ-2", <-gate.started)
	cancel()
	close(gate.release)

	got := <-done
	require.ErrorIs(t, got.err, context.Canceled)
	require.NotNil(t, got.resp)
	require.Len(t, got.resp.Results, 1, "the next issue is not started")
	assert.Equal(t, got.resp.Results, seen, "progress saw each result as it landed")

	res := got.resp.Results[0]
	assert.Equal(t, StatusCreated, res.Status)
	assert.True(t, e.committed(t, "api/tasks/"+res.Card+".md"), "the issue in flight is committed")
	assert.Equal(t, []string{"api/" + res.Card}, e.entryNames(t, got.resp.Playbook.ID))
	assert.Equal(t, 1, e.jiraCards(t, "api"))
}

// timedCards runs each CreateCard for hold, or until its context ends when
// hold is zero, and records how long the write ran on the synctest clock.
type timedCards struct {
	Cards

	hold    time.Duration
	started chan struct{} // closed when the create starts; one issue per test
	took    time.Duration
}

func (c *timedCards) CreateCard(ctx context.Context, project string, in service.CreateCardInput) (*board.Card, error) {
	start := time.Now()

	close(c.started)

	defer func() { c.took = time.Since(start) }()

	var held <-chan time.Time
	if c.hold > 0 {
		held = time.After(c.hold)
	}

	select {
	case <-held:
		return c.Cards.CreateCard(ctx, project, in)
	case <-ctx.Done():
		return nil, fmt.Errorf("create card: %w", ctx.Err())
	}
}

func TestImport_WriteBoundAndDrainGrace(t *testing.T) {
	// The shutdown phase that joins the imports has this budget.
	const phase4Budget = 10 * time.Second

	tests := []struct {
		name        string
		hold        time.Duration // 0 holds the write until its context ends
		importBound time.Duration // 0 keeps the default
		shutdown    bool          // shut down as soon as the write starts
		wantStatus  string
		wantClass   error // the error class whose phrase a failed row shows
		wantTook    time.Duration
	}{
		{
			name: "a write slower than 10 s succeeds", hold: 20 * time.Second,
			wantStatus: StatusCreated, wantTook: 20 * time.Second,
		},
		{
			name:       "a wedged write times out at the 30 s bound",
			wantStatus: StatusFailed, wantClass: context.DeadlineExceeded, wantTook: 30 * time.Second,
		},
		{
			name: "shutdown cancels the write in flight after the grace", shutdown: true,
			wantStatus: StatusFailed, wantClass: context.Canceled, wantTook: drainGrace,
		},
		{
			name: "the import's bound cancels the write in flight after the grace", importBound: 10 * time.Second,
			wantStatus: StatusFailed, wantClass: context.DeadlineExceeded, wantTook: 10*time.Second + drainGrace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)
			e.jira.add(story("102", "PROJ-2", "Cart API"))

			synctest.Test(t, func(t *testing.T) {
				cards := &timedCards{Cards: e.cards, hold: tt.hold, started: make(chan struct{})}
				bus := events.NewBus()

				ch, unsubscribe := bus.Subscribe()
				defer unsubscribe()

				svc := New(e.jira, cards, e.pbs, Config{SiteURL: testSite, Bus: bus, Clock: e.clk})
				if tt.importBound > 0 {
					svc.timeout = tt.importBound
				}

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				svc.Start(ctx)

				id, err := svc.Begin(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
				require.NoError(t, err)

				if tt.shutdown {
					<-cards.started

					budget, cancelBudget := context.WithTimeout(context.Background(), phase4Budget)
					defer cancelBudget()

					require.NoError(t, svc.Shutdown(budget), "the drain fits the shutdown phase's budget")
				}

				var results []Result

				for ev := range ch {
					if ev.Type == events.JiraImportDone && ev.Data["import_id"] == id {
						results, _ = ev.Data["results"].([]Result)

						break
					}
				}

				require.Len(t, results, 1)
				assert.Equal(t, tt.wantStatus, results[0].Status)

				if tt.wantClass != nil {
					assert.Equal(t, failureReason(tt.wantClass), results[0].Reason)
				}

				assert.Equal(t, tt.wantTook, cards.took)
			})
		})
	}
}

func TestFailureReason(t *testing.T) {
	const leak = "/srv/boards/api/tasks/API-001.md https://user:s3cret@git.example.com/boards.git"

	fallback := failureReason(fmt.Errorf("commit %s: %w", leak, errors.New("boom")))
	assert.NotContains(t, fallback, "s3cret")
	assert.NotContains(t, fallback, "/srv/")

	ve := &board.ValidationError{Err: board.ErrInvalidType, Field: "type", Message: "invalid type"}
	assert.Equal(t, "invalid type", failureReason(fmt.Errorf("create: %w", ve)), "a validation message is shown as is")

	tests := []struct {
		name string
		err  error
	}{
		{"shutdown", context.Canceled},
		{"timeout", context.DeadlineExceeded},
		{"project not found", storage.ErrProjectNotFound},
		{"invalid input", storage.ErrInvalidInput},
		{"field too long", service.ErrFieldTooLong},
		{"remote unreachable", service.ErrRemoteUnreachable},
		{"project has no repo", service.ErrPlaybookProjectNoRepo},
		{"card owned by another playbook", service.ErrPlaybookCardOwned},
		{"settings not applied", service.ErrPlaybookCardForce},
		{"run active", service.ErrPlaybookRunActive},
	}

	phrases := map[string]string{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failureReason(fmt.Errorf("op %s: %w", leak, tt.err))

			assert.NotEmpty(t, got)
			assert.NotEqual(t, fallback, got, "a known class has its own phrase")
			assert.NotContains(t, got, "s3cret")
			assert.NotContains(t, got, "/srv/")

			if other, dup := phrases[got]; dup {
				t.Errorf("%s shares its phrase with %s", tt.name, other)
			}

			phrases[got] = tt.name
		})
	}
}

func TestImport_TerminalSubtaskIsNotADependency(t *testing.T) {
	for _, state := range []string{board.StateNotPlanned, board.StateDone} {
		t.Run(state, func(t *testing.T) {
			e := newImportEnv(t, nil)
			e.jira.add(story("102", "PROJ-2", "Cart API"), subtask("103", "PROJ-3", "PROJ-2", "Cart schema"))

			ctx := context.Background()

			first, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-3", Project: "api"}}}, "human:alice")
			require.NoError(t, err)

			_, err = e.cards.TransitionTo(ctx, "api", first.Results[0].Card, state)
			require.NoError(t, err)

			second, err := e.importSync(ctx, "PROJ-1", ImportRequest{Selections: []Selection{{Key: "PROJ-2", Project: "api"}}}, "human:alice")
			require.NoError(t, err)
			require.Equal(t, StatusCreated, second.Results[0].Status)

			assert.Empty(t, e.card(t, "api", second.Results[0].Card).DependsOn)
		})
	}
}

func TestImport_StoredCardMatchesItsImportHash(t *testing.T) {
	e := newImportEnv(t, nil)
	empty := story("102", "PROJ-2", "Cart API")
	empty.Description = nil
	e.jira.add(empty, story("103", "PROJ-3", "Cart UI"))

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{
		Selections: []Selection{{Key: "PROJ-2", Project: "api"}, {Key: "PROJ-3", Project: "api"}},
	}, "human:alice")
	require.NoError(t, err)

	for _, res := range resp.Results {
		require.Equal(t, StatusCreated, res.Status)

		// The file on disk is what a later reader hashes: it ends in a newline.
		data, err := os.ReadFile(filepath.Join(e.dir, "api", "tasks", res.Card+".md"))
		require.NoError(t, err)

		c, err := board.ParseCard(data)
		require.NoError(t, err)
		assert.Equal(t, importHash(c.Title, c.Body), c.Custom[jira.CustomImportHash], res.Key)
	}
}

func TestImport_DependenciesAreCappedAtTheServiceLimit(t *testing.T) {
	e := newImportEnv(t, nil)

	issues := []jira.Issue{story("102", "PROJ-2", "Cart API")}
	sels := []Selection{{Key: "PROJ-2", Project: "api"}}

	for i := range service.MaxDependsOn + 1 {
		key := fmt.Sprintf("PROJ-%d", 1000+i)
		issues = append(issues, subtask(fmt.Sprintf("%d", 2000+i), key, "PROJ-2", fmt.Sprintf("Step %d", i)))
		sels = append(sels, Selection{Key: key, Project: "api"})
	}

	e.jira.add(issues...)

	resp, err := e.importSync(context.Background(), "PROJ-1", ImportRequest{Selections: sels}, "human:alice")
	require.NoError(t, err)

	var subs []string

	for _, res := range resp.Results {
		require.Equal(t, StatusCreated, res.Status, res.Key)

		if res.Key != "PROJ-2" {
			subs = append(subs, res.Card)
		}
	}

	parent := resultsByKey(resp.Results)["PROJ-2"]
	assert.Equal(t, subs[:service.MaxDependsOn], e.card(t, "api", parent.Card).DependsOn)
	assert.NotEmpty(t, parent.Reason)
}
