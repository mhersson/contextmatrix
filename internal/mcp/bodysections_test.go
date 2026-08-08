package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterBodySections(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		keep        []string
		wantBody    string
		wantOmitted []string
	}{
		{
			name:     "nil keep returns body unchanged",
			body:     "Intro.\n\n## Plan\n\n- step\n",
			keep:     nil,
			wantBody: "Intro.\n\n## Plan\n\n- step\n",
		},
		{
			name:     "no H2 headings falls back to full body",
			body:     "Just a description with no sections.\nSecond line.",
			keep:     []string{"## Plan"},
			wantBody: "Just a description with no sections.\nSecond line.",
		},
		{
			name:     "unvetted placeholder passes through untouched",
			body:     unvettedBodyPlaceholder,
			keep:     []string{"## Plan", "## Review Findings"},
			wantBody: unvettedBodyPlaceholder,
		},
		{
			name:     "empty body stays empty",
			body:     "",
			keep:     []string{"## Plan"},
			wantBody: "",
		},
		{
			name:        "keeps intro and matched section, omits the rest",
			body:        "Intro text.\n\n## Plan\n\n- step\n\n## Diagnosis\n\nRoot cause.\n\n## Notes\n\nMisc.\n",
			keep:        []string{"## Plan"},
			wantBody:    "Intro text.\n\n## Plan\n\n- step\n",
			wantOmitted: []string{"Diagnosis", "Notes"},
		},
		{
			name:     "prefix collision Planning vs Plan falls back",
			body:     "Intro.\n\n## Planning\n\nstuff\n",
			keep:     []string{"## Plan"},
			wantBody: "Intro.\n\n## Planning\n\nstuff\n",
		},
		{
			name:        "round-numbered variants all match",
			body:        "Intro.\n\n## Review Findings\n\nr1\n\n## Review Findings (Round 2)\n\nr2\n\n## Review Findings(Round 3)\n\nr3\n\n## Diagnosis\n\nd\n",
			keep:        []string{"## Review Findings"},
			wantBody:    "Intro.\n\n## Review Findings\n\nr1\n\n## Review Findings (Round 2)\n\nr2\n\n## Review Findings(Round 3)\n\nr3\n",
			wantOmitted: []string{"Diagnosis"},
		},
		{
			name:        "heading match is case-insensitive",
			body:        "## plan\n\n- a\n\n## Diagnosis\n\nd\n",
			keep:        []string{"## Plan"},
			wantBody:    "## plan\n\n- a\n",
			wantOmitted: []string{"Diagnosis"},
		},
		{
			name:        "section at body start with no intro",
			body:        "## Plan\n\n- a\n\n## Notes\n\nn\n",
			keep:        []string{"## Plan"},
			wantBody:    "## Plan\n\n- a\n",
			wantOmitted: []string{"Notes"},
		},
		{
			name:        "matched last section without trailing newline stays intact",
			body:        "intro\n## Diagnosis\nstuff\n## Plan\n- a",
			keep:        []string{"## Plan"},
			wantBody:    "intro\n## Plan\n- a",
			wantOmitted: []string{"Diagnosis"},
		},
		{
			name:        "H2 inside fenced code block is not a boundary",
			body:        "## Plan\n\nTemplate:\n\n```markdown\n## Notes\n\ninside fence\n```\n\nReal content.\n\n## Notes\n\nreal notes\n",
			keep:        []string{"## Plan"},
			wantBody:    "## Plan\n\nTemplate:\n\n```markdown\n## Notes\n\ninside fence\n```\n\nReal content.\n",
			wantOmitted: []string{"Notes"},
		},
		{
			name:     "no keep entry matches any section falls back",
			body:     "Intro.\n\n## Design\n\nd\n\n## Diagnosis\n\nr\n",
			keep:     []string{"## Plan", "## Review Findings"},
			wantBody: "Intro.\n\n## Design\n\nd\n\n## Diagnosis\n\nr\n",
		},
		{
			name:        "H3 headings do not start sections",
			body:        "## Plan\n\n### Root cause\n\ndetail\n\n## Diagnosis\n\nd\n",
			keep:        []string{"## Plan"},
			wantBody:    "## Plan\n\n### Root cause\n\ndetail\n",
			wantOmitted: []string{"Diagnosis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBody, gotOmitted := filterBodySections(tt.body, tt.keep)
			assert.Equal(t, tt.wantBody, gotBody)
			assert.Equal(t, tt.wantOmitted, gotOmitted)
		})
	}
}

