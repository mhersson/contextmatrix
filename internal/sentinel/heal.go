package sentinel

import (
	"fmt"
	"slices"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// relaunchGrace is how long a relaunched card may take to start its rerun.
const relaunchGrace = 10 * time.Minute

var reasonAttemptsUsed = fmt.Sprintf("heal attempts used (%[1]d of %[1]d)", state.MaxHealAttempts)

const reasonTakenOver = "a human or another run took over"

type stepKind int

const (
	stepWait stepKind = iota
	stepDispatch
	stepAdvance
	stepApply
	stepUsageRerun
	stepPark
	stepClose
	stepSettle
)

// step is what a pass does next for one heal.
type step struct {
	Kind          stepKind
	RunKind       string // stepDispatch
	Stage         string // stepAdvance and stepSettle
	Reason        string // stepPark and stepClose
	ChargeCeiling bool   // stepPark for a dispatch that never reported
}

// healView is what the pass read for one open heal.
type healView struct {
	Heal     state.Heal
	Card     *board.Card // nil = deleted
	Run      state.Run   // the card's current run row
	Healing  bool
	Budget   bool     // budget gate open for the effective mode
	Room     bool     // a worker slot is free for a run kind
	Listed   bool     // the in-flight dispatch's container is listed
	Held     []string // run kinds another heal of the card still has in flight
	Edited   []string // checkEdits violations
	Rejected []string // vetFix violations, computed at stage apply when the heal holds a fix
	Now      time.Time
}

// nextStep decides a heal's next step from what the pass read; the first rule
// that matches wins.
func nextStep(v healView) step {
	h := v.Heal

	switch {
	case h.Stage == state.StageRerun && v.Card != nil:
		return rerunStep(v)
	case v.Card == nil || board.IsTerminalState(v.Card.State):
		return closeStep("the card was closed")
	case takenOver(v):
		return closeStep(reasonTakenOver)
	case len(v.Edited) > 0:
		return parkStep("edited since launch: " + strings.Join(v.Edited, ", "))
	case inFlight(h.Dispatch):
		return inFlightStep(v)
	case !v.Healing:
		return waitStep()
	}

	switch h.Stage {
	case state.StageInvestigate:
		return investigateStep(v)
	case state.StageCheck:
		return checkStep(v)
	case state.StageApply:
		return applyStep(v)
	case state.StageUsageWait:
		return usageWaitStep(v)
	}

	return waitStep()
}

// rerunStep waits for the relaunched run, then settles on its outcome. The
// rerun holds the card's claim and worker, so only a run Sentinel's rerun
// could not have launched counts as a takeover.
func rerunStep(v healView) step {
	if !rerunStarted(v.Heal, v.Run) {
		if younger(v.Heal.RelaunchedAt, relaunchGrace, v.Now) {
			return waitStep()
		}

		return parkStep("the rerun did not start")
	}

	switch {
	case !rerunOrigin(v.Run.Launch.Origin):
		return closeStep(reasonTakenOver)
	case workerBusy(v.Card):
		return waitStep()
	case v.Card.State == board.StateDone:
		return settleStep(state.StageHealed)
	}

	key, settled := causeKey(v.Run, v.Card, nil, v.Now)

	switch {
	case !settled:
		return waitStep()
	case key == protocol.StopCauseDone:
		return settleStep(state.StageHealed)
	}

	return settleStep(state.StageRerunFailed)
}

// rerunStarted reports whether the run row is the rerun's: every launch
// replaces the row, so the rerun's started at or after the relaunch.
func rerunStarted(h state.Heal, run state.Run) bool {
	return h.RelaunchedAt != nil && !run.StartedAt.Before(*h.RelaunchedAt)
}

// rerunOrigin reports whether a Sentinel rerun launches with origin: alone
// it is sentinel, through a playbook run's Play it is playbook.
func rerunOrigin(origin string) bool {
	return origin == state.OriginSentinel || origin == state.OriginPlaybook
}

// takenOver reports whether someone else is working the card, or a newer run
// replaced the stopped one.
func takenOver(v healView) bool {
	return workerBusy(v.Card) || v.Card.AssignedAgent != "" || !v.Run.StartedAt.Equal(v.Heal.RunStarted)
}

func inFlightStep(v healView) step {
	d := v.Heal.Dispatch
	if !unreported(d, v.Listed, v.Now) {
		return waitStep()
	}

	return step{Kind: stepPark, Reason: unreportedReason(d.Kind), ChargeCeiling: true}
}

// unreported reports whether an in-flight dispatch can no longer report: its
// container is not listed and the settle grace since it was sent has passed.
func unreported(d state.Dispatch, listed bool, now time.Time) bool {
	return !listed && !younger(d.At, settleGrace, now)
}

func unreportedReason(kind string) string {
	return fmt.Sprintf("the %s run did not report", kind)
}

func investigateStep(v healView) step {
	h := v.Heal

	switch {
	case !dispatched(h, protocol.RunKindInvestigate) && v.Budget && sendable(v, protocol.RunKindInvestigate):
		return dispatchStep(protocol.RunKindInvestigate)
	case !dispatched(h, protocol.RunKindInvestigate):
		return waitStep()
	case h.Investigation == nil:
		return parkStep("investigation failed: " + h.Dispatch.Failure)
	case h.Response == RespInvestigatePark:
		return parkStep(diagnosisReason(*h.Investigation))
	}

	return diagnosisStep(*h.Investigation)
}

// diagnosisStep acts on the investigation of a cause Sentinel may fix.
func diagnosisStep(inv protocol.InvestigateResult) step {
	cardFix := inv.Cause == protocol.DiagnosisCard && inv.Fix != nil

	switch {
	case cardFix && needsCheck(*inv.Fix):
		return advanceStep(state.StageCheck)
	case cardFix, inv.Cause == protocol.DiagnosisModel:
		return advanceStep(state.StageApply)
	case inv.Cause == protocol.DiagnosisEnvironment, inv.Cause == protocol.DiagnosisEcosystem:
		return parkStep(diagnosisReason(inv))
	}

	return parkStep("the investigation proposed no usable fix")
}

// healFix is the fix a heal may apply: only a card diagnosis holds one,
// whatever another diagnosis's fix field carries.
func healFix(h state.Heal) *protocol.CardFix {
	if h.Investigation == nil || h.Investigation.Cause != protocol.DiagnosisCard {
		return nil
	}

	return h.Investigation.Fix
}

// needsCheck reports whether a fix has anything for the check run kind: it
// trials a verify change and checks section text, so a retire-only fix skips it.
func needsCheck(fix protocol.CardFix) bool {
	return fix.Section != "" || fix.Verify != nil
}

func diagnosisReason(inv protocol.InvestigateResult) string {
	return fmt.Sprintf("%s diagnosis: %s", inv.Cause, inv.Diagnosis)
}

// checkStep runs the check kind, which runs no model, so the budget gate does
// not hold it.
func checkStep(v healView) step {
	h := v.Heal

	switch {
	case !dispatched(h, protocol.RunKindCheck) && sendable(v, protocol.RunKindCheck):
		return dispatchStep(protocol.RunKindCheck)
	case !dispatched(h, protocol.RunKindCheck):
		return waitStep()
	case h.Check == nil:
		return parkStep("check failed: " + h.Dispatch.Failure)
	}

	return advanceStep(state.StageApply)
}

func applyStep(v healView) step {
	switch {
	case len(v.Rejected) > 0:
		return parkStep("fix rejected: " + strings.Join(v.Rejected, ", "))
	case v.Run.Attempts >= state.MaxHealAttempts:
		return parkStep(reasonAttemptsUsed)
	case !v.Budget:
		return waitStep()
	}

	return step{Kind: stepApply}
}

func usageWaitStep(v healView) step {
	switch {
	case v.Run.UsageReruns >= state.MaxUsageReruns:
		return parkStep(fmt.Sprintf("usage-limit reruns used (%[1]d of %[1]d)", state.MaxUsageReruns))
	case v.Card.ParkResetsAt != nil && v.Card.ParkResetsAt.After(v.Now):
		return waitStep()
	case !v.Budget:
		return waitStep()
	}

	return step{Kind: stepUsageRerun}
}

// inFlight reports whether d was sent and has neither been answered nor
// marked failed.
func inFlight(d state.Dispatch) bool {
	return d.At != nil && d.AnsweredRunID == "" && d.Failure == ""
}

// sendable reports whether a run of kind may go out: a worker slot is free and
// no other heal of the card has one in flight, so a report always has one
// dispatch to answer.
func sendable(v healView, kind string) bool {
	return v.Room && !slices.Contains(v.Held, kind)
}

// dispatched reports whether the heal's latest dispatch is of kind. At stage
// check it still holds the answered investigation until the check is sent.
func dispatched(h state.Heal, kind string) bool {
	return h.Dispatch.Kind == kind && h.Dispatch.At != nil
}

// mayDispatch reports whether nextStep could send a run kind for h, so a pass
// reads the agent's health only when one may go out.
func mayDispatch(h state.Heal, budget bool) bool {
	switch h.Stage {
	case state.StageInvestigate:
		return budget && !dispatched(h, protocol.RunKindInvestigate)
	case state.StageCheck:
		return !dispatched(h, protocol.RunKindCheck)
	}

	return false
}

func workerBusy(card *board.Card) bool {
	return card.WorkerStatus == "queued" || card.WorkerStatus == "running"
}

// younger reports whether at is less than grace before now; a missing time is
// never younger.
func younger(at *time.Time, grace time.Duration, now time.Time) bool {
	return at != nil && now.Sub(*at) < grace
}

func waitStep() step { return step{Kind: stepWait} }

func dispatchStep(kind string) step { return step{Kind: stepDispatch, RunKind: kind} }

func advanceStep(stage string) step { return step{Kind: stepAdvance, Stage: stage} }

func settleStep(stage string) step { return step{Kind: stepSettle, Stage: stage} }

func parkStep(reason string) step { return step{Kind: stepPark, Reason: reason} }

func closeStep(reason string) step { return step{Kind: stepClose, Reason: reason} }
