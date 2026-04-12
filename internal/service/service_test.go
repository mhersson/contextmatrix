package service

import (
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
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProject creates a test project configuration.
func testProject() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

// setupTest creates a test environment with all service dependencies.
func setupTest(t *testing.T) (*CardService, string, func()) {
	t.Helper()

	// Create temp directory
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	// Create test project
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	// Create dependencies
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	cleanup := func() {
		// Cleanup handled by t.TempDir()
	}

	return svc, tmpDir, cleanup
}

func TestCreateCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Create card
	input := CreateCardInput{
		Title:    "Test Card",
		Type:     "task",
		Priority: "medium",
		Labels:   []string{"backend"},
		Body:     "## Description\nTest body",
	}

	card, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)

	// Verify card
	assert.Equal(t, "TEST-001", card.ID)
	assert.Equal(t, "Test Card", card.Title)
	assert.Equal(t, "test-project", card.Project)
	assert.Equal(t, "task", card.Type)
	assert.Equal(t, "todo", card.State) // Default to first state
	assert.Equal(t, "medium", card.Priority)
	assert.Equal(t, []string{"backend"}, card.Labels)
	assert.Equal(t, "## Description\nTest body", card.Body)
	assert.False(t, card.Created.IsZero())
	assert.False(t, card.Updated.IsZero())

	// Verify event was published
	select {
	case event := <-ch:
		assert.Equal(t, events.CardCreated, event.Type)
		assert.Equal(t, "test-project", event.Project)
		assert.Equal(t, "TEST-001", event.CardID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardCreated event")
	}

	// Verify git commit
	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "TEST-001")
	assert.Contains(t, msg, "created")

	// Verify next ID was incremented
	cfg, err := svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.NextID)
}

func TestCreateCardWithSource(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		Title:    "Imported Card",
		Type:     "bug",
		Priority: "high",
		Source: &board.Source{
			System:      "jira",
			ExternalID:  "PROJ-123",
			ExternalURL: "https://jira.example.com/PROJ-123",
		},
	}

	card, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)

	assert.NotNil(t, card.Source)
	assert.Equal(t, "jira", card.Source.System)
	assert.Equal(t, "PROJ-123", card.Source.ExternalID)
}

func TestCreateCardInvalidType(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		Title:    "Bad Card",
		Type:     "invalid-type",
		Priority: "medium",
	}

	_, err := svc.CreateCard(ctx, "test-project", input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

// TestCreateCard_CustomID verifies that a caller-supplied ID is used directly.
func TestCreateCard_CustomID(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		ID:       "TEST-042",
		Title:    "Custom ID Card",
		Type:     "task",
		Priority: "medium",
	}

	card, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)
	assert.Equal(t, "TEST-042", card.ID)
	assert.Equal(t, "Custom ID Card", card.Title)
}

// TestCreateCard_CustomID_NextIDAdvances verifies that NextID is advanced past
// the numeric suffix of a caller-supplied ID.
func TestCreateCard_CustomID_NextIDAdvances(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		ID:       "TEST-100",
		Title:    "High Number Card",
		Type:     "task",
		Priority: "medium",
	}

	_, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)

	// NextID must now be at least 101 so auto-generated IDs don't collide.
	cfg, err := svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cfg.NextID, 101)
}

// TestCreateCard_CustomID_Duplicate verifies that creating a card with an
// already-used ID returns an error.
func TestCreateCard_CustomID_Duplicate(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		ID:       "TEST-007",
		Title:    "First Card",
		Type:     "task",
		Priority: "medium",
	}

	_, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)

	// Attempt to create again with the same ID.
	input2 := CreateCardInput{
		ID:       "TEST-007",
		Title:    "Duplicate Card",
		Type:     "task",
		Priority: "low",
	}

	_, err = svc.CreateCard(ctx, "test-project", input2)
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrCardExists)
}

// TestCreateCard_AutoGeneratedUnaffected verifies that auto-generated IDs still
// work correctly when no custom ID is supplied.
func TestCreateCard_AutoGeneratedUnaffected(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	input := CreateCardInput{
		Title:    "Auto ID Card",
		Type:     "task",
		Priority: "medium",
	}

	card, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)
	// First auto-generated card should be TEST-001 (NextID starts at 1).
	assert.Equal(t, "TEST-001", card.ID)

	card2, err := svc.CreateCard(ctx, "test-project", input)
	require.NoError(t, err)
	assert.Equal(t, "TEST-002", card2.ID)
}

// TestCreateCard_CustomID_NextIDAlreadyAhead verifies that NextID is NOT
// decreased when the custom ID's suffix is lower than the current NextID.
func TestCreateCard_CustomID_NextIDAlreadyAhead(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Auto-generate a card first to advance NextID to 2.
	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Auto Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Now create a custom card with a suffix lower than current NextID.
	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		ID:       "TEST-001",
		Title:    "Custom Low ID",
		Type:     "task",
		Priority: "medium",
	})
	// TEST-001 was just used by the auto-generated card, expect duplicate error.
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrCardExists)
}

// TestCreateCard_CustomID_LowerSuffixThanNextID verifies that NextID is NOT
// decreased when a custom ID's suffix is already behind the counter.
func TestCreateCard_CustomID_LowerSuffixThanNextID(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Advance NextID manually by creating an auto card.
	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Auto Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create a custom card with suffix 050 — well above the current NextID (2).
	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		ID:       "TEST-050",
		Title:    "High Custom",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	cfg, err := svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cfg.NextID, 51)
}

func TestUpdateCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card first
	createInput := CreateCardInput{
		Title:    "Original Title",
		Type:     "task",
		Priority: "low",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Update card
	updateInput := UpdateCardInput{
		Title:    "Updated Title",
		Type:     "task",
		State:    "in_progress",
		Priority: "high",
		Labels:   []string{"urgent"},
		Body:     "Updated body",
	}

	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, updateInput)
	require.NoError(t, err)

	assert.Equal(t, "Updated Title", updated.Title)
	assert.Equal(t, "in_progress", updated.State)
	assert.Equal(t, "high", updated.Priority)
	assert.Equal(t, []string{"urgent"}, updated.Labels)
	assert.Equal(t, "Updated body", updated.Body)

	// Immutable fields preserved
	assert.Equal(t, card.ID, updated.ID)
	assert.Equal(t, card.Project, updated.Project)
	assert.True(t, card.Created.Equal(updated.Created))

	// Updated timestamp changed
	assert.True(t, updated.Updated.After(card.Created))

	// Verify state change event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardStateChanged, event.Type)
		assert.Equal(t, "todo", event.Data["old_state"])
		assert.Equal(t, "in_progress", event.Data["new_state"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardStateChanged event")
	}
}

func TestUpdateCardInvalidTransition(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Try invalid transition: todo -> done (not allowed)
	updateInput := UpdateCardInput{
		Title:    "Test",
		Type:     "task",
		State:    "done",
		Priority: "medium",
	}

	_, err = svc.UpdateCard(ctx, "test-project", card.ID, updateInput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transition")
}

func TestPatchCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Original",
		Type:     "task",
		Priority: "low",
		Body:     "Original body",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Patch only title (other fields unchanged)
	newTitle := "Patched Title"
	patchInput := PatchCardInput{
		Title: &newTitle,
	}

	patched, err := svc.PatchCard(ctx, "test-project", card.ID, patchInput)
	require.NoError(t, err)

	assert.Equal(t, "Patched Title", patched.Title)
	assert.Equal(t, "low", patched.Priority)          // Unchanged
	assert.Contains(t, patched.Body, "Original body") // Unchanged (may have trailing newline)
}

func TestPatchCardStateTransition(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Patch state
	newState := "in_progress"
	patchInput := PatchCardInput{
		State: &newState,
	}

	patched, err := svc.PatchCard(ctx, "test-project", card.ID, patchInput)
	require.NoError(t, err)

	assert.Equal(t, "in_progress", patched.State)

	// Verify event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardStateChanged, event.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event")
	}
}

func TestDeleteCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "To Delete",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Delete card
	err = svc.DeleteCard(ctx, "test-project", card.ID)
	require.NoError(t, err)

	// Verify card is gone
	_, err = svc.GetCard(ctx, "test-project", card.ID)
	assert.Error(t, err)

	// Verify event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardDeleted, event.Type)
		assert.Equal(t, card.ID, event.CardID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardDeleted event")
	}
}

func TestAddLogEntry(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Add log entry
	entry := board.ActivityEntry{
		Agent:   "test-agent",
		Action:  "status_update",
		Message: "Started working",
	}
	err = svc.AddLogEntry(ctx, "test-project", card.ID, entry)
	require.NoError(t, err)

	// Verify entry was added
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.Len(t, updated.ActivityLog, 1)
	assert.Equal(t, "test-agent", updated.ActivityLog[0].Agent)
	assert.Equal(t, "status_update", updated.ActivityLog[0].Action)
	assert.False(t, updated.ActivityLog[0].Timestamp.IsZero())

	// Verify event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardLogAdded, event.Type)
		assert.Equal(t, "test-agent", event.Agent)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardLogAdded event")
	}
}

func TestAddLogEntryCapping(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Add more than 50 entries
	for range 55 {
		entry := board.ActivityEntry{
			Agent:   "agent",
			Action:  "update",
			Message: "Entry",
		}
		err = svc.AddLogEntry(ctx, "test-project", card.ID, entry)
		require.NoError(t, err)
	}

	// Verify capped at 50
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Len(t, updated.ActivityLog, 50)
}

func TestClaimCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Claim card
	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	assert.Equal(t, "agent-1", claimed.AssignedAgent)
	assert.NotNil(t, claimed.LastHeartbeat)

	// Verify event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardClaimed, event.Type)
		assert.Equal(t, "agent-1", event.Agent)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardClaimed event")
	}

	// Verify git commit
	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "[agent:agent-1]")
	assert.Contains(t, msg, "claimed")
}

func TestClaimCardAlreadyClaimed(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create and claim card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Try to claim with different agent
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already claimed")
}

func TestReleaseCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create and claim card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Release card
	released, err := svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	assert.Empty(t, released.AssignedAgent)
	assert.Nil(t, released.LastHeartbeat)

	// Verify event
	select {
	case event := <-ch:
		assert.Equal(t, events.CardReleased, event.Type)
		assert.Equal(t, "agent-1", event.Agent)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected CardReleased event")
	}
}

func TestReleaseCardWrongAgent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create and claim card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Try to release with wrong agent
	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not own")
}

func TestHeartbeatCard(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create and claim card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	firstHeartbeat := claimed.LastHeartbeat

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Heartbeat
	err = svc.HeartbeatCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Verify heartbeat updated
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.True(t, updated.LastHeartbeat.After(*firstHeartbeat))
}

func TestGetCardContext(t *testing.T) {
	svc, tmpDir, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create template
	templateDir := filepath.Join(tmpDir, "boards", "test-project", "templates")
	require.NoError(t, os.MkdirAll(templateDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(templateDir, "task.md"),
		[]byte("## Plan\n\n## Progress\n"),
		0644,
	))

	// Create card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	// Get context
	cardCtx, err := svc.GetCardContext(ctx, "test-project", card.ID)
	require.NoError(t, err)

	assert.Equal(t, card.ID, cardCtx.Card.ID)
	assert.Equal(t, "test-project", cardCtx.Project.Name)
	assert.Equal(t, "## Plan\n\n## Progress\n", cardCtx.Template)
}

func TestConcurrentCardCreation(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	var wg sync.WaitGroup
	cardCount := 10
	cards := make([]*board.Card, cardCount)
	errs := make([]error, cardCount)

	// Create cards concurrently
	for i := range cardCount {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input := CreateCardInput{
				Title:    "Concurrent Card",
				Type:     "task",
				Priority: "medium",
			}
			cards[idx], errs[idx] = svc.CreateCard(ctx, "test-project", input)
		}(i)
	}
	wg.Wait()

	// Verify all created successfully
	for i, err := range errs {
		require.NoError(t, err, "card %d failed", i)
	}

	// Verify unique IDs
	ids := make(map[string]bool)
	for _, card := range cards {
		assert.False(t, ids[card.ID], "duplicate ID: %s", card.ID)
		ids[card.ID] = true
	}
}

