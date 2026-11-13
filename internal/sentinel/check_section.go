package sentinel

import "strings"

// checkSection refuses an H2 line (it would split the section) and an
// unbalanced fence (it would swallow the sections after it).
func checkSection(c fixContext) []string {
	var violations []string

	fences := 0

	for line := range strings.SplitSeq(c.Fix.Section, "\n") {
		t := strings.TrimSpace(line)

		if strings.HasPrefix(t, "## ") || t == "##" {
			violations = append(violations, "section holds an H2 line")
		}

		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fences++
		}
	}

	if fences%2 != 0 {
		violations = append(violations, "section has an unbalanced code fence")
	}

	return violations
}
