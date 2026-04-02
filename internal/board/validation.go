package board

import (
	"errors"
	"fmt"
	"slices"
)

// SubtaskType is the built-in card type assigned to all cards that have a parent.
// It is always valid regardless of whether it appears in ProjectConfig.Types.
const SubtaskType = "subtask"

// Sentinel errors for validation failures.
var (
	// ErrInvalidType indicates the card type is not in ProjectConfig.Types.
	ErrInvalidType = errors.New("invalid card type")

	// ErrInvalidState indicates the card state is not in ProjectConfig.States.
	ErrInvalidState = errors.New("invalid card state")

	// ErrInvalidPriority indicates the card priority is not in ProjectConfig.Priorities.
	ErrInvalidPriority = errors.New("invalid card priority")

	// ErrInvalidTransition indicates the state transition is not allowed.
	ErrInvalidTransition = errors.New("invalid state transition")

	// ErrDependenciesNotMet indicates that not all depends_on cards are done.
	ErrDependenciesNotMet = errors.New("dependencies not met")

	// ErrNoPath indicates no sequence of valid transitions connects two states.
	ErrNoPath = errors.New("no transition path exists")

	// ErrInvalidAutonomousConfig indicates an invalid combination of autonomous fields.
	ErrInvalidAutonomousConfig = errors.New("invalid autonomous configuration")

	// ErrInvalidRunnerStatus indicates an invalid runner_status value.
	ErrInvalidRunnerStatus = errors.New("invalid runner status")
)

