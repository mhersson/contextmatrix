package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

func TestTrimFix(t *testing.T) {
	verify := &protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 1800}
	withVerify := protocol.CardFix{Section: "Run gen-tool first.", Verify: verify}
	verifyOnly := protocol.CardFix{Verify: verify}

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
			"only fence violations plus verify", withVerify,
			[]violation{section("section has an unbalanced code fence")},
			verifyOnly,
			[]string{"section has an unbalanced code fence"},
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
			kept, dropped, ok := trimFix(tc.fix, tc.violations)
			assert.Equal(t, tc.kept, kept)
			assert.Equal(t, tc.dropped, dropped)
			assert.Equal(t, tc.ok, ok)
		})
	}
}
