package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// CreateCardInput contains the fields for creating a new card.
// Server-managed fields (id, created, updated, activity_log) are not included.
type CreateCardInput struct {
	Title               string
	Type                string
	Priority            string
	Labels              []string
	Parent              string
	Body                string
	Source              *board.Source // Optional, immutable after creation
	Autonomous          bool
	UseOpusOrchestrator bool
	FeatureBranch       bool
	CreatePR            bool
	Vetted              bool
}

// UpdateCardInput contains all mutable fields for a full card update.
// Immutable fields (id, project, created, source) are not included.
// Value types match PUT's full-replacement semantics (omitted = zero value).
type UpdateCardInput struct {
	Title               string
	Type                string
	State               string
	Priority            string
	Labels              []string
	Parent              string
	Subtasks            []string
	DependsOn           []string
	Context             []string
	Custom              map[string]any
	Body                string
	ImmediateCommit     bool // If true, commit immediately even when gitDeferredCommit is on.
	Autonomous          bool
	UseOpusOrchestrator bool
	FeatureBranch       bool
	CreatePR            bool
	Vetted              bool
}

// PatchCardInput contains optional fields for partial card updates.
// Nil values mean "do not change".
type PatchCardInput struct {
	Title               *string
	State               *string
	Priority            *string
	Labels              []string // nil = don't change, empty slice = clear
	Body                *string
	ImmediateCommit     bool // If true, commit immediately even when gitDeferredCommit is on.
	Autonomous          *bool
	UseOpusOrchestrator *bool
	FeatureBranch       *bool
	CreatePR            *bool
	Vetted              *bool
	BaseBranch          *string
	// AgentID, when non-empty, is checked against the card's AssignedAgent.
	// If the card is claimed by a different agent, ErrAgentMismatch is returned
	// before any mutations are applied. Empty AgentID skips the check (backward
	// compatible for callers like the runner that do not supply an agent ID).
	AgentID string
}

// CardContext contains a card with its project configuration and template.
type CardContext struct {
	Card     *board.Card
	Project  *board.ProjectConfig
	Template string // Template body for the card's type
}

// ErrFieldTooLong is returned when a user-supplied field exceeds its length limit.
var ErrFieldTooLong = fmt.Errorf("field exceeds maximum length")

// ErrSourceImmutable is returned when an update attempts to change a card's source after creation.
var ErrSourceImmutable = fmt.Errorf("source is immutable after creation")

// branchNameSlugPattern matches anything that's not lowercase alphanumeric.
var branchNameSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// ListCards returns all cards in a project matching the filter.
func (s *CardService) ListCards(ctx context.Context, project string, filter storage.CardFilter) ([]*board.Card, error) {
	filter.Parent = strings.ToUpper(filter.Parent)

	cards, err := s.store.ListCards(ctx, project, filter)
	if err != nil {
		return nil, err
	}

	for _, card := range cards {
		s.enrichDependenciesMet(ctx, card)
	}

	return cards, nil
}

// GetCard returns a specific card.
func (s *CardService) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	id = strings.ToUpper(id)

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, err
	}

	s.enrichDependenciesMet(ctx, card)

	return card, nil
}

