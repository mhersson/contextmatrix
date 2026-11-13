package sentinel

import (
	"fmt"
	"slices"
)

func checkShape(c fixContext) []string {
	var violations []string

	if c.Fix.Section == "" && c.Fix.Verify == nil && len(c.Fix.RetireSubtasks) == 0 {
		violations = append(violations, "fix changes nothing")
	}

	for _, id := range c.Fix.RetireSubtasks {
		if !slices.Contains(c.SentSubtasks, id) {
			violations = append(violations, fmt.Sprintf("retires %s, which was not sent", id))
		}
	}

	return violations
}
