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

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "runner:"+card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "skill_engaged", got.ActivityLog[0].Action)
	assert.Equal(t, "go-development", got.ActivityLog[0].Skill)
	assert.Equal(t, "runner:"+card.ID, got.ActivityLog[0].Agent,
		"activity entry must record the agent that engaged the skill, not a hardcoded 'runner'")
}

// TestRecordSkillEngaged_RewritesParentRunnerToCardRunner verifies that
// when the orchestrator (which holds a single 'runner:PARENT' claim
// across the whole task tree) reports a skill engagement on a subtask,
// the activity entry on the subtask shows 'runner:SUBTASK', not the
// parent's runner agent. Without this rewrite the subtask card's log
// would always be attributed to the parent and operators couldn't tell
// which card actually used the skill.
func TestRecordSkillEngaged_RewritesParentRunnerToCardRunner(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	parent, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "parent", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
	})
	require.NoError(t, err)

	// Caller passes the parent's runner agent_id (the orchestrator's
	// claim) but reports against the subtask. Entry must surface the
	// subtask's runner agent.
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, subtask.ID, "runner:"+parent.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, subtask.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "runner:"+subtask.ID, got.ActivityLog[0].Agent,
		"skill_engaged on a subtask must be attributed to the subtask's runner agent, not the parent's")
}

// TestRecordSkillEngaged_PreservesHumanActor verifies that human:* agent
// ids are not rewritten to runner:CARDID. Skill engagements logged by a
// human (HITL) keep their original attribution.
func TestRecordSkillEngaged_PreservesHumanActor(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "human:morten", "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "human:morten", got.ActivityLog[0].Agent,
		"human-engaged skills must not be rewritten to a runner agent")
}

// TestAddLogEntry_SkillEngagedRollsUpToParent verifies that a
// skill_engaged entry written to a subtask card is also appended to its
// parent card so an operator looking at the parent can see what work the
// subtask did. The rollup carries the subtask's own runner actor (set by
// normalizeSkillEngagedActor) — never the parent's. This replaces the
// runner-side dispatcher safety net which used to write a misleading
// `runner:PARENT` entry to the parent for every subtask engagement.
func TestAddLogEntry_SkillEngagedRollsUpToParent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	parent, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "parent", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
	})
	require.NoError(t, err)

	require.NoError(t, svc.AddLogEntry(ctx, testProjectName, subtask.ID, board.ActivityEntry{
		Agent:   "runner:" + parent.ID,
		Action:  "skill_engaged",
		Message: "engaged go-development",
	}))

	// Subtask gets the entry with subtask actor.
	gotChild, err := svc.GetCard(ctx, testProjectName, subtask.ID)
	require.NoError(t, err)
	require.Len(t, gotChild.ActivityLog, 1)
	assert.Equal(t, "runner:"+subtask.ID, gotChild.ActivityLog[0].Agent)
	assert.Equal(t, "skill_engaged", gotChild.ActivityLog[0].Action)
	assert.Equal(t, "go-development", gotChild.ActivityLog[0].Message)
	assert.Equal(t, "go-development", gotChild.ActivityLog[0].Skill,
		"the agent-driven add_log path must populate the structured Skill field "+
			"from the normalized message — the assertSkillEngaged integration check "+
			"and UI badges read the Skill field, not the message")

	// Parent gets the rollup with the SUBTASK's actor — never the parent's.
	gotParent, err := svc.GetCard(ctx, testProjectName, parent.ID)
	require.NoError(t, err)
	require.Len(t, gotParent.ActivityLog, 1,
		"parent must receive a rolled-up skill_engaged entry from the subtask")
	assert.Equal(t, "runner:"+subtask.ID, gotParent.ActivityLog[0].Agent,
		"parent rollup must keep the subtask's actor, not be re-attributed to the parent")
	assert.Equal(t, "skill_engaged", gotParent.ActivityLog[0].Action)
	assert.Equal(t, "go-development", gotParent.ActivityLog[0].Message)
	assert.Equal(t, "go-development", gotParent.ActivityLog[0].Skill,
		"the parent rollup must carry the same structured Skill field as the subtask entry")
}

