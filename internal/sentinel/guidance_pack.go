package sentinel

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// maxBodyLine is the most runes of a finding's text or evidence the PR body
// shows.
const maxBodyLine = 300

// maxBodyBytes is the agent's limit on a guidance PR body.
const maxBodyBytes = 60 << 10

// maxPRTitleBytes is the agent's limit on a guidance PR title.
const maxPRTitleBytes = 256

const prBodyIntro = "Sentinel proposes this guidance from friction in this project's agent runs; each item names the evidence it came from."

const reasonTooLong = "too long for the guidance section"

// packSection joins whole findings' texts by a blank line, proposed first and
// then pending, in their order, while the section stays within maxBytes. A
// finding that does not fit is skipped and the next one tried. A pack that
// holds no pending finding proposes nothing new, so it is empty.
func packSection(proposed, pending []state.Finding, maxBytes int) (section string, ids []int64) {
	var (
		b          strings.Builder
		hasPending bool
	)

	for i, f := range slices.Concat(proposed, pending) {
		text := strings.TrimSpace(f.Text)

		sep := ""
		if b.Len() > 0 {
			sep = "\n\n"
		}

		if text == "" || b.Len()+len(sep)+len(text) > maxBytes {
			continue
		}

		b.WriteString(sep + text)

		ids = append(ids, f.ID)
		hasPending = hasPending || i >= len(proposed)
	}

	if !hasPending {
		return "", nil
	}

	return b.String(), ids
}

// tooLong lists the findings whose text alone, trimmed as packSection packs
// it, is over maxBytes: no section can hold them.
func tooLong(fs []state.Finding, maxBytes int) []state.Finding {
	var long []state.Finding

	for _, f := range fs {
		if len(strings.TrimSpace(f.Text)) > maxBytes {
			long = append(long, f)
		}
	}

	return long
}

func prTitle(project string) string {
	const prefix = "Sentinel guidance for "

	if len(prefix)+len(project) <= maxPRTitleBytes {
		return prefix + project
	}

	const ellipsis = "..."

	budget := maxPRTitleBytes - len(prefix) - len(ellipsis)

	// Cut at the last rune boundary that fits, so the title stays valid UTF-8.
	cut := 0

	for i, r := range project {
		end := i + utf8.RuneLen(r)
		if end > budget {
			break
		}

		cut = end
	}

	return prefix + project[:cut] + ellipsis
}

// prBody is one intro line, then an item per finding: its text's first line,
// and its evidence's below it. Items stop before the body would pass
// maxBodyBytes, and a last line counts the findings left out.
func prBody(fs []state.Finding) string {
	var b strings.Builder

	b.WriteString(prBodyIntro + "\n")

	for i, f := range fs {
		item := "\n- " + firstLine(f.Text, maxBodyLine) + "\n  Evidence: " + firstLine(f.Evidence, maxBodyLine)

		// Room is kept for the count line the items after this one would need.
		if b.Len()+len(item)+len(unlisted(len(fs)-i-1)) > maxBodyBytes {
			b.WriteString(unlisted(len(fs) - i))

			break
		}

		b.WriteString(item)
	}

	return b.String()
}

// unlisted is the body's last line when n findings are left out of it; none
// when n is 0.
func unlisted(n int) string {
	if n == 0 {
		return ""
	}

	return fmt.Sprintf("\n\nFindings in the section not listed here: %d.", n)
}

// withIDs is the findings of fs whose IDs ids lists, in fs's order.
func withIDs(fs []state.Finding, ids []int64) []state.Finding {
	return slices.DeleteFunc(slices.Clone(fs), func(f state.Finding) bool { return !slices.Contains(ids, f.ID) })
}
