package sentinel

import (
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
)

// settleGrace is how long a card whose worker ended may go unwritten before
// its run counts as settled without a terminal callback.
const settleGrace = backend.StaleWorkerGrace

// causeKey says whether the run has settled and names its cause. An agent
// parks the card over MCP before its container exits, so a parked card is not
// settled until the terminal callback brings the stop cause or the grace passes.
func causeKey(run Run, card *board.Card, forward *Heal, now time.Time) (key string, settled bool) {
	if card.State == board.StateStalled {
		return CauseStalled, true
	}

	if !settledRun(run, card, now) {
		return "", false
	}

	switch {
	case run.HumanStop:
		return CauseHumanStop, true
	case run.StopCause == protocol.StopCauseReviewParked && card.ReviewAttempts >= board.MaxReviewAttempts:
		return CauseReviewCap, true
	case run.StopCause != protocol.StopCauseDone && forwardedBefore(forward, run.StartedAt):
		return CauseForwardFixFailed, true
	case run.StopCause == "":
		return CauseUnknown, true
	}

	return run.StopCause, true
}

func settledRun(run Run, card *board.Card, now time.Time) bool {
	switch card.WorkerStatus {
	case "queued", "running":
		return false
	}

	if run.StoppedAt != nil {
		return true
	}

	switch card.WorkerStatus {
	case "failed", "killed", "parked":
		return now.Sub(card.Updated) >= settleGrace
	}

	return false
}

// forwardedBefore reports whether forward is a fix still on trial that was
// forwarded to the card before this run started.
func forwardedBefore(forward *Heal, runStart time.Time) bool {
	return forward != nil && forward.Kind == HealKindForward &&
		forward.Stage == StageForwarded && forward.CreatedAt.Before(runStart)
}

// eligible says whether Sentinel may act on a settled run at all, and if not, why.
func eligible(run Run, card *board.Card, claimedElsewhere bool) (ok bool, why string) {
	switch {
	case !run.Launch.Autonomous:
		return false, "not launched autonomous"
	case board.IsTerminalState(card.State):
		return false, "card is finished"
	case card.NeedsVetting():
		return false, "card needs vetting"
	case card.AssignedAgent != "":
		return false, "card is claimed"
	case claimedElsewhere:
		return false, "card is claimed by another instance"
	}

	return true, ""
}
