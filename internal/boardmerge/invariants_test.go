package boardmerge

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestApplyInvariants(t *testing.T) {
	c := testCtx()
	c.CardExists = func(_, id string) bool { return id == "ALPHA-000" }

	tests := []struct {
		name  string
		card  func() *board.Card
		check func(t *testing.T, got *board.Card, res []Resolution)
	}{
		{"not_planned clears claim", func() *board.Card {
			k := baseCard()
			k.State = "not_planned"
			k.AssignedAgent = "x"
			now := ts(1)
			k.LastHeartbeat = &now

			return k
		}, func(t *testing.T, got *board.Card, res []Resolution) {
			assert.Empty(t, got.AssignedAgent)
			assert.Nil(t, got.LastHeartbeat)
			assert.Equal(t, RuleInvariantRepair, res[0].Rule)
		}},
		{"done retains claim", func() *board.Card {
			// enforceTerminalStateInvariants (internal/service/service_transitions.go)
			// deliberately does NOT clear the claim on done: the holder keeps it so
			// ReleaseCard can flush deferred commits, and a re-claim by the holder is
			// a heartbeat refresh rather than a new agent taking finished work.
			k := baseCard()
			k.State = "done"
			k.AssignedAgent = "x"
			now := ts(1)
			k.LastHeartbeat = &now

			return k
		}, func(t *testing.T, got *board.Card, res []Resolution) {
			assert.Equal(t, "x", got.AssignedAgent)
			assert.NotNil(t, got.LastHeartbeat)
			assert.Empty(t, res)
		}},
		{"dangling parent cleared and type reset", func() *board.Card {
			k := baseCard()
			k.Parent = "ALPHA-404"
			k.Type = board.SubtaskType

			return k
		}, func(t *testing.T, got *board.Card, _ []Resolution) {
			assert.Empty(t, got.Parent)
			assert.Equal(t, "task", got.Type)
		}},
		{"parent forces subtask type", func() *board.Card {
			k := baseCard()
			k.Parent = "ALPHA-000"
			k.Type = "task"

			return k
		}, func(t *testing.T, got *board.Card, _ []Resolution) {
			assert.Equal(t, board.SubtaskType, got.Type)
		}},
		{"dangling refs dropped", func() *board.Card {
			k := baseCard()
			k.DependsOn = []string{"ALPHA-000", "ALPHA-404"}
			k.Subtasks = []string{"ALPHA-404"}

			return k
		}, func(t *testing.T, got *board.Card, _ []Resolution) {
			assert.Equal(t, []string{"ALPHA-000"}, got.DependsOn)
			assert.Empty(t, got.Subtasks)
		}},
		{"invalid falls back to theirs", func() *board.Card {
			k := baseCard()
			k.Priority = "urgent"

			return k
		}, func(t *testing.T, got *board.Card, res []Resolution) {
			assert.Equal(t, "medium", got.Priority)
			assert.Equal(t, RuleInvariantFallback, res[len(res)-1].Rule)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, res := applyInvariants(tt.card(), baseCard(), "alpha", c)
			tt.check(t, got, res)
		})
	}
}

func TestApplyInvariants_TrimsLogWhenProjectConfigFails(t *testing.T) {
	c := testCtx()
	c.Project = func(string) (*board.ProjectConfig, error) { return nil, errors.New("project config unavailable") }

	k := baseCard()
	k.State = "not_planned"
	k.AssignedAgent = "x"
	now := ts(1)
	k.LastHeartbeat = &now
	k.ActivityLog = make([]board.ActivityEntry, board.MaxActivityLogEntries+10)

	for i := range k.ActivityLog {
		k.ActivityLog[i] = board.ActivityEntry{Agent: "x", Action: "log", Timestamp: ts(i)}
	}

	got, res := applyInvariants(k, baseCard(), "alpha", c)

	assert.Empty(t, got.AssignedAgent)
	assert.Nil(t, got.LastHeartbeat)
	require.NotEmpty(t, res)
	assert.Equal(t, RuleInvariantRepair, res[0].Rule)
	assert.LessOrEqual(t, len(got.ActivityLog), board.MaxActivityLogEntries)
}

func TestApplyInvariants_FallbackCopiesTheirsActivityLog(t *testing.T) {
	c := testCtx()

	theirs := baseCard()
	card := baseCard()
	card.Priority = "urgent"

	got, res := applyInvariants(card, theirs, "alpha", c)

	assert.NotEmpty(t, res)
	assert.Equal(t, RuleInvariantFallback, res[len(res)-1].Rule)
	// theirs must not have been mutated by the fallback's own audit entry.
	assert.Len(t, theirs.ActivityLog, 1)
	assert.Greater(t, len(got.ActivityLog), len(theirs.ActivityLog))
}
