package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// depStatus describes a dependency that is not yet met.
type depStatus struct {
	ID    string
	State string
}

// checkDependencies checks if all cards in deps are in "done" state.
// Returns true if all deps are met (or deps is empty), plus a list of blocking deps.
func (s *CardService) checkDependencies(ctx context.Context, project string, deps []string) (bool, []depStatus) {
	if len(deps) == 0 {
		return true, nil
	}

	var blockers []depStatus

	for _, depID := range deps {
		dep, err := s.store.GetCard(ctx, project, depID)
		if err != nil {
			blockers = append(blockers, depStatus{ID: depID, State: "unknown"})

			continue
		}

		if dep.State != board.StateDone {
			blockers = append(blockers, depStatus{ID: depID, State: dep.State})
		}
	}

	return len(blockers) == 0, blockers
}

// dependencyError builds a ValidationError for unmet dependencies.
func dependencyError(targetState string, blockers []depStatus) error {
	parts := make([]string, len(blockers))
	for i, b := range blockers {
		parts[i] = fmt.Sprintf("%s (%s)", b.ID, b.State)
	}

	return fmt.Errorf("validate transition: %w", &board.ValidationError{
		Err:     board.ErrDependenciesNotMet,
		Field:   "state",
		Value:   targetState,
		Message: fmt.Sprintf("cannot transition to %q: blocked by dependencies: %s", targetState, strings.Join(parts, ", ")),
	})
}

// validateCardReferences checks that parent and depends_on IDs reference
// existing cards in the project. It also detects self-dependencies and
// circular dependency chains.
func (s *CardService) validateCardReferences(ctx context.Context, project, parent string, dependsOn []string) error {
	if parent != "" {
		parentCard, err := s.store.GetCard(ctx, project, parent)
		if err != nil {
			return fmt.Errorf("validate card: %w", &board.ValidationError{
				Err:     board.ErrParentNotFound,
				Field:   "parent",
				Value:   parent,
				Message: fmt.Sprintf("parent card %q does not exist", parent),
			})
		}

		if parentCard.Type == board.SubtaskType {
			return fmt.Errorf("validate card: %w", &board.ValidationError{
				Err:     board.ErrInvalidType,
				Field:   "parent",
				Value:   parent,
				Message: fmt.Sprintf("parent card %q is a subtask and cannot have children", parent),
			})
		}
	}

	for _, depID := range dependsOn {
		if _, err := s.store.GetCard(ctx, project, depID); err != nil {
			return fmt.Errorf("validate card: %w", &board.ValidationError{
				Err:     board.ErrDependenciesNotMet,
				Field:   "depends_on",
				Value:   depID,
				Message: fmt.Sprintf("dependency card %q does not exist", depID),
			})
		}
	}

	return nil
}

// detectDependencyCycle walks the dependency graph starting from cardID's
// dependsOn list. Returns the ID that completes a cycle, or "" if none.
func (s *CardService) detectDependencyCycle(ctx context.Context, project, cardID string, dependsOn []string) string {
	visited := map[string]bool{cardID: true}
	queue := make([]string, len(dependsOn))
	copy(queue, dependsOn)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur] {
			return cur
		}

		visited[cur] = true

		dep, err := s.store.GetCard(ctx, project, cur)
		if err != nil {
			continue
		}

		queue = append(queue, dep.DependsOn...)
	}

	return ""
}

// enrichDependenciesMet computes and sets the DependenciesMet field on a card.
func (s *CardService) enrichDependenciesMet(ctx context.Context, card *board.Card) {
	if len(card.DependsOn) == 0 {
		return
	}

	met, _ := s.checkDependencies(ctx, card.Project, card.DependsOn)
	card.DependenciesMet = &met
}
