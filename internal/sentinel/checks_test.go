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
		Fix:      protocol.CardFix{Section: "```bad"},
		Check:    &protocol.CheckResult{},
	}

	assert.Len(t, vetFix(c), 1, "section only")

	c.Fix = protocol.CardFix{}
	c.Fix.RetireSubtasks = nil
	assert.NotEmpty(t, vetFix(c), "shape")

	c.Fix = protocol.CardFix{Section: "```bad", RetireSubtasks: []string{"P-9"}}
	assert.GreaterOrEqual(t, len(vetFix(c)), 3, "section, shape and retire")
}

func TestVetFixTagsTheSectionChecks(t *testing.T) {
	card := &board.Card{ID: "P-1", Title: "t"}
	c := fixContext{
		Card:     card,
		Snapshot: board.TakeEditSnapshot(card),
		Fix:      protocol.CardFix{Section: "```gen-tool", Verify: &protocol.VerifyConfig{Command: "make test"}},
		Check:    &protocol.CheckResult{AbsentFromRepo: []string{"gen-tool"}, Candidates: []string{"make test"}},
	}

	var section, other []string

	for _, v := range vetFix(c) {
		if v.section {
			section = append(section, v.text)
		} else {
			other = append(other, v.text)
		}
	}

	assert.ElementsMatch(t, append(checkSection(c), checkText(c)...), section)
	assert.ElementsMatch(t, checkVerify(c), other, "the trial did not finish")
	assert.NotEmpty(t, other)
}

func TestVetForwardSkipsHealOnlyChecks(t *testing.T) {
	c := fixContext{
		Card:  &board.Card{ID: "P-1"},
		Fix:   protocol.CardFix{Section: "fine"},
		Check: &protocol.CheckResult{},
	}

	assert.Empty(t, vetForward(c), "no snapshot and no retire needed for a forward target")
	assert.NotEmpty(t, vetFix(c), "a heal needs its snapshot")

	c.Fix.Section = "```bad"
	assert.Equal(t, checkSection(c), vetForward(c), "a forward still runs the section checks")
}
