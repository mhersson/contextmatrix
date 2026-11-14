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
