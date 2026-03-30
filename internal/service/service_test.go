package service

import (
	"context"
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

	gitMgr, err := gitops.NewManager(tmpDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir)

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

	gitMgr, err := gitops.NewManager(tmpDir)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 50*time.Millisecond) // Very short timeout

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir)

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
