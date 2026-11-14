package sentinel

import (
	"strings"
	"unicode"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// prState is the state of rec's PR as GitHub shows it: open with the head
// Sentinel pushed, open with another head, merged, or closed unmerged.
func prState(rec state.StandingPR, p PullState) string {
	switch {
	case p.Merged:
		return state.PRMerged
	case !p.Open:
		return state.PRClosed
	case p.HeadSHA == rec.HeadSHA:
		return state.PROpen
	}

	return state.PRChangedOutside
}

// publishedText is a finding's text as the guidance file carries it: card IDs
// redacted with every project's prefix.
func publishedText(f state.Finding, prefixes []string) string {
	return redactCardIDs(f.Text, prefixes)
}

// mergedFates splits the proposed findings (texts as published) by the file at
// the merge commit. A finding is merged when at least one of its countable
// lines survives as a substring of the file's whitespace-normalised text (so a
// list marker or a fixed typo elsewhere does not matter); one whose countable
// lines are all gone, or any finding when the file is not found, is rejected.
// A line with no letter or digit, and a code fence, is not countable.
func mergedFates(file string, found bool, proposed []state.Finding) (merged, rejected []int64) {
	var text string
	if found {
		text = normalizeLines(file)
	}

	for _, f := range proposed {
		if found && survives(f.Text, text) {
			merged = append(merged, f.ID)
		} else {
			rejected = append(rejected, f.ID)
		}
	}

	return merged, rejected
}

func survives(findingText, normalizedFile string) bool {
	for line := range strings.SplitSeq(findingText, "\n") {
		l := strings.Join(strings.Fields(line), " ")
		if countable(l) && strings.Contains(normalizedFile, l) {
			return true
		}
	}

	return false
}

func countable(line string) bool {
	if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
		return false
	}

	return strings.ContainsFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}

// normalizeLines collapses each line's whitespace and trims it.
func normalizeLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}

	return strings.Join(lines, "\n")
}
