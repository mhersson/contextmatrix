package sentinel

import (
	"fmt"
	"slices"

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
)

// A run kind reports only its terminal status.
const (
	statusCompleted = "completed"
	statusFailed    = "failed"
)

var (
	trials       = []string{protocol.TrialFinished, protocol.TrialTimeout, protocol.TrialToolchain, protocol.TrialInfrastructure, protocol.TrialRejected}
	diagnoses    = []string{protocol.DiagnosisCard, protocol.DiagnosisModel, protocol.DiagnosisEnvironment, protocol.DiagnosisEcosystem}
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
	case p.Kind != protocol.RunKindCheck && p.Kind != protocol.RunKindInvestigate:
		return fmt.Errorf("unknown run kind %q", p.Kind)
	case p.WorkerStatus != statusCompleted && p.WorkerStatus != statusFailed:
		return fmt.Errorf("a run kind reports only completed or failed, not %q", p.WorkerStatus)
	case len(p.Usage) > maxUsageRows:
		return overLimit("usage", len(p.Usage), "rows", maxUsageRows)
	case p.Result == nil:
		return nil
	case p.WorkerStatus != statusCompleted || p.StopCause != protocol.StopCauseDone:
		return fmt.Errorf("a result comes only on a completed status with stop cause done")
	case p.Kind == protocol.RunKindCheck && p.Result.Check != nil && p.Result.Investigate == nil:
		return validateCheck(p.Result.Check)
	case p.Kind == protocol.RunKindInvestigate && p.Result.Investigate != nil && p.Result.Check == nil:
		return validateInvestigate(p.Result.Investigate)
	}

	return fmt.Errorf("the result must hold exactly the %s result", p.Kind)
}

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
	case len(r.Findings) > maxFindings:
		return overLimit("findings", len(r.Findings), "entries", maxFindings)
	}

	for _, f := range r.Findings {
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

func overLimit(what string, n int, unit string, limit int) error {
	return fmt.Errorf("%s has %d %s, over %d", what, n, unit, limit)
}
