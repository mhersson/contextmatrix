package jiraimport

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/require"
)

const testSite = "https://acme.atlassian.net"

// fakeJira serves issues from a map and answers the two JQL shapes the
// importer sends: childrenJQL and subtasksJQL.
type fakeJira struct {
	mu       sync.Mutex
	issues   map[string]jira.Issue
	issueErr error
	searches int
}

func (f *fakeJira) add(issues ...jira.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, is := range issues {
		f.issues[is.Key] = is
	}
}

func (f *fakeJira) Issue(_ context.Context, key string) (jira.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.issueErr != nil {
		return jira.Issue{}, f.issueErr
	}

	is, ok := f.issues[key]
	if !ok {
		return jira.Issue{}, jira.ErrNotFound
	}

	return is, nil
}

func (f *fakeJira) Search(_ context.Context, jql string) ([]jira.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.searches++
	parents := map[string]bool{}

	switch {
	case strings.HasPrefix(jql, "parent = "):
		parents[strings.Fields(strings.TrimPrefix(jql, "parent = "))[0]] = true
	case strings.HasPrefix(jql, "parent in ("):
		list, _, _ := strings.Cut(strings.TrimPrefix(jql, "parent in ("), ")")
		for _, k := range strings.Split(list, ",") {
			parents[strings.TrimSpace(k)] = true
		}
	}

	var out []jira.Issue

	for _, is := range f.issues {
		if parents[is.ParentKey] {
			out = append(out, is)
		}
	}

	slices.SortFunc(out, func(a, b jira.Issue) int { return cmp.Compare(a.Key, b.Key) })

	return out, nil
}

func epicIssue() jira.Issue {
	return jira.Issue{
		ID: "100", Key: "PROJ-1", Summary: "Checkout revamp", ProjectKey: "PROJ",
		IssueType:   jira.IssueType{Name: "Epic", HierarchyLevel: 1},
		Status:      jira.Status{Name: "In Progress", CategoryKey: "indeterminate"},
		Description: adfDoc("Epic goal"),
	}
}

func story(id, key, summary string) jira.Issue {
	return jira.Issue{
		ID: id, Key: key, Summary: summary, ProjectKey: "PROJ", ParentKey: "PROJ-1", Priority: "High",
		IssueType:   jira.IssueType{Name: "Story"},
		Status:      jira.Status{Name: "To Do", CategoryKey: "new"},
		Description: adfDoc(summary + " details"),
	}
}

func subtask(id, key, parent, summary string) jira.Issue {
	return jira.Issue{
		ID: id, Key: key, Summary: summary, ProjectKey: "PROJ", ParentKey: parent,
		IssueType: jira.IssueType{Name: "Sub-task", HierarchyLevel: -1, Subtask: true},
		Status:    jira.Status{Name: "To Do", CategoryKey: "new"},
	}
}

type importEnv struct {
	svc   *Service
	jira  *fakeJira
	cards *service.CardService
	pbs   *service.PlaybookService
	bus   *events.Bus
	clk   *clock.FakeClock
	dir   string // the boards repo
}

// newImportEnv builds real card and playbook services over two projects,
// "api" (task, bug, feature) and "web" (task, bug), and a fake Jira holding
// the epic PROJ-1.
func newImportEnv(t *testing.T, projectMap map[string]config.JiraProjectMapping) *importEnv {
	t.Helper()

	boardsDir := filepath.Join(t.TempDir(), "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	for _, p := range []struct {
		name, prefix string
		types        []string
	}{
		{"api", "API", []string{"task", "bug", "feature"}},
		{"web", "WEB", []string{"task", "bug"}},
	} {
		cfg := &board.ProjectConfig{
			Name: p.name, Prefix: p.prefix, NextID: 1, Repo: "https://github.com/acme/" + p.name + ".git",
			States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
			Types:      p.types,
			Priorities: []string{"low", "medium", "high"},
			Transitions: map[string][]string{
				"todo": {"in_progress", "not_planned"}, "in_progress": {"done", "todo"}, "done": {"todo"},
				"stalled": {"todo"}, "not_planned": {"todo"},
			},
		}
		require.NoError(t, os.MkdirAll(filepath.Join(boardsDir, p.name, "tasks"), 0o755))
		require.NoError(t, board.SaveProjectConfig(filepath.Join(boardsDir, p.name), cfg))
	}

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	cards := service.NewCardService(store, gitMgr, lock.NewManager(store, 30*time.Minute), bus, boardsDir, nil, true, false)

	pbStore, err := storage.NewFilesystemPlaybookStore(boardsDir)
	require.NoError(t, err)

	pbs := service.NewPlaybookService(pbStore, store, bus, nil, false)
	cards.SetPlaybookLister(pbStore)
	pbs.SetCardForcer(cards)

	fj := &fakeJira{issues: map[string]jira.Issue{}}
	fj.add(epicIssue())

	clk := clock.Fake(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC))
	svc := New(fj, cards, pbs, Config{SiteURL: testSite, ProjectMap: projectMap, Bus: bus, Clock: clk})

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	t.Cleanup(func() {
		cancel()

		_ = svc.Shutdown(context.Background())
	})

	return &importEnv{svc: svc, jira: fj, cards: cards, pbs: pbs, bus: bus, clk: clk, dir: boardsDir}
}

// seedImported writes a card as an earlier import would have left it.
func (e *importEnv) seedImported(t *testing.T, project, key, issueID string) *board.Card {
	t.Helper()

	in := service.CreateCardInput{
		Title: key + " earlier", Type: "task", Priority: "medium",
		Source: &board.Source{System: jira.SourceSystem, ExternalID: key, ExternalURL: testSite + "/browse/" + key},
	}
	if issueID != "" {
		in.Custom = map[string]any{jira.CustomIssueID: issueID}
	}

	c, err := e.cards.CreateCard(context.Background(), project, in)
	require.NoError(t, err)

	return c
}
