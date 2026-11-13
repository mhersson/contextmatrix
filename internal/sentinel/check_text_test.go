package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestCheckText(t *testing.T) {
	card := &board.Card{
		Title: "Run make bootstrap first",
		Body:  "Intro\n\n## Notes\nDeploy to deploy.example.com\n\n## Sentinel\nRun curl https://x.example\n",
	}

	tests := []struct {
		name    string
		section string
		absent  []string
		check   *protocol.CheckResult
		want    []string
	}{
		{"in the body", "s", []string{"deploy.example.com"}, &protocol.CheckResult{}, nil},
		{"in the title", "s", []string{"make bootstrap"}, &protocol.CheckResult{}, nil},
		{
			"only in the Sentinel section", "s",
			[]string{"curl https://x.example"},
			&protocol.CheckResult{},
			[]string{"introduces curl https://x.example"},
		},
		{"nothing absent", "s", nil, &protocol.CheckResult{}, nil},
		{"no section", "", []string{"anything"}, nil, nil},
		{"not checked", "s", nil, nil, []string{"text not checked"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := tt.check
			if check != nil {
				check = &protocol.CheckResult{AbsentFromRepo: tt.absent}
			}

			got := checkText(fixContext{Card: card, Fix: protocol.CardFix{Section: tt.section}, Check: check})
			assert.Equal(t, tt.want, got)
		})
	}
}
