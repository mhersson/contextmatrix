package boardmerge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func ts(sec int) time.Time { return time.Date(2026, 9, 3, 12, 0, sec, 0, time.UTC) }

func hb(sec int) *time.Time {
	t := ts(sec)

	return &t
}

func baseCard() *board.Card {
	return &board.Card{
		ID: "ALPHA-001", Project: "alpha", Title: "t", Type: "task", State: "todo",
		Priority: "medium", Labels: []string{"a"}, DependsOn: []string{"ALPHA-000"},
		Created: ts(0), Updated: ts(0), Body: "line1\nline2\n",
		ActivityLog: []board.ActivityEntry{{Agent: "x", Action: "created", Timestamp: ts(0)}},
	}
}

func serialize(t *testing.T, c *board.Card) []byte {
	t.Helper()

	b, err := board.SerializeCard(c)
	require.NoError(t, err)

	return b
}

func parse(t *testing.T, b []byte) *board.Card {
	t.Helper()

	c, err := board.ParseCard(b)
	require.NoError(t, err)

	return c
}

func testCtx() Context {
	return Context{
		Instance: "lap-a", Now: func() time.Time { return ts(99) },
		MergeBody:  func(_, _, _ string) (string, bool) { return "", false },
		CardExists: func(_, _ string) bool { return true },
		Project: func(_ string) (*board.ProjectConfig, error) {
			return &board.ProjectConfig{
				Name: "alpha", Prefix: "ALPHA", NextID: 5,
				States:      []string{"todo", "in_progress", "done", "stalled", "not_planned"},
				Types:       []string{"task", "bug"},
				Priorities:  []string{"low", "medium", "high"},
				Transitions: map[string][]string{"stalled": {"todo"}, "not_planned": {"todo"}},
			}, nil
		},
	}
}

