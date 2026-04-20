package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validProjectConfig(name, prefix string) *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       name,
		Prefix:     prefix,
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

func testCard(id, state string) *board.Card {
	now := time.Now().UTC().Truncate(time.Second)

	return &board.Card{
		ID:       id,
		Title:    "Test " + id,
		Project:  "test-project",
		Type:     "task",
		State:    state,
		Priority: "medium",
		Created:  now,
		Updated:  now,
	}
}

func setupTestProject(t *testing.T, boardsDir, projectName, prefix string) {
	t.Helper()

	cfg := validProjectConfig(projectName, prefix)
	require.NoError(t, board.SaveProjectConfig(boardsDir+"/"+projectName, cfg))
}

func TestNewFilesystemStore_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	projects, err := store.ListProjects(context.Background())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestNewFilesystemStore_WithProjects(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "project-alpha", "ALPHA")
	setupTestProject(t, dir, "project-beta", "BETA")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	projects, err := store.ListProjects(context.Background())
	require.NoError(t, err)
	assert.Len(t, projects, 2)
}

func TestFilesystemStore_GetProject(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	cfg, err := store.GetProject(context.Background(), "test-project")
	require.NoError(t, err)
	assert.Equal(t, "test-project", cfg.Name)
	assert.Equal(t, "TEST", cfg.Prefix)
}

func TestFilesystemStore_GetProject_NotFound(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	_, err = store.GetProject(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestFilesystemStore_SaveProject(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	cfg := validProjectConfig("new-project", "NEW")
	err = store.SaveProject(context.Background(), cfg)
	require.NoError(t, err)

	loaded, err := store.GetProject(context.Background(), "new-project")
	require.NoError(t, err)
	assert.Equal(t, "new-project", loaded.Name)
}

func TestFilesystemStore_CreateAndGetCard(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-001", "todo")
	err = store.CreateCard(context.Background(), "test-project", card)
	require.NoError(t, err)

	loaded, err := store.GetCard(context.Background(), "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", loaded.ID)
	assert.Equal(t, "Test TEST-001", loaded.Title)
	assert.Equal(t, "todo", loaded.State)
}

func TestFilesystemStore_CreateCard_ProjectNotFound(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-001", "todo")
	err = store.CreateCard(context.Background(), "nonexistent", card)
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestFilesystemStore_CreateCard_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-001", "todo")
	err = store.CreateCard(context.Background(), "test-project", card)
	require.NoError(t, err)

	err = store.CreateCard(context.Background(), "test-project", card)
	assert.ErrorIs(t, err, ErrCardExists)
}

func TestFilesystemStore_GetCard_NotFound(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	_, err = store.GetCard(context.Background(), "test-project", "TEST-999")
	assert.ErrorIs(t, err, ErrCardNotFound)
}

func TestFilesystemStore_UpdateCard(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-001", "todo")
	err = store.CreateCard(context.Background(), "test-project", card)
	require.NoError(t, err)

	card.State = "in_progress"
	card.Title = "Updated Title"
	err = store.UpdateCard(context.Background(), "test-project", card)
	require.NoError(t, err)

	loaded, err := store.GetCard(context.Background(), "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "in_progress", loaded.State)
	assert.Equal(t, "Updated Title", loaded.Title)
}

func TestFilesystemStore_UpdateCard_NotFound(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-999", "todo")
	err = store.UpdateCard(context.Background(), "test-project", card)
	assert.ErrorIs(t, err, ErrCardNotFound)
}

func TestFilesystemStore_DeleteCard(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("TEST-001", "todo")
	err = store.CreateCard(context.Background(), "test-project", card)
	require.NoError(t, err)

	err = store.DeleteCard(context.Background(), "test-project", "TEST-001")
	require.NoError(t, err)

	_, err = store.GetCard(context.Background(), "test-project", "TEST-001")
	assert.ErrorIs(t, err, ErrCardNotFound)
}

func TestFilesystemStore_DeleteCard_NotFound(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	err = store.DeleteCard(context.Background(), "test-project", "TEST-999")
	assert.ErrorIs(t, err, ErrCardNotFound)
}

func TestFilesystemStore_ListCards_NoFilter(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card2 := testCard("TEST-002", "in_progress")

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)
}

func TestFilesystemStore_ListCards_FilterByState(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card2 := testCard("TEST-002", "in_progress")
	card3 := testCard("TEST-003", "todo")

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{State: "todo"})
	require.NoError(t, err)
	assert.Len(t, cards, 2)

	for _, c := range cards {
		assert.Equal(t, "todo", c.State)
	}
}

func TestFilesystemStore_ListCards_FilterByType(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Type = "bug"
	card2 := testCard("TEST-002", "todo")
	card2.Type = "task"

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Type: "bug"})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "bug", cards[0].Type)
}