// ValidationError provides detailed validation failure information.
// It wraps a sentinel error (for errors.Is() checks) with contextual details.
type ValidationError struct {
	Err     error    // Sentinel error (e.g., ErrInvalidType)
	Field   string   // Field that failed validation
	Value   string   // The invalid value
	Allowed []string // Allowed values (for type/state/priority)
	Message string   // Human-readable message
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Validator provides card and transition validation against project configuration.
// All methods are pure functions - they do not modify any state.
type Validator struct{}

// NewValidator creates a new Validator instance.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateType checks if cardType is valid for the project.
// SubtaskType ("subtask") is always valid as a built-in type, even if not listed
// in ProjectConfig.Types — callers do not need to add it to their board config.
// Returns nil if valid, *ValidationError if invalid.
func (v *Validator) ValidateType(cfg *ProjectConfig, cardType string) error {
	if cardType == SubtaskType {
		return nil
	}
	if !slices.Contains(cfg.Types, cardType) {
		return &ValidationError{
			Err:     ErrInvalidType,
			Field:   "type",
			Value:   cardType,
			Allowed: cfg.Types,
			Message: fmt.Sprintf("invalid type %q; valid types: %v", cardType, cfg.Types),
		}
	}
	return nil
}

// ValidateState checks if cardState is valid for the project.
// Returns nil if valid, *ValidationError if invalid.
func (v *Validator) ValidateState(cfg *ProjectConfig, cardState string) error {
	if !slices.Contains(cfg.States, cardState) {
		return &ValidationError{
			Err:     ErrInvalidState,
			Field:   "state",
			Value:   cardState,
			Allowed: cfg.States,
			Message: fmt.Sprintf("invalid state %q; valid states: %v", cardState, cfg.States),
		}
	}
	return nil
}

// ValidatePriority checks if cardPriority is valid for the project.
// Returns nil if valid, *ValidationError if invalid.
func (v *Validator) ValidatePriority(cfg *ProjectConfig, cardPriority string) error {
	if !slices.Contains(cfg.Priorities, cardPriority) {
		return &ValidationError{
			Err:     ErrInvalidPriority,
			Field:   "priority",
			Value:   cardPriority,
			Allowed: cfg.Priorities,
			Message: fmt.Sprintf("invalid priority %q; valid priorities: %v", cardPriority, cfg.Priorities),
		}
	}
	return nil
}

// ValidateTransition checks if transitioning from fromState to toState is allowed.
// Special rule: any state can transition TO "stalled" (system-managed),
// but FROM "stalled" only follows Transitions["stalled"].
// The "not_planned" state follows normal transition rules — only states that
// explicitly list "not_planned" in their transitions can reach it.
// Returns nil if valid, *ValidationError if invalid.
func (v *Validator) ValidateTransition(cfg *ProjectConfig, fromState, toState string) error {
	// Validate both states exist
	if !slices.Contains(cfg.States, fromState) {
		return &ValidationError{
			Err:     ErrInvalidState,
			Field:   "from_state",
			Value:   fromState,
			Allowed: cfg.States,
			Message: fmt.Sprintf("invalid source state %q; valid states: %v", fromState, cfg.States),
		}
	}
	if !slices.Contains(cfg.States, toState) {
		return &ValidationError{
			Err:     ErrInvalidState,
			Field:   "to_state",
			Value:   toState,
			Allowed: cfg.States,
			Message: fmt.Sprintf("invalid target state %q; valid states: %v", toState, cfg.States),
		}
	}

	// Same state is always valid (no-op transition)
	if fromState == toState {
		return nil
	}

	// Special rule: any state can transition TO stalled
	if toState == StateStalled {
		return nil
	}

	// Check allowed transitions from current state
	allowed := cfg.Transitions[fromState]
	if !slices.Contains(allowed, toState) {
		return &ValidationError{
			Err:     ErrInvalidTransition,
			Field:   "state",
			Value:   toState,
			Allowed: allowed,
			Message: fmt.Sprintf("cannot transition from %q to %q; valid targets: %v", fromState, toState, allowed),
		}
	}

	return nil
}

// ValidateCard validates type, state, and priority fields against the project config.
// Returns the first validation error encountered, or nil if all fields are valid.
// Does NOT validate transitions - use ValidateTransition separately.
func (v *Validator) ValidateCard(cfg *ProjectConfig, card *Card) error {
	if err := v.ValidateType(cfg, card.Type); err != nil {
		return err
	}
	if err := v.ValidateState(cfg, card.State); err != nil {
		return err
	}
	if err := v.ValidatePriority(cfg, card.Priority); err != nil {
		return err
	}
	if err := v.ValidateAutonomousFields(card); err != nil {
		return err
	}
	return nil
}

// validRunnerStatuses is the set of valid runner_status values.
var validRunnerStatuses = []string{"", "queued", "running", "failed", "killed"}

// validRunnerCallbackStatuses is the subset of statuses a runner callback can set.
// "queued" and "killed" are server-only; the runner can only report "running" or "failed".
var validRunnerCallbackStatuses = []string{"running", "failed"}

// ValidateRunnerStatus checks if the given status is a valid runner_status value.
func (v *Validator) ValidateRunnerStatus(status string) error {
	if !slices.Contains(validRunnerStatuses, status) {
		return &ValidationError{
			Err:     ErrInvalidRunnerStatus,
			Field:   "runner_status",
			Value:   status,
			Allowed: validRunnerStatuses[1:], // exclude empty string from display
			Message: fmt.Sprintf("invalid runner_status %q; valid values: %v", status, validRunnerStatuses[1:]),
		}
	}
	return nil
}

// ValidateRunnerCallbackStatus checks if the status is valid for a runner callback.
// Only "running" and "failed" are accepted from the runner — other statuses are server-managed.
func (v *Validator) ValidateRunnerCallbackStatus(status string) error {
	if !slices.Contains(validRunnerCallbackStatuses, status) {
		return &ValidationError{
			Err:     ErrInvalidRunnerStatus,
			Field:   "runner_status",
			Value:   status,
			Allowed: validRunnerCallbackStatuses,
			Message: fmt.Sprintf("invalid runner callback status %q; valid values: %v", status, validRunnerCallbackStatuses),
		}
	}
	return nil
}

// ValidateAutonomousFields checks that autonomous-related field combinations are valid.
// create_pr requires feature_branch to be enabled.
func (v *Validator) ValidateAutonomousFields(card *Card) error {
	if card.CreatePR && !card.FeatureBranch {
		return &ValidationError{
			Err:     ErrInvalidAutonomousConfig,
			Field:   "create_pr",
			Value:   "true",
			Message: "create_pr requires feature_branch to be enabled",
		}
	}
	return nil
}

// CanTransition returns true if the transition is allowed, false otherwise.
// Convenience method that wraps ValidateTransition for boolean checks.
func (v *Validator) CanTransition(cfg *ProjectConfig, fromState, toState string) bool {
	return v.ValidateTransition(cfg, fromState, toState) == nil
}

// AllowedTransitions returns the list of valid target states from the given state.
// For the "stalled" state, returns Transitions["stalled"].
// For any other state, includes the explicit Transitions[state] plus "stalled".
// The "not_planned" state is not auto-injected — it only appears if explicitly
// listed in the source state's transitions in .board.yaml.
// Returns nil if fromState is not a valid state.
func (v *Validator) AllowedTransitions(cfg *ProjectConfig, fromState string) []string {
	if !slices.Contains(cfg.States, fromState) {
		return nil
	}

	explicit := cfg.Transitions[fromState]

	// From stalled, only return explicit transitions
	if fromState == StateStalled {
		return explicit
	}

	// From any other state, add stalled to the list (if not already present)
	if slices.Contains(explicit, StateStalled) {
		return explicit
	}

	result := make([]string, len(explicit)+1)
	copy(result, explicit)
	result[len(explicit)] = StateStalled
	return result
}

// FindShortestPath returns the shortest sequence of states to traverse from
// fromState to toState, using BFS on the transition graph. The returned path
// excludes fromState but includes toState. Returns an empty slice if fromState
// equals toState. Returns ErrNoPath if no valid path exists.
func (v *Validator) FindShortestPath(cfg *ProjectConfig, fromState, toState string) ([]string, error) {
	if err := v.ValidateState(cfg, fromState); err != nil {
		return nil, err
	}
	if err := v.ValidateState(cfg, toState); err != nil {
		return nil, err
	}

	if fromState == toState {
		return nil, nil
	}

	// BFS
	visited := map[string]string{fromState: ""} // state -> parent
	queue := []string{fromState}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, next := range v.AllowedTransitions(cfg, current) {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = current
			if next == toState {
				// Reconstruct path
				var path []string
				for s := toState; s != fromState; s = visited[s] {
					path = append(path, s)
				}
				// Reverse
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path, nil
			}
			queue = append(queue, next)
		}
	}

	return nil, &ValidationError{
		Err:     ErrNoPath,
		Field:   "state",
		Value:   toState,
		Message: fmt.Sprintf("no transition path from %q to %q", fromState, toState),
	}
}