func TestTimeoutCheckerIntegration(t *testing.T) {
	// Use short timeout for test
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	// Create test project
	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	// Create dependencies with short timeout
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 50*time.Millisecond) // Very short timeout

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	ctx := t.Context()

	// Create and claim a card
	createInput := CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
	}
	card, err := svc.CreateCard(ctx, "test-project", createInput)
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Subscribe to events
	ch, unsub := bus.Subscribe()
	defer unsub()

	// Start timeout checker
	svc.StartTimeoutChecker(ctx, 25*time.Millisecond)

	// Wait for stall detection
	select {
	case event := <-ch:
		assert.Equal(t, events.CardStalled, event.Type)
		assert.Equal(t, "agent-1", event.Data["previous_agent"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected CardStalled event")
	}

	// Verify card state
	stalled, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "stalled", stalled.State)
	assert.Empty(t, stalled.AssignedAgent)
}

func TestListProjectsAndGetProject(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// List projects
	projects, err := svc.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "test-project", projects[0].Name)

	// Get project
	project, err := svc.GetProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, "test-project", project.Name)
	assert.Equal(t, "TEST", project.Prefix)
}

func TestUpdateCard_BlockedByDependency(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create dependency card (stays in todo)
	depCard, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependency",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create card that depends on depCard
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Try to transition to in_progress with unmet dependency
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:     "Dependent Card",
		Type:      "task",
		State:     "in_progress",
		Priority:  "medium",
		DependsOn: []string{depCard.ID},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, board.ErrDependenciesNotMet)
	assert.Contains(t, err.Error(), depCard.ID)
	assert.Contains(t, err.Error(), "todo")
}

func TestPatchCard_BlockedByDependency(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create dependency card (stays in todo)
	depCard, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependency",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create card with depends_on set via UpdateCard (to set DependsOn)
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Set dependency via full update (no state change)
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:     "Dependent Card",
		Type:      "task",
		State:     "todo",
		Priority:  "medium",
		DependsOn: []string{depCard.ID},
	})
	require.NoError(t, err)

	// Try to patch state to in_progress
	newState := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		State: &newState,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, board.ErrDependenciesNotMet)
	assert.Contains(t, err.Error(), depCard.ID)
}

func TestUpdateCard_DependenciesMet(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create dependency card and complete it
	depCard, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependency",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Transition dep: todo -> in_progress -> done
	_, err = svc.UpdateCard(ctx, "test-project", depCard.ID, UpdateCardInput{
		Title: "Dependency", Type: "task", State: "in_progress", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateCard(ctx, "test-project", depCard.ID, UpdateCardInput{
		Title: "Dependency", Type: "task", State: "done", Priority: "medium",
	})
	require.NoError(t, err)

	// Create dependent card and transition to in_progress (should succeed)
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:     "Dependent Card",
		Type:      "task",
		State:     "in_progress",
		Priority:  "medium",
		DependsOn: []string{depCard.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updated.State)
}

func TestGetCard_DependenciesMetField(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create dependency card (todo)
	depCard, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependency",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create card depending on depCard
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Set dependency
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:     "Dependent Card",
		Type:      "task",
		State:     "todo",
		Priority:  "medium",
		DependsOn: []string{depCard.ID},
	})
	require.NoError(t, err)

	// Fetch card — DependenciesMet should be false
	fetched, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.DependenciesMet)
	assert.False(t, *fetched.DependenciesMet)

	// Complete the dependency
	_, err = svc.UpdateCard(ctx, "test-project", depCard.ID, UpdateCardInput{
		Title: "Dependency", Type: "task", State: "in_progress", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateCard(ctx, "test-project", depCard.ID, UpdateCardInput{
		Title: "Dependency", Type: "task", State: "done", Priority: "medium",
	})
	require.NoError(t, err)

	// Fetch again — DependenciesMet should be true
	fetched, err = svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.DependenciesMet)
	assert.True(t, *fetched.DependenciesMet)
}

func TestListCards_DependenciesMetField(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create dep card (todo)
	depCard, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependency",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create card with dependency
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Dependent",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:     "Dependent",
		Type:      "task",
		State:     "todo",
		Priority:  "medium",
		DependsOn: []string{depCard.ID},
	})
	require.NoError(t, err)

	// List cards — check DependenciesMet on each
	cards, err := svc.ListCards(ctx, "test-project", storage.CardFilter{})
	require.NoError(t, err)

	for _, c := range cards {
		if c.ID == card.ID {
			require.NotNil(t, c.DependenciesMet)
			assert.False(t, *c.DependenciesMet)
		}
		if c.ID == depCard.ID {
			// No deps, should be nil
			assert.Nil(t, c.DependenciesMet)
		}
	}
}

func TestUpdateCard_NoDeps_NoBlock(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create card with no dependencies
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "No Deps",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Transition to in_progress should work fine
	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:    "No Deps",
		Type:     "task",
		State:    "in_progress",
		Priority: "medium",
	})
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updated.State)
}

// testProjectWithReview creates a project config with a review state,
// matching the real contextmatrix project config.
func testProjectWithReview() *board.ProjectConfig {
	return &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "blocked", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "not_planned"},
			"in_progress": {"blocked", "review", "todo", "done"},
			"blocked":     {"in_progress", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

// setupTestWithReview creates a test environment with a project that has a review state.
func setupTestWithReview(t *testing.T) (*CardService, string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectWithReview()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	return svc, tmpDir, func() {}
}

// createParentWithSubtasks creates a parent card and the given number of subtask cards,
// setting the parent's Subtasks field and each child's Parent field.
func createParentWithSubtasks(t *testing.T, svc *CardService, project string, numSubtasks int) (*board.Card, []*board.Card) {
	t.Helper()
	ctx := context.Background()

	parent, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title:    "Parent Task",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	subtasks := make([]*board.Card, numSubtasks)
	subtaskIDs := make([]string, numSubtasks)

	for i := range numSubtasks {
		child, err := svc.CreateCard(ctx, project, CreateCardInput{
			Title:    fmt.Sprintf("Subtask %d", i+1),
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		subtasks[i] = child
		subtaskIDs[i] = child.ID
	}

	// Update parent with subtask list
	updated, err := svc.UpdateCard(ctx, project, parent.ID, UpdateCardInput{
		Title:    parent.Title,
		Type:     parent.Type,
		State:    parent.State,
		Priority: parent.Priority,
		Subtasks: subtaskIDs,
	})
	require.NoError(t, err)
	return updated, subtasks
}

func TestParentAutoTransition_ChildInProgressMovesParentToInProgress(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)
	require.Equal(t, "todo", parent.State)

	// Transition first subtask to in_progress → parent should also move to in_progress
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Verify parent is now in_progress
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

func TestParentAutoTransition_SecondChildInProgressIdempotent(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)

	// Transition first subtask to in_progress
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Verify parent in_progress
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)

	// Transition second subtask to in_progress → parent stays in_progress (idempotent)
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	updatedParent, err = svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

func TestParentAutoTransition_OneSubtaskDoneParentStaysInProgress(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parentCard, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)

	// Transition both subtasks to in_progress (this also moves parent to in_progress)
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Complete first subtask: in_progress → done
	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Re-fetch parent — should still be in_progress (not all subtasks done)
	updatedParent, err := svc.GetCard(ctx, "test-project", parentCard.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

func TestParentAutoTransition_AllSubtasksDoneParentStaysInProgress(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)

	// Transition both subtasks to in_progress (parent also moves to in_progress)
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Complete first subtask: in_progress → done
	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Complete last subtask: in_progress → done
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Parent stays in in_progress — the orchestrator spawns a documentation
	// sub-agent first, then manually transitions the parent to review.
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

func TestParentAutoTransition_NoParentNoOp(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	// Create a standalone card (no parent)
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Standalone",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Transition to in_progress — should succeed without error (no parent to touch)
	inProgress := "in_progress"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	assert.Equal(t, "in_progress", patched.State)
}

func TestParentAutoTransition_GitCommitForParent(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 1)
	require.Equal(t, "todo", parent.State)

	// Transition subtask to in_progress → parent should also transition and commit
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// The last git commit should reference the parent card
	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, parent.ID)
}

// createParentWithChildrenByParentField creates a parent card and the given number
// of child cards using only the parent field on the child — the parent's Subtasks
// field is NOT populated. This reflects how agents typically create subtasks.
func createParentWithChildrenByParentField(t *testing.T, svc *CardService, project string, numChildren int) (*board.Card, []*board.Card) {
	t.Helper()
	ctx := context.Background()

	parent, err := svc.CreateCard(ctx, project, CreateCardInput{
		Title:    "Parent Task",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	children := make([]*board.Card, numChildren)
	for i := range numChildren {
		child, err := svc.CreateCard(ctx, project, CreateCardInput{
			Title:    fmt.Sprintf("Child %d", i+1),
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		children[i] = child
	}

	// Re-fetch parent — its Subtasks field is intentionally NOT updated
	parent, err = svc.GetCard(ctx, project, parent.ID)
	require.NoError(t, err)
	assert.Empty(t, parent.Subtasks, "parent Subtasks must be empty for this test to be meaningful")

	return parent, children
}

// TestParentAutoTransition_QueryBased_OneOfThreeDoneStaysInProgress verifies that
// completing only 1 of 3 children (linked via parent field, not Subtasks list) does
// NOT transition the parent to review.
func TestParentAutoTransition_QueryBased_OneOfThreeDoneStaysInProgress(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, children := createParentWithChildrenByParentField(t, svc, "test-project", 3)
	require.Equal(t, "todo", parent.State)

	// Transition all children to in_progress (first one also transitions parent)
	inProgress := "in_progress"
	for _, child := range children {
		_, err := svc.PatchCard(ctx, "test-project", child.ID, PatchCardInput{State: &inProgress})
		require.NoError(t, err)
	}

	// Verify parent is in_progress
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	require.Equal(t, "in_progress", updatedParent.State)

	// Complete only the first child
	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", children[0].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Parent must still be in_progress — two children remain
	updatedParent, err = svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

// TestParentAutoTransition_QueryBased_AllThreeDoneParentStaysInProgress verifies that
// completing all 3 children (linked via parent field only) does NOT auto-transition
// the parent to review. The orchestrator handles that after documentation.
func TestParentAutoTransition_QueryBased_AllThreeDoneParentStaysInProgress(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, children := createParentWithChildrenByParentField(t, svc, "test-project", 3)

	// Transition all children to in_progress
	inProgress := "in_progress"
	for _, child := range children {
		_, err := svc.PatchCard(ctx, "test-project", child.ID, PatchCardInput{State: &inProgress})
		require.NoError(t, err)
	}

	// Complete all children
	done := "done"
	for _, child := range children {
		_, err := svc.PatchCard(ctx, "test-project", child.ID, PatchCardInput{State: &done})
		require.NoError(t, err)
	}

	// Parent stays in in_progress — orchestrator transitions after documentation
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

// TestParentAutoTransition_QueryBased_NoChildrenNoTransition verifies that a parent
// card with no children does not auto-transition when completed cards reference it.
func TestParentAutoTransition_QueryBased_NoChildrenNoTransition(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	// Create a parent card with no children
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent with no children",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	require.Equal(t, "todo", parent.State)

	// Manually transition parent to in_progress so it has a non-todo state to stay in
	inProgress := "in_progress"
	parent, err = svc.PatchCard(ctx, "test-project", parent.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	require.Equal(t, "in_progress", parent.State)

	// Create a standalone card (no parent) and mark it done — this should not affect the parent
	standalone, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Standalone",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.PatchCard(ctx, "test-project", standalone.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", standalone.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Parent must remain in_progress — it has no children so the empty-guard fires
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)
}

func TestCommitMessage(t *testing.T) {
	tests := []struct {
		agent    string
		cardID   string
		action   string
		expected string
	}{
		{"", "TEST-001", "created", "[contextmatrix] TEST-001: created"},
		{"agent-1", "TEST-001", "claimed", "[agent:agent-1] TEST-001: claimed"},
		{"human:alice", "TEST-002", "updated", "[agent:human:alice] TEST-002: updated"},
	}

	for _, tt := range tests {
		result := commitMessage(tt.agent, tt.cardID, tt.action)
		assert.Equal(t, tt.expected, result)
	}
}

func TestReportUsage(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Token test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	assert.Nil(t, card.TokenUsage)

	// Report usage
	updated, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenUsage)
	assert.Equal(t, int64(1000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(500), updated.TokenUsage.CompletionTokens)
	assert.InDelta(t, 0.0, updated.TokenUsage.EstimatedCostUSD, 0.0001) // no costs configured
}

func TestReportUsageAccumulates(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Accumulation test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Report three times
	for _, delta := range []ReportUsageInput{
		{AgentID: "a1", PromptTokens: 100, CompletionTokens: 50},
		{AgentID: "a1", PromptTokens: 200, CompletionTokens: 100},
		{AgentID: "a1", PromptTokens: 300, CompletionTokens: 150},
	} {
		_, err = svc.ReportUsage(ctx, "test-project", card.ID, delta)
		require.NoError(t, err)
	}

	// Verify accumulated totals
	result, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	require.NotNil(t, result.TokenUsage)
	assert.Equal(t, int64(600), result.TokenUsage.PromptTokens)
	assert.Equal(t, int64(300), result.TokenUsage.CompletionTokens)
}

func setupTestWithCosts(t *testing.T) (*CardService, string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	tokenCosts := map[string]ModelCost{
		"claude-sonnet-4-6": {Prompt: 0.000003, Completion: 0.000015},
		"claude-opus-4-6":   {Prompt: 0.000005, Completion: 0.000025},
	}

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, tokenCosts, true, false)

	return svc, tmpDir, func() {}
}

func TestReportUsageWithCost(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Cost test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Report with known model
	updated, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     10000,
		CompletionTokens: 2000,
	})
	require.NoError(t, err)
	// Expected: 10000 * 0.000003 + 2000 * 0.000015 = 0.03 + 0.03 = 0.06
	assert.InDelta(t, 0.06, updated.TokenUsage.EstimatedCostUSD, 0.0001)

	// Report again with different model — cost should accumulate as delta
	updated, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-opus-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)
	// Delta: 1000 * 0.000005 + 500 * 0.000025 = 0.005 + 0.0125 = 0.0175
	// Total: 0.06 + 0.0175 = 0.0775
	assert.InDelta(t, 0.0775, updated.TokenUsage.EstimatedCostUSD, 0.0001)
	assert.Equal(t, int64(11000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(2500), updated.TokenUsage.CompletionTokens)
}

func TestReportUsageEvent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Event test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Drain the CardCreated event
	<-ch

	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "test-model",
		PromptTokens:     500,
		CompletionTokens: 200,
	})
	require.NoError(t, err)

	select {
	case event := <-ch:
		assert.Equal(t, events.CardUsageReported, event.Type)
		assert.Equal(t, card.ID, event.CardID)
		assert.Equal(t, "agent-1", event.Agent)
		assert.Equal(t, int64(500), event.Data["prompt_tokens"])
		assert.Equal(t, int64(200), event.Data["completion_tokens"])
		assert.Equal(t, "test-model", event.Data["model"])
	case <-time.After(time.Second):
		t.Fatal("expected CardUsageReported event")
	}
}

// captureHandler is a slog.Handler that records log records for test assertions.
type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(name string) slog.Handler       { return h }

func TestReportUsageStoresModel(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Model storage test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	updated, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenUsage)
	assert.Equal(t, "claude-sonnet-4-6", updated.TokenUsage.Model)

	// Report again with a different model — model should be updated to latest
	updated, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-opus-4-6",
		PromptTokens:     500,
		CompletionTokens: 250,
	})
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-6", updated.TokenUsage.Model)

	// When no model is provided, the stored model should remain unchanged
	updated, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     100,
		CompletionTokens: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-6", updated.TokenUsage.Model)
}

