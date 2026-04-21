package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/storage"
)

// TestListCardsPage_WalkAllPages seeds a project with 50 cards, walks the
// paginated listing with limit=10, and asserts the walk visits every card
// exactly once in ID-ascending order. Guards against off-by-one slicing and
// cursor-skip regressions.
func TestListCardsPage_WalkAllPages(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	const total = 50

	for i := 0; i < total; i++ {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	var (
		seen    []string
		cursor  string
		pageNum int
	)

	for {
		pageNum++
		require.LessOrEqual(t, pageNum, 100, "pagination exceeded reasonable page count")

		page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{
			Limit:  10,
			Cursor: cursor,
		})
		require.NoError(t, err)

		if pageNum == 1 {
			assert.True(t, page.HasTotal, "first page must include total")
			assert.Equal(t, total, page.Total)
		} else {
			assert.False(t, page.HasTotal, "subsequent pages must not include total")
		}

		for _, c := range page.Items {
			seen = append(seen, c.ID)
		}

		if page.NextCursor == "" {
			break
		}

		cursor = page.NextCursor
	}

	assert.Len(t, seen, total)

	// Ordering should be ID-ascending, stable across pages.
	sorted := append([]string(nil), seen...)
	sort.Strings(sorted)
	assert.Equal(t, sorted, seen, "paginated walk must be ID-ascending")
}

// TestListCardsPage_InvalidCursor verifies a non-base64url cursor returns
// ErrInvalidCursor so the handler can map it to 400.
func TestListCardsPage_InvalidCursor(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{
		Limit:  10,
		Cursor: "!!!not-base64url",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCursor)
}

// TestListCardsPage_EmptyProject covers the zero-card case: Items is empty,
// NextCursor is empty, and Total is populated on the first (and only) page.
func TestListCardsPage_EmptyProject(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{Limit: 10})
	require.NoError(t, err)

	assert.Empty(t, page.Items)
	assert.Empty(t, page.NextCursor)
	assert.True(t, page.HasTotal)
	assert.Equal(t, 0, page.Total)
}

// TestListCardsPage_CursorEncoding confirms the emitted NextCursor decodes
// back to the last-seen card ID — clients must treat it as opaque but the
// server contract is documented base64url(lastID).
func TestListCardsPage_CursorEncoding(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Card %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{}, PageOpts{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.NextCursor)

	decoded, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, page.Items[0].ID, string(decoded))
}

// TestListCardsPage_FilterDoesNotAffectTotal confirms Total reflects the
// un-filtered project size. This lets clients show "showing X of Y" while a
// filter is active.
func TestListCardsPage_FilterDoesNotAffectTotal(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// 2 tasks + 1 bug = 3 total.
	for i := 0; i < 2; i++ {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("Task %d", i),
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
	}

	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Bug",
		Type:     "bug",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Filter down to bugs only; total should still report 3.
	page, err := svc.ListCardsPage(ctx, "test-project", storage.CardFilter{Type: "bug"}, PageOpts{Limit: 10})
	require.NoError(t, err)

	assert.Len(t, page.Items, 1)
	assert.True(t, page.HasTotal)
	assert.Equal(t, 3, page.Total, "Total must reflect un-filtered project size")
}
