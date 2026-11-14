package sentinel

import (
	"math"
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
		{"unknown kind", checkStatus, func(p *protocol.StatusCallbackPayload) { p.Kind = "weather" }, 100, false},
		{"check without a card", checkStatus, func(p *protocol.StatusCallbackPayload) { p.CardID = "" }, 100, false},
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
		{"negative input tokens", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].InputTokens = -1 }, 100, false},
		{"negative output tokens", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].OutputTokens = -1 }, 100, false},
		{"negative cache read tokens", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].CacheReadTokens = -1 }, 100, false},
		{"negative cache creation tokens", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].CacheCreateTokens = -1
		}, 100, false},
		{"gateway cost", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].CostUSD = 0.15 }, 100, true},
		{"negative cost", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].CostUSD = -0.01 }, 100, false},
		{"NaN cost", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].CostUSD = math.NaN() }, 100, false},
		{"infinite cost", investigateStatus, func(p *protocol.StatusCallbackPayload) { p.Usage[0].CostUSD = math.Inf(1) }, 100, false},
		{"costs summing past the float range", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].CostUSD = math.MaxFloat64
			p.Usage = append(p.Usage, p.Usage[0])
		}, 100, false},
		{"subscription window over its limit", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].Subscription = []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: 1.2}}
		}, 100, true},
		{"unnamed subscription window", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].Subscription = []protocol.SubscriptionUsage{{Utilization: 0.2}}
		}, 100, false},
		{"negative utilization", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].Subscription = []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: -0.1}}
		}, 100, false},
		{"NaN utilization", investigateStatus, func(p *protocol.StatusCallbackPayload) {
			p.Usage[0].Subscription = []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: math.NaN()}}
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

func analyseStatus() protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		CardID: "ALPHA-001", Project: "alpha", WorkerStatus: "completed", RunID: "k1",
		StopCause: protocol.StopCauseDone, Kind: protocol.RunKindAnalyse,
		Result: &protocol.RunKindResult{Analyse: &protocol.AnalyseResult{Findings: findings(2)}},
	}
}

func TestValidateRunKindStatusAnalyse(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *protocol.StatusCallbackPayload)
		ok     bool
	}{
		{"two findings", nil, true},
		{"no findings", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings = nil }, true},
		{"50 findings", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings = findings(50) }, true},
		{"51 findings", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings = findings(51) }, false},
		{"unknown finding kind", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings[0].Kind = "gossip" }, false},
		{"title over 200 B", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings[0].Title = bytesOf(201) }, false},
		{"text over 16 KiB", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings[0].Text = bytesOf(16<<10 + 1) }, false},
		{"evidence over 4 KiB", func(p *protocol.StatusCallbackPayload) { p.Result.Analyse.Findings[0].Evidence = bytesOf(4<<10 + 1) }, false},
		{"an investigate result", func(p *protocol.StatusCallbackPayload) {
			p.Result = &protocol.RunKindResult{Investigate: investigateStatus().Result.Investigate}
		}, false},
		{"both results", func(p *protocol.StatusCallbackPayload) { p.Result.Check = checkStatus().Result.Check }, false},
		{"no card", func(p *protocol.StatusCallbackPayload) { p.CardID = "" }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := analyseStatus()
			if tt.mutate != nil {
				tt.mutate(&p)
			}

			err := ValidateRunKindStatus(p, 100)
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func guidanceStatus() protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		Project: "alpha", WorkerStatus: "completed", RunID: "g1",
		StopCause: protocol.StopCauseDone, Kind: protocol.RunKindGuidancePR,
		Result: &protocol.RunKindResult{GuidancePR: &protocol.GuidancePRResult{
			Outcome: protocol.GuidancePushed, PRURL: "https://github.com/o/r/pull/7",
			HeadSHA: strings.Repeat("a", 40), File: "AGENTS.md",
		}},
	}
}

