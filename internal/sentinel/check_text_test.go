package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckText(t *testing.T) {
	const body = "Intro\n\n## Notes\nDeploy to deploy.example.com\n\n" +
		"## Plan\nRun make itest\n\n## Review Findings (Round 2)\nCheck gofmt -l\n\n" +
		"## Sentinel\nRun curl https://x.example\n"

	tests := []struct {
		name    string
		body    string
		section string
		absent  []string
		check   *protocol.CheckResult
		want    []string
	}{
		{"in the body", body, "s", []string{"deploy.example.com"}, &protocol.CheckResult{}, nil},
		{"in the title", body, "s", []string{"make bootstrap"}, &protocol.CheckResult{}, nil},
		{
			"only in the Sentinel section", body, "s",
			[]string{"curl https://x.example"},
			&protocol.CheckResult{},
			[]string{"introduces curl https://x.example"},
		},
		{"only in the agent's plan", body, "s", []string{"make itest"}, &protocol.CheckResult{}, []string{"introduces make itest"}},
		{"only in a later review round", body, "s", []string{"gofmt -l"}, &protocol.CheckResult{}, []string{"introduces gofmt -l"}},
		{"in the intro too", "Run make itest first.\n\n" + body, "s", []string{"make itest"}, &protocol.CheckResult{}, nil},
		{"nothing absent", body, "s", nil, &protocol.CheckResult{}, nil},
		{"no section", body, "", []string{"anything"}, nil, nil},
		{"not checked", body, "s", nil, nil, []string{"text not checked"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := tt.check
			if check != nil {
				check = &protocol.CheckResult{AbsentFromRepo: tt.absent}
			}

			card := &board.Card{Title: "Run make bootstrap first", Body: tt.body}
			got := checkText(fixContext{Card: card, Fix: protocol.CardFix{Section: tt.section}, Check: check})
			assert.Equal(t, tt.want, got)
		})
	}
}
