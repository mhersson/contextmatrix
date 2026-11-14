package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

func TestCheckSection(t *testing.T) {
	tests := []struct {
		name    string
		section string
		bad     bool
	}{
		{"plain text", "use make test", false},
		{"H2", "## Foo", false},
		{"indented H2", "  ## Foo", false},
		{"bare H2", "x\n##", false},
		{"H3", "### Foo", false},
		{"H2 inside a fence", "```\n## x\n```", false},
		{"unbalanced fence", "```go\nx", true},
		{"unbalanced tilde fence", "~~~\nx", true},
		{"balanced fence", "```\nx\n```", false},
		{"two fences", "```\na\n```\n~~~\nb\n~~~", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkSection(fixContext{Fix: protocol.CardFix{Section: tt.section}})
			assert.Equal(t, tt.bad, len(got) > 0, "%v", got)
		})
	}
}