// CreateCard creates a new card in the project.
// Flow: generate ID → validate → store → git commit → publish event.
func (s *CardService) CreateCard(ctx context.Context, project string, input CreateCardInput) (*board.Card, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Lock to ensure atomic ID generation
	s.mu.Lock()

	// Load project config
	cfg, err := s.getConfigLocked(ctx, project)
	if err != nil {
		s.mu.Unlock()

		return nil, fmt.Errorf("get project config: %w", err)
	}

	// Generate card ID (increments NextID)
	cardID := board.GenerateCardID(cfg)

	// Persist updated NextID
	if err := s.store.SaveProject(ctx, cfg); err != nil {
		s.mu.Unlock()

		return nil, fmt.Errorf("save project config: %w", err)
	}

	s.mu.Unlock()

	// Cards with a parent are always subtasks regardless of what the caller passes.
	parentID := strings.ToUpper(strings.TrimSpace(input.Parent))

	cardType := input.Type
	if parentID != "" {
		cardType = board.SubtaskType
	}

	// Build card
	now := time.Now()
	card := &board.Card{
		ID:                  cardID,
		Title:               input.Title,
		Project:             project,
		Type:                cardType,
		State:               cfg.States[0], // Default to first state
		Priority:            input.Priority,
		Labels:              input.Labels,
		Parent:              parentID,
		Source:              input.Source,
		Autonomous:          input.Autonomous,
		UseOpusOrchestrator: input.UseOpusOrchestrator,
		FeatureBranch:       input.FeatureBranch,
		CreatePR:            input.CreatePR,
		Vetted:              input.Vetted,
		Created:             now,
		Updated:             now,
		Body:                input.Body,
	}

	// Cards without an external source are implicitly vetted.
	// GitHub/Jira importers set Source but leave Vetted false (Go zero value)
	// until a human approves, so only auto-default when no source is set.
	if input.Source == nil {
		card.Vetted = true
	}

	// Auto-generate branch name when feature_branch is enabled.
	if card.FeatureBranch {
		card.BranchName = generateBranchName(card.ID, card.Title)
	}

	// Validate field length limits.
	if err := validateFieldLimits(card.Title, card.Body, card.Labels); err != nil {
		return nil, err
	}

	// Validate card fields
	if err := s.validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Validate parent references an existing card
	if err := s.validateCardReferences(ctx, project, card.Parent, nil); err != nil {
		return nil, err
	}

	// Dedup guard: if this is a subtask, check for an existing subtask with the
	// same title (case-insensitive, trimmed) that is not in a terminal state.
	// writeMu is held so there is no TOCTOU race.
	if parentID != "" {
		existing, listErr := s.store.ListCards(ctx, project, storage.CardFilter{Parent: parentID})
		if listErr != nil {
			return nil, fmt.Errorf("list subtasks for dedup check: %w", listErr)
		}

		titleNorm := strings.ToLower(strings.TrimSpace(input.Title))
		for _, sub := range existing {
			if strings.ToLower(strings.TrimSpace(sub.Title)) == titleNorm &&
				sub.State != board.StateDone && sub.State != board.StateNotPlanned {
				ctxlog.Logger(ctx).Info("duplicate subtask detected, returning existing card",
					"existing_id", sub.ID,
					"parent_id", parentID,
					"title", sub.Title,
					"state", sub.State,
				)
				s.enrichDependenciesMet(ctx, sub)

				return sub, nil
			}
		}
	}

	// Persist card
	if err := s.store.CreateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}

	// Card creation always commits immediately — even when gitDeferredCommit is
	// true — because a new card is a discrete, durable event. Both the card file
	// and .board.yaml (next_id increment) must be persisted together so the card
	// survives a git pull on another machine.
	if s.gitAutoCommit {
		cardPath := s.cardPath(project, cardID)
		configPath := filepath.Join(project, ".board.yaml")

		msg := commitMessage("", cardID, "created")
		if gitErr := s.git.CommitFiles(ctx, []string{cardPath, configPath}, msg); gitErr != nil {
			// Rollback: remove the orphaned card file and restore NextID so
			// the sequence has no gap on the next creation attempt.
			var rollbackErrs []error

			if delErr := s.store.DeleteCard(ctx, project, card.ID); delErr != nil {
				ctxlog.Logger(ctx).Error("failed to rollback card after git error", "card_id", card.ID, "error", delErr)
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback delete card: %w", delErr))
			}

			cfg.NextID--
			if saveErr := s.store.SaveProject(ctx, cfg); saveErr != nil {
				ctxlog.Logger(ctx).Error("failed to rollback NextID after git error",
					"card_id", card.ID, "next_id", cfg.NextID, "error", saveErr)
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback save project: %w", saveErr))
			}

			return nil, errors.Join(append([]error{fmt.Errorf("git commit: %w", gitErr)}, rollbackErrs...)...)
		}

		s.notifyCommit()
	}

	// Publish event — include source metadata so SSE listeners can
	// display contextual notifications (e.g. "New issue from GitHub").
	var eventData map[string]any
	if input.Source != nil {
		eventData = map[string]any{
			"source_system": input.Source.System,
			"title":         input.Title,
		}
	}

	s.bus.Publish(events.Event{
		Type:      events.CardCreated,
		Project:   project,
		CardID:    cardID,
		Timestamp: now,
		Data:      eventData,
	})

	s.enrichDependenciesMet(ctx, card)

	return card, nil
}

