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

func TestBodySections(t *testing.T) {
	tests := []struct {
		name, body, wantIntro string
		want                  []Section
	}{
		{name: "empty", body: ""},
		{
			name: "intro and two sections", body: "intro\n## A\na\n## B\nb\n",
			wantIntro: "intro\n", want: []Section{{"A", "a\n"}, {"B", "b\n"}},
		},
		{
			name: "fence hides heading", body: "```\n## not\n```\n## A\nx",
			wantIntro: "```\n## not\n```\n", want: []Section{{"A", "x"}},
		},
		{
			name: "indented heading is not one", body: "  ## indented\n## A\n",
			wantIntro: "  ## indented\n", want: []Section{{"A", ""}},
		},
		{name: "h3 stays in section", body: "## A\n### sub\n", want: []Section{{"A", "### sub\n"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intro, sections := BodySections(tt.body)
			assert.Equal(t, tt.wantIntro, intro)
			assert.Equal(t, tt.want, sections)
		})
	}
}

func TestDemoteHeadings(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"H2 at column 0", "## Foo\ntext", "### Foo\ntext"},
		{"H2 inside a backtick fence", "```\n## x\n```", "```\n## x\n```"},
		{"H2 inside a tilde fence", "~~~\n## x\n~~~", "~~~\n## x\n~~~"},
		{"indented H2", "  ## Foo", "  ## Foo"},
		{"bare ## is no heading", "##", "##"},
		{"multiple headings", "## A\nx\n## B\n```\n## C\n```\n## D\n", "### A\nx\n### B\n```\n## C\n```\n### D\n"},
		{"CRLF", "## A\r\nx\r\n", "### A\r\nx\r\n"},
		{"a backtick fence closes a tilde fence", "~~~\nx\n```\n## A", "~~~\nx\n```\n### A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DemoteHeadings(tt.in)
			assert.Equal(t, tt.want, got)

			_, sections := BodySections(got)
			assert.Empty(t, sections, "no demoted text is a section")
		})
	}
}