func TestMergeCards(t *testing.T) {
	tests := []struct {
		name   string
		ours   func(c *board.Card)
		theirs func(c *board.Card)
		check  func(t *testing.T, got *board.Card, res []Resolution)
	}{
		{
			"disjoint scalars",
			func(c *board.Card) { c.Priority = "high"; c.Updated = ts(1) },
			func(c *board.Card) { c.Title = "new"; c.Updated = ts(2) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "high", got.Priority)
				assert.Equal(t, "new", got.Title)
				assert.Equal(t, ts(2), got.Updated)
				assert.Empty(t, res)
			},
		},
		{
			"both change same scalar later wins",
			func(c *board.Card) { c.Priority = "high"; c.Updated = ts(5) },
			func(c *board.Card) { c.Priority = "low"; c.Updated = ts(2) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "high", got.Priority)
				require.Len(t, res, 1)
				assert.Equal(t, RuleLaterUpdated, res[0].Rule)
				assert.Equal(t, board.MergeAction, got.ActivityLog[len(got.ActivityLog)-1].Action)
			},
		},
		{
			"terminal beats later non-terminal",
			func(c *board.Card) { c.State = "in_progress"; c.Updated = ts(9) },
			func(c *board.Card) { c.State = "not_planned"; c.Updated = ts(2) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "not_planned", got.State)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleTerminalWins, res[0].Rule)
				// The local in_progress lost, even though it was updated last.
				assert.Contains(t, lastAudit(t, got).Message, "from local")
			},
		},
		{
			"terminal on one side only is not an override",
			func(_ *board.Card) {},
			func(c *board.Card) { c.State = "done" },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "done", got.State)
				assert.Empty(t, res)
			},
		},
		{
			"sets union",
			func(c *board.Card) { c.Labels = []string{"a", "b"}; c.DependsOn = nil },
			func(c *board.Card) { c.Labels = []string{"c"} },
			func(t *testing.T, got *board.Card, _ []Resolution) {
				assert.ElementsMatch(t, []string{"b", "c"}, got.Labels)
				assert.Empty(t, got.DependsOn)
			},
		},
		{
			"skills union when both declare a list",
			func(c *board.Card) { c.Skills = &[]string{"a", "b"} },
			func(c *board.Card) { c.Skills = &[]string{"a", "c"} },
			func(t *testing.T, got *board.Card, res []Resolution) {
				require.NotNil(t, got.Skills)
				assert.ElementsMatch(t, []string{"a", "b", "c"}, *got.Skills)
				assert.Empty(t, res)
			},
		},
		{
			"skills declared on one side only",
			func(_ *board.Card) {},
			func(c *board.Card) { c.Skills = &[]string{"a"} },
			func(t *testing.T, got *board.Card, res []Resolution) {
				require.NotNil(t, got.Skills)
				assert.Equal(t, []string{"a"}, *got.Skills)
				assert.Empty(t, res)
			},
		},
		{
			"bools changed on different sides",
			func(c *board.Card) { c.Autonomous = true; c.Updated = ts(1) },
			func(c *board.Card) { c.Vetted = true; c.Updated = ts(4) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.True(t, got.Autonomous)
				assert.True(t, got.Vetted)
				assert.Empty(t, res)
			},
		},
		{
			"counter changed on both later wins",
			func(c *board.Card) { c.BestOfN = 2; c.Updated = ts(6) },
			func(c *board.Card) { c.BestOfN = 3 },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, 2, got.BestOfN)
				require.Len(t, res, 1)
				assert.Equal(t, RuleLaterUpdated, res[0].Rule)
				assert.Contains(t, lastAudit(t, got).Message, "from remote")
			},
		},
		{
			"review attempts take the max",
			func(c *board.Card) { c.ReviewAttempts = 3 },
			func(c *board.Card) { c.ReviewAttempts = 1 },
			func(t *testing.T, got *board.Card, _ []Resolution) {
				assert.Equal(t, 3, got.ReviewAttempts)
			},
		},
		{
			"newest heartbeat wins",
			func(c *board.Card) { c.LastHeartbeat = new(ts(3)) },
			func(c *board.Card) { c.LastHeartbeat = new(ts(7)) },
			func(t *testing.T, got *board.Card, _ []Resolution) {
				require.NotNil(t, got.LastHeartbeat)
				assert.Equal(t, ts(7), *got.LastHeartbeat)
			},
		},
		{
			"custom key changed on both later wins",
			func(c *board.Card) { c.Custom = map[string]any{"k": "ours"}; c.Updated = ts(6) },
			func(c *board.Card) { c.Custom = map[string]any{"k": "theirs", "other": 1} },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "ours", got.Custom["k"])
				assert.Equal(t, 1, got.Custom["other"])
				require.Len(t, res, 1)
				assert.Equal(t, RuleLaterUpdated, res[0].Rule)
			},
		},
		{
			"additive usage",
			func(c *board.Card) {
				c.TokenUsage = &board.TokenUsage{PromptTokens: 10, EstimatedCostUSD: 1}
				c.UsageBreakdown = []board.UsageBucket{{Agent: "x", Model: "m", PromptTokens: 10, CostUSD: 1, CostSource: "estimated"}}
			},
			func(c *board.Card) {
				c.TokenUsage = &board.TokenUsage{PromptTokens: 20, EstimatedCostUSD: 2}
				c.UsageBreakdown = []board.UsageBucket{{Agent: "x", Model: "m", PromptTokens: 20, CostUSD: 2, CostSource: "actual", CountsSource: "collector"}}
			},
			func(t *testing.T, got *board.Card, _ []Resolution) {
				assert.Equal(t, int64(30), got.TokenUsage.PromptTokens)
				assert.InDelta(t, 3.0, got.TokenUsage.EstimatedCostUSD, 1e-9)
				require.Len(t, got.UsageBreakdown, 1)
				assert.Equal(t, int64(30), got.UsageBreakdown[0].PromptTokens)
				assert.Equal(t, "actual", got.UsageBreakdown[0].CostSource)
				assert.Equal(t, "collector", got.UsageBreakdown[0].CountsSource)
			},
		},
		{
			"identical seed bucket counts once",
			func(c *board.Card) {
				c.UsageBreakdown = []board.UsageBucket{{Agent: "legacy", Model: "m", PromptTokens: 7, CostUSD: 1}}
			},
			func(c *board.Card) {
				c.UsageBreakdown = []board.UsageBucket{{Agent: "legacy", Model: "m", PromptTokens: 7, CostUSD: 1}}
			},
			func(t *testing.T, got *board.Card, _ []Resolution) {
				require.Len(t, got.UsageBreakdown, 1)
				assert.Equal(t, int64(7), got.UsageBreakdown[0].PromptTokens)
			},
		},
		{
			"activity union sorted and trimmed",
			func(c *board.Card) {
				c.ActivityLog = append(c.ActivityLog, board.ActivityEntry{Agent: "a", Action: "log", Message: "ours", Timestamp: ts(3)})
			},
			func(c *board.Card) {
				c.ActivityLog = append(c.ActivityLog, board.ActivityEntry{Agent: "b", Action: "log", Message: "theirs", Timestamp: ts(2)})
			},
			func(t *testing.T, got *board.Card, _ []Resolution) {
				msgs := []string{}
				for _, e := range got.ActivityLog {
					msgs = append(msgs, e.Message)
				}

				assert.Equal(t, []string{"", "theirs", "ours"}, msgs)
			},
		},
		{
			"computed fields are not carried across",
			func(c *board.Card) { c.DependenciesMet = new(true); c.SubtaskCostUSD = 4 },
			func(c *board.Card) { c.InPlaybooks = []string{"release"}; c.SubtaskCostHasEstimates = true },
			func(t *testing.T, got *board.Card, _ []Resolution) {
				assert.Nil(t, got.DependenciesMet)
				assert.Nil(t, got.InPlaybooks)
				assert.Zero(t, got.SubtaskCostUSD)
				assert.False(t, got.SubtaskCostHasEstimates)
			},
		},
		{
			"body one-sided",
			func(c *board.Card) { c.Body = "line1\nline2\nours\n" },
			func(_ *board.Card) {},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "line1\nline2\nours\n", got.Body)
				assert.Empty(t, res)
			},
		},
		{
			"body conflict later wins with audit",
			func(c *board.Card) { c.Body = "ours\n"; c.Updated = ts(1) },
			func(c *board.Card) { c.Body = "theirs\n"; c.Updated = ts(4) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "theirs\n", got.Body)
				require.Len(t, res, 1)
				assert.Equal(t, RuleBodyLaterUpdated, res[0].Rule)
				assert.Equal(t, board.MergeAction, got.ActivityLog[len(got.ActivityLog)-1].Action)
			},
		},
		{
			"higher epoch supplies the whole claim tuple",
			func(c *board.Card) {
				c.State, c.AssignedAgent, c.ClaimEpoch, c.Updated = "stalled", "", 1, ts(9)
			},
			func(c *board.Card) {
				c.State, c.AssignedAgent, c.ClaimedVia, c.ClaimedAt = "in_progress", "agent-ALPHA-001", "lap-b", hb(2)
				c.ClaimEpoch, c.WorkerStatus, c.Phase, c.LastHeartbeat, c.Updated = 2, "running", "execute", hb(3), ts(3)
			},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "in_progress", got.State, "the takeover at the higher epoch wins even though the stall is newer")
				assert.Equal(t, "agent-ALPHA-001", got.AssignedAgent)
				assert.Equal(t, "lap-b", got.ClaimedVia)
				assert.Equal(t, 2, got.ClaimEpoch)
				assert.Equal(t, "running", got.WorkerStatus)
				assert.Equal(t, "execute", got.Phase)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleEpochWins, res[0].Rule)
				assert.Contains(t, lastAudit(t, got).Message, "from local")
			},
		},
		{
			"bare stall at the higher epoch loses to a terminal state",
			func(c *board.Card) {
				c.State, c.AssignedAgent, c.ClaimEpoch, c.Updated = "stalled", "", 3, ts(9)
			},
			func(c *board.Card) {
				c.State, c.AssignedAgent, c.ClaimedVia, c.ClaimEpoch, c.Updated = "done", "x", "lap-b", 2, ts(2)
			},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "done", got.State)
				assert.Equal(t, "x", got.AssignedAgent, "done keeps its claim until release")
				assert.Equal(t, 2, got.ClaimEpoch)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleTerminalOverStall, res[0].Rule)
			},
		},
		{
			"double claim goes to the earlier claimed_at",
			func(c *board.Card) {
				c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.ClaimEpoch, c.LastHeartbeat, c.Updated = "agent-ALPHA-001", "lap-a", hb(5), 1, hb(5), ts(5)
			},
			func(c *board.Card) {
				c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.ClaimEpoch, c.LastHeartbeat, c.Updated = "agent-ALPHA-001", "lap-b", hb(3), 1, hb(3), ts(3)
			},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "lap-b", got.ClaimedVia)
				assert.Equal(t, hb(3), got.ClaimedAt)
				assert.Equal(t, 1, got.ClaimEpoch)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleDoubleClaim, res[0].Rule)
				assert.Contains(t, lastAudit(t, got).Message, "from local")
			},
		},
		{
			"same claim on both sides keeps the newest heartbeat without an audit",
			func(c *board.Card) {
				c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.ClaimEpoch, c.LastHeartbeat = "a", "lap-a", hb(1), 1, hb(5)
			},
			func(c *board.Card) {
				c.AssignedAgent, c.ClaimedVia, c.ClaimedAt, c.ClaimEpoch, c.LastHeartbeat = "a", "lap-a", hb(1), 1, hb(3)
			},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, hb(5), got.LastHeartbeat)
				assert.Equal(t, 1, got.ClaimEpoch)
				assert.Empty(t, res)
			},
		},
		{
			"epoch wins without an audit when the losing side never touched the claim",
			func(c *board.Card) {
				c.State, c.AssignedAgent, c.ClaimedVia = "in_progress", "agent-ALPHA-001", "lap-a"
				c.ClaimedAt, c.LastHeartbeat, c.ClaimEpoch, c.Updated = hb(5), hb(5), 1, ts(5)
			},
			func(c *board.Card) { c.Labels = []string{"a", "hot"}; c.Updated = ts(3) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "agent-ALPHA-001", got.AssignedAgent)
				assert.Equal(t, "lap-a", got.ClaimedVia)
				assert.Equal(t, 1, got.ClaimEpoch)
				assert.ElementsMatch(t, []string{"a", "hot"}, got.Labels)
				assert.Empty(t, res, "the remote left the claim alone, so it was overridden in nothing")
			},
		},
		{
			"release at a higher epoch beats a stale heartbeat",
			func(c *board.Card) {
				c.AssignedAgent, c.ClaimedVia, c.ClaimEpoch, c.LastHeartbeat, c.Updated = "a", "lap-a", 1, hb(8), ts(8)
			},
			func(c *board.Card) {
				c.State, c.ClaimEpoch, c.Updated = "done", 3, ts(6)
			},
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Empty(t, got.AssignedAgent)
				assert.Equal(t, "done", got.State)
				assert.Equal(t, 3, got.ClaimEpoch)
				assert.Equal(t, RuleEpochWins, res[0].Rule)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, o, th := baseCard(), baseCard(), baseCard()
			tt.ours(o)
			tt.theirs(th)

			got, res := mergeCards(b, o, th, "alpha", testCtx())
			tt.check(t, got, res)
		})
	}
}

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