// UpdateCard performs a full update of a card's mutable fields.
// Immutable fields (id, project, created, source) are preserved.
func (s *CardService) UpdateCard(ctx context.Context, project, id string, input UpdateCardInput) (*board.Card, error) {
	id = strings.ToUpper(id)
	input.Parent = strings.ToUpper(strings.TrimSpace(input.Parent))
	input.Subtasks = normalizeIDs(input.Subtasks)
	input.DependsOn = normalizeIDs(input.DependsOn)

	// Caller-level validation (length limits). Kept here so a rejected call
	// never touches writeMu.
	if err := validateFieldLimits(input.Title, input.Body, input.Labels); err != nil {
		return nil, err
	}

	return s.applyCardMutation(ctx, project, id, s.buildUpdateApply(ctx, input), mutationOpts{
		immediateCommit: input.ImmediateCommit,
		commitAction:    "updated",
	})
}

// buildUpdateApply returns the mutation closure for UpdateCard: it enforces
// subtask/parent type invariants, validates the state transition, and assigns
// all mutable fields from input onto the loaded card.
func (s *CardService) buildUpdateApply(ctx context.Context, input UpdateCardInput) func(*board.Card, *board.ProjectConfig) error {
	return func(card *board.Card, cfg *board.ProjectConfig) error {
		oldState := card.State
		stateChanged := input.State != oldState

		if stateChanged {
			if err := s.validator.ValidateTransition(cfg, oldState, input.State); err != nil {
				return fmt.Errorf("validate transition: %w", err)
			}

			if input.State == board.StateInProgress {
				met, blockers := s.checkDependencies(ctx, card.Project, input.DependsOn)
				if !met {
					return dependencyError(input.State, blockers)
				}
			}
		}

		// Enforce subtask type invariants based on parent field transitions.
		switch {
		case input.Parent != "" && card.Parent == "":
			// Card is gaining a parent: auto-force type to "subtask".
			input.Type = board.SubtaskType
		case input.Parent == "" && card.Parent != "":
			// Card is losing its parent: reset "subtask" to first project type.
			if input.Type == board.SubtaskType {
				input.Type = cfg.Types[0]
			}
		case input.Parent == "" && card.Parent == "":
			// No parent before or after: reject "subtask" — it requires a parent.
			if input.Type == board.SubtaskType {
				return fmt.Errorf("validate card: %w", &board.ValidationError{
					Err:     board.ErrInvalidType,
					Field:   "type",
					Value:   input.Type,
					Message: "only cards with a parent can have type \"subtask\"",
				})
			}
		case input.Parent != "" && card.Parent != "":
			// Already a subtask and staying a subtask: can't change type.
			if input.Type != board.SubtaskType {
				return fmt.Errorf("validate card: %w", &board.ValidationError{
					Err:     board.ErrInvalidType,
					Field:   "type",
					Value:   input.Type,
					Message: "cannot change type of a subtask; cards with a parent must have type \"subtask\"",
				})
			}
		}

		card.Title = input.Title
		card.Type = input.Type
		card.State = input.State
		card.Priority = input.Priority
		card.Labels = input.Labels
		card.Parent = input.Parent
		card.Subtasks = input.Subtasks
		card.DependsOn = input.DependsOn
		card.Context = input.Context
		card.Custom = input.Custom
		card.Body = input.Body
		card.Autonomous = input.Autonomous
		card.UseOpusOrchestrator = input.UseOpusOrchestrator
		card.FeatureBranch = input.FeatureBranch
		card.Vetted = input.Vetted

		// BranchName is immutable after first generation — only set when empty.
		if card.FeatureBranch && card.BranchName == "" {
			card.BranchName = generateBranchName(card.ID, card.Title)
		}
		// Auto-clear create_pr when feature_branch is disabled.
		if !card.FeatureBranch {
			card.CreatePR = false
		} else {
			card.CreatePR = input.CreatePR
		}

		return nil
	}
}

// PatchCard applies partial updates to a card.
// Only non-nil fields in the input are updated.
func (s *CardService) PatchCard(ctx context.Context, project, id string, input PatchCardInput) (*board.Card, error) {
	id = strings.ToUpper(id)

	// Field length limits for provided fields. Checked before acquiring writeMu
	// so a rejected call has no side effects.
	if err := validatePatchFieldLimits(input); err != nil {
		return nil, err
	}

	return s.applyCardMutation(ctx, project, id, s.buildPatchApply(ctx, input), mutationOpts{
		immediateCommit: input.ImmediateCommit,
		commitAction:    "updated",
	})
}

