package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
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
	require.NoError(t, store.ReloadIndex())

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
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, cards)
	})

	t.Run("mid-loop cancellation returns partial results and error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Cancel the context after a short delay so the loop starts but does
		// not finish reading all cards before it is interrupted.
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		cards, err := store.ListCards(ctx, "test-project", CardFilter{})

		// The context must have been cancelled.
		assert.ErrorIs(t, err, context.Canceled)

		// We expect a partial result: fewer than all cards returned.
		assert.Less(t, len(cards), numCards, "expected fewer than all %d cards to be returned on cancellation", numCards)
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
