package board

import "strings"

// UpsertSection replaces the "## <heading>" block in body (up to the next H2
// outside code fences) or appends it when absent. heading is the H2 title
// without the "## " prefix. Idempotent: applying the same heading and
// content twice produces the same output, so retries and resume paths never
// duplicate sections.
func UpsertSection(body, heading, content string) string {
	marker := "## " + heading
	section := marker + "\n\n" + strings.TrimRight(content, "\n") + "\n"

	lines := strings.SplitAfter(body, "\n")
	inFence := false

	start, end := -1, len(lines)
	for i, line := range lines {
		// Right-trim only, so CRLF-terminated lines still compare equal to
		// the LF-only marker; the original bytes in lines are untouched, so
		// prefix/suffix joins stay byte-preserving.
		trimmed := strings.TrimRight(line, "\r\n")

		// Fence delimiters are matched left-trimmed (an indented fence, e.g.
		// under a list item, still hides its contents from the boundary
		// scan) but heading boundaries are not - an indented "## X" is never
		// a heading, matching internal/mcp/bodysections.go's convention.
		fenceCandidate := strings.TrimLeft(trimmed, " \t")
		if strings.HasPrefix(fenceCandidate, "```") || strings.HasPrefix(fenceCandidate, "~~~") {
			inFence = !inFence

			continue
		}

		if inFence {
			continue
		}

		if start == -1 && trimmed == marker {
			start = i

			continue
		}

		if start != -1 && strings.HasPrefix(trimmed, "## ") {
			end = i

			break
		}
	}

	if start == -1 {
		if strings.TrimSpace(body) == "" {
			return section
		}

		return strings.TrimRight(body, "\n") + "\n\n" + section
	}

	prefix := strings.Join(lines[:start], "")

	suffix := strings.Join(lines[end:], "")
	if suffix != "" {
		return prefix + section + "\n" + suffix
	}

	return prefix + section
}