func TestFilterBodySectionsExact(t *testing.T) {
	tests := []struct {
		name string
		body string
		keep []string
		want string
	}{
		{
			name: "nil keep returns empty",
			body: "Intro.\n\n## Plan\n\n- step\n",
			keep: nil,
			want: "",
		},
		{
			name: "empty body returns empty",
			body: "",
			keep: []string{"## Plan"},
			want: "",
		},
		{
			name: "no keep entry matches any section returns empty",
			body: "Intro.\n\n## Design\n\nd\n\n## Diagnosis\n\nr\n",
			keep: []string{"## Plan", "## Review Findings"},
			want: "",
		},
		{
			name: "matching section returned without intro",
			body: "Intro text.\n\n## Plan\n\n- step\n\n## Diagnosis\n\nRoot cause.\n",
			keep: []string{"## Plan"},
			want: "## Plan\n\n- step\n",
		},
		{
			name: "matching section plus intro when requested",
			body: "Intro text.\n\n## Plan\n\n- step\n\n## Diagnosis\n\nRoot cause.\n",
			keep: []string{"intro", "## Plan"},
			want: "Intro text.\n\n## Plan\n\n- step\n",
		},
		{
			name: "intro alone returns only the pre-heading text",
			body: "Intro text.\n\n## Plan\n\n- step\n",
			keep: []string{"intro"},
			want: "Intro text.\n",
		},
		{
			name: "intro requested but no keep matches still returns intro",
			body: "Intro text.\n\n## Plan\n\n- step\n",
			keep: []string{"intro", "## Bogus"},
			want: "Intro text.\n",
		},
		{
			name: "intro is case-insensitive",
			body: "Intro text.\n\n## Plan\n\n- step\n",
			keep: []string{"Intro"},
			want: "Intro text.\n",
		},
		{
			name: "no H2 headings and intro requested returns full body",
			body: "Just a description with no sections.\nSecond line.",
			keep: []string{"intro"},
			want: "Just a description with no sections.\nSecond line.",
		},
		{
			name: "no H2 headings and intro not requested returns empty",
			body: "Just a description with no sections.\nSecond line.",
			keep: []string{"## Plan"},
			want: "",
		},
		{
			name: "multiple matched sections keep original order",
			body: "Intro.\n\n## Diagnosis\n\nd\n\n## Plan\n\np\n\n## Notes\n\nn\n",
			keep: []string{"## Diagnosis", "## Plan"},
			want: "## Diagnosis\n\nd\n\n## Plan\n\np\n",
		},
		{
			// keep is deliberately reversed relative to body order (Plan is
			// requested first but appears second in body) - output must
			// still follow body order, not keep order. The case above can't
			// distinguish this: its keep order happens to coincide with
			// body order.
			name: "output follows body order even when keep is reversed",
			body: "Intro.\n\n## Diagnosis\n\nd\n\n## Plan\n\np\n\n## Notes\n\nn\n",
			keep: []string{"## Plan", "## Diagnosis"},
			want: "## Diagnosis\n\nd\n\n## Plan\n\np\n",
		},
		{
			name: "round-numbered variants all match",
			body: "Intro.\n\n## Review Findings\n\nr1\n\n## Review Findings (Round 2)\n\nr2\n\n## Diagnosis\n\nd\n",
			keep: []string{"## Review Findings"},
			want: "## Review Findings\n\nr1\n\n## Review Findings (Round 2)\n\nr2\n",
		},
		{
			name: "H2 inside fenced code block is not a boundary",
			body: "## Plan\n\nTemplate:\n\n```markdown\n## Notes\n\ninside fence\n```\n\nReal content.\n\n## Notes\n\nreal notes\n",
			keep: []string{"## Plan"},
			want: "## Plan\n\nTemplate:\n\n```markdown\n## Notes\n\ninside fence\n```\n\nReal content.\n",
		},
		{
			name: "fenced heading is not selectable even when its title is requested",
			body: "## Plan\n\n```markdown\n## Notes\n\ninside fence\n```\n",
			keep: []string{"## Notes"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterBodySectionsExact(tt.body, tt.keep)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOmittedSectionsNote(t *testing.T) {
	t.Run("empty omitted list renders nothing", func(t *testing.T) {
		assert.Empty(t, omittedSectionsNote("TEST-001", nil))
	})

	t.Run("names sections without heading prefix and points at get_card", func(t *testing.T) {
		note := omittedSectionsNote("TEST-001", []string{"Diagnosis", "Notes"})
		assert.Equal(t, "[Body sections omitted from this context: Diagnosis; Notes. Run get_card(card_id='TEST-001') to read the full body.]", note)
		assert.NotContains(t, note, "## ")
		assert.NotContains(t, note, "\n")
	})
}
