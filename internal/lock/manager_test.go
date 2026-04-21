package lock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*storage.FilesystemStore, string) {
	t.Helper()
	boardsDir := t.TempDir()

	// Create a test project
	projectDir := boardsDir + "/test-project"
	cfg := &board.ProjectConfig{
		Name:       "test-project",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done", "todo"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
	require.NoError(t, board.SaveProjectConfig(projectDir, cfg))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	return store, boardsDir
}

func createTestCard(t *testing.T, store storage.Store, project string, id string, agent string) *board.Card {
	t.Helper()

	return createTestCardAt(t, store, project, id, agent, time.Now())
}

func createTestCardAt(t *testing.T, store storage.Store, project string, id string, agent string, now time.Time) *board.Card {
	t.Helper()

	card := &board.Card{
		ID:       id,
		Title:    "Test Card " + id,
		Project:  project,
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  now,
		Updated:  now,
	}

	if agent != "" {
		card.AssignedAgent = agent
		card.LastHeartbeat = &now
	}

	err := store.CreateCard(context.Background(), project, card)
	require.NoError(t, err)

	return card
}

func TestNewManager(t *testing.T) {
	store, _ := setupTestStore(t)
	timeout := 30 * time.Minute

	mgr := NewManager(store, timeout)

	assert.NotNil(t, mgr)
	assert.Equal(t, timeout, mgr.Timeout())
}

func TestClaim_Unclaimed(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create an unclaimed card
	createTestCard(t, store, "test-project", "TEST-001", "")

	// Claim it
	card, err := mgr.Claim(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.Equal(t, "agent-1", card.AssignedAgent)
	assert.NotNil(t, card.LastHeartbeat)
	assert.WithinDuration(t, time.Now(), *card.LastHeartbeat, time.Second)
}

func TestClaim_AlreadyClaimed_SameAgent(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create a card already claimed by agent-1
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	// Re-claim by same agent should succeed (refresh heartbeat)
	card, err := mgr.Claim(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.Equal(t, "agent-1", card.AssignedAgent)
	assert.NotNil(t, card.LastHeartbeat)
}

func TestClaim_AlreadyClaimed_DifferentAgent(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create a card already claimed by agent-1
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	// Attempt claim by different agent
	card, err := mgr.Claim(ctx, "test-project", "TEST-001", "agent-2")

	assert.Nil(t, card)
	require.ErrorIs(t, err, ErrAlreadyClaimed)
	assert.Contains(t, err.Error(), "agent-1")
}

func TestClaim_CardNotFound(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	card, err := mgr.Claim(ctx, "test-project", "NONEXISTENT", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, storage.ErrCardNotFound)
}

func TestClaim_ProjectNotFound(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	card, err := mgr.Claim(ctx, "nonexistent-project", "TEST-001", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, storage.ErrProjectNotFound)
}

func TestRelease_Success(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create a claimed card
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	// Release it
	card, err := mgr.Release(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.Empty(t, card.AssignedAgent)
	assert.Nil(t, card.LastHeartbeat)
}

func TestRelease_NotClaimed(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create an unclaimed card
	createTestCard(t, store, "test-project", "TEST-001", "")

	// Attempt release
	card, err := mgr.Release(ctx, "test-project", "TEST-001", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, ErrNotClaimed)
}

func TestRelease_AgentMismatch(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create a card claimed by agent-1
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	// Attempt release by different agent
	card, err := mgr.Release(ctx, "test-project", "TEST-001", "agent-2")

	assert.Nil(t, card)
	require.ErrorIs(t, err, ErrAgentMismatch)
	assert.Contains(t, err.Error(), "agent-1")
}

func TestRelease_CardNotFound(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	card, err := mgr.Release(ctx, "test-project", "NONEXISTENT", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, storage.ErrCardNotFound)
}

func TestHeartbeat_Success(t *testing.T) {
	store, _ := setupTestStore(t)
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	mgr := NewManagerWithClock(store, 30*time.Minute, fake)
	ctx := context.Background()

	// Create a claimed card with old heartbeat, using the fake clock's current time.
	original := createTestCardAt(t, store, "test-project", "TEST-001", "agent-1", fake.Now())
	oldHeartbeat := original.LastHeartbeat

	// Advance the fake clock so the next Heartbeat() picks a strictly later time.
	fake.Advance(10 * time.Millisecond)

	// Heartbeat
	card, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.Equal(t, "agent-1", card.AssignedAgent)
	assert.NotNil(t, card.LastHeartbeat)
	assert.True(t, card.LastHeartbeat.After(*oldHeartbeat))
}

func TestHeartbeat_NotClaimed(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create an unclaimed card
	createTestCard(t, store, "test-project", "TEST-001", "")

	card, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, ErrNotClaimed)
}

func TestHeartbeat_AgentMismatch(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create a card claimed by agent-1
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	card, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "agent-2")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, ErrAgentMismatch)
}

func TestHeartbeat_CardNotFound(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	card, err := mgr.Heartbeat(ctx, "test-project", "NONEXISTENT", "agent-1")

	assert.Nil(t, card)
	assert.ErrorIs(t, err, storage.ErrCardNotFound)
}

func TestFindStalled_NoStalledCards(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create fresh claimed card
	createTestCard(t, store, "test-project", "TEST-001", "agent-1")

	// Create unclaimed card
	createTestCard(t, store, "test-project", "TEST-002", "")

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)

	assert.Empty(t, stalled)
}

