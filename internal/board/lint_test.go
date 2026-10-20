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
		repos []string
		wantN int
	}{
		{name: "clean body", text: "Update internal/api/auth.go to return 403.\n\nAcceptance: make test passes.", repos: foreign, wantN: 0},
		{name: "linux home path", text: "Follow the design in /home/alice/docs/design.md", repos: foreign, wantN: 1},
		{name: "macos home path", text: "See /Users/bob/notes.txt for details", repos: foreign, wantN: 1},
		{name: "tilde path", text: "Config lives at ~/config/app.yaml", repos: foreign, wantN: 1},
		{name: "windows path", text: `Copy from C:\Users\alice\spec.docx`, repos: foreign, wantN: 1},
		{name: "file url", text: "Reference: file:///tmp/spec.pdf", repos: foreign, wantN: 1},
		{name: "foreign repo url", text: "Match the pattern in https://github.com/mhersson/other-project", repos: foreign, wantN: 1},
		{name: "foreign repo owner-name form", text: "Port the helper from mhersson/other-project first", repos: foreign, wantN: 1},
		{name: "own in-repo paths untouched", text: "Files: internal/board/card.go, web/src/App.tsx", repos: foreign, wantN: 0},
		{name: "multiple findings", text: "Read /home/a/x.md and ~/y.md", repos: foreign, wantN: 2},
		{name: "clean short body", text: "plain body", repos: foreign, wantN: 0},
		{name: "no foreign repos configured", text: "plain body", repos: nil, wantN: 0},
		{name: "ssh repo url matched by owner-name", text: "Port the helper from mhersson/other-project first", repos: []string{"git@github.com:mhersson/other-project.git"}, wantN: 1},
		{name: "prefix collision no false positive", text: "See mhersson/other-project-v2 for details", repos: []string{"https://github.com/mhersson/other-project.git"}, wantN: 0},
		{name: "in-repo path resembling /home/ mid-path", text: "web/src/pages/home/HomePage.tsx", repos: foreign, wantN: 0},
		{name: "in-repo path resembling /Users/ mid-path", text: "api/Users/controller.cs", repos: foreign, wantN: 0},
		{name: "linux home path at line start", text: "/home/alice/docs/design.md", repos: foreign, wantN: 1},
		{name: "linux home path mid-sentence", text: "Follow this: /home/alice/docs/design.md", repos: foreign, wantN: 1},
		{name: "bare last segment of foreign repo no false positive", text: "See other-project for details", repos: foreign, wantN: 0},
		{name: "path inside a mockup block", text: "```mockup\n<p>x</p>\n/home/anna/Documents\n```\n", repos: foreign, wantN: 0},
		{name: "path after the block closes", text: "```mockup\n<p>x</p>\n```\nSee /home/a/x.md", repos: foreign, wantN: 1},
		{name: "unclosed block runs to the end", text: "```mockup\n<p>x</p>\n/home/a/x.md", repos: foreign, wantN: 0},
		{name: "tilde fence is not a mockup block", text: "~~~mockup\n/home/a/x.md\n~~~\n", repos: foreign, wantN: 1},
		{name: "indented fence is not a mockup block", text: " ```mockup\n/home/a/x.md\n ```\n", repos: foreign, wantN: 1},
		{name: "an info string beyond mockup is not one", text: "```mockup extra\n/home/a/x.md\n```\n", repos: foreign, wantN: 1},
		{name: "a shorter fence does not close it", text: "````mockup\n/home/a\n```\n~/b\n````\nok", repos: foreign, wantN: 0},
		{name: "a longer fence closes it", text: "```mockup\n/home/a\n`````\nSee ~/b", repos: foreign, wantN: 1},
		{name: "closing fence with trailing spaces", text: "```mockup\n/home/a\n```  \t\nSee ~/b", repos: foreign, wantN: 1},
		{name: "foreign repo inside the block", text: "```mockup\nmhersson/other-project\n```\n", repos: foreign, wantN: 0},
		{name: "CRLF body", text: "```mockup\r\n/home/a\r\n```\r\nSee ~/b", repos: foreign, wantN: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LintSelfContained(tt.text, tt.repos)
			assert.Len(t, got, tt.wantN)
		})
	}
}
