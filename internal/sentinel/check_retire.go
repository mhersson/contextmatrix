package sentinel

import (
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
)

// checkRetire allows retiring only a subtask of this card that has not started.
func checkRetire(c fixContext) []string {
	var violations []string

	for _, id := range c.Fix.RetireSubtasks {
		if reason := notRetirable(c, id); reason != "" {
			violations = append(violations, fmt.Sprintf("cannot retire %s: %s", id, reason))
		}
	}

	return violations
}

func notRetirable(c fixContext, id string) string {
	for _, s := range c.Subtasks {
		if s.ID != id {
			continue
		}

		switch {
		case s.Parent != c.Card.ID:
			return "not a subtask of this card"
		case s.State != board.StateTodo:
			return "already started"
		case s.AssignedAgent != "":
			return "claimed"
		case workerBusy(s):
			return "has a worker"
		}

		return ""
	}

	return "no such subtask"
}
