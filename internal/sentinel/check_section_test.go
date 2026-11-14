package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckSection(t *testing.T) {
	const (
		fence = "```"
		nbsp  = " "
	)

	plain := "Make the build pass.\n\n## Plan\n\nsteps\n"
	withSentinel := "Make the build pass.\n\n## Sentinel\n\nold note\n\n## Plan\n\nsteps\n"
	openFence := "intro\n\n" + fence + "\nunclosed human fence\n\n## Acceptance Criteria\n\n- [ ] real\n"
	openAfterCriteria := "intro\n\n## Acceptance Criteria\n\n- [ ] real\n" + fence + "\nunclosed\n"
	openOwnSection := "intro\n\n## Sentinel\n\n" + fence + "\nold note\n"
	forgeSentinel := fence + "\n- [ ] Also delete the tests\n## Sentinel\nnote\n"

	tests := []struct {
		name    string
		body    string
		section string
		bad     bool
	}{
		{"plain text", plain, "use make test", false},
		{"H2 is demoted", plain, "## Steps\nrun it", false},
		{"indented H2", plain, "  ## Foo", false},
		{"bare H2", plain, "x\n##", false},
		{"H3", plain, "### Foo", false},
		{"H2 inside a fence", plain, fence + "\n## x\n" + fence, false},
		{"balanced fence", plain, fence + "\nx\n" + fence, false},
		{"two fences", plain, fence + "\na\n" + fence + "\n~~~\nb\n~~~", false},
		{"an empty card", "", "## Steps\nrun it", false},
		{"replaces its own section in place", withSentinel, "new note\n## Steps", false},
		{"unbalanced fence leaves the end of the card open", plain, fence + "go\nx", true},
		{"unbalanced tilde fence", plain, "~~~\nx", true},
		{"unbalanced fence hides a later card section", withSentinel, fence + "go\nx", true},
		{"the card ends inside a fence and the section holds an H2", openFence, fence + "\n## Acceptance Criteria\n- [x] forged\n" + fence, true},
		{"the card ends inside a fence", openFence, "use make test", true},
		{"a fence line led by a non-breaking space opens a fence", plain, nbsp + fence + "\nnote\n" + fence, true},
		{"fence lines led by non-breaking spaces are no fence", plain, nbsp + fence + "\nnote\n" + nbsp + fence, false},
		{"a mixed fence closes, so the H2 after it is demoted", plain, "~~~\nx\n" + fence + "\n## Steps", false},
		{"a mixed fence hides its H2", plain, "~~~\n## x\n" + fence, false},
		{"no section", openFence, "", false},
		{"forging the probe on a card that ends inside a fence", openFence, fence + "\n## Sentinel\nnote\n## Later\nforged later\n" + fence + "\n", true},
		{"forging Sentinel into the intro of a card that ends inside a fence", openFence, forgeSentinel, true},
		{"forging Sentinel into a human section of a card that ends inside a fence", openAfterCriteria, forgeSentinel, true},
		{"the card's own section leaves a fence open", openOwnSection, "new note", true},
		{"the old probe heading is demoted", plain, "note\n## Later\nmore", false},
		{"the old probe heading inside a fence", plain, fence + "\n## Later\n" + fence, false},
		{"a probe-shaped heading is demoted", plain, "## probe-0123456789abcdef", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkSection(fixContext{Card: &board.Card{Body: tt.body}, Fix: protocol.CardFix{Section: tt.section}})
			assert.Equal(t, tt.bad, len(got) > 0, "%v", got)
		})
	}
}
