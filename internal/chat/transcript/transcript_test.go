package transcript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuild_EmptyTranscriptReturnsNil(t *testing.T) {
	got := Build(nil, BuildOpts{})
	assert.Nil(t, got, "no messages should produce no ResumeContext")

	got = Build([]Message{}, BuildOpts{})
	assert.Nil(t, got, "empty slice should produce no ResumeContext")
}

func TestBuild_FiltersByRole(t *testing.T) {
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "hello"},
		{Seq: 2, Role: RoleAssistantThinking, Content: "thinking aloud"},
		{Seq: 3, Role: RoleAssistantText, Content: "hi back"},
		{Seq: 4, Role: RoleToolCall, Content: "Bash: ls"},
		{Seq: 5, Role: RoleToolResult, Content: "file1\nfile2\n"},
		{Seq: 6, Role: RoleStderr, Content: "container plumbing"},
		{Seq: 7, Role: RoleSystem, Content: "system boilerplate"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)

	roles := rolesOf(got.Turns)
	assert.Equal(t,
		[]string{"user", "assistant_text", "tool_call", "tool_result_summary"},
		roles,
		"only user/assistant_text/tool_call/tool_result_summary should survive")
}

func TestBuild_PreservesUserQuestionRole(t *testing.T) {
	// user_question entries are preserved in the resume payload with their
	// native role; the runner's chatResumeRolePattern accepts user_question
	// directly so no CM-side remap is needed.
	payload := `{"questions":[{"question":"Which library?","options":[{"label":"a"},{"label":"b"}]}]}`
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "help"},
		{Seq: 2, Role: RoleUserQuestion, Content: payload},
		{Seq: 3, Role: RoleUser, Content: "a"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)
	require.Len(t, got.Turns, 3)
	assert.Equal(t, RoleUserQuestion, got.Turns[1].Role,
		"user_question role must pass through unchanged")
	assert.Equal(t, payload, got.Turns[1].Content,
		"user_question payload must be preserved verbatim")
}

func TestBuild_ToolResultSummarized_OK(t *testing.T) {
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "list files"},
		{Seq: 2, Role: RoleToolCall, Content: "Bash: ls"},
		{Seq: 3, Role: RoleToolResult, Content: strings.Repeat("a", 5000)},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)
	require.Len(t, got.Turns, 3)
	assert.Equal(t, RoleToolResultSummary, got.Turns[2].Role)
	assert.Equal(t, "→ ok", got.Turns[2].Content)
}

func TestBuild_ToolResultSummarized_Failed(t *testing.T) {
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "do thing"},
		{Seq: 2, Role: RoleToolCall, Content: "Bash: gh repo clone foo/bar"},
		{Seq: 3, Role: RoleToolResult, Content: "fatal: error: repository not found"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)
	require.Len(t, got.Turns, 3)
	assert.True(t, strings.HasPrefix(got.Turns[2].Content, "→ failed:"),
		"tool_result with error indicator should start with '→ failed:'; got %q", got.Turns[2].Content)
	assert.Contains(t, got.Turns[2].Content, "repository not found")
}

func TestBuild_RehydrationPhaseExcluded(t *testing.T) {
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "original turn"},
		{Seq: 2, Role: RoleAssistantText, Content: "rehydration narration", RehydrationPhase: true},
		{Seq: 3, Role: RoleToolCall, Content: "Bash: git clone", RehydrationPhase: true},
		{Seq: 4, Role: RoleToolResult, Content: "ok", RehydrationPhase: true},
		{Seq: 5, Role: RoleAssistantText, Content: "real reply"},
		{Seq: 6, Role: RoleUser, Content: "follow up"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)

	seqs := seqsOf(got.Turns)
	assert.Equal(t, []int64{1, 5, 6}, seqs,
		"rehydration_phase=TRUE messages must be excluded from the resume payload")
}

func TestBuild_AlwaysKeepsFirstUserAndLastK(t *testing.T) {
	in := make([]Message, 0, 50)
	in = append(in, Message{Seq: 1, Role: RoleUser, Content: "the original goal"})

	for seq := int64(2); seq <= 50; seq++ {
		role := RoleAssistantText
		if seq%2 == 0 {
			role = RoleUser
		}

		in = append(in, Message{Seq: seq, Role: role, Content: strings.Repeat("x", 1000)})
	}

	got := Build(in, BuildOpts{BudgetTokens: 6000})
	require.NotNil(t, got)
	require.True(t, got.Clipped, "should mark Clipped when truncated")

	seqs := seqsOf(got.Turns)
	assert.Equal(t, int64(1), seqs[0], "first user turn must be preserved at position 0")

	lastTwenty := seqs[len(seqs)-20:]

	expectedLast := make([]int64, 0, 20)
	for s := int64(31); s <= 50; s++ {
		expectedLast = append(expectedLast, s)
	}

	assert.Equal(t, expectedLast, lastTwenty, "last 20 turns must be preserved")
}

func TestBuild_TruncatesOverBudget(t *testing.T) {
	in := make([]Message, 0, 600)

	for seq := int64(1); seq <= 600; seq++ {
		role := RoleAssistantText
		if seq == 1 {
			role = RoleUser
		}

		in = append(in, Message{Seq: seq, Role: role, Content: strings.Repeat("y", 400)})
	}

	got := Build(in, BuildOpts{BudgetTokens: 40000})
	require.NotNil(t, got)
	require.True(t, got.Clipped)

	totalTokens := 0
	for _, turn := range got.Turns {
		totalTokens += estimateTokens(turn.Content)
	}

	assert.LessOrEqual(t, totalTokens, 40000, "kept turns must fit within the budget")

	require.NotEmpty(t, got.Turns)
	assert.Equal(t, int64(1), got.Turns[0].Seq)
	assert.Equal(t, int64(600), got.Turns[len(got.Turns)-1].Seq)
}

