package jiraimport

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/jira"
)

// subtaskChunk bounds the key list in one "parent in (...)" query.
const subtaskChunk = 50

// Keys in these queries come from IsIssueKey or from Jira's own responses.
func childrenJQL(epicKey string) string {
	return "parent = " + epicKey + " ORDER BY key ASC"
}

func subtasksJQL(keys []string) string {
	return "parent in (" + strings.Join(keys, ", ") + ") ORDER BY key ASC"
}

type tree struct {
	epic     jira.Issue
	children []jira.Issue
	subtasks map[string][]jira.Issue // by parent key
	byKey    map[string]jira.Issue   // every child and sub-task
}

// fetchTree reads the epic, its children and their sub-tasks. Epic level is
// decided by hierarchy level, never by type name: sites rename the levels.
func (s *Service) fetchTree(ctx context.Context, epicKey string) (*tree, error) {
	if !jira.IsIssueKey(epicKey) {
		return nil, fmt.Errorf("%w: %q", jira.ErrInvalidRef, epicKey)
	}

	epic, err := s.jira.Issue(ctx, epicKey)
	if err != nil {
		return nil, wrapJira("fetch epic "+epicKey, err)
	}

	if epic.IssueType.HierarchyLevel != 1 {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotEpic, epic.Key, epic.IssueType.Name)
	}

	children, err := s.jira.Search(ctx, childrenJQL(epic.Key))
	if err != nil {
		return nil, wrapJira("search children of "+epic.Key, err)
	}

	t := &tree{epic: epic, children: children, subtasks: map[string][]jira.Issue{}, byKey: map[string]jira.Issue{}}
	keys := make([]string, 0, len(children))

	for _, c := range children {
		keys = append(keys, c.Key)
		t.byKey[c.Key] = c
	}

	for chunk := range slices.Chunk(keys, subtaskChunk) {
		subs, err := s.jira.Search(ctx, subtasksJQL(chunk))
		if err != nil {
			return nil, wrapJira("search sub-tasks under "+epic.Key, err)
		}

		for _, st := range subs {
			t.subtasks[st.ParentKey] = append(t.subtasks[st.ParentKey], st)
			t.byKey[st.Key] = st
		}
	}

	return t, nil
}

// wrapJira keeps the jira package's sentinels visible to errors.Is and marks
// every other Jira failure as ErrJiraUnavailable.
func wrapJira(op string, err error) error {
	if errors.Is(err, jira.ErrUnauthorized) || errors.Is(err, jira.ErrNotFound) ||
		errors.Is(err, jira.ErrRateLimited) || errors.Is(err, jira.ErrInvalidRef) {
		return fmt.Errorf("%s: %w", op, err)
	}

	return fmt.Errorf("%s: %w: %w", op, ErrJiraUnavailable, err)
}
