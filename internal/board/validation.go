package board

import (
	"errors"
	"fmt"
	"slices"
)

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
// Returns nil if valid, *ValidationError if invalid.
func (v *Validator) ValidateType(cfg *ProjectConfig, cardType string) error {
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
	if toState == stalledState {
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
// Returns nil if fromState is not a valid state.
func (v *Validator) AllowedTransitions(cfg *ProjectConfig, fromState string) []string {
	if !slices.Contains(cfg.States, fromState) {
		return nil
	}

	explicit := cfg.Transitions[fromState]

	// From stalled, only return explicit transitions
	if fromState == stalledState {
		return explicit
	}

	// From any other state, add stalled to the list (if not already present)
	if slices.Contains(explicit, stalledState) {
		return explicit
	}

	result := make([]string, len(explicit)+1)
	copy(result, explicit)
	result[len(explicit)] = stalledState
	return result
}
