package sentinel

import (
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

// Launch origins. Only OriginREST resets a card's heal attempts.
const (
	OriginREST     = "rest"     // a UI or REST Run: the only origin that resets attempts
	OriginMCP      = "mcp"      // run_card
	OriginPlaybook = "playbook" // a playbook step, from run_playbook or a UI Play
	OriginSentinel = "sentinel" // Sentinel's own rerun of a card outside a playbook run
)

// MaxHealAttempts is the heal reruns allowed per card between UI or REST Runs.
const MaxHealAttempts = 2

// LaunchFacts is what was true when a run was launched.
type LaunchFacts struct {
	Snapshot   board.EditSnapshot
	Autonomous bool
	Origin     string
}

// Run is a card's run row: the last run this instance launched for the card.
type Run struct {
	Project, CardID, Mode string
	StartedAt             time.Time
	EndedAt, StoppedAt    *time.Time // EndedAt: the card's worker ended; StoppedAt: the run's terminal status arrived
	Launch                LaunchFacts
	Attempts              int    // heal reruns since the last OriginREST launch
	RunID, StopCause      string // StopCause "" = unknown
	HumanStop             bool
}
