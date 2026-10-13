package jiraimport

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreview_TreeDefaultsAndEpic(t *testing.T) {
	e := newImportEnv(t, map[string]config.JiraProjectMapping{
		"PROJ": {Default: "api", Components: map[string]string{"Frontend": "web", "Ghost": "no-such-project"}},
	})

	front := story("103", "PROJ-3", "Cart UI")
	front.Components = []string{"Ghost", "Frontend"}
	e.jira.add(story("102", "PROJ-2", "Cart API"), front, subtask("104", "PROJ-4", "PROJ-3", "Cart button"))

	p, err := e.svc.Preview(context.Background(), "PROJ-1")
	require.NoError(t, err)

	assert.Equal(t, "PROJ-1", p.Epic.Key)
	assert.Equal(t, testSite+"/browse/PROJ-1", p.Epic.URL)
	assert.Contains(t, p.Epic.Description, "Epic goal")
	assert.Nil(t, p.Playbook)

	require.Len(t, p.Children, 2)
	assert.Equal(t, "PROJ-2", p.Children[0].Key)
	assert.Equal(t, "api", p.Children[0].DefaultProject)
	assert.Equal(t, "new", p.Children[0].StatusCategory)

	ui := p.Children[1]
	assert.Equal(t, "web", ui.DefaultProject, "a component mapping wins; one naming no project is skipped")
	require.Len(t, ui.Subtasks, 1)
	assert.Equal(t, "PROJ-4", ui.Subtasks[0].Key)
	assert.Equal(t, "web", ui.Subtasks[0].DefaultProject, "a sub-task inherits its parent's default")
}

func TestPreview_ImportedMarkers(t *testing.T) {
	e := newImportEnv(t, nil)
	e.jira.add(story("102", "PROJ-2", "By id"), story("103", "PROJ-3", "By key"), story("105", "PROJ-5", "Key reused"))

	moved := e.seedImported(t, "web", "OLD-9", "102") // the issue moved projects: matched on id
	byKey := e.seedImported(t, "api", "PROJ-3", "")
	e.seedImported(t, "api", "PROJ-5", "999") // same key, another issue id: not this issue

	_, err := e.pbs.Create(context.Background(), service.CreatePlaybookInput{
		Title: "PROJ-1 Checkout", AgentID: "human:alice",
		Source:  &board.Source{System: jira.SourceSystem, ExternalID: "PROJ-1", ExternalURL: testSite + "/browse/PROJ-1"},
		Entries: []service.PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "api", Card: byKey.ID}},
	})
	require.NoError(t, err)

	p, err := e.svc.Preview(context.Background(), "PROJ-1")
	require.NoError(t, err)
	require.NotNil(t, p.Playbook)
	assert.Equal(t, "proj-1-checkout", p.Playbook.ID)

	nodes := map[string]Node{}
	for _, n := range p.Children {
		nodes[n.Key] = n
	}

	require.NotNil(t, nodes["PROJ-2"].Imported)
	assert.Equal(t, ImportedRef{Project: "web", Card: moved.ID, InPlaybook: false}, *nodes["PROJ-2"].Imported)

	require.NotNil(t, nodes["PROJ-3"].Imported)
	assert.Equal(t, ImportedRef{Project: "api", Card: byKey.ID, InPlaybook: true}, *nodes["PROJ-3"].Imported)

	assert.Nil(t, nodes["PROJ-5"].Imported, "a key match whose issue id differs is a different issue")
}

func TestPreview_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("not an epic", func(t *testing.T) {
		e := newImportEnv(t, nil)
		e.jira.add(story("102", "PROJ-2", "A story"))

		_, err := e.svc.Preview(ctx, "PROJ-2")
		require.ErrorIs(t, err, ErrNotEpic)
	})

	t.Run("invalid key", func(t *testing.T) {
		e := newImportEnv(t, nil)

		_, err := e.svc.Preview(ctx, "proj-1")
		require.ErrorIs(t, err, jira.ErrInvalidRef)
		assert.Zero(t, e.jira.searches)
	})

	t.Run("jira sentinel kept", func(t *testing.T) {
		e := newImportEnv(t, nil)
		e.jira.issueErr = jira.ErrUnauthorized

		_, err := e.svc.Preview(ctx, "PROJ-1")
		require.ErrorIs(t, err, jira.ErrUnauthorized)
		assert.NotErrorIs(t, err, ErrJiraUnavailable)
	})

	t.Run("jira answers with a malformed epic key", func(t *testing.T) {
		e := newImportEnv(t, nil)

		epic := epicIssue()
		epic.Key = "PROJ-1 OR project = OPS"
		e.jira.issues["PROJ-1"] = epic

		_, err := e.svc.Preview(ctx, "PROJ-1")
		require.ErrorIs(t, err, ErrJiraUnavailable)
		assert.Zero(t, e.jira.searches, "no query is built from it")
	})

	t.Run("other jira failure", func(t *testing.T) {
		e := newImportEnv(t, nil)
		e.jira.issueErr = context.DeadlineExceeded

		_, err := e.svc.Preview(ctx, "PROJ-1")
		require.ErrorIs(t, err, ErrJiraUnavailable)
	})
}