// TestMergeCards_EqualEpochClaims covers the region where both sides raised
// the epoch from the same claimed base. An active claim beats a tuple emptied
// into a non-terminal state; two emptied tuples converge on the empty one.
func TestMergeCards_EqualEpochClaims(t *testing.T) {
	claimed := func(c *board.Card) {
		c.State, c.AssignedAgent, c.ClaimedVia = "in_progress", "agent-ALPHA-001", "lap-a"
		c.ClaimedAt, c.LastHeartbeat, c.ClaimEpoch = hb(1), hb(1), 1
	}
	// A heartbeat timeout writes a bare stall and bumps the epoch.
	stall := func(updated int) func(*board.Card) {
		return func(c *board.Card) { c.State, c.ClaimEpoch, c.Updated = "stalled", 2, ts(updated) }
	}
	// A force-release empties the tuple and leaves the state alone.
	release := func(c *board.Card) { c.State, c.ClaimEpoch, c.Updated = "todo", 2, ts(9) }
	// A takeover claims through another instance at the same epoch.
	takeover := func(via string, updated int) func(*board.Card) {
		return func(c *board.Card) {
			c.State, c.AssignedAgent, c.ClaimedVia = "in_progress", "agent-ALPHA-001", via
			c.ClaimedAt, c.LastHeartbeat, c.ClaimEpoch, c.Updated = hb(4), hb(4), 2, ts(updated)
		}
	}

	tests := []struct {
		name   string
		ours   func(c *board.Card)
		theirs func(c *board.Card)
		check  func(t *testing.T, got *board.Card, res []Resolution)
	}{
		{
			"a remote takeover survives our bare stall",
			stall(9),
			takeover("lap-b", 4),
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "in_progress", got.State, "the running takeover wins although our stall is newer")
				assert.Equal(t, "agent-ALPHA-001", got.AssignedAgent)
				assert.Equal(t, "lap-b", got.ClaimedVia)
				assert.Equal(t, hb(4), got.ClaimedAt)
				assert.Equal(t, 2, got.ClaimEpoch)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleActiveOverRelease, res[0].Rule)
				assert.Contains(t, lastAudit(t, got).Message, "from local")
			},
		},
		{
			"our takeover survives a remote bare stall",
			takeover("lap-a", 4),
			stall(9),
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "in_progress", got.State)
				assert.Equal(t, "lap-a", got.ClaimedVia)
				assert.Equal(t, hb(4), got.ClaimedAt)
				assert.Equal(t, 2, got.ClaimEpoch)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleActiveOverRelease, res[0].Rule)
				assert.Contains(t, lastAudit(t, got).Message, "from remote")
			},
		},
		{
			"a remote takeover survives our force-release",
			release,
			takeover("lap-b", 4),
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "in_progress", got.State)
				assert.Equal(t, "agent-ALPHA-001", got.AssignedAgent)
				assert.Equal(t, "lap-b", got.ClaimedVia)
				assert.Equal(t, 2, got.ClaimEpoch)
				require.NotEmpty(t, res)
				assert.Equal(t, RuleActiveOverRelease, res[0].Rule)
			},
		},
		{
			"two bare stalls converge on the empty tuple",
			stall(9),
			stall(4),
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.Equal(t, "stalled", got.State)
				assert.Empty(t, got.AssignedAgent)
				assert.Empty(t, got.ClaimedVia)
				assert.Nil(t, got.ClaimedAt)
				assert.Nil(t, got.LastHeartbeat)
				assert.Equal(t, 2, got.ClaimEpoch)
				assert.NotContains(t, ruleNames(res), RuleActiveOverRelease)
				assert.Empty(t, res)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, o, th := baseCard(), baseCard(), baseCard()
			claimed(b)
			tt.ours(o)
			tt.theirs(th)

			got, res := mergeCards(b, o, th, "alpha", testCtx())
			tt.check(t, got, res)
		})
	}
}

