package service

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProjectName = "test-project"

func TestRecordSkillEngaged_AppendsEntry(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "skill_engaged", got.ActivityLog[0].Action)
	assert.Equal(t, "go-development", got.ActivityLog[0].Skill)
}

func TestRecordSkillEngaged_DedupsWithinWindow(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 1, "duplicate within window should be suppressed")
}

func TestRecordSkillEngaged_DistinctSkillsBothLogged(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "documentation"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 2)
}

func TestRecordSkillEngaged_DedupsAcrossPathAandPathB(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	// Simulate Path A: agent calls AddLogEntry with action="skill_engaged",
	// message="engaged go-development", no Skill field set.
	require.NoError(t, svc.AddLogEntry(context.Background(), testProjectName, card.ID, board.ActivityEntry{
		Agent:   "claude-test",
		Action:  "skill_engaged",
		Message: "engaged go-development",
	}))

	// Path B: runner callback hits RecordSkillEngaged with structured Skill field.
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 1, "Path B must dedup against Path A entry parsed from message")
}

func TestRecordPush_AppendsMultipleRecords(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title:    "Multi-repo push",
		Type:     "task",
		Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, testProjectName, card.ID, "agent-1")
	require.NoError(t, err)

	_, err = svc.RecordPush(ctx, testProjectName, card.ID, "agent-1",
		"auth-svc", "cm/"+card.ID, "https://github.com/acme/auth-svc/pull/1")
	require.NoError(t, err)

	_, err = svc.RecordPush(ctx, testProjectName, card.ID, "agent-1",
		"billing-svc", "cm/"+card.ID, "https://github.com/acme/billing-svc/pull/2")
	require.NoError(t, err)

	fresh, err := svc.GetCard(ctx, testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, fresh.PushRecords, 2)
	assert.Equal(t, "auth-svc", fresh.PushRecords[0].Repo)
	assert.Equal(t, "cm/"+card.ID, fresh.PushRecords[0].Branch)
	assert.Equal(t, "https://github.com/acme/auth-svc/pull/1", fresh.PushRecords[0].PRURL)
	assert.False(t, fresh.PushRecords[0].PushedAt.IsZero())
	assert.Equal(t, "billing-svc", fresh.PushRecords[1].Repo)
}

func TestRecordPush_BackwardCompatEmptyRepo(t *testing.T) {
	// Existing callers without repo still get a record (with empty Repo)
	// and the legacy PRUrl side-effect.
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "Single push", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)
	_, err = svc.ClaimCard(ctx, testProjectName, card.ID, "agent-1")
	require.NoError(t, err)

	_, err = svc.RecordPush(ctx, testProjectName, card.ID, "agent-1",
		"", "feat/login", "https://github.com/acme/legacy/pull/1")
	require.NoError(t, err)

	fresh, err := svc.GetCard(ctx, testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, fresh.PushRecords, 1)
	assert.Empty(t, fresh.PushRecords[0].Repo)
	assert.Equal(t, "feat/login", fresh.PushRecords[0].Branch)
	// Legacy single-PR field still set:
	assert.Equal(t, "https://github.com/acme/legacy/pull/1", fresh.PRUrl)
}

func TestRecordSkillEngaged_AfterWindowAppendsAgain(t *testing.T) {
	prev := SkillEngagedDedupWindow
	SkillEngagedDedupWindow = 10 * time.Millisecond

	t.Cleanup(func() { SkillEngagedDedupWindow = prev })

	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 2, "after window expires, a fresh entry must be appended")
}