func TestPreview_EpicIsDecidedByHierarchyLevelNotName(t *testing.T) {
	tests := []struct {
		name    string
		typ     jira.IssueType
		wantErr error
	}{
		{"a renamed level-1 type is an epic", jira.IssueType{Name: "Initiative", HierarchyLevel: 1}, nil},
		{"a level-0 type named Epic is not", jira.IssueType{Name: "Epic", HierarchyLevel: 0}, ErrNotEpic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newImportEnv(t, nil)

			epic := epicIssue()
			epic.IssueType = tt.typ
			e.jira.add(epic)

			_, err := e.svc.Preview(context.Background(), "PROJ-1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestPreview_SkipsAChildWithAMalformedKey(t *testing.T) {
	const bad = "PROJ-2 OR project = OPS"

	e := newImportEnv(t, nil)
	e.jira.add(story("102", bad, "Injected"), subtask("105", "PROJ-5", bad, "Under the bad key"),
		story("103", "PROJ-3", "Cart UI"), subtask("104", "PROJ-4", "PROJ-3", "Cart button"))

	p, err := e.svc.Preview(context.Background(), "PROJ-1")
	require.NoError(t, err)

	require.Len(t, p.Children, 1)
	assert.Equal(t, "PROJ-3", p.Children[0].Key)
	require.Len(t, p.Children[0].Subtasks, 1, "a well-formed sibling keeps its sub-tasks")
	assert.Equal(t, "PROJ-4", p.Children[0].Subtasks[0].Key)

	jqls := e.jira.queries()
	require.Len(t, jqls, 2, "the children query and one sub-task query")

	for _, jql := range jqls {
		assert.NotContains(t, jql, bad)
	}
}

func TestPreview_RunActive(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		status string // empty: no run block
		want   bool
	}{
		{"no run", "", false},
		{"running", board.RunStatusRunning, true},
		{"waiting", board.RunStatusWaiting, true},
		{"stopped", board.RunStatusStopped, false},
		{"completed", board.RunStatusCompleted, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t, nil)
			card := e.seedImported(t, "api", "PROJ-2", "102")
			vetted := true
			_, err := e.cards.PatchCard(ctx, "api", card.ID, service.PatchCardInput{Vetted: &vetted})
			require.NoError(t, err)

			d, err := e.pbs.Create(ctx, service.CreatePlaybookInput{
				Title: "PROJ-1 Checkout", AgentID: "human:alice",
				Source:  &board.Source{System: jira.SourceSystem, ExternalID: "PROJ-1", ExternalURL: testSite + "/browse/PROJ-1"},
				Entries: []service.PlaybookEntryInput{{Type: board.EntryTypeCard, Project: "api", Card: card.ID}},
			})
			require.NoError(t, err)

			if tc.status != "" {
				runnable := true
				_, err = e.pbs.UpdateMeta(ctx, d.ID, service.UpdatePlaybookInput{Runnable: &runnable}, "human:alice")
				require.NoError(t, err)

				now := time.Now().UTC()
				_, err = e.pbs.SetRunIf(ctx, d.ID, nil, &board.PlaybookRun{
					Status: tc.status, StartedAt: now, UpdatedAt: now, Entry: d.Entries[0].ID,
				}, "human:alice")
				require.NoError(t, err)
			}

			p, err := e.svc.Preview(ctx, "PROJ-1")
			require.NoError(t, err)
			require.NotNil(t, p.Playbook)
			assert.Equal(t, tc.want, p.Playbook.RunActive)
		})
	}
}
