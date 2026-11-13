package sentinel

import (
	"errors"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

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

// MaxUsageReruns is the usage-limit reruns allowed per card between UI or REST Runs.
const MaxUsageReruns = 3

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
	UsageReruns           int    // usage-limit reruns since the last OriginREST launch
	RunID, StopCause      string // StopCause "" = unknown
	HumanStop             bool
	Decided               bool // Sentinel has looked at this run
}

// Heal row kinds: a heal follows one stopped run; a forward row records a fix
// applied to a card from another card's heal.
const (
	HealKindHeal    = "heal"
	HealKindForward = "forward"
)

// Heal stages; the last three are a forward row's.
const (
	StageInvestigate   = "investigate"
	StageCheck         = "check"
	StageApply         = "apply"
	StageUsageWait     = "usage_wait"
	StageRerun         = "rerun"
	StageHealed        = "healed"
	StageRerunFailed   = "rerun_failed"
	StageParked        = "parked"
	StageClosed        = "closed"
	StageForwarded     = "forwarded"
	StageForwardPassed = "forward_passed"
	StageForwardFailed = "forward_failed"
)

// Terminal reports whether a row in stage never moves again.
func Terminal(stage string) bool {
	switch stage {
	case StageHealed, StageRerunFailed, StageParked, StageClosed, StageForwardPassed, StageForwardFailed:
		return true
	}

	return false
}

// Dispatch is a heal's latest run kind, in flight while At is set and neither
// AnsweredRunID nor Failure is.
type Dispatch struct {
	Kind, Mode    string // the run kind; the LLM channel mode it ran on
	CeilingUSD    float64
	At            *time.Time
	AnsweredRunID string // set once its terminal status is recorded
	Failure       string // a terminal status without a result: its message, else its stop cause
}

// Change is what a heal or a forward did to the card.
type Change struct {
	SectionBefore, SectionAfter string
	VerifyBefore, VerifyAfter   *board.VerifyConfig
	Retired                     []string
	Fresh                       bool
}

// ErrHealChanged is SaveHeal's answer when the row was written after the heal
// was read.
var ErrHealChanged = errors.New("heal changed since it was read")

// Heal is one heal or forward row.
type Heal struct {
	ID                         int64
	Kind, Project, CardID      string
	RunStarted                 time.Time // the stopped run's start; zero for a forward row
	RunID, Cause, Response     string
	Stage, Reason              string
	Dispatch                   Dispatch
	Investigation              *protocol.InvestigateResult
	Check                      *protocol.CheckResult
	SentSubtasks, SentSiblings []string
	Change                     *Change
	RelaunchedAt               *time.Time
	CostBaseUSD                float64 // the card's cost at relaunch
	SourceCard                 string  // forward rows: the card the fix came from
	SpendUSD                   float64 // read-only: sum of the heal's ledger rows
	Version                    int64   // read-only: bumped by every write to the row
	CreatedAt, UpdatedAt       time.Time
}

// Finding is one friction finding a heal's investigation reported.
type Finding struct {
	HealID                      int64
	Kind, Title, Text, Evidence string
	CreatedAt                   time.Time
}

// RunKindReport is a run kind's terminal status as the store records it.
type RunKindReport struct {
	Project, CardID, Kind, RunID string
	Failure                      string // terminal status without a result: message, else stop cause
	// Result is the kind's own result object as JSON, RunKindResult.Investigate
	// or .Check, never the RunKindResult wrapper; nil when failed.
	Result        []byte
	SpendUSD      float64
	ChargeCeiling bool // model kind ended without usage
	At            time.Time
}