func TestFindStalled_WithStalledCards(t *testing.T) {
	store, boardsDir := setupTestStore(t)
	ctx := context.Background()

	// Create a card with old heartbeat (stalled)
	oldTime := time.Now().Add(-1 * time.Hour)
	stalledCard := &board.Card{
		ID:            "TEST-001",
		Title:         "Stalled Card",
		Project:       "test-project",
		Type:          "task",
		State:         "in_progress",
		Priority:      "high",
		AssignedAgent: "agent-1",
		LastHeartbeat: &oldTime,
		Created:       oldTime,
		Updated:       oldTime,
	}
	require.NoError(t, store.CreateCard(ctx, "test-project", stalledCard))

	// Create a fresh card (not stalled)
	createTestCard(t, store, "test-project", "TEST-002", "agent-2")

	// Create unclaimed card (not stalled)
	createTestCard(t, store, "test-project", "TEST-003", "")

	// Re-initialize store to reload from disk (get the stalled card's old heartbeat)
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	// 30s is far longer than any CI filesystem operation between card creation
	// and FindStalled; it only rejects the -1h heartbeat below.
	mgr := NewManager(store, 30*time.Second)

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)

	assert.Len(t, stalled, 1)
	assert.Equal(t, "TEST-001", stalled[0].Card.ID)
	assert.Equal(t, "test-project", stalled[0].Project)
}

func TestFindStalled_ClaimedNoHeartbeat(t *testing.T) {
	store, boardsDir := setupTestStore(t)
	ctx := context.Background()

	// Create a card that's claimed but has no heartbeat
	now := time.Now()
	card := &board.Card{
		ID:            "TEST-001",
		Title:         "No Heartbeat Card",
		Project:       "test-project",
		Type:          "task",
		State:         "in_progress",
		Priority:      "high",
		AssignedAgent: "agent-1",
		LastHeartbeat: nil, // No heartbeat
		Created:       now,
		Updated:       now,
	}
	require.NoError(t, store.CreateCard(ctx, "test-project", card))

	// Re-initialize store to reload from disk
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	mgr := NewManager(store, 30*time.Minute)

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)

	// Card with no heartbeat but claimed should be considered stalled
	assert.Len(t, stalled, 1)
	assert.Equal(t, "TEST-001", stalled[0].Card.ID)
}