func TestReportUsageWarnsUnknownModel(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Unknown model test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Install a capturing log handler for the duration of this test
	handler := &captureHandler{}
	original := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(original)

	// Report with an unknown model — should warn but not error
	updated, err := svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "unknown-model-xyz",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenUsage)

	// Token counts accumulate even when model is unknown
	assert.Equal(t, int64(1000), updated.TokenUsage.PromptTokens)
	assert.Equal(t, int64(500), updated.TokenUsage.CompletionTokens)

	// Cost remains zero because model is not in the cost map
	assert.InDelta(t, 0.0, updated.TokenUsage.EstimatedCostUSD, 0.0001)

	// A Warn log entry should have been emitted
	var warnFound bool
	for _, rec := range handler.records {
		if rec.Level == slog.LevelWarn {
			warnFound = true
			break
		}
	}
	assert.True(t, warnFound, "expected slog.Warn for unknown model")
}

func TestAggregateUsage(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	// Create 3 cards, report usage on 2
	card1, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Card 1", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Card 2", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Card 3 (no usage)", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card1.ID, ReportUsageInput{
		AgentID: "a1", Model: "claude-sonnet-4-6", PromptTokens: 1000, CompletionTokens: 500,
	})
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID: "a2", Model: "claude-sonnet-4-6", PromptTokens: 2000, CompletionTokens: 1000,
	})
	require.NoError(t, err)

	usage, err := svc.AggregateUsage(ctx, "test-project")
	require.NoError(t, err)

	assert.Equal(t, int64(3000), usage.PromptTokens)
	assert.Equal(t, int64(1500), usage.CompletionTokens)
	assert.Equal(t, 2, usage.CardCount)
	// Cost: (1000*0.000003 + 500*0.000015) + (2000*0.000003 + 1000*0.000015) = 0.0105 + 0.021 = 0.0315
	assert.InDelta(t, 0.0315, usage.EstimatedCostUSD, 0.0001)
}

func TestGetDashboard(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	// Create cards in different states.
	card1, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Todo card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	card2, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "In-progress card", Type: "task", Priority: "high",
	})
	require.NoError(t, err)

	card3, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Done card", Type: "bug", Priority: "low",
	})
	require.NoError(t, err)

	// Move card2 to in_progress and claim it.
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card2.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card2.ID, "agent-1")
	require.NoError(t, err)

	// Move card3 to in_progress then done.
	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", card3.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.PatchCard(ctx, "test-project", card3.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Report usage on card1 and card2.
	_, err = svc.ReportUsage(ctx, "test-project", card1.ID, ReportUsageInput{
		AgentID: "agent-1", Model: "claude-sonnet-4-6", PromptTokens: 1000, CompletionTokens: 500,
	})
	require.NoError(t, err)
	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID: "agent-1", Model: "claude-sonnet-4-6", PromptTokens: 2000, CompletionTokens: 1000,
	})
	require.NoError(t, err)

	dashboard, err := svc.GetDashboard(ctx, "test-project")
	require.NoError(t, err)

	// State counts.
	assert.Equal(t, 1, dashboard.StateCounts["todo"])
	assert.Equal(t, 1, dashboard.StateCounts["in_progress"])
	assert.Equal(t, 1, dashboard.StateCounts["done"])

	// Active agents: only card2 is in_progress with an agent.
	require.Len(t, dashboard.ActiveAgents, 1)
	assert.Equal(t, "agent-1", dashboard.ActiveAgents[0].AgentID)
	assert.Equal(t, card2.ID, dashboard.ActiveAgents[0].CardID)

	// Cards completed today: card3 was transitioned to done just now.
	assert.Equal(t, 1, dashboard.CardsCompletedToday)

	// Total cost: same as aggregate.
	// (1000*0.000003 + 500*0.000015) + (2000*0.000003 + 1000*0.000015) = 0.0315
	assert.InDelta(t, 0.0315, dashboard.TotalCostUSD, 0.0001)

	// Card costs: 2 cards have usage.
	assert.Len(t, dashboard.CardCosts, 2)

	// Agent costs: card1 has no assigned agent (grouped as "unassigned"), card2 has "agent-1".
	assert.Len(t, dashboard.AgentCosts, 2)
}

// setupEmptyTest creates a test environment with no projects.
func setupEmptyTest(t *testing.T) (*CardService, string) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)
	return svc, boardsDir
}