func TestBuild_HardTurnCap(t *testing.T) {
	in := make([]Message, 0, 700)
	in = append(in, Message{Seq: 1, Role: RoleUser, Content: "first goal"})

	for seq := int64(2); seq <= 700; seq++ {
		in = append(in, Message{Seq: seq, Role: RoleAssistantText, Content: "tiny"})
	}

	got := Build(in, BuildOpts{BudgetTokens: 10_000_000})
	require.NotNil(t, got)
	require.True(t, got.Clipped, "should mark Clipped at hard turn cap")
	assert.LessOrEqual(t, len(got.Turns), MaxTurns, "must respect MaxTurns hard cap")
	assert.Equal(t, int64(1), got.Turns[0].Seq, "first user turn must be preserved at the hard cap")
}

func TestBuild_HardContentSizeCap(t *testing.T) {
	huge := strings.Repeat("z", MaxContentBytes*2)

	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "ok"},
		{Seq: 2, Role: RoleAssistantText, Content: huge},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)
	require.Len(t, got.Turns, 2)
	assert.LessOrEqual(t, len(got.Turns[1].Content), MaxContentBytes,
		"per-content hard cap must be enforced")
	assert.Contains(t, got.Turns[1].Content, truncationMarker)
}

func TestBuild_OrigSeqIsLastInputSeq(t *testing.T) {
	in := []Message{
		{Seq: 5, Role: RoleUser, Content: "a"},
		{Seq: 9, Role: RoleAssistantText, Content: "b"},
		{Seq: 42, Role: RoleUser, Content: "c"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.OrigSeq, "OrigSeq must equal the max seq of the input")
}

func TestBuild_AllRehydrationPhase_ReturnsNil(t *testing.T) {
	msgs := []Message{
		{Seq: 1, Role: RoleAssistantThinking, Content: "x", RehydrationPhase: true},
		{Seq: 2, Role: RoleToolCall, Content: "y", RehydrationPhase: true},
	}
	out := Build(msgs, BuildOpts{BudgetTokens: 1000})
	assert.Nil(t, out, "all-rehydration-phase messages should produce nil")
}

func TestBuild_ExactlyAtBudget(t *testing.T) {
	// Two small messages well within budget. Clipped should be false.
	msgs := []Message{
		{Seq: 1, Role: RoleUser, Content: "hello"},
		{Seq: 2, Role: RoleAssistantText, Content: "hi"},
	}
	out := Build(msgs, BuildOpts{BudgetTokens: 1_000_000})
	require.NotNil(t, out)
	require.Len(t, out.Turns, 2)
	assert.False(t, out.Clipped, "messages well within budget should not be clipped")
}

func TestBuild_FirstUserAndLastKCollision(t *testing.T) {
	// 5 messages: msg 1 is user; the K=20 last-tail trivially includes all 5
	// including msg 1. Verify msg 1 appears exactly once (no duplication from the
	// first-user pin).
	msgs := []Message{
		{Seq: 1, Role: RoleUser, Content: "first user"},
		{Seq: 2, Role: RoleAssistantText, Content: "a"},
		{Seq: 3, Role: RoleUser, Content: "b"},
		{Seq: 4, Role: RoleAssistantText, Content: "c"},
		{Seq: 5, Role: RoleUser, Content: "d"},
	}
	out := Build(msgs, BuildOpts{BudgetTokens: 1_000_000})
	require.NotNil(t, out)
	require.Len(t, out.Turns, 5)

	// Verify Seq 1 appears exactly once.
	firstUserCount := 0

	for _, m := range out.Turns {
		if m.Seq == 1 {
			firstUserCount++
		}
	}

	assert.Equal(t, 1, firstUserCount,
		"first user message must appear exactly once (no duplication from pin)")
}

// TestBuild_SkipsClearedMessages verifies that rehydration_phase=true rows
// — the marker stamped by Manager.ClearContext — are excluded from the
// resume payload while phase=false rows pass through normally. This is
// the wire-side guarantee that a cleared session does not re-feed pre-clear
// turns into the runner on the next cold reopen.
func TestBuild_SkipsClearedMessages(t *testing.T) {
	in := []Message{
		{Seq: 1, Role: RoleUser, Content: "pre-clear turn", RehydrationPhase: true},
		{Seq: 2, Role: RoleAssistantText, Content: "pre-clear reply", RehydrationPhase: true},
		{Seq: 3, Role: RoleSystem, Content: "Context cleared", RehydrationPhase: true},
		{Seq: 4, Role: RoleUser, Content: "post-clear turn"},
		{Seq: 5, Role: RoleAssistantText, Content: "post-clear reply"},
	}

	got := Build(in, BuildOpts{})
	require.NotNil(t, got)

	assert.Equal(t, []int64{4, 5}, seqsOf(got.Turns),
		"only post-clear rows must appear in the resume payload")
}

func rolesOf(turns []ResumeTurn) []string {
	out := make([]string, len(turns))
	for i, t := range turns {
		out[i] = t.Role
	}

	return out
}

func seqsOf(turns []ResumeTurn) []int64 {
	out := make([]int64, len(turns))
	for i, t := range turns {
		out[i] = t.Seq
	}

	return out
}
