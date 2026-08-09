package mcp

import (
	"fmt"
	"strings"
)

// Section allowlists for the skill surfaces whose injected card body is
// filtered. Only late-run, high-fan-out surfaces filter: early-run skills
// (create-plan, plan-draft, brainstorming, systematic-debugging,
// run-autonomous) and the execute-task subtask's own card keep the full
// body - at those points the body is small or is itself the spec the skill
// consumes.
var (
	// reviewTaskBodySections: spec compliance needs the description (the
	// pre-heading intro, always kept) plus the plan; round numbering needs
	// every prior findings section; the planner's recorded decisions inform
	// the design and scope judgment. Diagnosis/Design are fetched via
	// get_card on demand.
	reviewTaskBodySections = []string{"## Plan", "## Review Findings", "## Decisions"}
	// documentTaskBodySections: documentation derives from the plan, the
	// subtask work, and the planner's recorded decisions (the why behind
	// the docs); review findings never feed docs (fix subtasks carry the
	// finding text verbatim in their own bodies).
	documentTaskBodySections = []string{"## Plan", "## Decisions"}
	// executeParentBodySections: execute-task names exactly one parent
	// section - "Parent card's plan (under ## Plan)".
	executeParentBodySections = []string{"## Plan"}
)

// bodySection is one H2 heading's title and the line index it starts at,
// as found by scanBodySections.
type bodySection struct {
	title string
	start int
}

// scanBodySections splits body into lines and locates its H2 ("## ")
// section boundaries. H2 lines inside fenced code blocks (``` or ~~~) are
// not boundaries - card bodies embed markdown templates containing literal
// section headings. Returns the split lines and the sections found, in
// document order; sections is nil when body has no H2 headings.
func scanBodySections(body string) (lines []string, sections []bodySection) {
	lines = strings.Split(body, "\n")

	inFence := false

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence

			continue
		}

		if inFence {
			continue
		}

		if strings.HasPrefix(line, "## ") {
			sections = append(sections, bodySection{title: strings.TrimSpace(line[3:]), start: i})
		}
	}

	return lines, sections
}

// sectionEnd returns the line index (exclusive) where the section at index
// idx ends: the start of the next section, or the end of the document.
func sectionEnd(lines []string, sections []bodySection, idx int) int {
	if idx+1 < len(sections) {
		return sections[idx+1].start
	}

	return len(lines)
}

// filterBodySections reduces body to its pre-heading intro plus the H2
// sections whose headings match keep. Returns the filtered body and the
// titles (without "## ") of omitted sections, in original order.
//
// Fallback contract: nil keep, a body with no H2 headings, or a body in
// which no keep entry matches anything all return (body, nil) unchanged -
// early-run bodies, per-type templates, and the unvetted-body placeholder
// pass through. The failure direction is always over-injection, never
// wrongful omission.
func filterBodySections(body string, keep []string) (string, []string) {
	if len(keep) == 0 || body == "" {
		return body, nil
	}

	lines, sections := scanBodySections(body)
	if len(sections) == 0 {
		return body, nil
	}

	anyMatch := false

	for _, s := range sections {
		if sectionMatches(s.title, keep) {
			anyMatch = true

			break
		}
	}

	if !anyMatch {
		return body, nil
	}

	var kept, omitted []string

	kept = append(kept, lines[:sections[0].start]...)

	for idx, s := range sections {
		if sectionMatches(s.title, keep) {
			kept = append(kept, lines[s.start:sectionEnd(lines, sections, idx)]...)
		} else {
			omitted = append(omitted, s.title)
		}
	}

	return strings.Join(kept, "\n"), omitted
}

// filterBodySectionsExact is the strict variant for caller-supplied section
// requests: unlike filterBodySections, a keep list that matches nothing
// returns "", never the full body - the caller asked for less and must get
// less. Only H2 titles match, via the same sectionMatches semantics as
// filterBodySections; the pre-heading intro is included only when keep
// contains the pseudo-entry "intro" (case-insensitive), matched separately
// from the heading titles.
func filterBodySectionsExact(body string, keep []string) string {
	if len(keep) == 0 || body == "" {
		return ""
	}

	wantIntro := false
	headingKeep := make([]string, 0, len(keep))

	for _, k := range keep {
		if strings.EqualFold(strings.TrimSpace(k), "intro") {
			wantIntro = true

			continue
		}

		headingKeep = append(headingKeep, k)
	}

	lines, sections := scanBodySections(body)

	if len(sections) == 0 {
		if wantIntro {
			return body
		}

		return ""
	}

	var kept []string

	if wantIntro {
		kept = append(kept, lines[:sections[0].start]...)
	}

	matched := false

	for idx, s := range sections {
		if sectionMatches(s.title, headingKeep) {
			kept = append(kept, lines[s.start:sectionEnd(lines, sections, idx)]...)
			matched = true
		}
	}

	if !matched && !wantIntro {
		return ""
	}

	return strings.Join(kept, "\n")
}

// sectionMatches reports whether a heading title matches a keep entry:
// case-insensitively equal, or a case-insensitive prefix whose next byte is
// a space or "(" - so "Review Findings (Round 2)" matches
// "## Review Findings" while "Planning" does not match "## Plan".
func sectionMatches(title string, keep []string) bool {
	for _, k := range keep {
		want := strings.TrimSpace(strings.TrimPrefix(k, "## "))
		if len(title) < len(want) || !strings.EqualFold(title[:len(want)], want) {
			continue
		}

		if len(title) == len(want) {
			return true
		}

		if next := title[len(want)]; next == ' ' || next == '(' {
			return true
		}
	}

	return false
}

// omittedSectionsNote renders the single-line marker naming body sections
// removed by filterBodySections, or "" when nothing was omitted. Titles
// carry no "## " prefix so substring checks for headings cannot
// false-positive on the note.
func omittedSectionsNote(cardID string, omitted []string) string {
	if len(omitted) == 0 {
		return ""
	}

	return fmt.Sprintf("[Body sections omitted from this context: %s. Run get_card(card_id='%s') to read the full body.]",
		strings.Join(omitted, "; "), cardID)
}
