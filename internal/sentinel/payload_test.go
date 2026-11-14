package sentinel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

func investigateBase() protocol.RunKindPayload {
	return protocol.RunKindPayload{
		CardID: "ALPHA-001", Project: "alpha", RepoURL: "https://github.com/acme/alpha.git", GitToken: "tok", MaxCostUSD: 2,
	}
}

// files returns n card files of size bytes each, IDs prefix-1 to prefix-n.
func files(prefix string, n, size int) []cardFile {
	out := make([]cardFile, n)
	for i := range out {
		out[i] = cardFile{ID: fmt.Sprintf("%s-%d", prefix, i+1), File: strings.Repeat("x", size)}
	}

	return out
}

func encodedLen(t *testing.T, p protocol.RunKindPayload) int {
	t.Helper()

	b, err := json.Marshal(p)
	require.NoError(t, err)

	return len(b)
}

func TestInvestigatePayloadFitsUnderOneMiB(t *testing.T) {
	const kib = 1 << 10

	tests := []struct {
		name               string
		card               string
		subtasks, siblings []cardFile
		wantSub, wantSib   []string
		ok                 bool
	}{
		{
			"small card, 3 subtasks and 2 siblings all sent", strings.Repeat("c", 4*kib),
			files("SUB", 3, 4*kib), files("SIB", 2, 4*kib),
			[]string{"SUB-1", "SUB-2", "SUB-3"},
			[]string{"SIB-1", "SIB-2"},
			true,
		},
		{
			"a subtask over the cap ends the list and the siblings", strings.Repeat("c", 600*kib),
			files("SUB", 3, 300*kib), files("SIB", 2, kib),
			[]string{"SUB-1"},
			nil, true,
		},
		{
			"siblings follow the subtasks", strings.Repeat("c", 600*kib),
			files("SUB", 1, 100*kib), files("SIB", 3, 150*kib),
			[]string{"SUB-1"},
			[]string{"SIB-1", "SIB-2"},
			true,
		},
		{"card alone over the cap", strings.Repeat("c", 1<<20), files("SUB", 1, kib), nil, nil, nil, false},
		{"escaping counts: 200 KiB of < encodes to 1.2 MiB", strings.Repeat("<", 200*kib), nil, nil, nil, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := protocol.InvestigateInput{RunID: "run-1", StopCause: protocol.StopCauseVerifyParked, Card: tc.card}

			p, sub, sib, ok := investigatePayload(investigateBase(), in, tc.subtasks, tc.siblings)
			require.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.wantSub, sub)
			assert.Equal(t, tc.wantSib, sib)

			if !ok {
				return
			}

			assert.Equal(t, protocol.RunKindInvestigate, p.Kind)
			assert.Nil(t, p.Check)
			require.NotNil(t, p.Investigate)
			assert.Equal(t, "run-1", p.Investigate.RunID)
			assert.Equal(t, tc.card, p.Investigate.Card)
			assert.Len(t, p.Investigate.Subtasks, len(tc.wantSub))
			assert.Len(t, p.Investigate.Siblings, len(tc.wantSib))
			assert.LessOrEqual(t, encodedLen(t, p), maxRunKindBody)
		})
	}
}