func ruleNames(res []Resolution) []string {
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, r.Rule)
	}

	return out
}

func TestMergeCards_TerminalWinsOverOneSidedReopen(t *testing.T) {
	b, o, th := baseCard(), baseCard(), baseCard()
	b.State, o.State, th.State = "done", "done", "todo"
	o.Priority = "high"

	got, res := mergeCards(b, o, th, "alpha", testCtx())
	assert.Equal(t, "done", got.State)
	assert.Equal(t, "high", got.Priority)
	require.Len(t, res, 1)
	assert.Equal(t, RuleTerminalWins, res[0].Rule)
	// The remote reopen lost, even though neither side was updated last.
	assert.Contains(t, lastAudit(t, got).Message, "from remote")
}

func TestMergeCards_SkillsClearedOnOneSide(t *testing.T) {
	b, o, th := baseCard(), baseCard(), baseCard()
	b.Skills, th.Skills = &[]string{"a"}, &[]string{"a"}
	o.Skills = nil

	got, res := mergeCards(b, o, th, "alpha", testCtx())
	assert.Nil(t, got.Skills)
	assert.Empty(t, res)
}

func TestMergeCards_DoesNotMutateInputs(t *testing.T) {
	b, o, th := baseCard(), baseCard(), baseCard()
	o.Priority, o.Updated = "high", ts(5)
	th.Priority, th.Body = "low", "theirs\n"

	_, res := mergeCards(b, o, th, "alpha", testCtx())
	require.NotEmpty(t, res)
	assert.Len(t, o.ActivityLog, 1)
	assert.Len(t, th.ActivityLog, 1)
	assert.Equal(t, []string{"a"}, o.Labels)
	assert.Equal(t, "high", o.Priority)
	assert.Equal(t, "low", th.Priority)
}

