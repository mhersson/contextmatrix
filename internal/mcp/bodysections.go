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

// filterBodySections reduces body to its pre-heading intro plus the H2
// sections whose headings match keep. Returns the filtered body and the
// titles (without "## ") of omitted sections, in original order.
//
// Fallback contract: nil keep, a body with no H2 headings, or a body in
// which no keep entry matches anything all return (body, nil) unchanged -
// early-run bodies, per-type templates, and the unvetted-body placeholder
// pass through. The failure direction is always over-injection, never
// wrongful omission.
//
// H2 lines inside fenced code blocks (``` or ~~~) are not boundaries - card
// bodies embed markdown templates containing literal section headings.
func filterBodySections(body string, keep []string) (string, []string) {
	if len(keep) == 0 || body == "" {
		return body, nil
	}

	lines := strings.Split(body, "\n")

	type section struct {
		title string
		start int
	}

	var sections []section

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
			sections = append(sections, section{title: strings.TrimSpace(line[3:]), start: i})
		}
	}

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
		end := len(lines)
		if idx+1 < len(sections) {
			end = sections[idx+1].start
		}

		if sectionMatches(s.title, keep) {
			kept = append(kept, lines[s.start:end]...)
		} else {
			omitted = append(omitted, s.title)
		}
	}

	return strings.Join(kept, "\n"), omitted
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
