package sentinel

import (
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// queueAction is what the analysis queue does with one unqueued run.
type queueAction int

const (
	queueWait queueAction = iota // the run is still going; look again next pass
	queueSkip                    // mark the run queued without a row
	queueRow                     // store an analysis row and mark the run queued
)

// queueStep decides an unqueued run; card is nil when it no longer exists. A
// human stop is no friction to learn from.
func queueStep(run state.Run, card *board.Card, now time.Time) queueAction {
	switch {
	case card == nil, run.HumanStop:
		return queueSkip
	case !settledRun(run, card, now):
		return queueWait
	}

	return queueRow
}

// analysisMaxAge is how long an analysis may wait for its dispatch.
const analysisMaxAge = 7 * 24 * time.Hour

// Why an analysis row ended without its result.
const (
	reasonAnalysisUnreported   = "the analysis never reported"
	reasonAnalysisCardGone     = "the card was deleted"
	reasonAnalysisTooOld       = "not analysed within 7 days"
	reasonAnalysisInvestigated = "the investigation covered this run"
)

// analysisAction is what a pass does next for one open analysis.
type analysisAction int

const (
	analysisWait analysisAction = iota
	analysisDispatch
	analysisEnd
)

// analysisStep is one open analysis's next step.
type analysisStep struct {
	Action        analysisAction
	Stage, Reason string // analysisEnd
	ChargeCeiling bool   // analysisEnd for a dispatch that never reported
}

// analysisView is what the pass read for one open analysis.
type analysisView struct {
	Row      state.Heal
	Card     *board.Card // nil = deleted
	Heal     *state.Heal // the heal of the analysed run; nil = none
	Run      *state.Run  // the card's current run row; nil = none
	On       bool        // analyses are switched on
	Healing  bool
	GateOpen bool // the analysis gate is open for the effective mode
	Room     bool // a worker slot is free for a run kind
	Held     bool // another row of the card has an analyse in flight, or the agent holds the kind
	Listed   bool // the in-flight dispatch's container is listed
	Now      time.Time
}

// nextAnalysis decides an open analysis's next step; the first rule that
// matches wins. A run kind in flight or answered still finishes while
// analyses are off.
func nextAnalysis(v analysisView) analysisStep {
	row, wait := v.Row, analysisStep{Action: analysisWait}

	switch {
	case inFlight(row.Dispatch) && !unreported(row.Dispatch, v.Listed, v.Now):
		return wait
	case inFlight(row.Dispatch):
		s := analysisEnded(state.StageAnalyseFailed, reasonAnalysisUnreported)
		s.ChargeCeiling = true

		return s
	case row.Dispatch.At != nil && row.Analysis != nil:
		return analysisEnded(state.StageAnalysed, "")
	case row.Dispatch.At != nil:
		return analysisEnded(state.StageAnalyseFailed, row.Dispatch.Failure)
	case v.Card == nil:
		return analysisEnded(state.StageAnalyseSkipped, reasonAnalysisCardGone)
	case v.Now.Sub(row.CreatedAt) > analysisMaxAge:
		return analysisEnded(state.StageAnalyseSkipped, reasonAnalysisTooOld)
	case v.Heal != nil && v.Heal.Investigation != nil:
		return analysisEnded(state.StageAnalyseSkipped, reasonAnalysisInvestigated)
	case v.Healing && v.Heal != nil && v.Heal.Stage == state.StageInvestigate:
		return wait
	case v.Healing && v.Heal == nil && awaitsDecision(row, v.Run):
		return wait
	case !v.On, !v.GateOpen, !v.Room, v.Held:
		return wait
	}

	return analysisStep{Action: analysisDispatch}
}

// awaitsDecision reports whether healing may still open a heal for the
// analysed run: it is the card's run row, autonomous and undecided. The pass
// considers no other run, so another may stay undecided.
func awaitsDecision(row state.Heal, run *state.Run) bool {
	return run != nil && run.StartedAt.Equal(row.RunStarted) && run.Launch.Autonomous && !run.Decided
}

func analysisEnded(stage, reason string) analysisStep {
	return analysisStep{Action: analysisEnd, Stage: stage, Reason: reason}
}