func validCreateProjectInput() CreateProjectInput {
	return CreateProjectInput{
		Name:       "my-project",
		Prefix:     "MYPRJ",
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

func TestCreateProject(t *testing.T) {
	svc, boardsDir := setupEmptyTest(t)
	ctx := context.Background()

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	input := validCreateProjectInput()
	input.Repo = "git@github.com:org/my-project.git"

	cfg, err := svc.CreateProject(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "my-project", cfg.Name)
	assert.Equal(t, "MYPRJ", cfg.Prefix)
	assert.Equal(t, 1, cfg.NextID)
	assert.Equal(t, "git@github.com:org/my-project.git", cfg.Repo)

	// Verify project is retrievable
	got, err := svc.GetProject(ctx, "my-project")
	require.NoError(t, err)
	assert.Equal(t, "my-project", got.Name)

	// Verify tasks directory was created
	_, err = os.Stat(filepath.Join(boardsDir, "my-project", "tasks"))
	assert.NoError(t, err)

	// Verify event
	select {
	case evt := <-ch:
		assert.Equal(t, events.ProjectCreated, evt.Type)
		assert.Equal(t, "my-project", evt.Project)
	case <-time.After(time.Second):
		t.Fatal("expected ProjectCreated event")
	}
}

func TestCreateProject_AlreadyExists(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	input := validCreateProjectInput()
	_, err := svc.CreateProject(ctx, input)
	require.NoError(t, err)

	_, err = svc.CreateProject(ctx, input)
	assert.ErrorIs(t, err, storage.ErrProjectExists)
}

func TestCreateProject_InvalidName(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	tests := []struct {
		name string
	}{
		{""},
		{"has spaces"},
		{"-starts-with-hyphen"},
		{"has/slash"},
		{"has.dot"},
	}

	for _, tt := range tests {
		input := validCreateProjectInput()
		input.Name = tt.name
		_, err := svc.CreateProject(ctx, input)
		assert.Error(t, err, "name %q should be rejected", tt.name)
	}
}

func TestCreateProject_MissingStalledState(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	input := validCreateProjectInput()
	input.States = []string{"todo", "done"} // missing stalled
	input.Transitions = map[string][]string{
		"todo": {"done"},
		"done": {"todo"},
	}

	_, err := svc.CreateProject(ctx, input)
	assert.Error(t, err)
}

func TestUpdateProject(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	input := UpdateProjectInput{
		Repo:       "git@github.com:org/test.git",
		States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature", "chore"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	cfg, err := svc.UpdateProject(ctx, "test-project", input)
	require.NoError(t, err)
	assert.Equal(t, "test-project", cfg.Name)
	assert.Equal(t, "TEST", cfg.Prefix) // Immutable
	assert.Contains(t, cfg.States, "review")
	assert.Contains(t, cfg.Types, "chore")
	assert.Equal(t, "git@github.com:org/test.git", cfg.Repo)

	// Verify event
	select {
	case evt := <-ch:
		assert.Equal(t, events.ProjectUpdated, evt.Type)
	case <-time.After(time.Second):
		t.Fatal("expected ProjectUpdated event")
	}
}

func TestUpdateProject_CannotRemoveInUseState(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create a card in "todo" state
	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Try to remove "todo" from states
	input := UpdateProjectInput{
		States:     []string{"in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"in_progress": {"done"},
			"done":        {"in_progress"},
			"stalled":     {"in_progress"},
			"not_planned": {"in_progress"},
		},
	}

	_, err = svc.UpdateProject(ctx, "test-project", input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove state")
}

func TestUpdateProject_CannotRemoveInUseType(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	input := UpdateProjectInput{
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"bug", "feature"}, // removed "task"
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	_, err = svc.UpdateProject(ctx, "test-project", input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove type")
}

func TestUpdateProject_SubtaskTypeDoesNotBlockUpdate(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create a parent card and a subtask
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Parent", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Subtask", Type: "task", Priority: "medium", Parent: parent.ID,
	})
	require.NoError(t, err)

	// Now update project settings - should NOT fail because of subtask type
	// The subtask type is built-in and auto-assigned, not user-configured
	input := UpdateProjectInput{
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"}, // subtask is NOT here (it's built-in)
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "done"},
			"in_progress": {"done", "todo"},
			"done":        {},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	_, err = svc.UpdateProject(ctx, "test-project", input)
	require.NoError(t, err, "Update should succeed - subtask is a built-in type that shouldn't block updates")
}

func TestUpdateProject_NotFound(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	_, err := svc.UpdateProject(ctx, "nonexistent", UpdateProjectInput{})
	assert.ErrorIs(t, err, storage.ErrProjectNotFound)
}

func TestUpdateProject_TodoToDone(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// Add todo → done transition so cards can go directly from todo to done
	input := UpdateProjectInput{
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress", "done", "not_planned"},
			"in_progress": {"done", "todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}

	cfg, err := svc.UpdateProject(ctx, "test-project", input)
	require.NoError(t, err)
	assert.Contains(t, cfg.Transitions["todo"], "done")

	// done has no transitions entry — it is a valid terminal state
	_, hasDoneTransitions := cfg.Transitions["done"]
	assert.False(t, hasDoneTransitions, "done should be a terminal state with no transitions entry")
}

func TestUpdateProject_FrontendNormalization(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// First, set up a project where "done" is a terminal state (no transitions entry)
	// This simulates the state after the user already configured done as terminal
	setupInput := UpdateProjectInput{
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			// done is intentionally omitted - it's a terminal state
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
	_, err := svc.UpdateProject(ctx, "test-project", setupInput)
	require.NoError(t, err, "Setup: creating project with terminal 'done' state")

	// Get the current project config (simulates frontend loading config)
	cfg, err := svc.GetProject(ctx, "test-project")
	require.NoError(t, err)

	// Verify done has no transitions entry (terminal state)
	_, hasDone := cfg.Transitions["done"]
	require.False(t, hasDone, "Setup: done should be terminal (no transitions entry)")

	// Simulate frontend normalization: add empty arrays for states without transitions
	normalizedTransitions := make(map[string][]string)
	for k, v := range cfg.Transitions {
		normalizedTransitions[k] = v
	}
	for _, s := range cfg.States {
		if _, ok := normalizedTransitions[s]; !ok {
			normalizedTransitions[s] = []string{}
		}
	}

	// Now done should be in the normalized map with empty array
	doneAfterNorm, hasDoneAfterNorm := normalizedTransitions["done"]
	require.True(t, hasDoneAfterNorm, "After normalization: done should be in map")
	require.Empty(t, doneAfterNorm, "After normalization: done should have empty transitions")

	// Add todo → done transition (what user wants to do)
	normalizedTransitions["todo"] = append(normalizedTransitions["todo"], "done")

	// This is exactly what the frontend sends - including done: [] from normalization
	input := UpdateProjectInput{
		States:      cfg.States,
		Types:       cfg.Types,
		Priorities:  cfg.Priorities,
		Transitions: normalizedTransitions,
	}

	updatedCfg, err := svc.UpdateProject(ctx, "test-project", input)
	require.NoError(t, err, "Frontend-style update with normalized transitions should succeed")
	assert.Contains(t, updatedCfg.Transitions["todo"], "done", "todo should now have done as a valid transition")

	// done should have an entry (empty array from normalization)
	doneTransitions, hasDoneInResult := updatedCfg.Transitions["done"]
	assert.True(t, hasDoneInResult, "done should be in transitions (with empty array from frontend normalization)")
	assert.Empty(t, doneTransitions, "done should have empty transitions (terminal state)")
}

func TestDeleteProject(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	// Create a project first
	input := validCreateProjectInput()
	_, err := svc.CreateProject(ctx, input)
	require.NoError(t, err)

	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	err = svc.DeleteProject(ctx, "my-project")
	require.NoError(t, err)

	// Verify gone
	_, err = svc.GetProject(ctx, "my-project")
	assert.ErrorIs(t, err, storage.ErrProjectNotFound)

	// Verify event
	select {
	case evt := <-ch:
		assert.Equal(t, events.ProjectDeleted, evt.Type)
	case <-time.After(time.Second):
		t.Fatal("expected ProjectDeleted event")
	}
}

func TestDeleteProject_HasCards(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// test-project already has setupTest, create a card
	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	err = svc.DeleteProject(ctx, "test-project")
	assert.ErrorIs(t, err, storage.ErrProjectHasCards)
}

func TestDeleteProject_NotFound(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	err := svc.DeleteProject(ctx, "nonexistent")
	assert.ErrorIs(t, err, storage.ErrProjectNotFound)
}

// TestGitAutoCommitDisabled verifies that when gitAutoCommit is false,
// card mutations write files to disk but do not create git commits.
func TestGitAutoCommitDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// Create service with gitAutoCommit disabled
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, false, false)
	ctx := context.Background()

	// Create a card — should write file but not commit
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "No-commit card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// File must exist on disk
	cardFile := filepath.Join(boardsDir, "test-project", "tasks", card.ID+".md")
	_, statErr := os.Stat(cardFile)
	assert.NoError(t, statErr, "card file should exist on disk")

	// Git repo must have zero commits.
	// GetLastCommitMessage returns ("", nil) when the repo has no commits.
	msg, headErr := gitMgr.GetLastCommitMessage()
	require.NoError(t, headErr)
	assert.Empty(t, msg, "no commit message expected when gitAutoCommit is false")
}

// setupDeferredTest creates a test environment with gitDeferredCommit enabled.
func setupDeferredTest(t *testing.T) (*CardService, *gitops.Manager) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// gitAutoCommit=true, gitDeferredCommit=true
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, true)
	return svc, gitMgr
}

// TestDeferredCommitAccumulates verifies that with deferred mode on,
// CreateCard commits immediately but subsequent mutations do not.
func TestDeferredCommitAccumulates(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — must commit immediately even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Deferred Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Creation commit must exist.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID, "creation commit must reference the card ID")

	// Remember the creation commit message before further mutations.
	creationMsg := msg

	// Update card twice — these should be deferred, no new commits.
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Updated Once", Type: "task", State: "todo", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Updated Twice", Type: "task", State: "todo", Priority: "medium",
	})
	require.NoError(t, err)

	// No new commits after updates — last commit is still the creation commit.
	msg, err = gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, creationMsg, msg, "no new commit expected after updates in deferred mode")

	// Deferred paths should be non-empty (updates accumulated).
	svc.writeMu.Lock()
	pathCount := len(svc.deferredPaths[card.ID])
	svc.writeMu.Unlock()
	assert.Greater(t, pathCount, 0, "deferredPaths should have entries after updates")
}

// TestDeferredCommitFlushOnDone verifies that transitioning to "done" does NOT
// flush deferred commits — the flush happens at ReleaseCard instead.
func TestDeferredCommitFlushOnDone(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Complete", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)

	// Claim and work on card.
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Update body (deferred).
	body := "## Progress\n- [x] Step 1"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body})
	require.NoError(t, err)

	// Transition todo → in_progress → done.
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// PatchCard(done) should NOT flush — last commit is still creation.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, creationMsg, msg, "PatchCard(done) should not flush in deferred mode")

	// Deferred paths should still be present.
	svc.writeMu.Lock()
	pathCount := len(svc.deferredPaths[card.ID])
	svc.writeMu.Unlock()
	assert.Greater(t, pathCount, 0, "deferredPaths should still have entries before release")

	// Release the card — this is where the flush should happen.
	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	msg, err = gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID)
	assert.Contains(t, msg, "completed (deferred commit)")

	// deferredPaths should be cleared after release.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after release flush")
}

// TestDeferredCommitFlushOnStalled verifies that when a card is marked stalled
// via the timeout checker, accumulated deferred commits are flushed.
func TestDeferredCommitFlushOnStalled(t *testing.T) {
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	// Use a very short timeout (1ms) so the card stalls immediately.
	lockMgr := lock.NewManager(store, 1*time.Millisecond)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, true)
	ctx := context.Background()

	// Create and claim a card.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Stall", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "stale-agent")
	require.NoError(t, err)

	// Update card body (deferred, no commit yet).
	body := "## Progress\n- [ ] Step 1"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body})
	require.NoError(t, err)

	// Wait past the 1ms timeout, then trigger processStalled.
	time.Sleep(10 * time.Millisecond)
	err = svc.processStalled(ctx)
	require.NoError(t, err)

	// Card should now be stalled.
	stalledCard, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "stalled", stalledCard.State)

	// A deferred flush commit should have been produced.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, msg, "expected a commit after stall flush")
	assert.Contains(t, msg, card.ID)

	// deferredPaths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after stall flush")
}

// TestDeferredCommitNoOpFlush verifies that flushing a card with no deferred paths is a no-op.
func TestDeferredCommitNoOpFlush(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create a card via non-deferred path (temporarily disable deferred).
	// We do this by directly calling flushDeferredCommit on a card ID that has no deferred paths.
	_ = ctx

	// Flush on card with no deferred paths — should not produce a commit.
	svc.writeMu.Lock()
	err := svc.flushDeferredCommit("NONEXISTENT-001", "test-agent")
	svc.writeMu.Unlock()
	require.NoError(t, err)

	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Empty(t, msg, "no commit expected for no-op flush")
}

// TestDeferredCommitNonDeferredUnchanged verifies that with gitDeferredCommit=false,
// every mutation commits immediately (existing behavior).
func TestDeferredCommitNonDeferredUnchanged(t *testing.T) {
	svc, _, cleanup := setupTest(t) // setupTest uses gitAutoCommit=true, gitDeferredCommit=false
	defer cleanup()
	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Immediate Commit", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Should have committed immediately.
	msg, err := svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID)
	assert.Contains(t, msg, "created")

	// Update and verify immediate commit.
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Updated", Type: "task", State: "todo", Priority: "medium",
	})
	require.NoError(t, err)

	msg, err = svc.git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID)
	assert.Contains(t, msg, "updated")

	// deferredPaths must remain empty.
	svc.writeMu.Lock()
	totalDeferred := len(svc.deferredPaths)
	svc.writeMu.Unlock()
	assert.Equal(t, 0, totalDeferred, "deferredPaths should be empty in non-deferred mode")
}

// TestDeferredCommitProjectOpsUnaffected verifies that project-level operations
// always commit immediately regardless of the deferred flag.
func TestDeferredCommitProjectOpsUnaffected(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create a new project (different from the test-project already in boardsDir).
	proj, err := svc.CreateProject(ctx, CreateProjectInput{
		Name:       "another-project",
		Prefix:     "ANOTH",
		States:     []string{"todo", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"medium"},
		Transitions: map[string][]string{
			"todo":        {"done"},
			"done":        {"todo"},
			"stalled":     {"todo"},
			"not_planned": {"todo"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "another-project", proj.Name)

	// Project create should have committed immediately.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, msg, "project create should commit immediately")
	assert.Contains(t, msg, "another-project")
}

func TestRecalculateCosts(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	// card1: has token usage but $0 cost, no model stored — should be recalculated with defaultModel
	card1, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Zero cost card 1", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ReportUsage(ctx, "test-project", card1.ID, ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     10000,
		CompletionTokens: 2000,
		// No Model — cost stays $0
	})
	require.NoError(t, err)

	// card2: has token usage and a stored model but $0 cost (model was set but rate lookup
	// failed at the time). Simulate by directly reporting usage without a model, then patching
	// the stored model name via a second report with the model set but zero tokens so the
	// cost formula yields $0. Actually: report with model+tokens and then clear cost via
	// another report with zero tokens. Simpler: just use ReportUsage without a model for
	// token counts and manually verify that card has the stored model by seeding via a
	// store update — but that bypasses the service. Instead, just test the case where
	// card.TokenUsage.Model is already set (non-empty) on a $0 card: report with model name
	// but also confirm RecalculateCosts uses it.
	// We'll report usage with a Model so TokenUsage.Model is set, but then create a fresh
	// card whose TokenUsage we manually set via a separate ReportUsage call. To get a card
	// whose model is stored but cost is $0, we call ReportUsage with a model that IS in
	// the cost map — but that would produce a non-zero cost. So instead, report without
	// a model (cost=$0, model="") and rely on defaultModel for recalculation.
	// card2 tests the path where card.TokenUsage.Model is already set:
	card2, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Zero cost card 2 (with stored model)", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	// Report once with opus model but no tokens to set model field only
	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID: "agent-1",
		Model:   "claude-opus-4-6",
		// 0 tokens → cost $0, but model gets stored
	})
	require.NoError(t, err)
	// Report again without model to accumulate tokens (cost still $0 because model not provided in this call)
	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     1000,
		CompletionTokens: 500,
		// No model: no cost delta
	})
	require.NoError(t, err)

	// card3: already has a non-zero cost — must NOT be touched
	card3, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Already costed card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ReportUsage(ctx, "test-project", card3.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     5000,
		CompletionTokens: 1000,
	})
	require.NoError(t, err)
	// Verify card3 has non-zero cost before recalculation
	c3, err := svc.GetCard(ctx, "test-project", card3.ID)
	require.NoError(t, err)
	require.NotNil(t, c3.TokenUsage)
	require.Greater(t, c3.TokenUsage.EstimatedCostUSD, 0.0)
	card3CostBefore := c3.TokenUsage.EstimatedCostUSD

	result, err := svc.RecalculateCosts(ctx, "test-project", "claude-sonnet-4-6")
	require.NoError(t, err)
	require.NotNil(t, result)

	// card1: 10000*0.000003 + 2000*0.000015 = 0.03 + 0.03 = 0.06
	// card2: opus model stored — 1000*0.000005 + 500*0.000025 = 0.005 + 0.0125 = 0.0175
	// Total: 0.06 + 0.0175 = 0.0775
	assert.Equal(t, 2, result.CardsUpdated)
	assert.InDelta(t, 0.0775, result.TotalCostRecalculated, 0.0001)

	// card1 should now have cost
	updated1, err := svc.GetCard(ctx, "test-project", card1.ID)
	require.NoError(t, err)
	assert.InDelta(t, 0.06, updated1.TokenUsage.EstimatedCostUSD, 0.0001)

	// card2 should now have cost (using its stored opus model)
	updated2, err := svc.GetCard(ctx, "test-project", card2.ID)
	require.NoError(t, err)
	assert.InDelta(t, 0.0175, updated2.TokenUsage.EstimatedCostUSD, 0.0001)

	// card3 must be unchanged
	updated3, err := svc.GetCard(ctx, "test-project", card3.ID)
	require.NoError(t, err)
	assert.InDelta(t, card3CostBefore, updated3.TokenUsage.EstimatedCostUSD, 0.0001)
}