// validatePatchFieldLimits checks the length limits for PatchCard-supplied
// optional fields.
func validatePatchFieldLimits(input PatchCardInput) error {
	if input.Title != nil && len(*input.Title) > maxTitleLen {
		return fmt.Errorf("title length %d exceeds limit of %d: %w", len(*input.Title), maxTitleLen, ErrFieldTooLong)
	}

	if input.Body != nil && len(*input.Body) > maxBodyLen {
		return fmt.Errorf("body length %d exceeds limit of %d: %w", len(*input.Body), maxBodyLen, ErrFieldTooLong)
	}

	if input.Labels != nil {
		if len(input.Labels) > maxLabels {
			return fmt.Errorf("label count %d exceeds limit of %d: %w", len(input.Labels), maxLabels, ErrFieldTooLong)
		}

		for _, l := range input.Labels {
			if len(l) > maxLabelLen {
				return fmt.Errorf("label %q length %d exceeds limit of %d: %w", l, len(l), maxLabelLen, ErrFieldTooLong)
			}
		}
	}

	return nil
}

// buildPatchApply returns the mutation closure for PatchCard: it verifies
// agent ownership, validates the optional state transition, and applies only
// the fields present in input.
func (s *CardService) buildPatchApply(ctx context.Context, input PatchCardInput) func(*board.Card, *board.ProjectConfig) error {
	return func(card *board.Card, cfg *board.ProjectConfig) error {
		// Verify agent ownership before applying any mutations so a rejected
		// call produces no side effects. Empty AgentID skips the check for
		// backward-compatible callers that do not supply an agent ID.
		if input.AgentID != "" && card.AssignedAgent != "" && card.AssignedAgent != input.AgentID {
			return fmt.Errorf("agent authorization: %w", lock.ErrAgentMismatch)
		}

		oldState := card.State

		if input.Title != nil {
			card.Title = *input.Title
		}

		if input.State != nil {
			newState := *input.State
			if newState != oldState {
				if err := s.validator.ValidateTransition(cfg, oldState, newState); err != nil {
					return fmt.Errorf("validate transition: %w", err)
				}

				if newState == board.StateInProgress {
					met, blockers := s.checkDependencies(ctx, card.Project, card.DependsOn)
					if !met {
						return dependencyError(newState, blockers)
					}
				}

				card.State = newState
			}
		}

		if input.Priority != nil {
			card.Priority = *input.Priority
		}

		if input.Labels != nil {
			card.Labels = input.Labels
		}

		if input.Body != nil {
			card.Body = *input.Body
		}

		if input.Autonomous != nil {
			card.Autonomous = *input.Autonomous
		}

		if input.UseOpusOrchestrator != nil {
			card.UseOpusOrchestrator = *input.UseOpusOrchestrator
		}

		if input.FeatureBranch != nil {
			card.FeatureBranch = *input.FeatureBranch
			// BranchName is immutable after first generation — only set when empty.
			if card.FeatureBranch && card.BranchName == "" {
				card.BranchName = generateBranchName(card.ID, card.Title)
			}
			// Auto-clear create_pr and base_branch when feature_branch is disabled.
			if !card.FeatureBranch {
				card.CreatePR = false
				card.BaseBranch = ""
			}
		}

		if input.CreatePR != nil && card.FeatureBranch {
			card.CreatePR = *input.CreatePR
		}

		if input.Vetted != nil {
			card.Vetted = *input.Vetted
		}

		if input.BaseBranch != nil {
			card.BaseBranch = *input.BaseBranch
		}

		return nil
	}
}