// TestInvestigatePayloadExactCap pins the cap at the byte: an encoding of
// exactly 1 MiB is sent, one byte more is not, for the card and for an added file.
func TestInvestigatePayloadExactCap(t *testing.T) {
	empty := investigateBase()
	empty.Kind = protocol.RunKindInvestigate
	empty.Investigate = &protocol.InvestigateInput{RunID: "run-1"}
	room := maxRunKindBody - encodedLen(t, empty)

	in := protocol.InvestigateInput{RunID: "run-1", Card: strings.Repeat("c", room)}
	_, _, _, ok := investigatePayload(investigateBase(), in, nil, nil)
	assert.True(t, ok, "a card filling the cap exactly fits")

	in.Card += "c"
	_, _, _, ok = investigatePayload(investigateBase(), in, nil, nil)
	assert.False(t, ok, "one byte over does not")

	// `,"subtasks":["` + `"]` around the first file and `,"` + `"` around the second.
	const firstOverhead, nextOverhead = len(`,"subtasks":[""]`), len(`,""`)

	in.Card = strings.Repeat("c", room-firstOverhead-10-nextOverhead-5)
	exact := []cardFile{{ID: "SUB-1", File: strings.Repeat("s", 10)}, {ID: "SUB-2", File: strings.Repeat("s", 5)}}
	p, sub, _, ok := investigatePayload(investigateBase(), in, exact, nil)
	require.True(t, ok)
	assert.Equal(t, []string{"SUB-1", "SUB-2"}, sub)
	assert.Equal(t, maxRunKindBody, encodedLen(t, p))

	over := []cardFile{exact[0], {ID: "SUB-2", File: strings.Repeat("s", 6)}}
	_, sub, _, ok = investigatePayload(investigateBase(), in, over, nil)
	require.True(t, ok)
	assert.Equal(t, []string{"SUB-1"}, sub)
}

func TestCheckPayload(t *testing.T) {
	base := protocol.RunKindPayload{CardID: "ALPHA-001", Project: "alpha", RepoURL: "https://github.com/acme/alpha.git", GitToken: "tok"}
	before := &board.VerifyConfig{Command: "make test", TimeoutSeconds: 300, Env: []string{"GOFLAGS"}}

	tests := []struct {
		name          string
		before, after *board.VerifyConfig
		section       string
		wantVerify    *protocol.VerifyConfig
		wantCurrent   string
	}{
		{
			"unchanged command, timeout raised", before,
			&board.VerifyConfig{Command: "make test", TimeoutSeconds: 900, Env: []string{"GOFLAGS"}}, "",
			&protocol.VerifyConfig{Command: "make test", TimeoutSeconds: 900, Env: []string{"GOFLAGS"}}, "make test",
		},
		{"no verify change", before, nil, "Run make lint first.", nil, "make test"},
		{
			"no verify in force", nil, &board.VerifyConfig{Command: "go test ./..."}, "",
			&protocol.VerifyConfig{Command: "go test ./..."}, "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := checkPayload(base, tc.before, tc.after, tc.section)

			assert.Equal(t, protocol.RunKindCheck, p.Kind)
			assert.Equal(t, "tok", p.GitToken)
			assert.Nil(t, p.Investigate)
			require.NotNil(t, p.Check)
			assert.Equal(t, tc.wantVerify, p.Check.Verify)
			assert.Equal(t, tc.wantCurrent, p.Check.CurrentCommand)
			assert.Equal(t, tc.section, p.Check.Text)
		})
	}
}

func TestAnalysePayloadFillsUnderTheCap(t *testing.T) {
	const kib = 1 << 10

	in := protocol.AnalyseInput{RunID: "run-1", StopCause: protocol.StopCauseDone, Card: strings.Repeat("c", 900*kib)}

	p, ok := analysePayload(investigateBase(), in, files("SUB", 2, 100*kib))
	require.True(t, ok)
	assert.Equal(t, protocol.RunKindAnalyse, p.Kind)
	assert.Nil(t, p.Investigate)
	assert.Nil(t, p.Check)
	require.NotNil(t, p.Analyse)
	assert.Equal(t, "run-1", p.Analyse.RunID)
	assert.Equal(t, []string{strings.Repeat("x", 100*kib)}, p.Analyse.Subtasks, "only the first subtask fits")
	assert.LessOrEqual(t, encodedLen(t, p), maxRunKindBody)

	in.Card = strings.Repeat("c", 1100*kib)
	_, ok = analysePayload(investigateBase(), in, nil)
	assert.False(t, ok, "a card alone over the cap is not sent")
}
