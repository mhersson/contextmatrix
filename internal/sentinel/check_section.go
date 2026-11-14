package sentinel

import (
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// laterHeading stands for a section written after Sentinel's, appended as
// UpsertSection appends one; it is compared by position, so a card heading of
// the same name does not matter.
const laterHeading = "Later"

// checkSection judges the write, not the text alone. It builds the body as
// the writer does, then a section appended after it, and the card's parser
// must read the card's own headings, then Sentinel's when the card had none,
// then the later one. So the section cannot create, hide or swallow a
// section, whatever fence the card or the section leaves open.
func checkSection(c fixContext) []string {
	if c.Fix.Section == "" {
		return nil
	}

	want := headings(c.Card.Body)
	if !slices.Contains(want, SectionHeading) {
		want = append(want, SectionHeading)
	}

	written := board.UpsertSection(c.Card.Body, SectionHeading, board.DemoteHeadings(c.Fix.Section))
	later := strings.TrimRight(written, "\n") + "\n\n## " + laterHeading + "\n"

	if !slices.Equal(headings(later), append(want, laterHeading)) {
		return []string{"section would change the card's sections"}
	}

	return nil
}

func headings(body string) []string {
	_, sections := board.BodySections(body)

	hs := make([]string, 0, len(sections))
	for _, s := range sections {
		hs = append(hs, s.Heading)
	}

	return hs
}
