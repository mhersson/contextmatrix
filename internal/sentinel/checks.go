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
	Cause        string              // the heal's cause; a forward's is its source heal's
	Fix          protocol.CardFix
	Check        *protocol.CheckResult
}

// check returns one violation per problem it finds, none when the fix passes.
type check func(fixContext) []string

// violation is one problem vetFix found; section marks a section check's,
// which dropping the fix's section clears.
type violation struct {
	text    string
	section bool
}

// A new check is one function appended to these lists. The section checks
// judge only the fix's section, and heals and forwards both run them.
var (
	healChecks    = []check{checkShape, checkRetire, checkEdits, checkVerify}
	forwardChecks = []check{checkShape, checkVerify}
	sectionChecks = []check{checkSection, checkText}
)

func vetFix(c fixContext) []violation {
	return append(tag(runChecks(healChecks, c), false), tag(runChecks(sectionChecks, c), true)...)
}

func vetForward(c fixContext) []string {
	return append(runChecks(forwardChecks, c), runChecks(sectionChecks, c)...)
}

func runChecks(checks []check, c fixContext) []string {
	var violations []string

	for _, ck := range checks {
		violations = append(violations, ck(c)...)
	}

	return violations
}

func tag(texts []string, section bool) []violation {
	vs := make([]violation, 0, len(texts))
	for _, t := range texts {
		vs = append(vs, violation{text: t, section: section})
	}

	return vs
}

// texts are the violations as a park reason names them.
func texts(vs []violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.text)
	}

	return out
}
