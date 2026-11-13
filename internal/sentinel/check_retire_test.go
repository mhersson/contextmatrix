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