func lastAudit(t *testing.T, c *board.Card) board.ActivityEntry {
	t.Helper()

	require.NotEmpty(t, c.ActivityLog)

	last := c.ActivityLog[len(c.ActivityLog)-1]
	require.Equal(t, board.MergeAction, last.Action)

	return last
}

func TestMergeCards_BodyConflictNamesTheLosingCommit(t *testing.T) {
	c := testCtx()
	c.OursCommit, c.TheirsCommit = "aaa111", "bbb222"

	b, o, th := baseCard(), baseCard(), baseCard()
	o.Body, o.Updated = "ours\n", ts(8)
	th.Body = "theirs\n"

	got, res := mergeCards(b, o, th, "alpha", c)
	assert.Equal(t, "ours\n", got.Body)
	require.Len(t, res, 1)

	// The audit must point at the commit still holding the overridden text,
	// which is the losing side's, never the winner's.
	assert.Contains(t, res[0].Detail, "bbb222")
	assert.NotContains(t, res[0].Detail, "aaa111")
	assert.Contains(t, lastAudit(t, got).Message, "bbb222")
	assert.Contains(t, lastAudit(t, got).Message, "from remote")
}

func TestMergeCards_Renames(t *testing.T) {
	c := testCtx()
	c.Renames = map[string]string{"alpha/ALPHA-001": "ALPHA-005"}

	t.Run("references we add follow the re-mint", func(t *testing.T) {
		b, o, th := baseCard(), baseCard(), baseCard()
		b.DependsOn, th.DependsOn = nil, nil
		o.DependsOn = []string{"ALPHA-001"}
		o.Parent = "ALPHA-001"
		o.Subtasks = []string{"ALPHA-001"}

		got, res := mergeCards(b, o, th, "alpha", c)
		assert.Equal(t, "ALPHA-005", got.Parent)
		assert.Equal(t, []string{"ALPHA-005"}, got.DependsOn)
		assert.Equal(t, []string{"ALPHA-005"}, got.Subtasks)
		assert.Empty(t, res)
	})
	t.Run("references already in the ancestor are untouched", func(t *testing.T) {
		b, o, th := baseCard(), baseCard(), baseCard()
		b.Parent, o.Parent, th.Parent = "ALPHA-001", "ALPHA-001", "ALPHA-001"
		b.DependsOn = []string{"ALPHA-001"}
		o.DependsOn, th.DependsOn = []string{"ALPHA-001"}, []string{"ALPHA-001"}

		got, _ := mergeCards(b, o, th, "alpha", c)
		assert.Equal(t, "ALPHA-001", got.Parent)
		assert.Equal(t, []string{"ALPHA-001"}, got.DependsOn)
	})
	t.Run("a rename for another project is ignored", func(t *testing.T) {
		other := c
		other.Renames = map[string]string{"beta/ALPHA-001": "BETA-005"}

		b, o, th := baseCard(), baseCard(), baseCard()
		b.DependsOn, th.DependsOn = nil, nil
		o.DependsOn = []string{"ALPHA-001"}
		o.Parent = "ALPHA-001"

		got, _ := mergeCards(b, o, th, "alpha", other)
		assert.Equal(t, "ALPHA-001", got.Parent)
		assert.Equal(t, []string{"ALPHA-001"}, got.DependsOn)
	})
}