// DeleteCard removes a card from the project.
func (s *CardService) DeleteCard(ctx context.Context, project, id string) error {
	id = strings.ToUpper(id)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Verify card exists
	_, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	// Reject deletion if card has children (subtasks)
	children, err := s.store.ListCards(ctx, project, storage.CardFilter{Parent: id})
	if err != nil {
		return fmt.Errorf("check children: %w", err)
	}

	if len(children) > 0 {
		childIDs := make([]string, len(children))
		for i, c := range children {
			childIDs[i] = c.ID
		}

		return fmt.Errorf("delete card: %w", &board.ValidationError{
			Err:     board.ErrInvalidType,
			Field:   "id",
			Value:   id,
			Message: fmt.Sprintf("cannot delete card with %d subtask(s): %s", len(children), strings.Join(childIDs, ", ")),
		})
	}

	// Delete from store
	if err := s.store.DeleteCard(ctx, project, id); err != nil {
		return fmt.Errorf("delete card: %w", err)
	}

	// Clean up any deferred paths for this card
	delete(s.deferredPaths, id)

	// Git commit deletion
	if s.gitAutoCommit {
		path := s.cardPath(project, id)

		msg := commitMessage("", id, "deleted")
		if err := s.git.CommitFile(ctx, path, msg); err != nil {
			return fmt.Errorf("git commit delete: %w", err)
		}

		s.notifyCommit()
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardDeleted,
		Project:   project,
		CardID:    id,
		Timestamp: time.Now(),
	})

	return nil
}

// AddLogEntry appends an activity log entry to a card.
// The activity log is capped at 50 entries (oldest dropped).
func (s *CardService) AddLogEntry(ctx context.Context, project, id string, entry board.ActivityEntry) error {
	id = strings.ToUpper(id)

	if len(entry.Message) > maxLogMessage {
		return fmt.Errorf("message length %d exceeds limit of %d: %w", len(entry.Message), maxLogMessage, ErrFieldTooLong)
	}

	if len(entry.Action) > maxLogAction {
		return fmt.Errorf("action length %d exceeds limit of %d: %w", len(entry.Action), maxLogAction, ErrFieldTooLong)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Load card
	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != "" && card.AssignedAgent != entry.Agent {
		return fmt.Errorf("agent authorization: %w", lock.ErrAgentMismatch)
	}

	// Set timestamp if not provided
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Append entry
	card.ActivityLog = append(card.ActivityLog, entry)

	// Cap at max entries (keep most recent)
	if len(card.ActivityLog) > maxActivityLogEntries {
		card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
	}

	card.Updated = time.Now()

	// Persist
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if err := s.commitCardChange(ctx, project, id, entry.Agent, "log: "+entry.Action); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardLogAdded,
		Project:   project,
		CardID:    id,
		Agent:     entry.Agent,
		Timestamp: entry.Timestamp,
		Data: map[string]any{
			"action":  entry.Action,
			"message": entry.Message,
		},
	})

	return nil
}

// GetCardContext returns a card with its project configuration and template.
func (s *CardService) GetCardContext(ctx context.Context, project, id string) (*CardContext, error) {
	id = strings.ToUpper(id)
	// Load card
	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Load project config
	cfg, err := s.getConfig(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project config: %w", err)
	}

	// Load templates
	templates, err := s.getTemplates(project)
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	return &CardContext{
		Card:     card,
		Project:  cfg,
		Template: templates[card.Type],
	}, nil
}

// mutationOpts controls the behaviour of applyCardMutation.
type mutationOpts struct {
	// skipValidators, when true, skips validateCardReferences and
	// detectDependencyCycle. validator.ValidateCard always runs. Intended for
	// internal callers (e.g. transitions) that know the references are stable.
	skipValidators bool
	// immediateCommit forces an immediate git commit even when gitDeferredCommit
	// is enabled. Used for human-initiated edits.
	immediateCommit bool
	// commitAgentID is the agent attributed in the commit message; empty means
	// a system ([contextmatrix]) commit.
	commitAgentID string
	// commitAction is the action verb in the commit message (e.g. "updated").
	commitAction string
}

