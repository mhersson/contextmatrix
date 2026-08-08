package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpsertSection(t *testing.T) {
	tests := []struct {
		name, body, heading, content, want string
	}{
		{
			name:    "append to empty body",
			body:    "",
			heading: "Review Findings (Round 1)",
			content: "- finding one",
			want:    "## Review Findings (Round 1)\n\n- finding one\n",
		},
		{
			name:    "append after existing sections",
			body:    "intro\n\n## Plan\n\nstep 1\n",
			heading: "Decisions",
			content: "- keep it",
			want:    "intro\n\n## Plan\n\nstep 1\n\n## Decisions\n\n- keep it\n",
		},
		{
			name:    "replace existing section, preserve neighbors",
			body:    "intro\n\n## Plan\n\nold plan\n\n## Decisions\n\n- keep\n",
			heading: "Plan",
			content: "new plan",
			want:    "intro\n\n## Plan\n\nnew plan\n\n## Decisions\n\n- keep\n",
		},
		{
			name:    "H2 inside code fence is not a boundary",
			body:    "## Plan\n\n```md\n## Fake\n```\nreal tail\n",
			heading: "Plan",
			content: "replaced",
			want:    "## Plan\n\nreplaced\n",
		},
		{
			name:    "exact heading match only - parenthesized variant is a different section",
			body:    "## Review Findings\n\nround zero\n",
			heading: "Review Findings (Round 1)",
			content: "round one",
			want:    "## Review Findings\n\nround zero\n\n## Review Findings (Round 1)\n\nround one\n",
		},
		{
			name:    "CRLF body replace",
			body:    "## Plan\r\n\r\nold\r\n",
			heading: "Plan",
			content: "new plan",
			want:    "## Plan\n\nnew plan\n",
		},
		{
			name:    "CRLF preserves untouched neighbor bytes",
			body:    "intro\r\n\r\n## Plan\r\n\r\nold plan\r\n\r\n## Decisions\r\n\r\n- keep\r\n",
			heading: "Plan",
			content: "new plan",
			want:    "intro\r\n\r\n## Plan\n\nnew plan\n\n## Decisions\r\n\r\n- keep\r\n",
		},
		{
			name:    "indented fence under a list item is not a boundary",
			body:    "## Plan\n\n- item\n  ```md\n## Fake\n  ```\ntail\n",
			heading: "Plan",
			content: "replaced",
			want:    "## Plan\n\nreplaced\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UpsertSection(tt.body, tt.heading, tt.content))
		})
	}
}

func TestUpsertSectionIdempotent(t *testing.T) {
	once := UpsertSection("intro\n", "Plan", "the plan")
	twice := UpsertSection(once, "Plan", "the plan")
	assert.Equal(t, once, twice)
}

func TestUpsertSectionCRLFIdempotent(t *testing.T) {
	once := UpsertSection("## Plan\r\n\r\nold\r\n", "Plan", "new plan")
	twice := UpsertSection(once, "Plan", "new plan")
	assert.Equal(t, once, twice)
}
