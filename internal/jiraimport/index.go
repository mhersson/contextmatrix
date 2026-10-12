package jiraimport

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// importedIndex finds the card an earlier import created for an issue, in
// any project: by Jira issue id first (stable when an issue moves between
// Jira projects), then by key.
type importedIndex struct {
	byIssueID map[string]*board.Card
	byKey     map[string]*board.Card
}

func (s *Service) loadImported(ctx context.Context) (*importedIndex, error) {
	projects, err := s.cards.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	idx := &importedIndex{byIssueID: map[string]*board.Card{}, byKey: map[string]*board.Card{}}

	for _, p := range projects {
		cards, err := s.cards.ListCards(ctx, p.Name, storage.CardFilter{SourceSystem: jira.SourceSystem})
		if err != nil {
			return nil, fmt.Errorf("list jira cards in %s: %w", p.Name, err)
		}

		for _, c := range cards {
			if id := customString(c.Custom, jira.CustomIssueID); id != "" {
				idx.byIssueID[id] = c
			}

			if c.Source != nil && c.Source.ExternalID != "" {
				idx.byKey[c.Source.ExternalID] = c
			}
		}
	}

	return idx, nil
}

// find matches on the key only when the card's recorded issue id is unknown
// or agrees, so a card recorded for a different issue id is never adopted.
func (x *importedIndex) find(issue jira.Issue) *board.Card {
	if c := x.byIssueID[issue.ID]; c != nil {
		return c
	}

	c := x.byKey[issue.Key]
	if c == nil {
		return nil
	}

	if id := customString(c.Custom, jira.CustomIssueID); id != "" && id != issue.ID {
		return nil
	}

	return c
}

func customString(custom map[string]any, key string) string {
	v, ok := custom[key]
	if !ok || v == nil {
		return ""
	}

	return fmt.Sprint(v)
}

type cardRef struct{ project, card string }

// entryRef is one playbook entry; manual entries carry an empty ref, so
// indexes stay aligned with the playbook's entry positions.
type entryRef struct {
	id  string
	ref cardRef
}

func entryRefs(d *service.PlaybookDetail) []entryRef {
	out := make([]entryRef, len(d.Entries))
	for i, e := range d.Entries {
		out[i] = entryRef{id: e.ID, ref: cardRef{project: e.Project, card: e.Card}}
	}

	return out
}

func indexOf(entries []entryRef, ref cardRef) int {
	for i, e := range entries {
		if e.ref == ref {
			return i
		}
	}

	return -1
}