func TestFilesystemStore_ListCards_FilterByPriority(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Priority = "high"
	card2 := testCard("TEST-002", "todo")
	card2.Priority = "low"

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "high", cards[0].Priority)
}

func TestFilesystemStore_ListCards_FilterByAssignedAgent(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.AssignedAgent = "agent-1"
	card2 := testCard("TEST-002", "todo")
	card2.AssignedAgent = "agent-2"
	card3 := testCard("TEST-003", "todo")

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{AssignedAgent: "agent-1"})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "agent-1", cards[0].AssignedAgent)
}

func TestFilesystemStore_ListCards_FilterByLabel(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Labels = []string{"backend", "urgent"}
	card2 := testCard("TEST-002", "todo")
	card2.Labels = []string{"frontend"}
	card3 := testCard("TEST-003", "todo")
	card3.Labels = []string{"backend"}

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Label: "backend"})
	require.NoError(t, err)
	assert.Len(t, cards, 2)
}

func TestFilesystemStore_ListCards_FilterByParent(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card2 := testCard("TEST-002", "todo")
	card2.Parent = "TEST-001"
	card3 := testCard("TEST-003", "todo")
	card3.Parent = "TEST-001"

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Parent: "TEST-001"})
	require.NoError(t, err)
	assert.Len(t, cards, 2)
}

func TestFilesystemStore_ListCards_FilterByExternalID(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Source = &board.Source{
		System:     "jira",
		ExternalID: "JIRA-123",
	}
	card2 := testCard("TEST-002", "todo")
	card2.Source = &board.Source{
		System:     "jira",
		ExternalID: "JIRA-456",
	}
	card3 := testCard("TEST-003", "todo")

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{ExternalID: "JIRA-123"})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "JIRA-123", cards[0].Source.ExternalID)
}

func boolPtr(b bool) *bool { return &b }

func TestFilesystemStore_ListCards_FilterByVetted(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Vetted = true
	card2 := testCard("TEST-002", "todo")
	card2.Vetted = false
	card3 := testCard("TEST-003", "todo")
	card3.Vetted = true

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	t.Run("filter by Vetted=true returns only vetted cards", func(t *testing.T) {
		cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Vetted: boolPtr(true)})
		require.NoError(t, err)
		assert.Len(t, cards, 2)

		for _, c := range cards {
			assert.True(t, c.Vetted)
		}
	})

	t.Run("filter by Vetted=false returns only unvetted cards", func(t *testing.T) {
		cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Vetted: boolPtr(false)})
		require.NoError(t, err)
		assert.Len(t, cards, 1)
		assert.False(t, cards[0].Vetted)
	})

	t.Run("filter by Vetted=nil returns all cards", func(t *testing.T) {
		cards, err := store.ListCards(context.Background(), "test-project", CardFilter{Vetted: nil})
		require.NoError(t, err)
		assert.Len(t, cards, 3)
	})
}

func TestFilesystemStore_ListCards_MultipleFilters(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card1.Type = "bug"
	card1.Priority = "high"
	card2 := testCard("TEST-002", "todo")
	card2.Type = "bug"
	card2.Priority = "low"
	card3 := testCard("TEST-003", "in_progress")
	card3.Type = "bug"
	card3.Priority = "high"

	require.NoError(t, store.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card2))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", card3))

	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{
		State:    "todo",
		Type:     "bug",
		Priority: "high",
	})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "TEST-001", cards[0].ID)
}

