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
