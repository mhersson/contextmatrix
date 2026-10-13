package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Preview reads the epic's tree and marks what an import would do.
func (s *Service) Preview(ctx context.Context, epicKey string) (*Preview, error) {
	t, err := s.fetchTree(ctx, epicKey)
	if err != nil {
		return nil, err
	}

	idx, err := s.loadImported(ctx)
	if err != nil {
		return nil, err
	}

	known, err := s.projectNames(ctx)
	if err != nil {
		return nil, err
	}

	p := &Preview{
		Epic: Epic{
			Key: t.epic.Key, Summary: t.epic.Summary, URL: s.browseURL(t.epic.Key),
			Description: strings.TrimSpace(jira.ADFToMarkdown(t.epic.Description)),
		},
		Children: []Node{},
	}

	var entries []entryRef

	d, err := s.playbooks.FindBySource(ctx, s.source(t.epic.Key))

	switch {
	case err == nil:
		p.Playbook = &PlaybookRef{ID: d.ID, Title: d.Title, BoardsRepo: d.BoardsRepo, RunActive: d.Run.Active()}
		entries = entryRefs(d)
	case !errors.Is(err, storage.ErrPlaybookNotFound):
		return nil, fmt.Errorf("find playbook for %s: %w", t.epic.Key, err)
	}

	for _, c := range t.children {
		def := s.defaultProject(c, known)
		n := s.node(c, idx, entries, def)

		for _, st := range t.subtasks[c.Key] {
			n.Subtasks = append(n.Subtasks, s.node(st, idx, entries, def))
		}

		p.Children = append(p.Children, n)
	}

	p.ActiveImport = s.activeImport(t.epic.Key)

	return p, nil
}

func (s *Service) node(is jira.Issue, idx *importedIndex, entries []entryRef, defaultProject string) Node {
	n := Node{
		Key: is.Key, ID: is.ID, Summary: is.Summary, URL: s.browseURL(is.Key),
		IssueType: is.IssueType.Name, Status: is.Status.Name, StatusCategory: is.Status.CategoryKey,
		JiraProject: is.ProjectKey, Components: is.Components, DefaultProject: defaultProject,
	}

	if c := idx.find(is); c != nil {
		ref := cardRef{project: c.Project, card: c.ID}
		n.Imported = &ImportedRef{Project: c.Project, Card: c.ID, InPlaybook: indexOf(entries, ref) >= 0}
	}

	return n
}

// defaultProject picks the wizard default from project_map: the first of the
// issue's components that maps to an existing CM project, else the Jira
// project's default when it exists. Startup only warns about stale targets.
func (s *Service) defaultProject(is jira.Issue, known map[string]bool) string {
	m, ok := s.cfg.ProjectMap[is.ProjectKey]
	if !ok {
		return ""
	}

	for _, comp := range is.Components {
		if p := m.Components[comp]; known[p] {
			return p
		}
	}

	if known[m.Default] {
		return m.Default
	}

	return ""
}

func (s *Service) projectNames(ctx context.Context) (map[string]bool, error) {
	projects, err := s.cards.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	known := make(map[string]bool, len(projects))
	for _, p := range projects {
		known[p.Name] = true
	}

	return known, nil
}
