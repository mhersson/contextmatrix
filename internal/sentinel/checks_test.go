package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestVetFixRunsEveryCheck(t *testing.T) {
	card := &board.Card{ID: "P-1", Title: "t"}
	c := fixContext{
		Card:     card,
		Snapshot: board.TakeEditSnapshot(card),
		Fix:      protocol.CardFix{Section: "## bad"},
		Check:    &protocol.CheckResult{},
	}

	assert.Len(t, vetFix(c), 1, "section only")

	c.Fix = protocol.CardFix{}
	c.Fix.RetireSubtasks = nil
	assert.NotEmpty(t, vetFix(c), "shape")

	c.Fix = protocol.CardFix{Section: "## bad", RetireSubtasks: []string{"P-9"}}
	assert.GreaterOrEqual(t, len(vetFix(c)), 3, "section, shape and retire")
}

func TestVetForwardSkipsHealOnlyChecks(t *testing.T) {
	c := fixContext{
		Card:  &board.Card{ID: "P-1"},
		Fix:   protocol.CardFix{Section: "fine"},
		Check: &protocol.CheckResult{},
	}

	assert.Empty(t, vetForward(c), "no snapshot and no retire needed for a forward target")
	assert.NotEmpty(t, vetFix(c), "a heal needs its snapshot")
}
