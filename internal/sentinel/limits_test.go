package sentinel

import (
	"strings"
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

func checkStatus() protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		CardID: "ALPHA-001", Project: "alpha", WorkerStatus: "completed", RunID: "k1",
		StopCause: protocol.StopCauseDone, Kind: protocol.RunKindCheck,
		Result: &protocol.RunKindResult{Check: &protocol.CheckResult{
			Candidates: []string{"make test"}, Trial: protocol.TrialFinished, AbsentFromRepo: []string{"make lint"},
		}},
	}
}

func investigateStatus() protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		CardID: "ALPHA-001", Project: "alpha", WorkerStatus: "completed", RunID: "k1",
		StopCause: protocol.StopCauseDone, Kind: protocol.RunKindInvestigate,
		Result: &protocol.RunKindResult{Investigate: &protocol.InvestigateResult{
			Diagnosis: "the verify timeout is too short", Cause: protocol.DiagnosisCard,
			Fix:       &protocol.CardFix{Section: "Run the slow suite last.", RetireSubtasks: []string{"ALPHA-002"}},
			ForwardTo: []string{"ALPHA-003"},
			Findings:  []protocol.FrictionFinding{{Kind: protocol.FindingCode, Title: "t", Text: "x", Evidence: "e"}},
		}},
		Usage: []protocol.RunUsage{{Model: "vendor/model-a", LogTokenUsage: protocol.LogTokenUsage{InputTokens: 10}}},
	}
}

func inv(p *protocol.StatusCallbackPayload) *protocol.InvestigateResult { return p.Result.Investigate }

func chk(p *protocol.StatusCallbackPayload) *protocol.CheckResult { return p.Result.Check }

func bytesOf(n int) string { return strings.Repeat("a", n) }

func entries(n, size int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = bytesOf(size)
	}

	return out
}

func findings(n int) []protocol.FrictionFinding {
	out := make([]protocol.FrictionFinding, n)
	for i := range out {
		out[i] = protocol.FrictionFinding{Kind: protocol.FindingGuidance, Text: "x", Evidence: "e"}
	}

	return out
}

func TestValidateRunKindStatus(t *testing.T) {
	tests := []struct {
		name    string
		base    func() protocol.StatusCallbackPayload
		mutate  func(p *protocol.StatusCallbackPayload)
		bodyLen int
		ok      bool
	}{
		{"check result", checkStatus, nil, 100, true},
		{"investigate result", investigateStatus, nil, 100, true},
		{"body at 1 MiB", investigateStatus, nil, 1 << 20, true},
		{"body over 1 MiB", investigateStatus, nil, 1<<20 + 1, false},
		{"reserved kind", checkStatus, func(p *protocol.StatusCallbackPayload) { p.Kind = protocol.RunKindAnalyse }, 100, false},
		{"running status", checkStatus, func(p *protocol.StatusCallbackPayload) {
			p.WorkerStatus, p.StopCause, p.Result = "running", "", nil
		}, 100, false},
		{"failed without result", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.WorkerStatus, p.StopCause, p.Result = "failed", protocol.StopCauseBudgetExceeded, nil
		}, 100, true},
		{"completed without result", checkStatus, func(p *protocol.StatusCallbackPayload) { p.Result = nil }, 100, true},
		{"result on a failed status", checkStatus, func(p *protocol.StatusCallbackPayload) { p.WorkerStatus = "failed" }, 100, false},
		{"result with stop cause other than done", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.StopCause = protocol.StopCauseBudgetExceeded
		}, 100, false},
		{"investigate result on a check status", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Kind = protocol.RunKindCheck
		}, 100, false},
		{"both results", checkStatus, func(p *protocol.StatusCallbackPayload) {
			p.Result.Investigate = investigateStatus().Result.Investigate
		}, 100, false},
		{"empty result", checkStatus, func(p *protocol.StatusCallbackPayload) { p.Result = &protocol.RunKindResult{} }, 100, false},

		{"diagnosis at 32 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Diagnosis = bytesOf(32 << 10) }, 100, true},
		{"diagnosis over 32 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Diagnosis = bytesOf(32<<10 + 1) }, 100, false},
		{"cause ecosystem", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			inv(p).Cause, inv(p).Fix = protocol.DiagnosisEcosystem, nil
		}, 100, true},
		{"unknown cause", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Cause = "weather" }, 100, false},
		{"section at 16 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Fix.Section = bytesOf(16 << 10) }, 100, true},
		{"section over 16 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Fix.Section = bytesOf(16<<10 + 1) }, 100, false},
		{"50 retired subtasks", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Fix.RetireSubtasks = entries(50, 9) }, 100, true},
		{"51 retired subtasks", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Fix.RetireSubtasks = entries(51, 9) }, 100, false},
		{"50 forward IDs", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).ForwardTo = entries(50, 9) }, 100, true},
		{"51 forward IDs", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).ForwardTo = entries(51, 9) }, 100, false},
		{"50 findings", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings = findings(50) }, 100, true},
		{"51 findings", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings = findings(51) }, 100, false},
		{"unknown finding kind", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Kind = "gossip" }, 100, false},
		{"finding title at 200 B", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Title = bytesOf(200) }, 100, true},
		{"finding title over 200 B", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Title = bytesOf(201) }, 100, false},
		{"finding text at 16 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Text = bytesOf(16 << 10) }, 100, true},
		{"finding text over 16 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Text = bytesOf(16<<10 + 1) }, 100, false},
		{"evidence at 4 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Evidence = bytesOf(4 << 10) }, 100, true},
		{"evidence over 4 KiB", investigateStatus, func(p *protocol.StatusCallbackPayload) { inv(p).Findings[0].Evidence = bytesOf(4<<10 + 1) }, 100, false},

		{"32 candidates of 512 B", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Candidates = entries(32, 512) }, 100, true},
		{"33 candidates", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Candidates = entries(33, 8) }, 100, false},
		{"candidate over 512 B", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Candidates = entries(1, 513) }, 100, false},
		{"64 absent entries of 512 B", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).AbsentFromRepo = entries(64, 512) }, 100, true},
		{"65 absent entries", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).AbsentFromRepo = entries(65, 8) }, 100, false},
		{"absent entry over 512 B", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).AbsentFromRepo = entries(1, 513) }, 100, false},
		{"trial rejected", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Trial = protocol.TrialRejected }, 100, true},
		{"no trial", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Trial = "" }, 100, true},
		{"unknown trial", checkStatus, func(p *protocol.StatusCallbackPayload) { chk(p).Trial = "skipped" }, 100, false},

		{"16 usage rows", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage = make([]protocol.RunUsage, 16)
		}, 100, true},
		{"17 usage rows", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage = make([]protocol.RunUsage, 17)
		}, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.base()
			if tt.mutate != nil {
				tt.mutate(&p)
			}

			err := ValidateRunKindStatus(p, tt.bodyLen)
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
