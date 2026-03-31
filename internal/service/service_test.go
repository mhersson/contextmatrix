package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		States:     []string{"todo", "in_progress", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
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

	gitMgr, err := gitops.NewManager(boardsDir)
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
	for i := 0; i < 55; i++ {
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
	for i := 0; i < cardCount; i++ {
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

	gitMgr, err := gitops.NewManager(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 50*time.Millisecond) // Very short timeout

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		States:     []string{"todo", "in_progress", "blocked", "review", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"blocked", "review", "todo", "done"},
			"blocked":     {"in_progress", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
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

	gitMgr, err := gitops.NewManager(boardsDir)
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

func TestParentAutoTransition_AllSubtasksDoneMovesParentToReview(t *testing.T) {
	svc, _, cleanup := setupTestWithReview(t)
	defer cleanup()

	ctx := context.Background()

	parent, subtasks := createParentWithSubtasks(t, svc, "test-project", 2)

	// Subscribe to events to verify parent state change event is published
	ch, unsub := svc.bus.Subscribe()
	defer unsub()

	// Transition both subtasks to in_progress (parent also moves to in_progress)
	inProgress := "in_progress"
	_, err := svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	// Drain in_progress events
	drainEvents(ch)

	// Complete first subtask: in_progress → done
	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", subtasks[0].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Drain partial-done events
	drainEvents(ch)

	// Complete last subtask: in_progress → done
	_, err = svc.PatchCard(ctx, "test-project", subtasks[1].ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Parent should be in review
	updatedParent, err := svc.GetCard(ctx, "test-project", parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "review", updatedParent.State)

	// Verify parent state change event was published
	found := false
	timeout := time.After(200 * time.Millisecond)
	for !found {
		select {
		case event := <-ch:
			if event.Type == events.CardStateChanged && event.CardID == parent.ID {
				assert.Equal(t, "in_progress", event.Data["old_state"])
				assert.Equal(t, "review", event.Data["new_state"])
				found = true
			}
		case <-timeout:
			t.Fatal("expected CardStateChanged event for parent")
		}
	}
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

// drainEvents reads all buffered events from the channel without blocking.
func drainEvents(ch <-chan events.Event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
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

	gitMgr, err := gitops.NewManager(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	tokenCosts := map[string]ModelCost{
		"claude-sonnet-4": {Prompt: 0.000003, Completion: 0.000015},
		"claude-opus-4":   {Prompt: 0.000015, Completion: 0.000075},
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
		Model:            "claude-sonnet-4",
		PromptTokens:     10000,
		CompletionTokens: 2000,
	})
	require.NoError(t, err)
	// Expected: 10000 * 0.000003 + 2000 * 0.000015 = 0.03 + 0.03 = 0.06
	assert.InDelta(t, 0.06, updated.TokenUsage.EstimatedCostUSD, 0.0001)

	// Report again with different model — cost should accumulate as delta
	updated, err = svc.ReportUsage(ctx, "test-project", card.ID, ReportUsageInput{
		AgentID:          "agent-1",
		Model:            "claude-opus-4",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})
	require.NoError(t, err)
	// Delta: 1000 * 0.000015 + 500 * 0.000075 = 0.015 + 0.0375 = 0.0525
	// Total: 0.06 + 0.0525 = 0.1125
	assert.InDelta(t, 0.1125, updated.TokenUsage.EstimatedCostUSD, 0.0001)
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
		AgentID: "a1", Model: "claude-sonnet-4", PromptTokens: 1000, CompletionTokens: 500,
	})
	require.NoError(t, err)

	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID: "a2", Model: "claude-sonnet-4", PromptTokens: 2000, CompletionTokens: 1000,
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
		AgentID: "agent-1", Model: "claude-sonnet-4", PromptTokens: 1000, CompletionTokens: 500,
	})
	require.NoError(t, err)
	_, err = svc.ReportUsage(ctx, "test-project", card2.ID, ReportUsageInput{
		AgentID: "agent-1", Model: "claude-sonnet-4", PromptTokens: 2000, CompletionTokens: 1000,
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

	gitMgr, err := gitops.NewManager(boardsDir)
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
		States:     []string{"todo", "in_progress", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
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
		States:     []string{"todo", "in_progress", "review", "done", "stalled"},
		Types:      []string{"task", "bug", "feature", "chore"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
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
		States:     []string{"in_progress", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"in_progress": {"done"},
			"done":        {"in_progress"},
			"stalled":     {"in_progress"},
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
		States:     []string{"todo", "in_progress", "done", "stalled"},
		Types:      []string{"bug", "feature"}, // removed "task"
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
		},
	}

	_, err = svc.UpdateProject(ctx, "test-project", input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove type")
}

func TestUpdateProject_NotFound(t *testing.T) {
	svc, _ := setupEmptyTest(t)
	ctx := context.Background()

	_, err := svc.UpdateProject(ctx, "nonexistent", UpdateProjectInput{})
	assert.ErrorIs(t, err, storage.ErrProjectNotFound)
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

	gitMgr, err := gitops.NewManager(boardsDir)
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

	gitMgr, err := gitops.NewManager(boardsDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	// gitAutoCommit=true, gitDeferredCommit=true
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, true)
	return svc, gitMgr
}

// TestDeferredCommitAccumulates verifies that with deferred mode on,
// intermediate card mutations do not produce commits.
func TestDeferredCommitAccumulates(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card — should defer the commit.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Deferred Card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// No commit yet.
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Empty(t, msg, "no commit expected after create in deferred mode")

	// Update card twice.
	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Updated Once", Type: "task", State: "todo", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Updated Twice", Type: "task", State: "todo", Priority: "medium",
	})
	require.NoError(t, err)

	// Still no commit.
	msg, err = gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Empty(t, msg, "no commit expected after updates in deferred mode")

	// Deferred paths should be non-empty.
	svc.writeMu.Lock()
	pathCount := len(svc.deferredPaths[card.ID])
	svc.writeMu.Unlock()
	assert.Greater(t, pathCount, 0, "deferredPaths should have entries")
}

// TestDeferredCommitFlushOnDone verifies that transitioning to "done"
// produces a single deferred commit.
func TestDeferredCommitFlushOnDone(t *testing.T) {
	svc, gitMgr := setupDeferredTest(t)
	ctx := context.Background()

	// Create card.
	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Will Complete", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	// Update body (deferred).
	body := "## Progress\n- [x] Step 1"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Body: &body})
	require.NoError(t, err)

	// Transition todo → in_progress → done (PatchCard flushes on done).
	inProgress := "in_progress"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &inProgress})
	require.NoError(t, err)

	done := "done"
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{State: &done})
	require.NoError(t, err)

	// Now there should be exactly one commit (the deferred flush).
	msg, err := gitMgr.GetLastCommitMessage()
	require.NoError(t, err)
	assert.NotEmpty(t, msg, "expected a commit after transitioning to done")
	assert.Contains(t, msg, card.ID)
	assert.Contains(t, msg, "completed (deferred commit)")

	// deferredPaths should be cleared.
	svc.writeMu.Lock()
	_, hasPaths := svc.deferredPaths[card.ID]
	svc.writeMu.Unlock()
	assert.False(t, hasPaths, "deferredPaths should be cleared after flush")
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

	gitMgr, err := gitops.NewManager(boardsDir)
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
		States:     []string{"todo", "done", "stalled"},
		Types:      []string{"task"},
		Priorities: []string{"medium"},
		Transitions: map[string][]string{
			"todo":    {"done"},
			"done":    {"todo"},
			"stalled": {"todo"},
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
