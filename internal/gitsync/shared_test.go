package gitsync

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedConvergence is the acceptance test for the shared sync engine: two
// clones create, edit, delete and cancel cards without seeing each other, and
// after a few cycles they hold the same board. It also pins the two properties
// a convergence check alone would miss - that no card file was silently
// skipped by the index loader, and that neither clone is left mid-merge.
func TestSharedConvergence(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	ctx := context.Background()

	a.sync(t)
	b.sync(t)

	// Round 1: both create two cards without syncing in between, so both mint
	// the same two ids and every card is an add/add against the other side.
	for i := range 2 {
		a.create(t, fmt.Sprintf("a%d", i))
		b.create(t, fmt.Sprintf("b%d", i))
	}

	a.sync(t)
	r1 := b.sync(t)
	a.sync(t)

	require.NotNil(t, findRemint(r1.Resolutions), "round one must collide on ids and re-mint")
	assertConverged(t, a, b, 4)

	// Round 2: the same card is edited on both sides, one of them into a
	// terminal state.
	cards, err := a.store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)
	require.Len(t, cards, 4)

	target := cards[0].ID

	_, err = a.svc.UpdateCard(ctx, "test-project", target,
		service.UpdateCardInput{Title: "cancelled", Type: "task", State: "not_planned", Priority: "medium"})
	require.NoError(t, err)

	_, err = b.svc.UpdateCard(ctx, "test-project", target, service.UpdateCardInput{
		Title: cards[0].Title, Type: "task", State: "in_progress", Priority: "high", Labels: []string{"hot"},
	})
	require.NoError(t, err)

	b.sync(t)
	r2 := a.sync(t)
	b.sync(t)

	assertHasRule(t, r2.Resolutions, boardmerge.RuleEpochWins)
	assertConverged(t, a, b, 4)

	got, err := a.store.GetCard(ctx, "test-project", target)
	require.NoError(t, err)
	assert.Equal(t, "not_planned", got.State, "terminal absorbs")
	assert.Equal(t, "high", got.Priority, "the field the other side moved survives")
	assert.Equal(t, []string{"hot"}, got.Labels)
	assert.Empty(t, got.AssignedAgent, "not_planned holds no claim")
	assert.Equal(t, 1, got.ClaimEpoch, "the terminal transition bumped the epoch, which is what decided the merge")

	// Round 3: deleted on one side, edited on the other.
	other := cards[1].ID
	require.NoError(t, a.svc.DeleteCard(ctx, "test-project", other))

	_, err = b.svc.UpdateCard(ctx, "test-project", other,
		service.UpdateCardInput{Title: "zombie", Type: "task", State: "todo", Priority: "low"})
	require.NoError(t, err)

	a.sync(t)
	r3 := b.sync(t)
	a.sync(t)

	assertHasRule(t, r3.Resolutions, boardmerge.RuleDeleteWins)
	assertConverged(t, a, b, 3)

	_, err = a.store.GetCard(ctx, "test-project", other)
	require.Error(t, err, "delete wins over the concurrent edit")

	// Every file on disk parses: nothing was silently skipped by the loader.
	for _, n := range []*sharedNode{a, b} {
		files, err := filepath.Glob(filepath.Join(n.dir, "test-project", "tasks", "*.md"))
		require.NoError(t, err)

		listed, err := n.store.ListCards(ctx, "test-project", storage.CardFilter{})
		require.NoError(t, err)

		assert.Len(t, listed, len(files), "index skipped a file on %s", n.dir)
		assert.False(t, n.git.MergeInProgress(), "merge left in progress on %s", n.dir)

		clean, dirty, err := n.git.IsClean(ctx)
		require.NoError(t, err)
		assert.True(t, clean, dirty)
	}
}

// assertConverged fails unless both nodes hold the same want cards, with unique
// ids, equal user-visible fields, and the same next id to hand out.
func assertConverged(t *testing.T, a, b *sharedNode, want int) {
	t.Helper()

	ctx := context.Background()

	ca, err := a.store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)

	cb, err := b.store.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)

	require.Len(t, ca, want)
	require.Len(t, cb, want)

	ids := map[string]bool{}

	for _, c := range ca {
		require.False(t, ids[c.ID], "duplicate id %s", c.ID)
		ids[c.ID] = true
	}

	ma, mb := cardsByID(ca), cardsByID(cb)

	for id, x := range ma {
		y, ok := mb[id]
		require.True(t, ok, "card %s missing on b", id)

		assert.Equal(t, x.Title, y.Title, id)
		assert.Equal(t, x.State, y.State, id)
		assert.Equal(t, x.Priority, y.Priority, id)
		assert.Equal(t, x.Labels, y.Labels, id)
		assert.Equal(t, x.Body, y.Body, id)
	}

	pa, err := a.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	pb, err := b.store.GetProject(ctx, "test-project")
	require.NoError(t, err)

	assert.Equal(t, pa.NextID, pb.NextID)
}

// assertHasRule fails unless the resolver reported the given rule, which keeps
// each round of the convergence test honest: without it a round that merged
// nothing at all would still converge.
func assertHasRule(t *testing.T, res []boardmerge.Resolution, rule string) {
	t.Helper()

	for _, r := range res {
		if r.Rule == rule {
			return
		}
	}

	t.Fatalf("no %s resolution in %v", rule, res)
}

func cardsByID(cs []*board.Card) map[string]*board.Card {
	m := make(map[string]*board.Card, len(cs))
	for _, c := range cs {
		m[c.ID] = c
	}

	return m
}
