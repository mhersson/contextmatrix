package sentinel

import (
	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

// SectionHeading is the body section a fix may write.
const SectionHeading = "Sentinel"

// fixContext is everything a check reads; checks never touch I/O.
type fixContext struct {
	Card         *board.Card
	Snapshot     board.EditSnapshot  // the run row's; nil for a forward target
	Subtasks     []*board.Card       // the card's subtasks, read fresh
	SentSubtasks []string            // IDs the investigation saw
	Project      *board.VerifyConfig // the project's verify
	Fix          protocol.CardFix
	Check        *protocol.CheckResult
}

// check returns one violation per problem it finds, none when the fix passes.
type check func(fixContext) []string

// A new check is one function appended to these lists.
var (
	healChecks    = []check{checkShape, checkSection, checkRetire, checkEdits}
	forwardChecks = []check{checkShape, checkSection}
)

func vetFix(c fixContext) []string { return runChecks(healChecks, c) }

func vetForward(c fixContext) []string { return runChecks(forwardChecks, c) }

func runChecks(checks []check, c fixContext) []string {
	var violations []string

	for _, ck := range checks {
		violations = append(violations, ck(c)...)
	}

	return violations
}
