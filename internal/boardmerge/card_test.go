package boardmerge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func ts(sec int) time.Time { return time.Date(2026, 9, 3, 12, 0, sec, 0, time.UTC) }

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
			"bool changed on both later wins",
			func(c *board.Card) { c.Autonomous = true; c.Updated = ts(1) },
			func(c *board.Card) { c.Vetted = true; c.Updated = ts(4) },
			func(t *testing.T, got *board.Card, res []Resolution) {
				assert.True(t, got.Autonomous)
				assert.True(t, got.Vetted)
				assert.Empty(t, res)
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
			func(c *board.Card) { c.LastHeartbeat = ptr(ts(3)) },
			func(c *board.Card) { c.LastHeartbeat = ptr(ts(7)) },
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
			func(c *board.Card) { c.DependenciesMet = ptr(true); c.SubtaskCostUSD = 4 },
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

func ptr[T any](v T) *T { return &v }

func TestMergeCards_TerminalWinsOverOneSidedReopen(t *testing.T) {
	b, o, th := baseCard(), baseCard(), baseCard()
	b.State, o.State, th.State = "done", "done", "todo"
	o.Priority = "high"

	got, res := mergeCards(b, o, th, "alpha", testCtx())
	assert.Equal(t, "done", got.State)
	assert.Equal(t, "high", got.Priority)
	require.Len(t, res, 1)
	assert.Equal(t, RuleTerminalWins, res[0].Rule)
	assert.Equal(t, board.MergeAction, got.ActivityLog[len(got.ActivityLog)-1].Action)
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
