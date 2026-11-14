package sentinel

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// Run-kind status limits in bytes or entries. The worker applies them before it
// reports, so a status over one is malformed.
const (
	maxStatusBytes    = 1 << 20
	maxDiagnosisBytes = 32 << 10
	maxSectionBytes   = 16 << 10
	maxFixIDs         = 50 // retire_subtasks and forward_to each
	maxFindings       = 50
	maxTitleBytes     = 200
	maxFindingBytes   = 16 << 10
	maxEvidenceBytes  = 4 << 10
	maxCandidates     = 32
	maxAbsentFromRepo = 64
	maxRepoEntryBytes = 512
	maxUsageRows      = 16
	maxPRURLBytes     = 2 << 10
	maxFilePathBytes  = 512
)

// A run kind reports only its terminal status.
const (
	statusCompleted = "completed"
	statusFailed    = "failed"
)

var (
	trials       = []string{protocol.TrialFinished, protocol.TrialTimeout, protocol.TrialToolchain, protocol.TrialInfrastructure, protocol.TrialRejected}
	diagnoses    = []string{protocol.DiagnosisCard, protocol.DiagnosisModel, protocol.DiagnosisEnvironment, protocol.DiagnosisEcosystem}
	runKinds     = []string{protocol.RunKindCheck, protocol.RunKindInvestigate, protocol.RunKindAnalyse, protocol.RunKindGuidancePR}
	findingKinds = []string{
		protocol.FindingGuidance, protocol.FindingCode, protocol.FindingCardAdvice,
		protocol.FindingProjectVerify, protocol.FindingHardCard, protocol.FindingEcosystem,
	}
)

// ValidateRunKindStatus checks a run-kind status's values and limits; any
// breach is an error the handler answers with 422.
func ValidateRunKindStatus(p protocol.StatusCallbackPayload, bodyLen int) error {
	switch {
	case bodyLen > maxStatusBytes:
		return overLimit("the status body", bodyLen, "bytes", maxStatusBytes)
	case !slices.Contains(runKinds, p.Kind):
		return fmt.Errorf("unknown run kind %q", p.Kind)
	case p.Kind == protocol.RunKindGuidancePR && p.CardID != "":
		return errors.New("a guidance_pr status names no card")
	case p.Kind == protocol.RunKindGuidancePR && len(p.Usage) > 0:
		return errors.New("a guidance_pr status carries no usage")
	case p.Kind != protocol.RunKindGuidancePR && p.CardID == "":
		return errors.New("a run-kind status names its card")
	case p.WorkerStatus != statusCompleted && p.WorkerStatus != statusFailed:
		return fmt.Errorf("a run kind reports only completed or failed, not %q", p.WorkerStatus)
	}

	if err := validateUsage(p.Usage); err != nil {
		return err
	}

	switch {
	case p.Result == nil:
		return nil
	case p.WorkerStatus != statusCompleted || p.StopCause != protocol.StopCauseDone:
		return errors.New("a result comes only on a completed status with stop cause done")
	case resultMembers(p.Result) != 1:
		return fmt.Errorf("the result must hold exactly the %s result", p.Kind)
	}

	r := p.Result

	switch {
	case p.Kind == protocol.RunKindCheck && r.Check != nil:
		return validateCheck(r.Check)
	case p.Kind == protocol.RunKindInvestigate && r.Investigate != nil:
		return validateInvestigate(r.Investigate)
	case p.Kind == protocol.RunKindAnalyse && r.Analyse != nil:
		return validateFindings(r.Analyse.Findings)
	case p.Kind == protocol.RunKindGuidancePR && r.GuidancePR != nil:
		return validateGuidance(r.GuidancePR)
	}

	return fmt.Errorf("the result must hold exactly the %s result", p.Kind)
}

func resultMembers(r *protocol.RunKindResult) int {
	n := 0

	for _, set := range []bool{r.Check != nil, r.Investigate != nil, r.Analyse != nil, r.GuidancePR != nil} {
		if set {
			n++
		}
	}

	return n
}

// usesModel reports whether the kind runs a model, so an unreported cost is
// charged its ceiling.
func usesModel(kind string) bool {
	return kind == protocol.RunKindInvestigate || kind == protocol.RunKindAnalyse
}

// validateUsage holds usage to report_usage's rules: no negative token count
// or cost, and every subscription window named with a non-negative
// utilization. A cost total past the float range is refused too.
func validateUsage(rows []protocol.RunUsage) error {
	if len(rows) > maxUsageRows {
		return overLimit("usage", len(rows), "rows", maxUsageRows)
	}

	var total float64

	for _, r := range rows {
		if r.InputTokens < 0 || r.OutputTokens < 0 || r.CacheReadTokens < 0 || r.CacheCreateTokens < 0 {
			return fmt.Errorf("usage for %q has a negative token count", r.Model)
		}

		if !finiteNonNegative(r.CostUSD) {
			return fmt.Errorf("usage for %q has cost %v", r.Model, r.CostUSD)
		}

		total += r.CostUSD

		for _, w := range r.Subscription {
			if w.Window == "" || !finiteNonNegative(w.Utilization) {
				return fmt.Errorf("usage for %q has subscription window %q with utilization %v", r.Model, w.Window, w.Utilization)
			}
		}
	}

	if math.IsInf(total, 0) {
		return errors.New("usage costs sum past the float range")
	}

	return nil
}

