package sentinel

import "strings"

// checkSection refuses an unbalanced fence, which would swallow the sections
// after it. Headings are demoted when the section is written.
func checkSection(c fixContext) []string {
	var violations []string

	fences := 0

	for line := range strings.SplitSeq(c.Fix.Section, "\n") {
		t := strings.TrimSpace(line)

		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fences++
		}
	}

	if fences%2 != 0 {
		violations = append(violations, "section has an unbalanced code fence")
	}

	return violations
}
