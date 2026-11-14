package sentinel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
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

func TestSiblings(t *testing.T) {
	card := cardRef{"alpha", "ALPHA-002"}
	todo := map[string]*board.Card{
		"ALPHA-001": {ID: "ALPHA-001", State: board.StateTodo},
		"ALPHA-002": {ID: "ALPHA-002", State: board.StateTodo},
		"ALPHA-003": {ID: "ALPHA-003", State: board.StateTodo},
		"ALPHA-004": {ID: "ALPHA-004", State: board.StateTodo, WorkerStatus: "failed"},
		"ALPHA-005": {ID: "ALPHA-005", State: board.StateInProgress},
		"ALPHA-007": {ID: "ALPHA-007", State: board.StateTodo},
	}
	entry := func(id, typ, project, cardID string, complete bool) service.PlaybookEntryDetail {
		return service.PlaybookEntryDetail{
			PlaybookEntry: board.PlaybookEntry{ID: id, Type: typ, Project: project, Card: cardID},
			Complete:      complete,
		}
	}
	run := func(status, instance string) *service.PlaybookDetail {
		return &service.PlaybookDetail{
			Runnable: true,
			Run:      &board.PlaybookRun{Status: status, Instance: instance, StartedAt: time.Now()},
			Entries: []service.PlaybookEntryDetail{
				entry("e1", board.EntryTypeCard, "alpha", "ALPHA-007", false),
				entry("e2", board.EntryTypeCard, "alpha", "ALPHA-002", false), // the card itself
				entry("e3", board.EntryTypeManual, "", "", false),
				entry("e4", board.EntryTypeCard, "beta", "ALPHA-003", false),  // another project
				entry("e5", board.EntryTypeCard, "alpha", "ALPHA-001", true),  // complete
				entry("e6", board.EntryTypeCard, "alpha", "ALPHA-004", false), // a worker ran it
				entry("e7", board.EntryTypeCard, "alpha", "ALPHA-005", false), // not in todo
				entry("e8", board.EntryTypeCard, "alpha", "ALPHA-006", false), // gone
				entry("e9", board.EntryTypeCard, "alpha", "ALPHA-003", false),
			},
		}
	}

	tests := []struct {
		name string
		pb   *service.PlaybookDetail
		want []string
	}{
		{"no playbook", nil, nil},
		{"running here, in entry order", run(board.RunStatusRunning, "lap-a"), []string{"ALPHA-007", "ALPHA-003"}},
		{"waiting here", run(board.RunStatusWaiting, "lap-a"), []string{"ALPHA-007", "ALPHA-003"}},
		{"stopped", run(board.RunStatusStopped, "lap-a"), nil},
		{"another instance's run", run(board.RunStatusRunning, "lap-b"), nil},
		{"not runnable", func() *service.PlaybookDetail {
			pb := run(board.RunStatusRunning, "lap-a")
			pb.Runnable = false

			return pb
		}(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range siblings(tc.pb, "lap-a", card, todo) {
				got = append(got, c.ID)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}
