package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLintSelfContained(t *testing.T) {
	foreign := []string{"https://github.com/mhersson/other-project.git"}

	tests := []struct {
		name  string
		text  string
		wantN int
	}{
		{name: "clean body", text: "Update internal/api/auth.go to return 403.\n\nAcceptance: make test passes.", wantN: 0},
		{name: "linux home path", text: "Follow the design in /home/alice/docs/design.md", wantN: 1},
		{name: "macos home path", text: "See /Users/bob/notes.txt for details", wantN: 1},
		{name: "tilde path", text: "Config lives at ~/config/app.yaml", wantN: 1},
		{name: "windows path", text: `Copy from C:\Users\alice\spec.docx`, wantN: 1},
		{name: "file url", text: "Reference: file:///tmp/spec.pdf", wantN: 1},
		{name: "foreign repo url", text: "Match the pattern in https://github.com/mhersson/other-project", wantN: 1},
		{name: "foreign repo owner-name form", text: "Port the helper from mhersson/other-project first", wantN: 1},
		{name: "own in-repo paths untouched", text: "Files: internal/board/card.go, web/src/App.tsx", wantN: 0},
		{name: "multiple findings", text: "Read /home/a/x.md and ~/y.md", wantN: 2},
		{name: "no foreign repos configured", text: "plain body", wantN: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LintSelfContained(tt.text, foreign)
			assert.Len(t, got, tt.wantN)
		})
	}
}