func TestRecalculateCostsNoOp(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	// All cards have proper costs already.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Already costed", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)

	result, err := svc.RecalculateCosts(ctx, "test-project", "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, 0, result.CardsUpdated)
	assert.InDelta(t, 0.0, result.TotalCostRecalculated, 0.0001)
}

func TestRecalculateCostsSkipsUnknownModel(t *testing.T) {
	svc, _, cleanup := setupTestWithCosts(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Unknown model card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	// Report without a model so cost is $0
	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)

	// Use an unknown default model — card should be skipped
	result, err := svc.RecalculateCosts(ctx, "test-project", "unknown-model-xyz")
	require.NoError(t, err)
	assert.Equal(t, 0, result.CardsUpdated)

	// Card cost should still be $0
	updated, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, updated.TokenUsage.EstimatedCostUSD, 0.0001)
}

func TestCaseInsensitiveCardID(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card (ID will be uppercase, e.g. "TEST-001")
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Case test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	require.Equal(t, "TEST-001", card.ID)

	lowercaseID := "test-001"
	mixedCaseID := "Test-001"

	t.Run("GetCard", func(t *testing.T) {
		got, err := svc.GetCard(ctx, "test-project", lowercaseID)
		require.NoError(t, err)
		assert.Equal(t, "TEST-001", got.ID)

		got, err = svc.GetCard(ctx, "test-project", mixedCaseID)
		require.NoError(t, err)
		assert.Equal(t, "TEST-001", got.ID)
	})

	t.Run("UpdateCard", func(t *testing.T) {
		updated, err := svc.UpdateCard(ctx, "test-project", lowercaseID, UpdateCardInput{
			Title:    "Updated via lowercase",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.Equal(t, "Updated via lowercase", updated.Title)
	})

	t.Run("PatchCard", func(t *testing.T) {
		title := "Patched via lowercase"
		patched, err := svc.PatchCard(ctx, "test-project", lowercaseID, PatchCardInput{
			Title: &title,
		})
		require.NoError(t, err)
		assert.Equal(t, "Patched via lowercase", patched.Title)
	})

	t.Run("GetCardContext", func(t *testing.T) {
		cardCtx, err := svc.GetCardContext(ctx, "test-project", lowercaseID)
		require.NoError(t, err)
		assert.Equal(t, "TEST-001", cardCtx.Card.ID)
	})

	t.Run("AddLogEntry", func(t *testing.T) {
		err := svc.AddLogEntry(ctx, "test-project", lowercaseID, board.ActivityEntry{
			Agent:     "test-agent",
			Timestamp: time.Now(),
			Action:    "test",
			Message:   "lowercase ID log",
		})
		require.NoError(t, err)
	})

	t.Run("TransitionTo", func(t *testing.T) {
		transitioned, err := svc.TransitionTo(ctx, "test-project", lowercaseID, "in_progress")
		require.NoError(t, err)
		assert.Equal(t, "in_progress", transitioned.State)
	})

	t.Run("ClaimCard", func(t *testing.T) {
		// Transition back to todo first so we can claim
		_, err := svc.TransitionTo(ctx, "test-project", card.ID, "todo")
		require.NoError(t, err)

		claimed, err := svc.ClaimCard(ctx, "test-project", lowercaseID, "agent-1")
		require.NoError(t, err)
		assert.Equal(t, "agent-1", claimed.AssignedAgent)
	})

	t.Run("HeartbeatCard", func(t *testing.T) {
		err := svc.HeartbeatCard(ctx, "test-project", lowercaseID, "agent-1")
		require.NoError(t, err)
	})

	t.Run("ReleaseCard", func(t *testing.T) {
		released, err := svc.ReleaseCard(ctx, "test-project", lowercaseID, "agent-1")
		require.NoError(t, err)
		assert.Empty(t, released.AssignedAgent)
	})

	t.Run("ReportUsage", func(t *testing.T) {
		reported, err := svc.ReportUsage(ctx, "test-project", lowercaseID, ReportUsageInput{
			AgentID:          "agent-1",
			Model:            "claude-sonnet-4",
			PromptTokens:     100,
			CompletionTokens: 50,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(100), reported.TokenUsage.PromptTokens)
	})

	t.Run("ListCards_ParentFilter", func(t *testing.T) {
		// Create a child card
		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Child card",
			Type:     "task",
			Priority: "medium",
			Parent:   card.ID,
		})
		require.NoError(t, err)

		// Filter by lowercase parent ID
		cards, err := svc.ListCards(ctx, "test-project", storage.CardFilter{
			Parent: lowercaseID,
		})
		require.NoError(t, err)
		require.Len(t, cards, 1)
		assert.Equal(t, child.ID, cards[0].ID)
	})

	t.Run("DeleteCard", func(t *testing.T) {
		// Create another card to delete with lowercase ID
		toDelete, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Delete me",
			Type:     "task",
			Priority: "low",
		})
		require.NoError(t, err)

		err = svc.DeleteCard(ctx, "test-project", strings.ToLower(toDelete.ID))
		require.NoError(t, err)

		// Verify it's gone
		_, err = svc.GetCard(ctx, "test-project", toDelete.ID)
		assert.Error(t, err)
	})

	t.Run("CreateCard_LowercaseParent", func(t *testing.T) {
		child, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Child with lowercase parent",
			Type:     "task",
			Priority: "medium",
			Parent:   "test-001", // lowercase
		})
		require.NoError(t, err)
		assert.Equal(t, "TEST-001", child.Parent)
	})

	t.Run("UpdateCard_LowercaseRefs", func(t *testing.T) {
		// Create two more cards to use as subtask/dependency refs
		s1, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Sub1", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)
		s2, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title: "Sub2", Type: "task", Priority: "medium",
		})
		require.NoError(t, err)

		updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title:     "Updated refs",
			Type:      "task",
			State:     "todo",
			Priority:  "medium",
			Parent:    "",
			Subtasks:  []string{strings.ToLower(s1.ID), strings.ToLower(s2.ID)},
			DependsOn: []string{strings.ToLower(s1.ID)},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{s1.ID, s2.ID}, updated.Subtasks)
		assert.Equal(t, []string{s1.ID}, updated.DependsOn)
	})
}

// setupDeferredTestWithReview creates a deferred-commit test env with a project
// that has a review state (matching real board configs).
func setupDeferredTestWithReview(t *testing.T) (*CardService, *gitops.Manager) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectWithReview()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, true)
	return svc, gitMgr
}

// TestDeferredCommitFlushOnRelease verifies that releasing a card flushes
// any remaining deferred commits (e.g. the release change itself).
func TestDeferredCommitFlushOnRelease(t *testing.T) {
	svc, gitMgr := setupDeferredTestWithReview(t)
	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Release flush", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// CreateCard commits immediately; record that commit message.
	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, creationMsg, card.ID, "creation commit must reference the card ID")

	// Claim the card
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Transition to in_progress
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Post-creation mutations (claim, in_progress) should be deferred — no new commits.
	msg, _ := gitMgr.GetLastCommitMessage()
	assert.Equal(t, creationMsg, msg, "no new commits should exist while card is being worked on (post-creation mutations deferred)")

	// Release the card — should flush all deferred changes
	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	msg, err = gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, msg, "release should flush deferred commits")
	assert.Contains(t, msg, card.ID)

	// deferredPaths should be cleared
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after release flush")
}

// TestDeferredCommitParentManualReviewTransition verifies that when the parent
// card is manually transitioned to review (by the orchestrator after
// documentation), deferred commits are flushed.
func TestDeferredCommitParentManualReviewTransition(t *testing.T) {
	svc, gitMgr := setupDeferredTestWithReview(t)
	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)

	// Complete both subtasks: in_progress → review → done
	for _, sub := range subtasks {
		_, err := svc.ClaimCard(ctx, "test-project", sub.ID, "agent-1")
		require.NoError(t, err)

		inProgress := "in_progress"
		_, err = svc.PatchCard(ctx, "test-project", sub.ID, PatchCardInput{State: &inProgress})
		require.NoError(t, err)

		review := "review"
		_, err = svc.PatchCard(ctx, "test-project", sub.ID, PatchCardInput{State: &review})
		require.NoError(t, err)

		done := "done"
		_, err = svc.PatchCard(ctx, "test-project", sub.ID, PatchCardInput{State: &done})
		require.NoError(t, err)

		_, err = svc.ReleaseCard(ctx, "test-project", sub.ID, "agent-1")
		require.NoError(t, err)
	}

	// Parent stays in in_progress (no auto-transition to review)
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updatedParent.State)

	// Manually transition parent to review (as the orchestrator would after documentation)
	reviewState := "review"
	_, err = svc.PatchCard(ctx, "test-project", parent.ID, PatchCardInput{State: &reviewState})
	require.NoError(t, err)

	// Parent's deferred paths should be cleared (flushed on review transition)
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[parent.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "parent deferredPaths should be cleared after review transition flush")

	// Verify there are no uncommitted changes in the boards repo.
	hasUncommitted, err := gitMgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasUncommitted, "all changes should be committed after review transition")
}

// TestDeferredCommitBoardYamlIncluded verifies that .board.yaml changes (next_id
// increment) are included in deferred commits when cards are created.
func TestDeferredCommitBoardYamlIncluded(t *testing.T) {
	svc, gitMgr := setupDeferredTestWithReview(t)
	ctx := context.Background()

	// Create a card (increments next_id in .board.yaml)
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Board yaml test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Claim card, transition to done, then release to trigger flush.
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// After release flush, .board.yaml should also be committed (no uncommitted changes)
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID)

	hasUncommitted, err := gitMgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasUncommitted, ".board.yaml should be committed along with the card")
}

// TestCreateCard_CommitsImmediatelyWithDeferredMode verifies that CreateCard
// always produces an immediate commit even when gitDeferredCommit is true,
// and that subsequent mutations (log entries) still defer normally.
func TestCreateCard_CommitsImmediatelyWithDeferredMode(t *testing.T) {
	svc, gitMgr := setupDeferredTestWithReview(t)
	ctx := context.Background()

	// Create a card — must commit immediately even though deferredCommit=true
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Immediate commit test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// No uncommitted changes: both card file and .board.yaml must be committed.
	hasUncommitted, err := gitMgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasUncommitted, "CreateCard must commit immediately even with gitDeferredCommit=true")

	// The commit message must reference the new card ID.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, card.ID, "immediate creation commit should include the card ID")

	// Subsequent mutation (add log entry) must still defer.
	err = svc.AddLogEntry(ctx, "test-project", card.ID, board.ActivityEntry{
		Agent:  "test-agent",
		Action: "tested",
	})
	require.NoError(t, err)

	// After the log entry the working tree should have uncommitted changes
	// because deferred mode is on for post-creation mutations.
	hasUncommitted, err = gitMgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasUncommitted, "post-creation mutations must still defer when gitDeferredCommit=true")
}