func TestMergeCards_BodyCleanMerge(t *testing.T) {
	c := testCtx()
	c.MergeBody = func(_, _, _ string) (string, bool) { return "merged\n", true }

	b, o, th := baseCard(), baseCard(), baseCard()
	o.Body, th.Body = "o\n", "t\n"

	got, res := mergeCards(b, o, th, "alpha", c)
	assert.Equal(t, "merged\n", got.Body)
	assert.Empty(t, res)
}

func TestResolveCard_Shapes(t *testing.T) {
	c := testCtx()
	base := serialize(t, baseCard())
	o, th := baseCard(), baseCard()
	o.Title, th.Priority = "o", "high"
	ours, theirs := serialize(t, o), serialize(t, th)

	t.Run("three-way", func(t *testing.T) {
		out, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Base: base, Ours: ours, Theirs: theirs}, c)
		require.NoError(t, err)

		got := parse(t, out.Content)
		assert.Equal(t, "o", got.Title)
		assert.Equal(t, "high", got.Priority)
		require.Len(t, out.Resolutions, 1)
		assert.Equal(t, RuleFieldMerge, out.Resolutions[0].Rule)
	})
	t.Run("delete wins over modify", func(t *testing.T) {
		out, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Base: base, Ours: ours, Theirs: nil}, c)
		require.NoError(t, err)
		assert.True(t, out.Deleted)
		require.Len(t, out.Resolutions, 1)
		assert.Equal(t, RuleDeleteWins, out.Resolutions[0].Rule)
	})
	t.Run("unparseable ours takes theirs", func(t *testing.T) {
		out, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Base: base, Ours: []byte("garbage"), Theirs: theirs}, c)
		require.NoError(t, err)
		assert.Equal(t, theirs, out.Content)
		require.Len(t, out.Resolutions, 1)
		assert.Equal(t, RuleUnparseable, out.Resolutions[0].Rule)
	})
	t.Run("add add same source dedupes", func(t *testing.T) {
		o2, t2 := baseCard(), baseCard()
		o2.Source = &board.Source{System: "github", ExternalID: "42"}
		t2.Source = &board.Source{System: "github", ExternalID: "42"}

		out, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Ours: serialize(t, o2), Theirs: serialize(t, t2)}, c)
		require.NoError(t, err)
		require.Len(t, out.Resolutions, 1)
		assert.Equal(t, RuleSourceDedupe, out.Resolutions[0].Rule)
		assert.Empty(t, out.Extra)
	})
	t.Run("add add remints ours", func(t *testing.T) {
		c2 := c
		c2.MintID = func(string) (string, error) { return "ALPHA-005", nil }

		out, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Ours: ours, Theirs: theirs}, c2)
		require.NoError(t, err)
		assert.Equal(t, theirs, out.Content)
		require.Len(t, out.Extra, 1)
		assert.Equal(t, "alpha/tasks/ALPHA-005.md", out.Extra[0].Path)

		reminted := parse(t, out.Extra[0].Content)
		assert.Equal(t, "ALPHA-005", reminted.ID)
		assert.Equal(t, board.MergeAction, reminted.ActivityLog[len(reminted.ActivityLog)-1].Action)
		assert.Equal(t, "ALPHA-005", out.Renames["alpha/ALPHA-001"])
		require.Len(t, out.Resolutions, 1)
		assert.Equal(t, RuleAddAddRemint, out.Resolutions[0].Rule)
	})
	t.Run("add add without a minter errors", func(t *testing.T) {
		c2 := c
		c2.MintID = nil

		_, err := Resolve(Input{Path: "alpha/tasks/ALPHA-001.md", Ours: ours, Theirs: theirs}, c2)
		require.Error(t, err)
	})
}
