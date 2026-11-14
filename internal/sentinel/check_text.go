package sentinel

import (
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// agentHeadings are the H2 sections agents write on a card: the
// contextmatrix-agent orchestrator's recordSection headings and those CM's
// workflow-skills tell agents to upsert. Review Findings and Copilot Review
// rounds after the first carry a " (Round N)" suffix.
var agentHeadings = []string{
	"Plan", "Decisions", "Design", "Diagnosis", "Discussion", "Execute Discussions",
	"Split", "Unreachable Criteria", "Progress", "Verify Command", "Best-of-N Report",
	"Review Findings", "Review Approval", "Review Parked", "Copilot Review", "PR Gates",
}

// checkText fails when the check run kind found text the repo lacks and the
// card's human-written title and body lack too. Sentinel's own section and
// the sections agents write are excluded, so text a model wrote earlier
// cannot whitelist new text.
func checkText(c fixContext) []string {
	if c.Fix.Section == "" {
		return nil
	}

	if c.Check == nil {
		return []string{"text not checked"}
	}

	human := humanText(c.Card)

	var violations []string

	for _, entry := range c.Check.AbsentFromRepo {
		if !strings.Contains(human, entry) {
			violations = append(violations, "introduces "+entry)
		}
	}

	return violations
}

func humanText(card *board.Card) string {
	intro, sections := board.BodySections(card.Body)

	var b strings.Builder

	b.WriteString(card.Title + "\n" + intro)

	for _, s := range sections {
		if s.Heading != SectionHeading && !agentWritten(s.Heading) {
			b.WriteString("## " + s.Heading + "\n" + s.Content)
		}
	}

	return b.String()
}

// agentWritten reports whether heading is one of agentHeadings, round suffix
// aside.
func agentWritten(heading string) bool {
	base, _, _ := strings.Cut(heading, " (Round ")

	return slices.Contains(agentHeadings, base)
}
