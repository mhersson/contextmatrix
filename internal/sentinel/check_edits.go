package sentinel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

var editLabels = []struct{ key, label string }{
	{"title", "title edited"},
	{"verify", "verify edited"},
	{"autonomous", "autonomous changed"},
	{"intro", "intro edited"},
}

// checkEdits names every part of the card that differs from the launch snapshot.
func checkEdits(c fixContext) []string {
	if len(c.Snapshot) == 0 {
		return []string{"no launch snapshot"}
	}

	now := board.TakeEditSnapshot(c.Card)

	var violations []string

	for _, l := range editLabels {
		if now[l.key] != c.Snapshot[l.key] {
			violations = append(violations, l.label)
		}
	}

	var sections []string

	for k := range c.Snapshot {
		if strings.HasPrefix(k, "## ") {
			sections = append(sections, k)
		}
	}

	for k := range now {
		if strings.HasPrefix(k, "## ") && !slices.Contains(sections, k) {
			sections = append(sections, k)
		}
	}

	slices.Sort(sections)

	for _, k := range sections {
		before, hadBefore := c.Snapshot[k]
		after, hasNow := now[k]
		name := strings.TrimPrefix(k, "## ")

		switch {
		case !hadBefore:
			violations = append(violations, fmt.Sprintf("section %q added", name))
		case !hasNow:
			violations = append(violations, fmt.Sprintf("section %q removed", name))
		case before != after:
			violations = append(violations, fmt.Sprintf("section %q edited", name))
		}
	}

	return violations
}