func finiteNonNegative(x float64) bool { return x >= 0 && !math.IsInf(x, 1) }

func validateCheck(r *protocol.CheckResult) error {
	if err := validateEntries("candidates", r.Candidates, maxCandidates); err != nil {
		return err
	}

	if err := validateEntries("absent_from_repo", r.AbsentFromRepo, maxAbsentFromRepo); err != nil {
		return err
	}

	if r.Trial != "" && !slices.Contains(trials, r.Trial) {
		return fmt.Errorf("unknown trial %q", r.Trial)
	}

	return nil
}

func validateEntries(field string, entries []string, maxEntries int) error {
	if len(entries) > maxEntries {
		return overLimit(field, len(entries), "entries", maxEntries)
	}

	for _, e := range entries {
		if len(e) > maxRepoEntryBytes {
			return overLimit("an entry of "+field, len(e), "bytes", maxRepoEntryBytes)
		}
	}

	return nil
}

func validateInvestigate(r *protocol.InvestigateResult) error {
	switch {
	case len(r.Diagnosis) > maxDiagnosisBytes:
		return overLimit("diagnosis", len(r.Diagnosis), "bytes", maxDiagnosisBytes)
	case !slices.Contains(diagnoses, r.Cause):
		return fmt.Errorf("unknown cause %q", r.Cause)
	case len(r.ForwardTo) > maxFixIDs:
		return overLimit("forward_to", len(r.ForwardTo), "IDs", maxFixIDs)
	case r.Fix != nil && len(r.Fix.Section) > maxSectionBytes:
		return overLimit("fix.section", len(r.Fix.Section), "bytes", maxSectionBytes)
	case r.Fix != nil && len(r.Fix.RetireSubtasks) > maxFixIDs:
		return overLimit("fix.retire_subtasks", len(r.Fix.RetireSubtasks), "IDs", maxFixIDs)
	}

	return validateFindings(r.Findings)
}

func validateFindings(fs []protocol.FrictionFinding) error {
	if len(fs) > maxFindings {
		return overLimit("findings", len(fs), "entries", maxFindings)
	}

	for _, f := range fs {
		if err := validateFinding(f); err != nil {
			return err
		}
	}

	return nil
}

func validateFinding(f protocol.FrictionFinding) error {
	switch {
	case !slices.Contains(findingKinds, f.Kind):
		return fmt.Errorf("unknown finding kind %q", f.Kind)
	case len(f.Title) > maxTitleBytes:
		return overLimit("a finding title", len(f.Title), "bytes", maxTitleBytes)
	case len(f.Text) > maxFindingBytes:
		return overLimit("a finding text", len(f.Text), "bytes", maxFindingBytes)
	case len(f.Evidence) > maxEvidenceBytes:
		return overLimit("a finding's evidence", len(f.Evidence), "bytes", maxEvidenceBytes)
	}

	return nil
}

func validateGuidance(r *protocol.GuidancePRResult) error {
	switch r.Outcome {
	case protocol.GuidancePushed, protocol.GuidanceUnchanged:
		return validatePushedGuidance(r)
	case protocol.GuidanceRefused:
		if len(r.AbsentFromRepo) == 0 {
			return errors.New("a refused guidance result names the entries absent from the repo")
		}

		return validateEntries("absent_from_repo", r.AbsentFromRepo, maxAbsentFromRepo)
	case protocol.GuidanceStale:
		if r.PRURL != "" || r.HeadSHA != "" || r.File != "" || len(r.AbsentFromRepo) > 0 {
			return errors.New("a stale guidance result carries nothing")
		}

		return nil
	}

	return fmt.Errorf("unknown guidance outcome %q", r.Outcome)
}

func validatePushedGuidance(r *protocol.GuidancePRResult) error {
	switch {
	case !strings.HasPrefix(r.PRURL, "https://") || len(r.PRURL) > maxPRURLBytes:
		return fmt.Errorf("a %s guidance result needs an https pr_url of at most %d bytes", r.Outcome, maxPRURLBytes)
	case !lowerHex(r.HeadSHA, 40, 64):
		return fmt.Errorf("a %s guidance result needs a 40 or 64 digit lower-case hex head_sha", r.Outcome)
	case !repoRelativePath(r.File):
		return fmt.Errorf("a %s guidance result needs a clean repo-relative file", r.Outcome)
	}

	return nil
}

func lowerHex(s string, lengths ...int) bool {
	if !slices.Contains(lengths, len(s)) {
		return false
	}

	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

// repoRelativePath accepts a non-empty path inside the repo: no leading slash,
// no empty, "." or ".." segment, no NUL.
func repoRelativePath(p string) bool {
	if p == "" || len(p) > maxFilePathBytes || strings.ContainsRune(p, 0) {
		return false
	}

	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}

	return true
}

func overLimit(what string, n int, unit string, limit int) error {
	return fmt.Errorf("%s has %d %s, over %d", what, n, unit, limit)
}
