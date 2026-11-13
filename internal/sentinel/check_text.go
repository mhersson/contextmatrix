package sentinel

import (
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// checkText fails when the check run kind found text the repo lacks and the
// card's human-written title and body lack too. Sentinel's own section is
// excluded so earlier fix text cannot whitelist itself.
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
		if s.Heading != SectionHeading {
			b.WriteString("## " + s.Heading + "\n" + s.Content)
		}
	}

	return b.String()
}
