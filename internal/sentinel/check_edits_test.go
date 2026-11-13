package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckEdits(t *testing.T) {
	base := func() *board.Card {
		return &board.Card{ID: "P-1", Title: "t", Body: "intro\n\n## A\na\n\n## B\nb\n"}
	}

	tests := []struct {
		name string
		edit func(*board.Card)
		want []string
	}{
		{"unchanged", func(*board.Card) {}, nil},
		{"title", func(c *board.Card) { c.Title = "u" }, []string{"title edited"}},
		{"verify", func(c *board.Card) { c.Verify = &board.VerifyConfig{Command: "x"} }, []string{"verify edited"}},
		{"autonomous", func(c *board.Card) { c.Autonomous = true }, []string{"autonomous changed"}},
		{"intro", func(c *board.Card) { c.Body = "other\n\n## A\na\n\n## B\nb\n" }, []string{"intro edited"}},
		{"section edited", func(c *board.Card) { c.Body = "intro\n\n## A\nz\n\n## B\nb\n" }, []string{`section "A" edited`}},
		{"section added", func(c *board.Card) { c.Body = "intro\n\n## C\nc\n\n## A\na\n\n## B\nb\n" }, []string{`section "C" added`}},
		{"section removed", func(c *board.Card) { c.Body = "intro\n\n## B\nb\n" }, []string{`section "A" removed`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := board.TakeEditSnapshot(base())
			card := base()
			tt.edit(card)

			assert.Equal(t, tt.want, checkEdits(fixContext{Card: card, Snapshot: snap}))
		})
	}

	t.Run("no snapshot", func(t *testing.T) {
		assert.Equal(t, []string{"no launch snapshot"}, checkEdits(fixContext{Card: base()}))
	})
}