// TestCreateCard_SubtaskTypeEnforcement verifies that cards created with a parent
// always get type "subtask" regardless of what the caller passes.
func TestCreateCard_SubtaskTypeEnforcement(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a parent card first
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	assert.Equal(t, "task", parent.Type)

	tests := []struct {
		name         string
		inputType    string
		parent       string
		expectedType string
	}{
		{
			name:         "card with parent and type task gets subtask",
			inputType:    "task",
			parent:       parent.ID,
			expectedType: "subtask",
		},
		{
			name:         "card with parent and type bug gets subtask",
			inputType:    "bug",
			parent:       parent.ID,
			expectedType: "subtask",
		},
		{
			name:         "card with parent and type feature gets subtask",
			inputType:    "feature",
			parent:       parent.ID,
			expectedType: "subtask",
		},
		{
			name:         "card with parent and type subtask stays subtask",
			inputType:    "subtask",
			parent:       parent.ID,
			expectedType: "subtask",
		},
		{
			name:         "card without parent preserves task type",
			inputType:    "task",
			parent:       "",
			expectedType: "task",
		},
		{
			name:         "card without parent preserves bug type",
			inputType:    "bug",
			parent:       "",
			expectedType: "bug",
		},
		{
			name:         "card without parent preserves feature type",
			inputType:    "feature",
			parent:       "",
			expectedType: "feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
				Title:    "Test Card",
				Type:     tt.inputType,
				Priority: "medium",
				Parent:   tt.parent,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, card.Type)
		})
	}
}

// TestUpdateCard_SubtaskTypeEnforcement verifies that UpdateCard enforces subtask
// type invariants: subtasks cannot change type, and non-subtasks cannot become subtasks.
func TestUpdateCard_SubtaskTypeEnforcement(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a parent card
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Create a subtask (parent set → type auto-set to subtask)
	subtask, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Subtask Card",
		Type:     "task", // will be overridden to subtask
		Priority: "medium",
		Parent:   parent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "subtask", subtask.Type)

	// Create a standalone card
	standalone, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Standalone Card",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	require.Equal(t, "task", standalone.Type)

	t.Run("reject changing subtask type away from subtask", func(t *testing.T) {
		_, err := svc.UpdateCard(ctx, "test-project", subtask.ID, UpdateCardInput{
			Title:    subtask.Title,
			Type:     "task", // trying to change away from subtask
			State:    subtask.State,
			Priority: subtask.Priority,
			Parent:   parent.ID,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subtask")
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})

	t.Run("reject changing subtask type to bug", func(t *testing.T) {
		_, err := svc.UpdateCard(ctx, "test-project", subtask.ID, UpdateCardInput{
			Title:    subtask.Title,
			Type:     "bug",
			State:    subtask.State,
			Priority: subtask.Priority,
			Parent:   parent.ID,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})

	t.Run("allow keeping subtask type on subtask", func(t *testing.T) {
		updated, err := svc.UpdateCard(ctx, "test-project", subtask.ID, UpdateCardInput{
			Title:    "Updated Subtask Title",
			Type:     "subtask",
			State:    subtask.State,
			Priority: subtask.Priority,
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, "subtask", updated.Type)
		assert.Equal(t, "Updated Subtask Title", updated.Title)
	})

	t.Run("reject setting type to subtask on card without parent", func(t *testing.T) {
		_, err := svc.UpdateCard(ctx, "test-project", standalone.ID, UpdateCardInput{
			Title:    standalone.Title,
			Type:     "subtask", // invalid — no parent
			State:    standalone.State,
			Priority: standalone.Priority,
			Parent:   "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subtask")
		assert.ErrorIs(t, err, board.ErrInvalidType)
	})

	t.Run("allow normal type update on card without parent", func(t *testing.T) {
		updated, err := svc.UpdateCard(ctx, "test-project", standalone.ID, UpdateCardInput{
			Title:    standalone.Title,
			Type:     "bug",
			State:    standalone.State,
			Priority: standalone.Priority,
			Parent:   "",
		})
		require.NoError(t, err)
		assert.Equal(t, "bug", updated.Type)
	})

	t.Run("auto-set type to subtask when card gains a parent", func(t *testing.T) {
		// Create a fresh standalone card with type "task"
		fresh, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Fresh Standalone",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		require.Equal(t, "task", fresh.Type)
		require.Equal(t, "", fresh.Parent)

		// UpdateCard sets parent — type must be auto-forced to "subtask"
		updated, err := svc.UpdateCard(ctx, "test-project", fresh.ID, UpdateCardInput{
			Title:    fresh.Title,
			Type:     "task", // caller passes "task" — should be overridden
			State:    fresh.State,
			Priority: fresh.Priority,
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, "subtask", updated.Type)
		assert.Equal(t, parent.ID, updated.Parent)
	})

	t.Run("auto-reset type from subtask when card loses its parent", func(t *testing.T) {
		// Create a subtask via CreateCard so it has type "subtask"
		orphaned, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Soon-to-be-orphaned",
			Type:     "task", // overridden to "subtask"
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		require.Equal(t, "subtask", orphaned.Type)

		// UpdateCard clears parent and still passes type="subtask" — should be auto-reset
		updated, err := svc.UpdateCard(ctx, "test-project", orphaned.ID, UpdateCardInput{
			Title:    orphaned.Title,
			Type:     "subtask", // caller still passes subtask — should be reset to first project type
			State:    orphaned.State,
			Priority: orphaned.Priority,
			Parent:   "", // clearing parent
		})
		require.NoError(t, err)
		assert.Equal(t, "task", updated.Type, "type should be reset to first project type when parent is cleared")
		assert.Equal(t, "", updated.Parent)
	})
}

// TestCreateCard_RejectsSubtaskAsParent verifies that a subtask cannot be used
// as a parent for another card (no subtask-of-subtask chains).
func TestCreateCard_RejectsSubtaskAsParent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a task and a subtask under it.
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent Task",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Subtask",
		Type:     "task",
		Priority: "medium",
		Parent:   parent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "subtask", subtask.Type)

	// Attempting to create a child of the subtask should fail.
	_, err = svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Nested Subtask",
		Type:     "task",
		Priority: "medium",
		Parent:   subtask.ID,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot have children")
}

// TestPatchCard_DoesNotChangeType verifies that PatchCard never modifies the type field.
func TestPatchCard_DoesNotChangeType(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a parent and subtask
	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Parent",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Subtask",
		Type:     "task", // overridden to subtask
		Priority: "medium",
		Parent:   parent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "subtask", subtask.Type)

	// Patch title only — type must remain subtask
	newTitle := "Patched Subtask"
	patched, err := svc.PatchCard(ctx, "test-project", subtask.ID, PatchCardInput{
		Title: &newTitle,
	})
	require.NoError(t, err)
	assert.Equal(t, "subtask", patched.Type, "PatchCard must not change the type field")
	assert.Equal(t, "Patched Subtask", patched.Title)
}

// TestImmediateCommitPatchCard_WhenDeferredOn verifies that PatchCard with
// ImmediateCommit=true commits immediately even when gitDeferredCommit=true.
// It checks that a commit is produced (last commit message references the card ID),
// contrasting with the deferred mode where no commit exists yet.
func TestImmediateCommitPatchCard_WhenDeferredOn(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — commits immediately even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Human Editable Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Record the creation commit message.
	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, creationMsg, card.ID, "creation commit must reference the card ID")

	// Patch with ImmediateCommit=true — should produce a new commit immediately.
	newTitle := "Human Updated Title"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title:           &newTitle,
		ImmediateCommit: true,
	})
	require.NoError(t, err)

	// A new commit should have been produced for this patch.
	msgAfter, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEqual(t, creationMsg, msgAfter, "PatchCard with ImmediateCommit=true should produce a new commit")
	assert.Contains(t, msgAfter, card.ID, "commit message should reference the card ID")
	assert.Contains(t, msgAfter, "updated", "commit message should say updated")
}

// TestDeferredCommitPatchCard_WhenImmediateCommitFalse verifies that PatchCard
// with ImmediateCommit=false still defers when gitDeferredCommit=true.
func TestDeferredCommitPatchCard_WhenImmediateCommitFalse(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — commits immediately even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Agent Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Record the creation commit.
	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, creationMsg, card.ID)

	// Patch with ImmediateCommit=false (default) — should defer.
	newTitle := "Agent Updated Title"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title:           &newTitle,
		ImmediateCommit: false,
	})
	require.NoError(t, err)

	// No new commit should have been produced — last commit is still creation.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, creationMsg, msg, "PatchCard with ImmediateCommit=false should not commit in deferred mode")

	// deferredPaths should have accumulated entries.
	svc.writeMu.Lock()
	pathCount := len(svc.deferredPaths[card.ID])
	svc.writeMu.Unlock()
	assert.Greater(t, pathCount, 0, "deferredPaths should have entries when ImmediateCommit=false")
}

// TestImmediateCommitUpdateCard_WhenDeferredOn verifies that UpdateCard with
// ImmediateCommit=true commits immediately even when gitDeferredCommit=true.
func TestImmediateCommitUpdateCard_WhenDeferredOn(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — commits immediately even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Human Full Update Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Record the creation commit.
	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, creationMsg, card.ID, "creation commit must reference the card ID")

	// UpdateCard with ImmediateCommit=true — should produce a new commit immediately.
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title:           "Human Updated Full Title",
		Type:            "task",
		State:           "todo",
		Priority:        "high",
		ImmediateCommit: true,
	})
	require.NoError(t, err)

	// A new commit should have been produced for this update.
	msgAfter, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEqual(t, creationMsg, msgAfter, "UpdateCard with ImmediateCommit=true should produce a new commit")
	assert.Contains(t, msgAfter, card.ID, "commit message should reference the card ID")
	assert.Contains(t, msgAfter, "updated", "commit message should say updated")
}

// TestDeferredCommitFlushOnNotPlanned verifies that transitioning a card to
// "not_planned" flushes any accumulated deferred commits, mirroring the
// behaviour for "done" and "stalled".
func TestDeferredCommitFlushOnNotPlanned(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — immediate commit even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Be Not-Planned", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)

	// Accumulate a deferred mutation (body update, no commit yet).
	body := "## Notes\nDecided not to pursue this."
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body})
	require.NoError(t, err)

	// No new commit should have been produced yet (deferred mode).
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, creationMsg, msg, "no new commit expected before not_planned transition")

	// Transition todo → not_planned (direct transition is allowed for all states).
	notPlanned := "not_planned"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &notPlanned})
	require.NoError(t, err)

	// A deferred flush commit should now exist.
	msg, err = gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, msg, "expected a commit after transitioning to not_planned")
	assert.Contains(t, msg, card.ID)
	assert.Contains(t, msg, "completed (deferred commit)")

	// deferredPaths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after not_planned flush")
}

// TestNotPlannedReleasesAgent verifies that transitioning a claimed card to
// "not_planned" clears the assigned agent and last_heartbeat so the lock
// manager won't flag the card as stalled.
func TestNotPlannedReleasesAgent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	// Create and claim a card.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Agent Will Be Dropped", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-xyz")
	require.NoError(t, err)
	assert.Equal(t, "agent-xyz", claimed.AssignedAgent)
	assert.NotNil(t, claimed.LastHeartbeat)

	// Transition to not_planned via PatchCard (simulates human dragging the card).
	notPlanned := "not_planned"
	updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &notPlanned})
	require.NoError(t, err)
	assert.Equal(t, "not_planned", updated.State)
	assert.Empty(t, updated.AssignedAgent, "AssignedAgent must be cleared on not_planned transition")
	assert.Nil(t, updated.LastHeartbeat, "LastHeartbeat must be cleared on not_planned transition")

	// Reload from store to confirm persistence.
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "not_planned", reloaded.State)
	assert.Empty(t, reloaded.AssignedAgent)
	assert.Nil(t, reloaded.LastHeartbeat)
}

// TestNotPlannedDoesNotAppearInStalledDetection verifies that a card in the
// "not_planned" state is not picked up by processStalled even if it has an
// old heartbeat — agent clearing on transition prevents this.
func TestNotPlannedDoesNotAppearInStalledDetection(t *testing.T) {
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	// Very short timeout so any claimed card would normally be stalled.
	lockMgr := lock.NewManager(store, 1*time.Millisecond)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)
	ctx := context.Background()

	// Create, claim, then immediately move to not_planned.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Not-Planned Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "some-agent")
	require.NoError(t, err)

	notPlanned := "not_planned"
	updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &notPlanned})
	require.NoError(t, err)
	require.Equal(t, "not_planned", updated.State)
	require.Empty(t, updated.AssignedAgent, "agent must be cleared before stall check")

	// Wait well past the stall timeout.
	time.Sleep(20 * time.Millisecond)

	// Run processStalled — the not_planned card should NOT be returned.
	err = svc.processStalled(ctx)
	require.NoError(t, err)

	// Card must still be not_planned (not auto-transitioned to stalled).
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, "not_planned", reloaded.State, "not_planned card must not be stalled by timeout checker")
	assert.Empty(t, reloaded.AssignedAgent, "agent must remain cleared")
}

