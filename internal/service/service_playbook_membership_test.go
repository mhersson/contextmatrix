package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type fakePlaybookLister struct {
	playbooks []*board.Playbook
	err       error
}

func (f *fakePlaybookLister) List(_ context.Context) ([]*board.Playbook, error) {
	return f.playbooks, f.err
}

func cardEntry(project, card string) board.PlaybookEntry {
	return board.PlaybookEntry{ID: "e-" + card, Type: "card", Project: project, Card: card}
}

func testPlaybook(id string, entries ...board.PlaybookEntry) *board.Playbook {
	return &board.Playbook{
		ID:      id,
		Title:   id,
		Created: time.Now(),
		Updated: time.Now(),
		Entries: entries,
	}
}

func TestPlaybookMembership_GetCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	member, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "member", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	loner, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "loner", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	svc.SetPlaybookLister(&fakePlaybookLister{playbooks: []*board.Playbook{
		testPlaybook("rollout", cardEntry("test-project", member.ID)),
		testPlaybook("cleanup",
			board.PlaybookEntry{ID: "m1", Type: "manual", Text: "flip the switch"},
			cardEntry("test-project", member.ID),
			cardEntry("other-project", loner.ID),
		),
	}})

	got, err := svc.GetCard(ctx, "test-project", member.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"rollout", "cleanup"}, got.InPlaybooks,
		"member card lists its playbooks in lister order")

	got, err = svc.GetCard(ctx, "test-project", loner.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks,
		"same card ID in another project must not count as membership")
}

func TestPlaybookMembership_ListCards(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	member, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "member", Type: "task", Priority: "low",
	})
	require.NoError(t, err)
	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "loner", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	svc.SetPlaybookLister(&fakePlaybookLister{playbooks: []*board.Playbook{
		testPlaybook("rollout", cardEntry("test-project", member.ID)),
	}})

	cards, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)

	byID := map[string][]string{}
	for _, c := range cards {
		byID[c.ID] = c.InPlaybooks
	}

	assert.Equal(t, []string{"rollout"}, byID[member.ID])

	for id, pbs := range byID {
		if id != member.ID {
			assert.Empty(t, pbs, "non-member %s must have no playbooks", id)
		}
	}
}

func TestPlaybookMembership_BestEffort(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "card", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	// No lister wired: reads still work.
	got, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks)

	// Lister failure: reads still work, membership just stays empty.
	svc.SetPlaybookLister(&fakePlaybookLister{err: errors.New("boom")})
	got, err = svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Empty(t, got.InPlaybooks)
}
