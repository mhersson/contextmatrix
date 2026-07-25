package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAssigneeTestService builds a CardService wired with a fake clock so
// activity-log timestamps can be asserted deterministically against
// card.Updated.
func newAssigneeTestService(t *testing.T) (*CardService, *clock.FakeClock, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProject()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(t))
	require.NoError(t, err)

	bus := events.NewBus()
	fake := clock.Fake(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lockMgr := lock.NewManagerWithClock(store, 30*time.Minute, fake)

	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	commitQueue := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(commitQueue)

	cleanup := func() {
		_ = commitQueue.Close(context.Background())
	}

	return svc, fake, cleanup
}

func TestCreateCard_Assignee(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
		Assignee: "alice",
	})
	require.NoError(t, err)

	assert.Equal(t, "alice", card.Assignee)
	// No activity entry on create - the creation event/commit is enough.
	assert.Empty(t, card.ActivityLog)
}

func TestCreateCard_AssigneeTooLong(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "Test",
		Type:     "task",
		Priority: "medium",
		Assignee: strings.Repeat("a", 65),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFieldTooLong)
}

func TestUpdateCard_Assignee_Replace(t *testing.T) {
	svc, fake, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	fake.Advance(time.Minute)

	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Test", Type: "task", State: "todo", Priority: "medium",
		Assignee: "alice",
	})
	require.NoError(t, err)

	assert.Equal(t, "alice", updated.Assignee)
	require.Len(t, updated.ActivityLog, 1)
	entry := updated.ActivityLog[0]
	assert.Equal(t, "assigned", entry.Action)
	assert.Equal(t, "Assigned to alice", entry.Message)
	assert.Equal(t, "system", entry.Agent) // UpdateCardInput has no AgentID
	assert.True(t, entry.Timestamp.Equal(updated.Updated))
}

func TestUpdateCard_Assignee_Clear(t *testing.T) {
	svc, fake, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium", Assignee: "alice",
	})
	require.NoError(t, err)

	fake.Advance(time.Minute)

	updated, err := svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Test", Type: "task", State: "todo", Priority: "medium",
		Assignee: "", // PUT full-replace clears it
	})
	require.NoError(t, err)

	assert.Empty(t, updated.Assignee)
	require.Len(t, updated.ActivityLog, 1)
	entry := updated.ActivityLog[0]
	assert.Equal(t, "assigned", entry.Action)
	assert.Equal(t, "Unassigned (was alice)", entry.Message)
}

func TestUpdateCard_AssigneeTooLong(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	_, err = svc.UpdateCard(ctx, "test-project", card.ID, UpdateCardInput{
		Title: "Test", Type: "task", State: "todo", Priority: "medium",
		Assignee: strings.Repeat("a", 65),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFieldTooLong)
}

func TestPatchCard_Assignee_SetAppendsActivityEntry(t *testing.T) {
	svc, fake, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	fake.Advance(time.Minute)

	assignee := "bob"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Assignee: &assignee,
		AgentID:  "human:carol",
	})
	require.NoError(t, err)

	assert.Equal(t, "bob", patched.Assignee)
	require.Len(t, patched.ActivityLog, 1)
	entry := patched.ActivityLog[0]
	assert.Equal(t, "assigned", entry.Action)
	assert.Equal(t, "Assigned to bob", entry.Message)
	assert.Equal(t, "human:carol", entry.Agent)
	assert.True(t, entry.Timestamp.Equal(patched.Updated))
}

func TestPatchCard_Assignee_ClearAppendsActivityEntry(t *testing.T) {
	svc, fake, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium", Assignee: "bob",
	})
	require.NoError(t, err)

	fake.Advance(time.Minute)

	empty := ""
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Assignee: &empty,
	})
	require.NoError(t, err)

	assert.Empty(t, patched.Assignee)
	require.Len(t, patched.ActivityLog, 1)
	entry := patched.ActivityLog[0]
	assert.Equal(t, "assigned", entry.Action)
	assert.Equal(t, "Unassigned (was bob)", entry.Message)
	// No AgentID supplied on this patch: actor normalizes to "system".
	assert.Equal(t, "system", entry.Agent)
}

func TestPatchCard_Assignee_IdenticalValueNoEntry(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium", Assignee: "bob",
	})
	require.NoError(t, err)
	require.Empty(t, card.ActivityLog)

	assignee := "bob"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Assignee: &assignee,
	})
	require.NoError(t, err)

	assert.Equal(t, "bob", patched.Assignee)
	assert.Empty(t, patched.ActivityLog)
}

func TestPatchCard_Assignee_NilLeavesUnchanged(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium", Assignee: "bob",
	})
	require.NoError(t, err)

	newTitle := "Retitled"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Title: &newTitle, // Assignee left nil = unchanged
	})
	require.NoError(t, err)

	assert.Equal(t, "bob", patched.Assignee)
	assert.Empty(t, patched.ActivityLog)
}

func TestPatchCard_AssigneeTooLong(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	tooLong := strings.Repeat("a", 65)
	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Assignee: &tooLong,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFieldTooLong)
}

// TestAssignee_ClaimReleaseIndependence pins that Assignee (informational
// responsibility label) is fully independent of AssignedAgent (execution
// claim): claiming and releasing a card must not touch Assignee.
func TestAssignee_ClaimReleaseIndependence(t *testing.T) {
	svc, _, cleanup := newAssigneeTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Test", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	assignee := "dana"
	patched, err := svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{
		Assignee: &assignee,
	})
	require.NoError(t, err)
	require.Equal(t, "dana", patched.Assignee)

	claimed, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "dana", claimed.Assignee)
	assert.Equal(t, "agent-1", claimed.AssignedAgent)

	released, err := svc.ReleaseCard(ctx, "test-project", card.ID, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "dana", released.Assignee)
	assert.Empty(t, released.AssignedAgent)
}
