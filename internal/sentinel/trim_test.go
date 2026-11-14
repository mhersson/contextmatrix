package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestTrimFix(t *testing.T) {
	card := &board.Card{Verify: &board.VerifyConfig{Command: "make test"}}
	verify := &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 1800}
	withVerify := protocol.CardFix{Section: "Run gen-tool first.", Verify: verify}
	verifyOnly := protocol.CardFix{Verify: verify}
	sameVerify := protocol.CardFix{Section: "Run gen-tool first.", Verify: &protocol.VerifyConfig{Command: "make  test"}}

	section := func(s string) violation { return violation{text: s, section: true} }
	other := func(s string) violation { return violation{text: s} }

	tests := []struct {
		name       string
		fix        protocol.CardFix
		violations []violation
		kept       protocol.CardFix
		dropped    []string
		ok         bool
	}{
		{
			"only text violations plus verify", withVerify,
			[]violation{section("introduces gen-tool"), section("introduces gen.yaml")},
			verifyOnly,
			[]string{"introduces gen-tool", "introduces gen.yaml"},
			true,
		},
		{
			"only section violations plus verify", withVerify,
			[]violation{section("section would change the card's sections")},
			verifyOnly,
			[]string{"section would change the card's sections"},
			true,
		},
		{
			"a text violation plus a verify violation", withVerify,
			[]violation{section("introduces gen-tool"), other("verify not trial-run")},
			withVerify, nil, false,
		},
		{
			"a section-only fix",
			protocol.CardFix{Section: "Run gen-tool first."},
			[]violation{section("introduces gen-tool")},
			protocol.CardFix{Section: "Run gen-tool first."},
			nil, false,
		},
		{
			"the card's verify restated and a failing section", sameVerify,
			[]violation{section("introduces gen-tool")},
			sameVerify, nil, false,
		},
		{
			"text violations plus a retire",
			protocol.CardFix{Section: "s", Verify: verify, RetireSubtasks: []string{"ALPHA-011"}},
			[]violation{section("introduces gen-tool")},
			protocol.CardFix{Section: "s", Verify: verify, RetireSubtasks: []string{"ALPHA-011"}},
			nil, false,
		},
		{"no violations", withVerify, nil, withVerify, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped, ok := trimFix(fixContext{Card: card, Fix: tc.fix}, tc.violations)
			assert.Equal(t, tc.kept, kept)
			assert.Equal(t, tc.dropped, dropped)
			assert.Equal(t, tc.ok, ok)
		})
	}
}
