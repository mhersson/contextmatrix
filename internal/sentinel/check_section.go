package sentinel

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// checkSection judges the write, not the text alone. A card whose body ends
// inside a code fence takes no section: the parser would read the section's
// text in the opposite fence state to the one its headings were demoted in.
// Otherwise the body as written, with one more section appended under
// probeHeading, must read as the card's own headings, then Sentinel's when the
// card had none, then the probe. Demoted text read from outside a fence holds
// no heading, so the section can create, hide or swallow none.
func checkSection(c fixContext) []string {
	if c.Fix.Section == "" {
		return nil
	}

	if board.EndsInFence(c.Card.Body) {
		return []string{"the card ends inside a code fence"}
	}

	want := headings(c.Card.Body)
	if !slices.Contains(want, SectionHeading) {
		want = append(want, SectionHeading)
	}

	probe := probeHeading(c.Fix.Section)
	later := strings.TrimRight(writtenBody(c.Card.Body, c.Fix.Section), "\n") + "\n\n## " + probe + "\n"

	if !slices.Equal(headings(later), append(want, probe)) {
		return []string{"section would change the card's sections"}
	}

	return nil
}

// probeHeading names the section appended after the write, appended as
// UpsertSection appends one. It derives from the section's own hash, so the
// section would have to hold a hash of itself to forge it.
func probeHeading(section string) string {
	sum := sha256.Sum256([]byte(section))

	return "probe-" + hex.EncodeToString(sum[:8])
}

func headings(body string) []string {
	_, sections := board.BodySections(body)

	hs := make([]string, 0, len(sections))
	for _, s := range sections {
		hs = append(hs, s.Heading)
	}

	return hs
}
