package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckRetire(t *testing.T) {
	sub := func(mod func(*board.Card)) *board.Card {
		s := &board.Card{ID: "P-2", Parent: "P-1", State: board.StateTodo}
		mod(s)

		return s
	}

	tests := []struct {
		name string
		sub  *board.Card
		bad  bool
	}{
		{"todo unclaimed", sub(func(*board.Card) {}), false},
		{"in progress", sub(func(s *board.Card) { s.State = board.StateInProgress }), true},
		{"claimed", sub(func(s *board.Card) { s.AssignedAgent = "a" }), true},
		{"worker queued", sub(func(s *board.Card) { s.WorkerStatus = "queued" }), true},
		{"worker running", sub(func(s *board.Card) { s.WorkerStatus = "running" }), true},
		{"another card's subtask", sub(func(s *board.Card) { s.Parent = "P-9" }), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRetire(fixContext{
				Card:     &board.Card{ID: "P-1"},
				Subtasks: []*board.Card{tt.sub},
				Fix:      protocol.CardFix{RetireSubtasks: []string{"P-2"}},
			})
			assert.Equal(t, tt.bad, len(got) > 0, "%v", got)
		})
	}

	t.Run("unknown ID", func(t *testing.T) {
		got := checkRetire(fixContext{
			Card: &board.Card{ID: "P-1"},
			Fix:  protocol.CardFix{RetireSubtasks: []string{"P-5"}},
		})
		assert.Len(t, got, 1)
	})
}

func TestCheckRetireStartedSubtasks(t *testing.T) {
	sub := func(id, state string) *board.Card {
		return &board.Card{ID: id, Parent: "P-1", State: state}
	}

	tests := []struct {
		name   string
		other  string
		retire []string
		bad    bool
	}{
		{"retire, others todo", board.StateTodo, []string{"P-2"}, false},
		{"retire, another in progress", board.StateInProgress, []string{"P-2"}, true},
		{"retire, another done", board.StateDone, []string{"P-2"}, true},
		{"retire, another not planned", board.StateNotPlanned, []string{"P-2"}, false},
		{"no retire, a started subtask", board.StateInProgress, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRetire(fixContext{
				Card:     &board.Card{ID: "P-1"},
				Subtasks: []*board.Card{sub("P-2", board.StateTodo), sub("P-3", tt.other)},
				Fix:      protocol.CardFix{RetireSubtasks: tt.retire},
			})
			assert.Equal(t, tt.bad, len(got) > 0, "%v", got)
		})
	}
}