// applyCardMutation is the shared write path for UpdateCard and PatchCard.
// It owns the standard flow:
//
//  1. Acquire writeMu.
//  2. Load card and project config.
//  3. Call apply to mutate the card in place and perform mutation-specific
//     validation (state transition, dependency check, etc.). Apply receives
//     the card and cfg; it returns an error to abort with no side effects.
//  4. Stamp Updated.
//  5. Enforce terminal-state invariants (pre-persist).
//  6. Validate the card; if opts.skipValidators is false, also validate
//     cross-card references and detect dependency cycles.
//  7. Persist via store.UpdateCard.
//  8. Commit (immediate or deferred, per opts.immediateCommit).
//  9. Post-commit state-change side effects (deferred flush on
//     not_planned/review).
//  10. Publish the CardUpdated or CardStateChanged event.
//  11. Auto-transition parent if state changed.
//  12. Enrich DependenciesMet and return the card.
//
// Keeping these steps in one place prevents UpdateCard and PatchCard from
// drifting on validator order, commit path, or side-effect sequencing.
func (s *CardService) applyCardMutation(
	ctx context.Context,
	project, id string,
	apply func(*board.Card, *board.ProjectConfig) error,
	opts mutationOpts,
) (*board.Card, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	cfg, err := s.getConfig(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project config: %w", err)
	}

	oldState := card.State

	if err := apply(card, cfg); err != nil {
		return nil, err
	}

	stateChanged := card.State != oldState
	card.Updated = time.Now()

	// Release agent claim on not_planned and clear runner_status on terminal
	// states. Must happen before validate+persist so the written card reflects
	// the invariants.
	enforceTerminalStateInvariants(card, stateChanged)

	if err := s.validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	if !opts.skipValidators {
		if err := s.validateCardReferences(ctx, project, card.Parent, card.DependsOn); err != nil {
			return nil, err
		}

		if len(card.DependsOn) > 0 {
			if cycleID := s.detectDependencyCycle(ctx, project, id, card.DependsOn); cycleID != "" {
				return nil, fmt.Errorf("validate card: %w", &board.ValidationError{
					Err:     board.ErrDependenciesNotMet,
					Field:   "depends_on",
					Value:   cycleID,
					Message: fmt.Sprintf("circular dependency detected: %s and %s depend on each other", id, cycleID),
				})
			}
		}
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (immediate or deferred).
	if opts.immediateCommit && s.gitAutoCommit {
		cardPath := s.cardPath(project, id)

		msg := commitMessage(opts.commitAgentID, id, opts.commitAction)
		if err := s.git.CommitFile(ctx, cardPath, msg); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}

		s.notifyCommit()
	} else {
		if err := s.commitCardChange(ctx, project, id, opts.commitAgentID, opts.commitAction); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
	}

	// Post-commit state-change side effects (flush deferred on not_planned/review).
	s.applyStateChangeSideEffects(ctx, card, stateChanged)

	eventType := events.CardUpdated
	if stateChanged {
		eventType = events.CardStateChanged
	}

	s.bus.Publish(events.Event{
		Type:      eventType,
		Project:   project,
		CardID:    id,
		Timestamp: card.Updated,
		Data: map[string]any{
			"old_state": oldState,
			"new_state": card.State,
		},
	})

	if stateChanged {
		s.maybeTransitionParent(ctx, card)
	}

	s.enrichDependenciesMet(ctx, card)

	return card, nil
}

// normalizeIDs uppercases all card IDs in a slice.
func normalizeIDs(ids []string) []string {
	if ids == nil {
		return nil
	}

	seen := make(map[string]bool, len(ids))

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		upper := strings.ToUpper(id)
		if !seen[upper] {
			seen[upper] = true
			out = append(out, upper)
		}
	}

	return out
}

// validateFieldLimits checks that user-supplied string fields do not exceed length limits.
func validateFieldLimits(title, body string, labels []string) error {
	if len(title) > maxTitleLen {
		return fmt.Errorf("title length %d exceeds limit of %d: %w", len(title), maxTitleLen, ErrFieldTooLong)
	}

	if len(body) > maxBodyLen {
		return fmt.Errorf("body length %d exceeds limit of %d: %w", len(body), maxBodyLen, ErrFieldTooLong)
	}

	if len(labels) > maxLabels {
		return fmt.Errorf("label count %d exceeds limit of %d: %w", len(labels), maxLabels, ErrFieldTooLong)
	}

	for _, l := range labels {
		if len(l) > maxLabelLen {
			return fmt.Errorf("label %q length %d exceeds limit of %d: %w", l, len(l), maxLabelLen, ErrFieldTooLong)
		}
	}

	return nil
}

// generateBranchName creates a git branch name from a card ID and title.
// Format: alpha-042/fix-login-validation (lowercase, alphanumeric + hyphens).
// Non-ASCII characters are stripped (e.g. "über" becomes "ber").
func generateBranchName(cardID, title string) string {
	slug := strings.ToLower(title)
	slug = branchNameSlugPattern.ReplaceAllString(slug, "-")

	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.TrimRight(slug, "-")
	}

	prefix := strings.ToLower(cardID)
	if slug == "" {
		return prefix
	}

	return prefix + "/" + slug
}
