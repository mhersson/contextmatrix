package board

import "strings"

// UpsertSection replaces the "## <heading>" block in body (up to the next H2
// outside code fences) or appends it when absent. heading is the H2 title
// without the "## " prefix. Idempotent: re-applying the same content is a
// no-op, so retries and resume paths never duplicate sections.
func UpsertSection(body, heading, content string) string {
	marker := "## " + heading
	section := marker + "\n\n" + strings.TrimRight(content, "\n") + "\n"

	lines := strings.SplitAfter(body, "\n")
	inFence := false

	start, end := -1, len(lines)
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
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