// TestAddLogEntry_SkillEngagedNoRollupWhenNoParent verifies that a
// top-level card's skill_engaged entries land only on that card — no
// phantom parent traversal.
func TestAddLogEntry_SkillEngagedNoRollupWhenNoParent(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "top", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.AddLogEntry(ctx, testProjectName, card.ID, board.ActivityEntry{
		Agent:   "runner:" + card.ID,
		Action:  "skill_engaged",
		Message: "go-development",
	}))

	got, err := svc.GetCard(ctx, testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 1, "top-level card gets exactly one entry — no rollup")
}

// TestAddLogEntry_SkillEngagedRollupKeepsHumanActor verifies that a
// HITL-engaged skill on a subtask rolls up to the parent with the
// human:* actor preserved. The rollup mirrors the subtask entry verbatim.
func TestAddLogEntry_SkillEngagedRollupKeepsHumanActor(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	parent, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "parent", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(ctx, testProjectName, CreateCardInput{
		Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
	})
	require.NoError(t, err)

	require.NoError(t, svc.AddLogEntry(ctx, testProjectName, subtask.ID, board.ActivityEntry{
		Agent:   "human:morten",
		Action:  "skill_engaged",
		Message: "code-review",
	}))

	gotParent, err := svc.GetCard(ctx, testProjectName, parent.ID)
	require.NoError(t, err)
	require.Len(t, gotParent.ActivityLog, 1)
	assert.Equal(t, "human:morten", gotParent.ActivityLog[0].Agent,
		"human-engaged skill must roll up with human attribution intact")
}

// TestAddLogEntry_SkillEngagedNormalization verifies the same actor and
// message normalization fires for the agent-side path: when the agent
// calls add_log with action="skill_engaged", the orchestrator-supplied
// runner agent_id is rewritten to the per-card runner, and the legacy
// "engaged " prefix is stripped from the message.
func TestAddLogEntry_SkillEngagedNormalization(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	parent, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "parent", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	subtask, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "child", Type: "task", Priority: "low", Parent: parent.ID,
	})
	require.NoError(t, err)

	require.NoError(t, svc.AddLogEntry(context.Background(), testProjectName, subtask.ID, board.ActivityEntry{
		Agent:   "runner:" + parent.ID,
		Action:  "skill_engaged",
		Message: "engaged go-development",
	}))

	got, err := svc.GetCard(context.Background(), testProjectName, subtask.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "runner:"+subtask.ID, got.ActivityLog[0].Agent,
		"add_log skill_engaged on a subtask must surface the subtask's runner agent")
	assert.Equal(t, "go-development", got.ActivityLog[0].Message,
		"add_log skill_engaged message must drop the legacy 'engaged ' prefix")
}

// TestRecordSkillEngaged_MessageOmitsEngagedPrefix verifies that the
// activity entry message is just the skill name, not "engaged <name>".
// The Action field already says "skill_engaged" — repeating "engaged" in
// the message is noise.
func TestRecordSkillEngaged_MessageOmitsEngagedPrefix(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "runner:"+card.ID, "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "go-development", got.ActivityLog[0].Message,
		"skill_engaged message should be just the skill name, not 'engaged <name>'")
}

func TestRecordSkillEngaged_FallsBackToRunnerWhenAgentEmpty(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	require.Len(t, got.ActivityLog, 1)
	assert.Equal(t, "runner", got.ActivityLog[0].Agent,
		"empty agent id from older runners must fall back to 'runner' for backwards compatibility")
}

func TestRecordSkillEngaged_DedupsWithinWindow(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	card, err := svc.CreateCard(context.Background(), testProjectName, CreateCardInput{
		Title: "t", Type: "task", Priority: "low",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))

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

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "documentation"))

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
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))

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

	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, svc.RecordSkillEngaged(context.Background(), testProjectName, card.ID, "", "go-development"))

	got, err := svc.GetCard(context.Background(), testProjectName, card.ID)
	require.NoError(t, err)
	assert.Len(t, got.ActivityLog, 2, "after window expires, a fresh entry must be appended")
}