func TestValidateRunKindStatusGuidancePR(t *testing.T) {
	gp := func(p *protocol.StatusCallbackPayload) *protocol.GuidancePRResult { return p.Result.GuidancePR }
	tests := []struct {
		name   string
		mutate func(p *protocol.StatusCallbackPayload)
		want   string // "" = valid, else a fragment of the error
	}{
		{"pushed", nil, ""},
		{"unchanged", func(p *protocol.StatusCallbackPayload) { gp(p).Outcome = protocol.GuidanceUnchanged }, ""},
		{"a card ID", func(p *protocol.StatusCallbackPayload) { p.CardID = "ABC-1" }, "a guidance_pr status names no card"},
		{"failed without a result", func(p *protocol.StatusCallbackPayload) {
			p.WorkerStatus, p.StopCause, p.Result = "failed", protocol.StopCauseKilled, nil
		}, ""},
		{"CLAUDE.md", func(p *protocol.StatusCallbackPayload) { gp(p).File = "CLAUDE.md" }, ""},
		{"a symlink target", func(p *protocol.StatusCallbackPayload) { gp(p).File = "docs/AGENTS.md" }, ""},
		{"no file", func(p *protocol.StatusCallbackPayload) { gp(p).File = "" }, "file"},
		{"a rooted file", func(p *protocol.StatusCallbackPayload) { gp(p).File = "/AGENTS.md" }, "file"},
		{"a dot segment", func(p *protocol.StatusCallbackPayload) { gp(p).File = "./AGENTS.md" }, "file"},
		{"a parent segment", func(p *protocol.StatusCallbackPayload) { gp(p).File = "../AGENTS.md" }, "file"},
		{"an inner parent segment", func(p *protocol.StatusCallbackPayload) { gp(p).File = "docs/../AGENTS.md" }, "file"},
		{"an empty segment", func(p *protocol.StatusCallbackPayload) { gp(p).File = "docs//AGENTS.md" }, "file"},
		{"a NUL", func(p *protocol.StatusCallbackPayload) { gp(p).File = "a\x00b" }, "file"},
		{"a file at 512 B", func(p *protocol.StatusCallbackPayload) { gp(p).File = bytesOf(512) }, ""},
		{"a file over 512 B", func(p *protocol.StatusCallbackPayload) { gp(p).File = bytesOf(513) }, "file"},
		{"a 64 digit sha", func(p *protocol.StatusCallbackPayload) { gp(p).HeadSHA = strings.Repeat("0", 64) }, ""},
		{"a short sha", func(p *protocol.StatusCallbackPayload) { gp(p).HeadSHA = "xyz" }, "head_sha"},
		{"an upper-case sha", func(p *protocol.StatusCallbackPayload) { gp(p).HeadSHA = strings.Repeat("A", 40) }, "head_sha"},
		{"an http URL", func(p *protocol.StatusCallbackPayload) { gp(p).PRURL = "http://github.com/o/r/pull/7" }, "pr_url"},
		{"no URL", func(p *protocol.StatusCallbackPayload) { gp(p).PRURL = "" }, "pr_url"},
		{"a URL over 2 KiB", func(p *protocol.StatusCallbackPayload) { gp(p).PRURL = "https://" + bytesOf(2<<10) }, "pr_url"},
		{"refused", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused, AbsentFromRepo: []string{"make gen"}}
		}, ""},
		{"refused with 64", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused, AbsentFromRepo: entries(64, 512)}
		}, ""},
		{"refused with none", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused}
		}, "absent"},
		{"refused with 65", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused, AbsentFromRepo: entries(65, 8)}
		}, "absent_from_repo"},
		{"refused with an entry over 512 B", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceRefused, AbsentFromRepo: entries(1, 513)}
		}, "absent_from_repo"},
		{"stale", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceStale}
		}, ""},
		{"stale with a URL", func(p *protocol.StatusCallbackPayload) { gp(p).Outcome = protocol.GuidanceStale }, "stale"},
		{"stale with an absent entry", func(p *protocol.StatusCallbackPayload) {
			*gp(p) = protocol.GuidancePRResult{Outcome: protocol.GuidanceStale, AbsentFromRepo: []string{"x"}}
		}, "stale"},
		{"an unknown outcome", func(p *protocol.StatusCallbackPayload) { gp(p).Outcome = "merged" }, "outcome"},
		{"usage", func(p *protocol.StatusCallbackPayload) {
			p.Usage = []protocol.RunUsage{{Model: "vendor/model-a", LogTokenUsage: protocol.LogTokenUsage{InputTokens: 10}}}
		}, "usage"},
		{"an analyse result", func(p *protocol.StatusCallbackPayload) {
			p.Result = &protocol.RunKindResult{Analyse: &protocol.AnalyseResult{}}
		}, "exactly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := guidanceStatus()
			if tt.mutate != nil {
				tt.mutate(&p)
			}

			err := ValidateRunKindStatus(p, 100)
			if tt.want == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.want)
			}
		})
	}
}
