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