func TestFilesystemStore_ListCards_ProjectNotFound(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	_, err = store.ListCards(context.Background(), "nonexistent", CardFilter{})
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestFilesystemStore_ConcurrentCreateAndRead(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	const (
		numWriters     = 5
		cardsPerWriter = 20
		numReaders     = 10
	)

	var wg sync.WaitGroup

	ctx := context.Background()

	for w := 0; w < numWriters; w++ {
		wg.Add(1)

		go func(writerID int) {
			defer wg.Done()

			for i := 0; i < cardsPerWriter; i++ {
				cardID := "TEST-" + string(rune('A'+writerID)) + "-" + string(rune('0'+i%10)) + string(rune('0'+i/10))
				card := testCard(cardID, "todo")
				_ = store.CreateCard(ctx, "test-project", card)
			}
		}(w)
	}

	for r := 0; r < numReaders; r++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < 50; i++ {
				_, _ = store.ListCards(ctx, "test-project", CardFilter{})
			}
		}()
	}

	wg.Wait()

	cards, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, numWriters*cardsPerWriter)
}

func TestFilesystemStore_LoadExistingCards(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store1, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card1 := testCard("TEST-001", "todo")
	card2 := testCard("TEST-002", "in_progress")

	require.NoError(t, store1.CreateCard(context.Background(), "test-project", card1))
	require.NoError(t, store1.CreateCard(context.Background(), "test-project", card2))

	store2, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	cards, err := store2.ListCards(context.Background(), "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)

	loaded1, err := store2.GetCard(context.Background(), "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "todo", loaded1.State)

	loaded2, err := store2.GetCard(context.Background(), "test-project", "TEST-002")
	require.NoError(t, err)
	assert.Equal(t, "in_progress", loaded2.State)
}

func TestFilesystemStore_DeleteProject(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	err = store.DeleteProject(context.Background(), "test-project")
	require.NoError(t, err)

	// Should be gone from index
	_, err = store.GetProject(context.Background(), "test-project")
	require.ErrorIs(t, err, ErrProjectNotFound)

	// Should be gone from disk
	projects, err := store.ListProjects(context.Background())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestFilesystemStore_DeleteProject_NotFound(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	err = store.DeleteProject(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestFilesystemStore_ProjectCardCount(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	// Empty project
	count, err := store.ProjectCardCount(context.Background(), "test-project")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add cards
	require.NoError(t, store.CreateCard(context.Background(), "test-project", testCard("TEST-001", "todo")))
	require.NoError(t, store.CreateCard(context.Background(), "test-project", testCard("TEST-002", "todo")))

	count, err = store.ProjectCardCount(context.Background(), "test-project")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestFilesystemStore_ProjectCardCount_NotFound(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	_, err = store.ProjectCardCount(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestFilesystemStore_ReloadIndex(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a card through the store
	card := testCard("TEST-001", "todo")
	require.NoError(t, store.CreateCard(ctx, "test-project", card))

	cards, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)

	// Simulate an external change: write a new card file directly to disk
	newCardContent := `---
id: TEST-002
title: External Card
project: test-project
type: task
state: todo
priority: high
created: 2026-04-01T00:00:00Z
updated: 2026-04-01T00:00:00Z
---

Created externally.
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-project", "tasks", "TEST-002.md"), []byte(newCardContent), 0o644))

	// Before reload, the index doesn't know about TEST-002
	cards, err = store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)

	// Reload the index
	require.NoError(t, store.ReloadIndex(context.Background()))

	// Now we should see both cards
	cards, err = store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)

	// Verify the externally created card is findable
	got, err := store.GetCard(ctx, "test-project", "TEST-002")
	require.NoError(t, err)
	assert.Equal(t, "External Card", got.Title)
	assert.Equal(t, "high", got.Priority)
}

func TestValidatePathComponent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"valid simple name", "test-project", false},
		{"valid card ID", "TEST-001", false},
		{"valid with underscores", "my_project", false},
		{"valid with dots", "v1.2.3", false},
		{"empty string", "", true},
		{"dot", ".", true},
		{"double dot", "..", true},
		{"forward slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
		{"path traversal", "../etc", true},
		{"nested traversal", "a/../b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathComponent(tt.input)
			if tt.wantError {
				assert.ErrorIs(t, err, ErrInvalidPath)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFilesystemStore_CreateCard_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	card := testCard("../../../evil", "todo")
	err = store.CreateCard(context.Background(), "test-project", card)
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestFilesystemStore_SaveProject_PathTraversal(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	cfg := validProjectConfig("../../escape", "ESC")
	err = store.SaveProject(context.Background(), cfg)
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestFilesystemStore_DeleteProject_PathTraversal(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	// Project doesn't exist in index, so we get ErrProjectNotFound before
	// path validation. This confirms the index lookup guards against
	// arbitrary names reaching the filesystem.
	err = store.DeleteProject(context.Background(), "../../escape")
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	t.Run("writes file with correct content and permissions", func(t *testing.T) {
		data := []byte("hello world")
		err := atomicWriteFile(path, data)
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, got)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	})

	t.Run("overwrites existing file atomically", func(t *testing.T) {
		original := []byte("original content")
		err := atomicWriteFile(path, original)
		require.NoError(t, err)

		updated := []byte("updated content that is longer than original")
		err = atomicWriteFile(path, updated)
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, updated, got)
	})

	t.Run("no temp files left on success", func(t *testing.T) {
		err := atomicWriteFile(path, []byte("clean"))
		require.NoError(t, err)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)

		for _, e := range entries {
			assert.False(t, strings.HasPrefix(e.Name(), ".tmp-"),
				"temp file %s should not remain after successful write", e.Name())
		}
	})

	t.Run("fails on non-existent directory", func(t *testing.T) {
		badPath := filepath.Join(dir, "nonexistent", "file.txt")
		err := atomicWriteFile(badPath, []byte("data"))
		assert.Error(t, err)
	})

	t.Run("concurrent writes produce valid content", func(t *testing.T) {
		target := filepath.Join(dir, "concurrent.txt")

		const (
			goroutines = 20
			iterations = 50
		)

		var wg sync.WaitGroup
		for g := range goroutines {
			wg.Add(1)

			go func(id int) {
				defer wg.Done()

				content := []byte(strings.Repeat(string(rune('A'+id%26)), 1024))
				for range iterations {
					_ = atomicWriteFile(target, content)
				}
			}(g)
		}

		wg.Wait()

		// After all writes complete, the file must contain content from
		// exactly one writer (no partial/mixed writes).
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Len(t, got, 1024, "file should be exactly 1024 bytes")

		// Every byte should be the same character (from a single write).
		first := got[0]
		for i, b := range got {
			assert.Equal(t, first, b,
				"byte %d differs: expected %c, got %c (partial write detected)", i, first, b)
		}
	})
}

// countingContext is a context.Context that cancels itself after a fixed number
// of Err() calls. The production ListCards loop calls ctx.Err() exactly once per
// card, so cancelAfter=N means the loop will see the cancellation on the (N+1)-th
// card and return at most N cards. This gives a deterministic bound that does not
// depend on wall-clock timing or I/O speed.
type countingContext struct {
	inner       context.Context //nolint:containedctx // test helper needs to delegate to inner ctx
	mu          sync.Mutex
	remaining   int
	cancel      context.CancelFunc
	cancelledAt int // Err() call number when cancel fired (1-based)
	total       int // total Err() calls so far
}

func newCountingContext(parent context.Context, cancelAfter int) (*countingContext, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cc := &countingContext{
		inner:     ctx,
		remaining: cancelAfter,
		cancel:    cancel,
	}

	return cc, cancel
}

// Deadline implements context.Context.
func (c *countingContext) Deadline() (deadline time.Time, ok bool) { return c.inner.Deadline() }

// Done implements context.Context.
func (c *countingContext) Done() <-chan struct{} { return c.inner.Done() }

// Value implements context.Context.
func (c *countingContext) Value(key any) any { return c.inner.Value(key) }

// Err implements context.Context. It decrements the remaining counter and fires
// the cancel after the configured number of calls.
func (c *countingContext) Err() error {
	if err := c.inner.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.total++
	if c.remaining > 0 {
		c.remaining--
		if c.remaining == 0 {
			c.cancelledAt = c.total
			c.cancel()
		}
	}

	return c.inner.Err()
}

func TestFilesystemStore_ListCards_ContextCancellation(t *testing.T) {
	const numCards = 500

	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	// Body content large enough to make each ReadFile measurably slow.
	largeBody := strings.Repeat("x", 4096)

	// Create enough large card files to make mid-loop cancellation detectable.
	for i := range numCards {
		id := fmt.Sprintf("TEST-%04d", i+1)
		c := testCard(id, "todo")
		c.Body = largeBody
		require.NoError(t, store.CreateCard(context.Background(), "test-project", c))
	}

	t.Run("entry cancellation returns error immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancel before calling ListCards

		cards, err := store.ListCards(ctx, "test-project", CardFilter{})
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, cards)
	})

	t.Run("mid-loop cancellation returns partial results and error", func(t *testing.T) {
		// cancelAfter=10 means Err() will return nil for the first 10 calls and
		// then cancel; the loop checks Err() once per card before reading it, so
		// at most 10 cards are read before the cancellation is observed.
		const cancelAfter = 10

		cc, cancel := newCountingContext(context.Background(), cancelAfter)
		defer cancel()

		cards, err := store.ListCards(cc, "test-project", CardFilter{})

		// The context must have been cancelled.
		require.ErrorIs(t, err, context.Canceled)

		// The loop checks ctx.Err() once per card before reading it.  After the
		// cancelAfter-th check the cancel fires; the loop detects it on the very
		// next iteration check.  So the number of cards returned must be exactly
		// cancelAfter (the ones already appended before the check fires).
		assert.LessOrEqual(t, len(cards), cancelAfter,
			"expected at most %d cards before cancellation was detected, got %d/%d",
			cancelAfter, len(cards), numCards)
	})
}

func TestFilesystemStore_LoadIndex_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	// Create a real card file
	tasksDir := filepath.Join(dir, "test-project", "tasks")
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))

	realContent := `---
id: TEST-001
title: Real Card
project: test-project
type: task
state: todo
priority: medium
created: 2026-04-01T00:00:00Z
updated: 2026-04-01T00:00:00Z
---
`
	realPath := filepath.Join(tasksDir, "TEST-001.md")
	require.NoError(t, os.WriteFile(realPath, []byte(realContent), 0o644))

	// Create a symlink card file pointing to something outside
	symlinkTarget := filepath.Join(dir, "secret.md")
	require.NoError(t, os.WriteFile(symlinkTarget, []byte(realContent), 0o644))
	require.NoError(t, os.Symlink(symlinkTarget, filepath.Join(tasksDir, "TEST-002.md")))

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	// Only the real card should be loaded, symlink skipped
	cards, err := store.ListCards(context.Background(), "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 1)
	assert.Equal(t, "TEST-001", cards[0].ID)
}

// TestFilesystemStore_CtxlogRequestID verifies that a context enriched with
// ctxlog.WithRequestID causes storage log output to contain the request_id.
func TestFilesystemStore_CtxlogRequestID(t *testing.T) {
	var buf bytes.Buffer

	origLogger := slog.Default()

	t.Cleanup(func() { slog.SetDefault(origLogger) })

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	const requestID = "test-req-id-storage-001"

	ctx := ctxlog.WithRequestID(context.Background(), requestID)

	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	// Write a corrupt card file directly so ListCards triggers an error log.
	tasksDir := dir + "/test-project/tasks"
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))
	require.NoError(t, os.WriteFile(tasksDir+"/TEST-BAD.md", []byte("not valid frontmatter {{{\n"), 0o644))

	// Reload the index so the corrupt file is in the index.
	require.NoError(t, store.ReloadIndex(ctx))

	// ListCards will encounter the corrupt file and call ctxlog.Logger(ctx).Error.
	_, _ = store.ListCards(ctx, "test-project", CardFilter{})

	output := buf.String()
	assert.Contains(t, output, "request_id="+requestID,
		"log output should contain the request_id from the enriched context")
}

// TestFilesystemStore_CardCache_GetCardServedFromCache verifies that GetCard
// returns the cached value without touching disk: after deleting the on-disk
// file out-of-band, GetCard still returns the cached card.
func TestFilesystemStore_CardCache_GetCardServedFromCache(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	card := testCard("TEST-001", "todo")
	require.NoError(t, store.CreateCard(ctx, "test-project", card))

	// Delete the on-disk file behind the cache's back.
	filePath := filepath.Join(dir, "test-project", "tasks", "TEST-001.md")
	require.NoError(t, os.Remove(filePath))

	// GetCard should still return the cached value — no disk read.
	got, err := store.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", got.ID)
	assert.Equal(t, "todo", got.State)
}

// TestFilesystemStore_CardCache_ListCardsServedFromCache verifies that
// ListCards returns cached cards without touching disk.
func TestFilesystemStore_CardCache_ListCardsServedFromCache(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.CreateCard(ctx, "test-project", testCard("TEST-001", "todo")))
	require.NoError(t, store.CreateCard(ctx, "test-project", testCard("TEST-002", "in_progress")))

	// Delete all on-disk card files.
	tasksDir := filepath.Join(dir, "test-project", "tasks")
	entries, err := os.ReadDir(tasksDir)
	require.NoError(t, err)

	for _, e := range entries {
		require.NoError(t, os.Remove(filepath.Join(tasksDir, e.Name())))
	}

	// ListCards should still see both cards from the cache.
	cards, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Len(t, cards, 2)
}

// TestFilesystemStore_CardCache_SaveUpdatesCache verifies that UpdateCard
// replaces the cached value so subsequent reads see the new data.
func TestFilesystemStore_CardCache_SaveUpdatesCache(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	card := testCard("TEST-001", "todo")
	card.Title = "Original"
	require.NoError(t, store.CreateCard(ctx, "test-project", card))

	// Mutate and update.
	card.Title = "Updated"
	card.State = "in_progress"
	require.NoError(t, store.UpdateCard(ctx, "test-project", card))

	// Delete the on-disk file to prove the read comes from the cache.
	require.NoError(t, os.Remove(filepath.Join(dir, "test-project", "tasks", "TEST-001.md")))

	got, err := store.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Title)
	assert.Equal(t, "in_progress", got.State)
}

// TestFilesystemStore_CardCache_DeleteRemovesFromCache verifies that
// DeleteCard drops the entry from the cache.
func TestFilesystemStore_CardCache_DeleteRemovesFromCache(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.CreateCard(ctx, "test-project", testCard("TEST-001", "todo")))
	require.NoError(t, store.DeleteCard(ctx, "test-project", "TEST-001"))

	_, err = store.GetCard(ctx, "test-project", "TEST-001")
	require.ErrorIs(t, err, ErrCardNotFound)

	cards, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	assert.Empty(t, cards)
}

// TestFilesystemStore_CardCache_DeepCopyContract verifies that mutating a
// card returned from GetCard or ListCards does not affect the cached value
// or subsequent reads.
func TestFilesystemStore_CardCache_DeepCopyContract(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	card := testCard("TEST-001", "todo")
	card.Labels = []string{"alpha", "beta"}
	card.DependsOn = []string{"OTHER-001"}
	card.Subtasks = []string{"SUB-001"}
	card.Context = []string{"ctx-a"}
	card.LastHeartbeat = &now
	card.Source = &board.Source{System: "jira", ExternalID: "JIRA-1", ExternalURL: "http://example"}
	card.TokenUsage = &board.TokenUsage{PromptTokens: 100, CompletionTokens: 50}
	card.Custom = map[string]any{"key": "value"}
	card.ActivityLog = []board.ActivityEntry{{Agent: "a", Timestamp: now, Action: "act", Message: "msg"}}
	require.NoError(t, store.CreateCard(ctx, "test-project", card))

	// First read, then mutate every clonable field.
	got1, err := store.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, err)

	got1.Title = "Mutated"
	got1.State = "done"
	got1.Labels[0] = "mutated"
	got1.DependsOn[0] = "mutated"
	got1.Subtasks[0] = "mutated"
	got1.Context[0] = "mutated"
	*got1.LastHeartbeat = now.Add(24 * time.Hour)
	got1.Source.ExternalID = "mutated"
	got1.TokenUsage.PromptTokens = 99999
	got1.Custom["key"] = "mutated"
	got1.ActivityLog[0].Message = "mutated"

	// Second read must be pristine.
	got2, err := store.GetCard(ctx, "test-project", "TEST-001")
	require.NoError(t, err)

	assert.Equal(t, "Test TEST-001", got2.Title)
	assert.Equal(t, "todo", got2.State)
	assert.Equal(t, []string{"alpha", "beta"}, got2.Labels)
	assert.Equal(t, []string{"OTHER-001"}, got2.DependsOn)
	assert.Equal(t, []string{"SUB-001"}, got2.Subtasks)
	assert.Equal(t, []string{"ctx-a"}, got2.Context)
	require.NotNil(t, got2.LastHeartbeat)
	assert.True(t, got2.LastHeartbeat.Equal(now), "LastHeartbeat should be unchanged")
	require.NotNil(t, got2.Source)
	assert.Equal(t, "JIRA-1", got2.Source.ExternalID)
	require.NotNil(t, got2.TokenUsage)
	assert.Equal(t, int64(100), got2.TokenUsage.PromptTokens)
	assert.Equal(t, "value", got2.Custom["key"])
	require.Len(t, got2.ActivityLog, 1)
	assert.Equal(t, "msg", got2.ActivityLog[0].Message)

	// Same contract for ListCards.
	list1, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	require.Len(t, list1, 1)

	list1[0].Title = "ListMutated"
	list1[0].Labels[0] = "mutated"

	list2, err := store.ListCards(ctx, "test-project", CardFilter{})
	require.NoError(t, err)
	require.Len(t, list2, 1)
	assert.Equal(t, "Test TEST-001", list2[0].Title)
	assert.Equal(t, []string{"alpha", "beta"}, list2[0].Labels)
}

// TestFilesystemStore_CardCache_ReloadPicksUpExternalWrites verifies that
// ReloadIndex (the hook invoked after a git rebase pulls in new cards) makes
// out-of-band card files visible via GetCard without a disk read.
func TestFilesystemStore_CardCache_ReloadPicksUpExternalWrites(t *testing.T) {
	dir := t.TempDir()
	setupTestProject(t, dir, "test-project", "TEST")

	store, err := NewFilesystemStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Write a card file directly to disk, bypassing the store.
	external := `---
id: TEST-EXT
title: External Card
project: test-project
type: task
state: in_progress
priority: high
created: 2026-04-01T00:00:00Z
updated: 2026-04-01T00:00:00Z
---

Created by an external writer (e.g. git rebase pulling in a commit).
`
	tasksDir := filepath.Join(dir, "test-project", "tasks")
	require.NoError(t, os.MkdirAll(tasksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tasksDir, "TEST-EXT.md"), []byte(external), 0o644))

	// Before reload, the cache does not know about the card.
	_, err = store.GetCard(ctx, "test-project", "TEST-EXT")
	// Cache miss falls through to disk and finds it, so this should succeed.
	require.NoError(t, err)

	// After reload, the card is fully hydrated in the cache. Prove this by
	// deleting the on-disk file and reading again — the cache must answer.
	require.NoError(t, store.ReloadIndex(ctx))
	require.NoError(t, os.Remove(filepath.Join(tasksDir, "TEST-EXT.md")))

	got, err := store.GetCard(ctx, "test-project", "TEST-EXT")
	require.NoError(t, err)
	assert.Equal(t, "TEST-EXT", got.ID)
	assert.Equal(t, "External Card", got.Title)
	assert.Equal(t, "in_progress", got.State)
}

// BenchmarkListCards_500Cards measures ListCards throughput with a warm cache.
func BenchmarkListCards_500Cards(b *testing.B) {
	dir := b.TempDir()

	cfg := validProjectConfig("bench-project", "BENCH")
	require.NoError(b, board.SaveProjectConfig(dir+"/bench-project", cfg))

	store, err := NewFilesystemStore(dir)
	require.NoError(b, err)

	ctx := context.Background()

	for i := range 500 {
		id := fmt.Sprintf("BENCH-%04d", i+1)
		c := testCard(id, "todo")
		c.Body = strings.Repeat("x", 1024)
		require.NoError(b, store.CreateCard(ctx, "bench-project", c))
	}

	b.ResetTimer()

	for range b.N {
		cards, err := store.ListCards(ctx, "bench-project", CardFilter{})
		if err != nil {
			b.Fatal(err)
		}

		if len(cards) != 500 {
			b.Fatalf("expected 500 cards, got %d", len(cards))
		}
	}
}
