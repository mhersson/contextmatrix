package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type fakeSentinelMarks struct {
	marks map[string]board.SentinelMark
	err   error
}

func (f fakeSentinelMarks) SentinelMarks(context.Context, string) (map[string]board.SentinelMark, error) {
	return f.marks, f.err
}

func TestListCardsCarriesSentinelMarks(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	marked, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "marked", Type: "task", Priority: "low"})
	require.NoError(t, err)
	plain, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "plain", Type: "task", Priority: "low"})
	require.NoError(t, err)

	svc.SetSentinelMarks(fakeSentinelMarks{marks: map[string]board.SentinelMark{
		marked.ID: {Action: "healed", Label: "Sentinel healed this card"},
	}})

	check := func(t *testing.T, cards []*board.Card) {
		t.Helper()
		require.Len(t, cards, 2)

		for _, c := range cards {
			if c.ID == marked.ID {
				require.NotNil(t, c.Sentinel)
				assert.Equal(t, "healed", c.Sentinel.Action)
			} else {
				assert.Nil(t, c.Sentinel)
			}
		}
	}

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	check(t, listed)

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{})
	require.NoError(t, err)
	check(t, page.Items)

	got, err := svc.GetCard(ctx, "test-project", marked.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Sentinel)

	got, err = svc.GetCard(ctx, "test-project", plain.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Sentinel)

	// The mark is read-only: it never reaches the stored card.
	svc.SetSentinelMarks(nil)

	stored, err := svc.GetCard(ctx, "test-project", marked.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.Sentinel)
}

func TestListCardsSurvivesSentinelMarkError(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{Title: "one", Type: "task", Priority: "low"})
	require.NoError(t, err)

	svc.SetSentinelMarks(fakeSentinelMarks{err: errors.New("ops.db down")})

	listed, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].Sentinel)
}