func TestFindStalled_MultipleProjects(t *testing.T) {
	boardsDir := t.TempDir()

	// Create two test projects
	for _, name := range []string{"project-alpha", "project-beta"} {
		cfg := &board.ProjectConfig{
			Name:       name,
			Prefix:     name[:5],
			NextID:     1,
			States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
			Types:      []string{"task"},
			Priorities: []string{"low", "medium", "high"},
			Transitions: map[string][]string{
				"todo":        {"in_progress"},
				"in_progress": {"done"},
				"done":        {"todo"},
				"stalled":     {"todo"},
				"not_planned": {"todo"},
			},
		}
		require.NoError(t, board.SaveProjectConfig(boardsDir+"/"+name, cfg))
	}

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)

	// Create stalled card in project-alpha
	card1 := &board.Card{
		ID:            "proje-001",
		Title:         "Stalled Alpha",
		Project:       "project-alpha",
		Type:          "task",
		State:         "in_progress",
		Priority:      "high",
		AssignedAgent: "agent-1",
		LastHeartbeat: &oldTime,
		Created:       oldTime,
		Updated:       oldTime,
	}
	require.NoError(t, store.CreateCard(ctx, "project-alpha", card1))

	// Create stalled card in project-beta
	card2 := &board.Card{
		ID:            "proje-001",
		Title:         "Stalled Beta",
		Project:       "project-beta",
		Type:          "task",
		State:         "in_progress",
		Priority:      "high",
		AssignedAgent: "agent-2",
		LastHeartbeat: &oldTime,
		Created:       oldTime,
		Updated:       oldTime,
	}
	require.NoError(t, store.CreateCard(ctx, "project-beta", card2))

	// Re-initialize store to reload from disk
	store, err = storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	mgr := NewManager(store, 100*time.Millisecond)

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)

	assert.Len(t, stalled, 2)

	// Check both projects are represented
	projects := make(map[string]bool)
	for _, s := range stalled {
		projects[s.Project] = true
	}

	assert.True(t, projects["project-alpha"])
	assert.True(t, projects["project-beta"])
}

func TestFindStalled_EmptyStore(t *testing.T) {
	boardsDir := t.TempDir()
	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	stalled, err := mgr.FindStalled(ctx)
	require.NoError(t, err)

	assert.Empty(t, stalled)
}

func TestClaimUpdatesTimestamp(t *testing.T) {
	store, _ := setupTestStore(t)
	mgr := NewManager(store, 30*time.Minute)
	ctx := context.Background()

	// Create an unclaimed card
	original := createTestCard(t, store, "test-project", "TEST-001", "")

	// Claim it
	card, err := mgr.Claim(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.True(t, card.Updated.After(original.Updated) || card.Updated.Equal(original.Updated))
}

func TestReleaseUpdatesTimestamp(t *testing.T) {
	store, _ := setupTestStore(t)
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	mgr := NewManagerWithClock(store, 30*time.Minute, fake)
	ctx := context.Background()

	// Create a claimed card at the fake clock's current time.
	original := createTestCardAt(t, store, "test-project", "TEST-001", "agent-1", fake.Now())

	fake.Advance(10 * time.Millisecond)

	// Release it
	card, err := mgr.Release(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.True(t, card.Updated.After(original.Updated))
}

func TestHeartbeatUpdatesTimestamp(t *testing.T) {
	store, _ := setupTestStore(t)
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	mgr := NewManagerWithClock(store, 30*time.Minute, fake)
	ctx := context.Background()

	// Create a claimed card at the fake clock's current time.
	original := createTestCardAt(t, store, "test-project", "TEST-001", "agent-1", fake.Now())

	fake.Advance(10 * time.Millisecond)

	// Heartbeat
	card, err := mgr.Heartbeat(ctx, "test-project", "TEST-001", "agent-1")
	require.NoError(t, err)

	assert.True(t, card.Updated.After(original.Updated))
}

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors can be used with errors.Is
	wrapped := errors.New("test")

	require.NotErrorIs(t, wrapped, ErrAlreadyClaimed)
	require.NotErrorIs(t, wrapped, ErrNotClaimed)
	assert.NotErrorIs(t, wrapped, ErrAgentMismatch)
}