func TestGenerateBranchName(t *testing.T) {
	tests := []struct {
		name     string
		cardID   string
		title    string
		expected string
	}{
		{"simple", "TEST-001", "Fix login bug", "test-001/fix-login-bug"},
		{"special chars", "ALPHA-042", "Add user auth & validation!", "alpha-042/add-user-auth-validation"},
		{"long title", "X-001", strings.Repeat("word ", 20), "x-001/word-word-word-word-word-word-word-word-word-word"},
		{"empty title", "TEST-001", "", "test-001"},
		{"unicode", "TEST-001", "Fix über bug", "test-001/fix-ber-bug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateBranchName(tt.cardID, tt.title)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateCard_AutonomousFields(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("creates card with autonomous fields", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:         "Auto task",
			Type:          "task",
			Priority:      "high",
			Autonomous:    true,
			FeatureBranch: true,
			CreatePR:      true,
		})
		require.NoError(t, err)
		assert.True(t, card.Autonomous)
		assert.True(t, card.FeatureBranch)
		assert.True(t, card.CreatePR)
		assert.NotEmpty(t, card.BranchName)
		assert.Contains(t, card.BranchName, strings.ToLower(card.ID))
		assert.Contains(t, card.BranchName, "auto-task")
	})

	t.Run("no branch name when feature_branch is false", func(t *testing.T) {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:      "Manual task",
			Type:       "task",
			Priority:   "medium",
			Autonomous: true,
		})
		require.NoError(t, err)
		assert.True(t, card.Autonomous)
		assert.False(t, card.FeatureBranch)
		assert.Empty(t, card.BranchName)
	})

	t.Run("create_pr without feature_branch rejected", func(t *testing.T) {
		_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Bad config",
			Type:     "task",
			Priority: "medium",
			CreatePR: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create_pr requires feature_branch")
	})
}

func TestPatchCard_AutonomousFields(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a plain card
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Plain task", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	assert.False(t, card.Autonomous)

	t.Run("enable autonomous via patch", func(t *testing.T) {
		autonomous := true
		patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			Autonomous: &autonomous,
		})
		require.NoError(t, err)
		assert.True(t, patched.Autonomous)
	})

	t.Run("enable feature_branch generates branch name", func(t *testing.T) {
		fb := true
		patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			FeatureBranch: &fb,
		})
		require.NoError(t, err)
		assert.True(t, patched.FeatureBranch)
		assert.NotEmpty(t, patched.BranchName)
		assert.Contains(t, patched.BranchName, "plain-task")

		// Branch name is immutable — toggling off and on keeps the same name
		savedName := patched.BranchName
		off := false
		patched, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			FeatureBranch: &off,
		})
		require.NoError(t, err)
		assert.False(t, patched.FeatureBranch)
		assert.Equal(t, savedName, patched.BranchName) // still there

		on := true
		patched, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
			FeatureBranch: &on,
		})
		require.NoError(t, err)
		assert.Equal(t, savedName, patched.BranchName) // unchanged
	})
}

func TestPatchCard_DisableFeatureBranch_ClearsCreatePR(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card with all autonomous fields enabled.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:         "Auto task",
		Type:          "task",
		Priority:      "medium",
		Autonomous:    true,
		FeatureBranch: true,
		CreatePR:      true,
	})
	require.NoError(t, err)
	assert.True(t, card.CreatePR)

	// Disable feature_branch via patch — create_pr should be auto-cleared.
	off := false
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		FeatureBranch: &off,
	})
	require.NoError(t, err)
	assert.False(t, patched.FeatureBranch)
	assert.False(t, patched.CreatePR, "create_pr should be auto-cleared when feature_branch is disabled")
}

func TestRecordPush_BranchProtection(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Push test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	t.Run("main rejected at service layer", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "main", "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProtectedBranch)
	})

	t.Run("master rejected at service layer", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "master", "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProtectedBranch)
	})

	t.Run("refs/heads/main rejected", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "refs/heads/main", "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProtectedBranch)
	})

	t.Run("feature branch allowed", func(t *testing.T) {
		pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", card.ID+"/fix-login", "")
		require.NoError(t, err)
		assert.NotNil(t, pushed)
	})
}

func TestIncrementReviewAttempts(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Review card", Type: "task", Priority: "medium",
		Autonomous: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, card.ReviewAttempts)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	updated, err := svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, 1, updated.ReviewAttempts)

	updated, err = svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, 2, updated.ReviewAttempts)
}

func TestPatchCard_CreatePRWithoutFeatureBranch_Rejected(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card with feature_branch + create_pr enabled.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:         "Auto task",
		Type:          "task",
		Priority:      "medium",
		FeatureBranch: true,
		CreatePR:      true,
	})
	require.NoError(t, err)
	assert.True(t, card.CreatePR)

	// Patch: disable feature_branch but try to keep create_pr — must be rejected.
	off := false
	on := true
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		FeatureBranch: &off,
		CreatePR:      &on,
	})
	require.NoError(t, err, "create_pr should be silently ignored when feature_branch is disabled")

	// Verify persisted state is consistent.
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.False(t, reloaded.FeatureBranch)
	assert.False(t, reloaded.CreatePR, "create_pr must not be true when feature_branch is false")
}

func TestRecordPush_InvalidPRUrl(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "URL test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	t.Run("javascript URL rejected", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/fix", "javascript:alert(1)")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPRUrl)
	})

	t.Run("data URL rejected", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/fix", "data:text/html,<h1>hi</h1>")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPRUrl)
	})

	t.Run("https URL accepted", func(t *testing.T) {
		pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/fix", "https://github.com/org/repo/pull/1")
		require.NoError(t, err)
		assert.Equal(t, "https://github.com/org/repo/pull/1", pushed.PRUrl)
	})

	t.Run("http URL accepted", func(t *testing.T) {
		pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/fix", "http://gitlab.local/pr/2")
		require.NoError(t, err)
		assert.Equal(t, "http://gitlab.local/pr/2", pushed.PRUrl)
	})

	t.Run("empty PR URL accepted (no validation needed)", func(t *testing.T) {
		pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/fix", "")
		require.NoError(t, err)
		assert.NotNil(t, pushed)
	})
}

func TestRecordPush_AgentOwnership(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Ownership test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-owner")
	require.NoError(t, err)

	t.Run("wrong agent rejected", func(t *testing.T) {
		_, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-intruder", "feat/fix", "")
		require.Error(t, err)
		assert.ErrorIs(t, err, lock.ErrAgentMismatch)
	})

	t.Run("correct agent allowed", func(t *testing.T) {
		pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-owner", "feat/fix", "")
		require.NoError(t, err)
		assert.NotNil(t, pushed)
	})
}

func TestIncrementReviewAttempts_AgentOwnership(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Review ownership", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-owner")
	require.NoError(t, err)

	t.Run("wrong agent rejected", func(t *testing.T) {
		_, err := svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-intruder")
		require.Error(t, err)
		assert.ErrorIs(t, err, lock.ErrAgentMismatch)
	})

	t.Run("correct agent allowed", func(t *testing.T) {
		updated, err := svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-owner")
		require.NoError(t, err)
		assert.Equal(t, 1, updated.ReviewAttempts)
	})
}

func TestIncrementReviewAttempts_Capped(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Cap test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Increment to the cap (5)
	for i := 0; i < 5; i++ {
		_, err := svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err, "increment %d should succeed", i+1)
	}

	// Next increment should be rejected
	_, err = svc.IncrementReviewAttempts(ctx, "test-project", card.ID, "agent-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReviewAttemptsCapped)

	// Verify counter stayed at 5
	reloaded, err := svc.GetCard(ctx, "test-project", card.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, reloaded.ReviewAttempts)
}

func TestRecordPush_Atomic(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Atomic push", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	// Record a push with PR URL — both PR URL and log entry should be set atomically
	pushed, err := svc.RecordPush(ctx, "test-project", card.ID, "agent-1", "feat/login", "https://github.com/org/repo/pull/42")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/org/repo/pull/42", pushed.PRUrl)

	// Verify the activity log was also written
	hasEntry := false
	for _, entry := range pushed.ActivityLog {
		if entry.Action == "pushed" {
			hasEntry = true
			assert.Contains(t, entry.Message, "feat/login")
			assert.Contains(t, entry.Message, "https://github.com/org/repo/pull/42")
		}
	}
	assert.True(t, hasEntry, "expected a 'pushed' activity log entry")
}

func TestUpdateRunnerStatus_Completed(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a card and claim it to simulate an active runner session.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "runner test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "runner:test-agent")
	require.NoError(t, err)

	// Transition to in_progress (claim does this), then set runner_status to running.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)

	t.Run("completed clears claim and runner_status", func(t *testing.T) {
		updated, err := svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "completed", "container exited normally")
		require.NoError(t, err)

		assert.Empty(t, updated.AssignedAgent, "claim should be cleared on completed")
		assert.Nil(t, updated.LastHeartbeat, "heartbeat should be cleared on completed")
		assert.Empty(t, updated.RunnerStatus, "runner_status should be cleared on completed")
	})
}

func TestUpdateRunnerStatus_Failed(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "runner fail test",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "runner:test-agent")
	require.NoError(t, err)

	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)

	t.Run("failed clears claim but keeps runner_status", func(t *testing.T) {
		updated, err := svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "failed", "container crashed")
		require.NoError(t, err)

		assert.Empty(t, updated.AssignedAgent, "claim should be cleared on failed")
		assert.Nil(t, updated.LastHeartbeat, "heartbeat should be cleared on failed")
		assert.Equal(t, "failed", updated.RunnerStatus, "runner_status should remain failed")
	})
}

// TestDeferredCommitFlushOnUpdateRunnerStatus verifies that when a card has
// deferred commits enabled, calling UpdateRunnerStatus after ReleaseCard
// results in the runner_status log entry being committed to git.
//
// This reproduces the bug where the "container exited normally" activity log
// entry was written to disk but never committed because UpdateRunnerStatus
// called commitCardChange (which defers the path) without ever flushing.
func TestDeferredCommitFlushOnUpdateRunnerStatus(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create and claim a card — simulates an autonomous runner session.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Runner deferred flush test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "runner:agent-1")
	require.NoError(t, err)

	// Accumulate a deferred mutation (body update, no commit yet).
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: card.Title, Type: card.Type, State: card.State, Priority: card.Priority,
		Body: "## Progress\n\n- [x] Step 1: done\n",
	})
	require.NoError(t, err)

	// Record commit message before release — should still be the creation commit.
	creationMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, creationMsg, card.ID, "pre-release commit must be the creation commit")

	// ReleaseCard flushes all deferred commits accumulated so far.
	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "runner:agent-1")
	require.NoError(t, err)

	releaseMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, releaseMsg, card.ID, "release should flush deferred commits")
	assert.Contains(t, releaseMsg, "deferred commit", "release flush commit should say 'deferred commit'")

	// Now simulate the runner calling UpdateRunnerStatus after the agent released.
	// This is the scenario from the bug: the runner sends "container exited normally"
	// after complete_task has already released the card.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "completed", "container exited normally")
	require.NoError(t, err)

	// The runner_status update must be committed — not left as an uncommitted deferred path.
	afterRunnerMsg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEqual(t, releaseMsg, afterRunnerMsg,
		"UpdateRunnerStatus must produce a new commit for the runner_status log entry")
	assert.Contains(t, afterRunnerMsg, card.ID,
		"runner_status commit must reference the card ID")

	// deferredPaths must be cleared after the flush.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after UpdateRunnerStatus flush")
}

func TestDeferredCommitPathsPreservedOnFailure(t *testing.T) {
	// Verifies that deferredPaths are NOT deleted when the commit fails,
	// so a subsequent flush can retry.
	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "")
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// gitAutoCommit=true, gitDeferredCommit=true
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, true)
	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Paths preserved test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Manually inject a deferred path pointing to a non-existent file.
	// This will cause the shell git commit to fail (can't stage missing file).
	svc.writeMu.Lock()
	svc.deferredPaths[card.ID] = []string{"test-project/tasks/DOES-NOT-EXIST.md"}
	svc.writeMu.Unlock()

	// The flush should return an error.
	svc.writeMu.Lock()
	err = svc.flushDeferredCommit(card.ID, "test-agent")
	svc.writeMu.Unlock()
	assert.Error(t, err, "flush should fail when staging a non-existent file")

	// The paths should still be in the map — not deleted.
	svc.writeMu.Lock()
	paths, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.True(t, hasPaths, "deferredPaths should be preserved after failed flush")
	assert.Len(t, paths, 1, "the failed path should still be in the deferred list")
}

