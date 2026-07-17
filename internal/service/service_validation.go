package service

import (
	"context"
	"fmt"
	"regexp"
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
//
// Uses DFS with an explicit recursion stack so we can distinguish "already
// visited on a different branch" (safe - diamond graph A→B→D, A→C→D) from
// "ancestor on the current path" (a true cycle). A BFS that marks-on-pop
// would flag a re-enqueued node as a cycle even when it was reached via
// two independent ancestors, producing false positives on diamond graphs.
func (s *CardService) detectDependencyCycle(ctx context.Context, project, cardID string, dependsOn []string) string {
	// visited tracks every node we have finished exploring. Re-visiting a
	// finished node is not a cycle - it just means two paths converge.
	visited := make(map[string]bool)
	// onStack tracks nodes on the current DFS path. Re-visiting a node on
	// the stack IS a cycle.
	onStack := map[string]bool{cardID: true}

	var dfs func(id string) string

	dfs = func(id string) string {
		if onStack[id] {
			return id
		}

		if visited[id] {
			return ""
		}

		onStack[id] = true

		defer func() {
			onStack[id] = false
			visited[id] = true
		}()

		// cardID itself is not in the store yet on create paths, so callers
		// pass its dependsOn list explicitly. For every other node, resolve
		// children from the store.
		dep, err := s.store.GetCard(ctx, project, id)
		if err != nil {
			return ""
		}

		for _, child := range dep.DependsOn {
			if c := dfs(child); c != "" {
				return c
			}
		}

		return ""
	}

	for _, child := range dependsOn {
		if c := dfs(child); c != "" {
			return c
		}
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

// taskSkillNamePattern restricts skill names to a safe charset that cannot
// reach outside a task-skills mount via path traversal. Mirrors the backend-
// side ValidateTaskSkills check.
var taskSkillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// validateSkillNames returns an error if any name in the slice fails the
// allowlist pattern. Nil and empty slices are valid (no skills).
func validateSkillNames(skills *[]string) error {
	if skills == nil {
		return nil
	}

	for _, s := range *skills {
		if !taskSkillNamePattern.MatchString(s) {
			return fmt.Errorf("invalid skill name %q: must match %s: %w",
				s, taskSkillNamePattern.String(), ErrFieldTooLong)
		}
	}

	return nil
}

// enforceVettingInvariant guarantees that any card without an external Source
// is treated as vetted. Cards from external importers (GitHub/Jira) keep
// whatever Vetted value the caller set; only when Source is nil do we
// auto-correct. Called from CreateCard and from the PUT/PATCH apply paths so
// the invariant holds regardless of which write path got called and whether
// the caller remembered to pass Vetted through.
func enforceVettingInvariant(card *board.Card) {
	if card.Source == nil {
		card.Vetted = true
	}
}