// TestDeferredCommitFullWorkflowCommitCount simulates a complete agent workflow
// (claim → heartbeat → log → transition → release) and verifies that only the
// expected number of commits are produced in deferred mode.
func TestDeferredCommitFullWorkflowCommitCount(t *testing.T) {
	svc, gitMgr := setupDeferredTestWithReview(t)
	ctx := context.Background()

	// Create card — commits immediately even in deferred mode.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Workflow commit count", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	countAfterCreate, err := gitMgr.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, 1, countAfterCreate, "creation should produce exactly 1 commit")

	// --- Agent claims card ---
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	count, _ := gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count, "claim should not produce a commit in deferred mode")

	// --- Auto-transition to in_progress ---
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count, "transition to in_progress should not produce a commit")

	// --- Heartbeat ---
	err = svc.HeartbeatCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count, "heartbeat should not produce a commit")

	// --- Add log entry ---
	err = svc.AddLogEntry(ctx, "test-project", card.ID, board.ActivityEntry{
		Agent: "agent-1", Action: "progress", Message: "step 1 done",
	})
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count, "add_log should not produce a commit")

	// --- Update body ---
	body := "## Progress\n- [x] Step 1"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body})
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count, "body update should not produce a commit")

	// --- Transition to review (flushes all deferred changes) ---
	review := "review"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &review})
	require.NoError(t, err)

	countAfterReview, _ := gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate+1, countAfterReview,
		"transition to review should flush deferred changes and produce 1 commit")

	// --- Release card (produces another commit for the release itself) ---
	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	countAfterRelease, _ := gitMgr.CommitCount()
	assert.Equal(t, countAfterReview+1, countAfterRelease,
		"release should produce exactly 1 commit")

	// Verify no uncommitted changes remain.
	hasUncommitted, err := gitMgr.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasUncommitted, "all changes should be committed after release")

	// Deferred paths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after release flush")
}

// TestDeferredCommitReportUsageAfterRelease verifies that calling ReportUsage
// after ReleaseCard flushes the usage data instead of leaving it as an
// orphaned deferred path.
func TestDeferredCommitReportUsageAfterRelease(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Usage flush test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	countAfterRelease, err := gitMgr.CommitCount()
	require.NoError(t, err)

	// Report usage after release (card has no agent).
	_, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID: "agent-1", Model: "test-model",
		PromptTokens: 100, CompletionTokens: 50,
	})
	require.NoError(t, err)

	countAfterUsage, err := gitMgr.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, countAfterRelease+1, countAfterUsage,
		"ReportUsage after release must produce a new commit")

	// Deferred paths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after post-release usage report")
}

// TestDeferredCommitUpdateRunnerStatusNonTerminal verifies that non-terminal
// runner statuses (queued, running) do NOT flush deferred commits.
func TestDeferredCommitUpdateRunnerStatusNonTerminal(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Runner non-terminal test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "runner:agent-1")
	require.NoError(t, err)

	countAfterCreate, err := gitMgr.CommitCount()
	require.NoError(t, err)

	// Non-terminal status: queued — should NOT flush.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "queued", "task queued for runner")
	require.NoError(t, err)

	count, _ := gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count,
		"non-terminal runner status 'queued' should not flush deferred commits")

	// Non-terminal status: running — should NOT flush.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "running", "container started")
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate, count,
		"non-terminal runner status 'running' should not flush deferred commits")

	// Deferred paths should still be present.
	svc.writeMu.Lock()
	pathCount := len(svc.deferredPaths[card.ID])
	svc.writeMu.Unlock()
	assert.Greater(t, pathCount, 0, "deferred paths should accumulate for non-terminal statuses")

	// Terminal status: failed — SHOULD flush.
	_, err = svc.UpdateRunnerStatus(ctx, "test-project", card.ID, "failed", "container exited with code 1")
	require.NoError(t, err)

	count, _ = gitMgr.CommitCount()
	assert.Equal(t, countAfterCreate+1, count,
		"terminal runner status 'failed' should produce exactly 1 commit (deferred flush)")

	// Deferred paths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after terminal runner status flush")
}

// TestCreateCard_DuplicateSubtaskGuard verifies that creating a subtask with the
// same title as an existing non-terminal sibling returns the existing card rather
// than creating a duplicate, and that terminal-state siblings do NOT trigger dedup.
func TestCreateCard_DuplicateSubtaskGuard(t *testing.T) {
	ctx := context.Background()

	// helper: create parent + one subtask, then optionally transition the subtask.
	setup := func(t *testing.T, subtaskState string) (*CardService, *board.Card, *board.Card) {
		t.Helper()
		svc, _, cleanup := setupTest(t)
		t.Cleanup(cleanup)

		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Parent Task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		subtask, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Existing Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		require.Equal(t, "todo", subtask.State)

		if subtaskState != "" && subtaskState != "todo" {
			subtask, err = svc.PatchCard(ctx, "test-project", subtask.ID, PatchCardInput{State: &subtaskState})
			require.NoError(t, err)
			require.Equal(t, subtaskState, subtask.State)
		}

		return svc, parent, subtask
	}

	t.Run("dedup in todo state returns existing card", func(t *testing.T) {
		svc, parent, existing := setup(t, "")

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Existing Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, existing.ID, got.ID, "should return the existing subtask, not create a new one")
	})

	t.Run("dedup in in_progress state returns existing card", func(t *testing.T) {
		inProgress := "in_progress"
		svc, parent, existing := setup(t, inProgress)

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Existing Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, existing.ID, got.ID, "should return the existing in_progress subtask, not create a new one")
	})

	t.Run("done state creates new card (terminal state)", func(t *testing.T) {
		// Need to go todo -> in_progress -> done
		svc, _, cleanup := setupTest(t)
		t.Cleanup(cleanup)

		parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Parent Task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		existing, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Done Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)

		inProgress := "in_progress"
		_, err = svc.PatchCard(ctx, "test-project", existing.ID, PatchCardInput{State: &inProgress})
		require.NoError(t, err)

		done := "done"
		_, err = svc.PatchCard(ctx, "test-project", existing.ID, PatchCardInput{State: &done})
		require.NoError(t, err)

		// Same title, same parent — but existing is done (terminal) so a new card must be created.
		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Done Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.NotEqual(t, existing.ID, got.ID, "terminal done state must not trigger dedup — new card expected")
	})

	t.Run("not_planned state creates new card (terminal state)", func(t *testing.T) {
		notPlanned := "not_planned"
		svc, parent, existing := setup(t, notPlanned)

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Existing Subtask",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.NotEqual(t, existing.ID, got.ID, "terminal not_planned state must not trigger dedup — new card expected")
	})

	t.Run("no dedup for top-level cards (no parent)", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		t.Cleanup(cleanup)

		first, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Top Level Card",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)

		second, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Top Level Card",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.NotEqual(t, first.ID, second.ID, "top-level cards must never be deduped")
	})

	t.Run("no dedup when titles differ", func(t *testing.T) {
		svc, parent, existing := setup(t, "")

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Different Title",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.NotEqual(t, existing.ID, got.ID, "different title must create a new card")
	})

	t.Run("case-insensitive title matching", func(t *testing.T) {
		svc, parent, existing := setup(t, "")

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "EXISTING SUBTASK",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, existing.ID, got.ID, "title matching must be case-insensitive")
	})

	t.Run("whitespace-trimmed title matching", func(t *testing.T) {
		svc, parent, existing := setup(t, "")

		got, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "  Existing Subtask  ",
			Type:     "task",
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, existing.ID, got.ID, "title matching must trim whitespace")
	})
}

// TestCreateCard_VettedAutoDefault verifies the auto-vetting logic on card creation.
func TestCreateCard_VettedAutoDefault(t *testing.T) {
	ctx := context.Background()

	t.Run("no source — auto-vetted", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Internal task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.True(t, card.Vetted, "cards without a source must be auto-vetted")
	})

	t.Run("with source — not vetted by default", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "GitHub issue",
			Type:     "bug",
			Priority: "high",
			Source: &board.Source{
				System:     "github",
				ExternalID: "42",
			},
		})
		require.NoError(t, err)
		assert.False(t, card.Vetted, "imported cards must not be auto-vetted")
	})

	t.Run("with source and explicit vetted=true", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Pre-approved issue",
			Type:     "bug",
			Priority: "high",
			Source: &board.Source{
				System:     "github",
				ExternalID: "99",
			},
			Vetted: true,
		})
		require.NoError(t, err)
		assert.True(t, card.Vetted, "explicit Vetted=true must be respected")
	})
}

// TestClaimCard_VettingEnforcement verifies the claim-time vetting gate.
func TestClaimCard_VettingEnforcement(t *testing.T) {
	ctx := context.Background()

	// makeCard creates a card with or without an external source.
	makeCard := func(t *testing.T, svc *CardService, withSource bool) *board.Card {
		t.Helper()
		input := CreateCardInput{
			Title:    "Test card",
			Type:     "task",
			Priority: "medium",
		}
		if withSource {
			input.Source = &board.Source{
				System:     "github",
				ExternalID: "1",
			}
		}
		card, err := svc.CreateCard(ctx, "test-project", input)
		require.NoError(t, err)
		return card
	}

	t.Run("agent can claim vetted card (no source)", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card := makeCard(t, svc, false)
		assert.True(t, card.Vetted)

		_, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err)
	})

	t.Run("agent blocked from claiming unvetted imported card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card := makeCard(t, svc, true)
		assert.False(t, card.Vetted)

		_, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCardNotVetted)
	})

	t.Run("agent can claim vetted imported card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card := makeCard(t, svc, true)
		assert.False(t, card.Vetted)

		// Vet the card via PatchCard (simulates human approval).
		vetted := true
		_, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Vetted: &vetted})
		require.NoError(t, err)

		_, err = svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
		require.NoError(t, err)
	})

	t.Run("human agent can claim unvetted imported card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card := makeCard(t, svc, true)
		assert.False(t, card.Vetted)

		_, err := svc.ClaimCard(ctx, "test-project", card.ID, "human:alice")
		require.NoError(t, err)
	})
}

// TestPatchCard_VettedToggle verifies that Vetted can be toggled via PatchCard.
func TestPatchCard_VettedToggle(t *testing.T) {
	ctx := context.Background()

	t.Run("set vetted=true on imported card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "GitHub issue",
			Type:     "task",
			Priority: "medium",
			Source: &board.Source{
				System:     "github",
				ExternalID: "7",
			},
		})
		require.NoError(t, err)
		assert.False(t, card.Vetted)

		vetted := true
		updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Vetted: &vetted})
		require.NoError(t, err)
		assert.True(t, updated.Vetted)
	})

	t.Run("set vetted=false on internal card", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Internal task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.True(t, card.Vetted)

		vetted := false
		updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Vetted: &vetted})
		require.NoError(t, err)
		assert.False(t, updated.Vetted)
	})

	t.Run("nil Vetted leaves field unchanged", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Internal task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.True(t, card.Vetted)

		title := "Updated title"
		updated, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Title: &title})
		require.NoError(t, err)
		assert.True(t, updated.Vetted, "nil Vetted must not change existing value")
	})
}

// TestUpdateCard_VettedField verifies that UpdateCard propagates the Vetted field.
func TestUpdateCard_VettedField(t *testing.T) {
	ctx := context.Background()

	t.Run("vetted=false on full update clears field", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "Internal task",
			Type:     "task",
			Priority: "medium",
		})
		require.NoError(t, err)
		assert.True(t, card.Vetted)

		updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title:    card.Title,
			Type:     card.Type,
			State:    card.State,
			Priority: card.Priority,
			Vetted:   false,
		})
		require.NoError(t, err)
		assert.False(t, updated.Vetted)
	})

	t.Run("vetted=true on full update sets field", func(t *testing.T) {
		svc, _, cleanup := setupTest(t)
		defer cleanup()

		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    "GitHub issue",
			Type:     "task",
			Priority: "medium",
			Source: &board.Source{
				System:     "github",
				ExternalID: "5",
			},
		})
		require.NoError(t, err)
		assert.False(t, card.Vetted)

		updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
			Title:    card.Title,
			Type:     card.Type,
			State:    card.State,
			Priority: card.Priority,
			Vetted:   true,
		})
		require.NoError(t, err)
		assert.True(t, updated.Vetted)
	})
}
