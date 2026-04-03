// Package service provides the CardService orchestration layer.
// It coordinates storage, git operations, lock management, event publishing,
// and state machine validation for all card mutations.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// maxActivityLogEntries is the maximum number of entries kept in a card's activity log.
	// Older entries are dropped but preserved in git history.
	maxActivityLogEntries = 50

	// maxReviewAttempts caps the review_attempts counter as defense-in-depth.
	// The autonomous prompt halts at 2, but a misbehaving agent could keep incrementing.
	maxReviewAttempts = 5
)

// CreateCardInput contains the fields for creating a new card.
// Server-managed fields (id, created, updated, activity_log) are not included.
type CreateCardInput struct {
	Title         string
	Type          string
	Priority      string
	Labels        []string
	Parent        string
	Body          string
	Source        *board.Source // Optional, immutable after creation
	Autonomous    bool
	FeatureBranch bool
	CreatePR      bool
}

// UpdateCardInput contains all mutable fields for a full card update.
// Immutable fields (id, project, created, source) are not included.
// Value types match PUT's full-replacement semantics (omitted = zero value).
type UpdateCardInput struct {
	Title           string
	Type            string
	State           string
	Priority        string
	Labels          []string
	Parent          string
	Subtasks        []string
	DependsOn       []string
	Context         []string
	Custom          map[string]any
	Body            string
	ImmediateCommit bool // If true, commit immediately even when gitDeferredCommit is on.
	Autonomous      bool
	FeatureBranch   bool
	CreatePR        bool
}

// PatchCardInput contains optional fields for partial card updates.
// Nil values mean "do not change".
type PatchCardInput struct {
	Title           *string
	State           *string
	Priority        *string
	Labels          []string // nil = don't change, empty slice = clear
	Body            *string
	ImmediateCommit bool // If true, commit immediately even when gitDeferredCommit is on.
	Autonomous      *bool
	FeatureBranch   *bool
	CreatePR        *bool
}

// ModelCost defines per-token cost rates for a model.
type ModelCost struct {
	Prompt     float64
	Completion float64
}

// CreateProjectInput contains the fields for creating a new project.
type CreateProjectInput struct {
	Name        string
	Prefix      string
	Repo        string
	States      []string
	Types       []string
	Priorities  []string
	Transitions map[string][]string
}

// UpdateProjectInput contains the mutable fields for updating a project.
// Name and Prefix are immutable and excluded.
type UpdateProjectInput struct {
	Repo        string
	States      []string
	Types       []string
	Priorities  []string
	Transitions map[string][]string
}

// validProjectName matches safe directory names: alphanumeric, hyphens, underscores.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ReportUsageInput contains the fields for reporting token usage on a card.
type ReportUsageInput struct {
	AgentID          string
	Model            string
	PromptTokens     int64
	CompletionTokens int64
}

// ProjectUsage contains aggregated token usage across all cards in a project.
type ProjectUsage struct {
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CardCount        int     `json:"card_count"`
}

// RecalculateCostsResult summarises the outcome of a cost recalculation pass.
type RecalculateCostsResult struct {
	CardsUpdated           int     `json:"cards_updated"`
	TotalCostRecalculated  float64 `json:"total_cost_recalculated"`
}

// ActiveAgent describes an agent currently working on a card.
type ActiveAgent struct {
	AgentID       string    `json:"agent_id"`
	CardID        string    `json:"card_id"`
	CardTitle     string    `json:"card_title"`
	Since         time.Time `json:"since"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// AgentCost contains per-agent cost aggregation.
type AgentCost struct {
	AgentID          string  `json:"agent_id"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CardCount        int     `json:"card_count"`
}

// CardCost contains per-card cost summary.
type CardCost struct {
	CardID           string  `json:"card_id"`
	CardTitle        string  `json:"card_title"`
	AssignedAgent    string  `json:"assigned_agent,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// DashboardData contains all data needed for the project dashboard view.
type DashboardData struct {
	StateCounts         map[string]int `json:"state_counts"`
	ActiveAgents        []ActiveAgent  `json:"active_agents"`
	TotalCostUSD        float64        `json:"total_cost_usd"`
	CardsCompletedToday int            `json:"cards_completed_today"`
	AgentCosts          []AgentCost    `json:"agent_costs"`
	CardCosts           []CardCost     `json:"card_costs"`
}

// CardContext contains a card with its project configuration and template.
type CardContext struct {
	Card     *board.Card
	Project  *board.ProjectConfig
	Template string // Template body for the card's type
}

// CardService orchestrates all card operations by coordinating
// storage, git, lock management, events, and validation.
type CardService struct {
	store               storage.Store
	git                 *gitops.Manager
	lock                *lock.Manager
	bus                 *events.Bus
	boardsDir           string
	tokenCosts          map[string]ModelCost
	gitAutoCommit       bool
	gitDeferredCommit   bool

	// writeMu serializes all card mutations (create, update, patch, delete,
	// claim, release, heartbeat, log). This prevents races like two agents
	// claiming the same card simultaneously.
	writeMu sync.Mutex

	// deferredPaths tracks card file paths awaiting a deferred commit.
	// Key is card ID; value is the list of relative file paths modified.
	// Protected by writeMu (always held during card mutations).
	deferredPaths map[string][]string

	// onCommit is called after each successful git commit.
	// Used by the sync layer to trigger push-after-commit.
	onCommit func()

	validator *board.Validator

	// Per-project caches
	mu        sync.RWMutex
	configs   map[string]*board.ProjectConfig
	templates map[string]map[string]string // project -> type -> template
}

// NewCardService creates a new CardService with the given dependencies.
func NewCardService(
	store storage.Store,
	git *gitops.Manager,
	lock *lock.Manager,
	bus *events.Bus,
	boardsDir string,
	tokenCosts map[string]ModelCost,
	gitAutoCommit bool,
	gitDeferredCommit bool,
) *CardService {
	return &CardService{
		store:             store,
		git:               git,
		lock:              lock,
		bus:               bus,
		boardsDir:         boardsDir,
		tokenCosts:        tokenCosts,
		gitAutoCommit:     gitAutoCommit,
		gitDeferredCommit: gitDeferredCommit,
		deferredPaths:     make(map[string][]string),
		validator:         board.NewValidator(),
		configs:           make(map[string]*board.ProjectConfig),
		templates:         make(map[string]map[string]string),
	}
}

// SetOnCommit registers a callback invoked after each successful git commit.
func (s *CardService) SetOnCommit(fn func()) {
	s.onCommit = fn
}

// notifyCommit calls the onCommit callback if set.
func (s *CardService) notifyCommit() {
	if s.onCommit != nil {
		s.onCommit()
	}
}

// ClearCaches resets all per-project caches (validators, configs, templates).
// Called after a git pull that may have changed project files.
func (s *CardService) ClearCaches() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = make(map[string]*board.ProjectConfig)
	s.templates = make(map[string]map[string]string)
}

// LockWrites acquires the write mutex, preventing all card mutations.
// Used by the sync layer during pull+index-rebuild.
func (s *CardService) LockWrites() {
	s.writeMu.Lock()
}

// UnlockWrites releases the write mutex.
func (s *CardService) UnlockWrites() {
	s.writeMu.Unlock()
}

// ListProjects returns all discovered projects.
func (s *CardService) ListProjects(ctx context.Context) ([]board.ProjectConfig, error) {
	return s.store.ListProjects(ctx)
}

// GetProject returns the configuration for a specific project.
func (s *CardService) GetProject(ctx context.Context, name string) (*board.ProjectConfig, error) {
	return s.store.GetProject(ctx, name)
}

// CreateProject creates a new project with directory structure and .board.yaml.
func (s *CardService) CreateProject(ctx context.Context, input CreateProjectInput) (*board.ProjectConfig, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Validate name format
	if !validProjectName.MatchString(input.Name) {
		return nil, fmt.Errorf("invalid project name %q: must be alphanumeric with hyphens/underscores: %w", input.Name, board.ErrInvalidProjectConfig)
	}

	// Check not already exists
	_, err := s.store.GetProject(ctx, input.Name)
	if err == nil {
		return nil, fmt.Errorf("project %q: %w", input.Name, storage.ErrProjectExists)
	}

	cfg := &board.ProjectConfig{
		Name:        input.Name,
		Prefix:      input.Prefix,
		NextID:      1,
		Repo:        input.Repo,
		States:      input.States,
		Types:       input.Types,
		Priorities:  input.Priorities,
		Transitions: input.Transitions,
	}

	// SaveProject validates config and creates directory + .board.yaml
	if err := s.store.SaveProject(ctx, cfg); err != nil {
		return nil, fmt.Errorf("save project: %w", err)
	}

	// Create tasks subdirectory
	tasksDir := filepath.Join(s.boardsDir, input.Name, "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tasks directory: %w", err)
	}

	// Git commit
	if s.gitAutoCommit {
		msg := fmt.Sprintf("[contextmatrix] %s: project created", input.Name)
		if err := s.git.CommitAll(msg); err != nil {
			slog.Warn("git commit after project create", "error", err)
		} else {
			s.notifyCommit()
		}
	}

	// Update cache
	s.mu.Lock()
	s.configs[input.Name] = cfg
	s.mu.Unlock()

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.ProjectCreated,
		Project:   input.Name,
		Timestamp: time.Now(),
	})

	return cfg, nil
}

// UpdateProject updates a project's mutable configuration.
// Rejects removal of states, types, or priorities currently in use by cards.
func (s *CardService) UpdateProject(ctx context.Context, name string, input UpdateProjectInput) (*board.ProjectConfig, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Load existing config
	cfg, err := s.store.GetProject(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}

	// Check for in-use values that would be removed
	cards, err := s.store.ListCards(ctx, name, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	if len(cards) > 0 {
		usedStates := make(map[string]bool)
		usedTypes := make(map[string]bool)
		usedPriorities := make(map[string]bool)
		for _, c := range cards {
			usedStates[c.State] = true
			usedTypes[c.Type] = true
			usedPriorities[c.Priority] = true
		}

		newStates := toSet(input.States)
		for state := range usedStates {
			if !newStates[state] {
				return nil, fmt.Errorf("cannot remove state %q: in use by cards: %w", state, board.ErrInvalidProjectConfig)
			}
		}

		newTypes := toSet(input.Types)
		for typ := range usedTypes {
			if !newTypes[typ] {
				return nil, fmt.Errorf("cannot remove type %q: in use by cards: %w", typ, board.ErrInvalidProjectConfig)
			}
		}

		newPriorities := toSet(input.Priorities)
		for pri := range usedPriorities {
			if !newPriorities[pri] {
				return nil, fmt.Errorf("cannot remove priority %q: in use by cards: %w", pri, board.ErrInvalidProjectConfig)
			}
		}
	}

	// Apply changes (name, prefix, next_id are immutable)
	cfg.Repo = input.Repo
	cfg.States = input.States
	cfg.Types = input.Types
	cfg.Priorities = input.Priorities
	cfg.Transitions = input.Transitions

	// SaveProject validates and persists
	if err := s.store.SaveProject(ctx, cfg); err != nil {
		return nil, fmt.Errorf("save project: %w", err)
	}

	// Git commit
	if s.gitAutoCommit {
		path := filepath.Join(name, ".board.yaml")
		msg := fmt.Sprintf("[contextmatrix] %s: project updated", name)
		if err := s.git.CommitFile(path, msg); err != nil {
			slog.Warn("git commit after project update", "error", err)
		} else {
			s.notifyCommit()
		}
	}

	// Invalidate caches so they rebuild with new config
	s.mu.Lock()
	s.configs[name] = cfg
	s.mu.Unlock()

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.ProjectUpdated,
		Project:   name,
		Timestamp: time.Now(),
	})

	return cfg, nil
}

// DeleteProject removes a project. Requires zero cards.
func (s *CardService) DeleteProject(ctx context.Context, name string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Check exists
	if _, err := s.store.GetProject(ctx, name); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	// Check no cards
	count, err := s.store.ProjectCardCount(ctx, name)
	if err != nil {
		return fmt.Errorf("count cards: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("project %q has %d cards: %w", name, count, storage.ErrProjectHasCards)
	}

	// Delete from store (removes directory and index)
	if err := s.store.DeleteProject(ctx, name); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	// Git commit
	if s.gitAutoCommit {
		msg := fmt.Sprintf("[contextmatrix] %s: project deleted", name)
		if err := s.git.CommitAll(msg); err != nil {
			slog.Warn("git commit after project delete", "error", err)
		} else {
			s.notifyCommit()
		}
	}

	// Purge all caches
	s.mu.Lock()
	delete(s.configs, name)
	delete(s.templates, name)
	s.mu.Unlock()

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.ProjectDeleted,
		Project:   name,
		Timestamp: time.Now(),
	})

	return nil
}

// toSet converts a slice to a set for membership checks.
func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

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
		ID:            cardID,
		Title:         input.Title,
		Project:       project,
		Type:          cardType,
		State:         cfg.States[0], // Default to first state
		Priority:      input.Priority,
		Labels:        input.Labels,
		Parent:        parentID,
		Source:        input.Source,
		Autonomous:    input.Autonomous,
		FeatureBranch: input.FeatureBranch,
		CreatePR:      input.CreatePR,
		Created:       now,
		Updated:       now,
		Body:          input.Body,
	}

	// Auto-generate branch name when feature_branch is enabled.
	if card.FeatureBranch {
		card.BranchName = generateBranchName(card.ID, card.Title)
	}

	// Validate card fields
	if err := s.validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Validate parent references an existing card
	if err := s.validateCardReferences(ctx, project, card.Parent, nil); err != nil {
		return nil, err
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
		if err := s.git.CommitFiles([]string{cardPath, configPath}, msg); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
		s.notifyCommit()
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardCreated,
		Project:   project,
		CardID:    cardID,
		Timestamp: now,
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Load existing card
	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Load project config
	cfg, err := s.getConfig(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project config: %w", err)
	}

	// Track if state changed for event type
	oldState := card.State
	stateChanged := input.State != oldState

	// Validate state transition if changed
	if stateChanged {
		validator := s.validator
		if err := validator.ValidateTransition(cfg, oldState, input.State); err != nil {
			return nil, fmt.Errorf("validate transition: %w", err)
		}
		// Block transition to in_progress if dependencies not met
		if input.State == board.StateInProgress {
			met, blockers := s.checkDependencies(ctx, project, input.DependsOn)
			if !met {
				return nil, dependencyError(input.State, blockers)
			}
		}
	}

	// Enforce subtask type invariants based on parent field transitions:
	switch {
	case input.Parent != "" && card.Parent == "":
		// Card is gaining a parent: auto-force type to "subtask", matching CreateCard behavior.
		input.Type = board.SubtaskType
	case input.Parent == "" && card.Parent != "":
		// Card is losing its parent: if the caller still passes "subtask", reset to the first
		// project type so the card becomes a valid standalone card again.
		if input.Type == board.SubtaskType {
			input.Type = cfg.Types[0]
		}
	case input.Parent == "" && card.Parent == "":
		// No parent before or after: reject "subtask" — it requires a parent.
		if input.Type == board.SubtaskType {
			return nil, fmt.Errorf("validate card: %w", &board.ValidationError{
				Err:     board.ErrInvalidType,
				Field:   "type",
				Value:   input.Type,
				Message: "only cards with a parent can have type \"subtask\"",
			})
		}
	case input.Parent != "" && card.Parent != "":
		// Already a subtask and staying a subtask: type changes away from "subtask" are not allowed.
		if input.Type != board.SubtaskType {
			return nil, fmt.Errorf("validate card: %w", &board.ValidationError{
				Err:     board.ErrInvalidType,
				Field:   "type",
				Value:   input.Type,
				Message: "cannot change type of a subtask; cards with a parent must have type \"subtask\"",
			})
		}
	}

	// Update mutable fields
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
	card.FeatureBranch = input.FeatureBranch
	// BranchName is immutable after first generation — only set when empty.
	// Not exposed on any input struct by design; this guard is defense-in-depth.
	if card.FeatureBranch && card.BranchName == "" {
		card.BranchName = generateBranchName(card.ID, card.Title)
	}
	// Auto-clear create_pr when feature_branch is disabled.
	if !card.FeatureBranch {
		card.CreatePR = false
	} else {
		card.CreatePR = input.CreatePR
	}
	card.Updated = time.Now()

	// Release agent claim when card moves to a terminal state that implies
	// the work is no longer active (not_planned is purely manual — the agent
	// claim must be cleared so the lock manager won't treat it as stalled).
	if stateChanged && card.State == board.StateNotPlanned {
		card.AssignedAgent = ""
		card.LastHeartbeat = nil
	}

	// Validate updated card
	if err := s.validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Validate parent and depends_on reference existing cards
	if err := s.validateCardReferences(ctx, project, card.Parent, card.DependsOn); err != nil {
		return nil, err
	}

	// Detect circular dependencies
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

	// Clear runner_status when card reaches a terminal state (before disk write).
	if stateChanged && (card.State == board.StateDone || card.State == board.StateNotPlanned) {
		card.RunnerStatus = ""
	}

	// Persist card
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if input.ImmediateCommit && s.gitAutoCommit {
		// Human edit: commit immediately regardless of deferred mode.
		cardPath := s.cardPath(project, id)
		msg := commitMessage("", id, "updated")
		if err := s.git.CommitFile(cardPath, msg); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
		s.notifyCommit()
	} else {
		if err := s.commitCardChange(project, id, "", "updated"); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
	}

	// Flush deferred commit when card reaches not_planned — no agent will
	// release this card so this is the only flush point.
	// For done/stalled: ReleaseCard (done) or markCardStalled (stalled)
	// handles the flush.
	if stateChanged && card.State == board.StateNotPlanned {
		if err := s.flushDeferredCommit(id, ""); err != nil {
			slog.Warn("flush deferred commit after state change", "card_id", id, "state", card.State, "error", err)
		}
	}

	// Publish event
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

	// Auto-transition parent if child state changed
	if stateChanged {
		s.maybeTransitionParent(ctx, card)
	}

	s.enrichDependenciesMet(ctx, card)
	return card, nil
}

// PatchCard applies partial updates to a card.
// Only non-nil fields in the input are updated.
func (s *CardService) PatchCard(ctx context.Context, project, id string, input PatchCardInput) (*board.Card, error) {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Load existing card
	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Load project config
	cfg, err := s.getConfig(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project config: %w", err)
	}

	// Track if state changed
	oldState := card.State
	stateChanged := false

	// Apply partial updates
	if input.Title != nil {
		card.Title = *input.Title
	}
	if input.State != nil {
		newState := *input.State
		if newState != oldState {
			// Validate state transition
			validator := s.validator
			if err := validator.ValidateTransition(cfg, oldState, newState); err != nil {
				return nil, fmt.Errorf("validate transition: %w", err)
			}
			// Block transition to in_progress if dependencies not met
			if newState == board.StateInProgress {
				met, blockers := s.checkDependencies(ctx, project, card.DependsOn)
				if !met {
					return nil, dependencyError(newState, blockers)
				}
			}
			card.State = newState
			stateChanged = true
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
	if input.FeatureBranch != nil {
		card.FeatureBranch = *input.FeatureBranch
		// BranchName is immutable after first generation — only set when empty.
		if card.FeatureBranch && card.BranchName == "" {
			card.BranchName = generateBranchName(card.ID, card.Title)
		}
		// Auto-clear create_pr when feature_branch is disabled.
		if !card.FeatureBranch {
			card.CreatePR = false
		}
	}
	if input.CreatePR != nil && card.FeatureBranch {
		card.CreatePR = *input.CreatePR
	}
	card.Updated = time.Now()

	// Release agent claim when card moves to not_planned so the lock manager
	// won't flag the card as stalled.
	if stateChanged && card.State == board.StateNotPlanned {
		card.AssignedAgent = ""
		card.LastHeartbeat = nil
	}

	// Validate updated card
	validator := s.validator
	if err := validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Clear runner_status when card reaches a terminal state (before disk write).
	if stateChanged && (card.State == board.StateDone || card.State == board.StateNotPlanned) {
		card.RunnerStatus = ""
	}

	// Persist card
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if input.ImmediateCommit && s.gitAutoCommit {
		// Human edit: commit immediately regardless of deferred mode.
		cardPath := s.cardPath(project, id)
		msg := commitMessage("", id, "updated")
		if err := s.git.CommitFile(cardPath, msg); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
		s.notifyCommit()
	} else {
		if err := s.commitCardChange(project, id, "", "updated"); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
	}

	// Flush deferred commit when card reaches not_planned — no agent will
	// release this card so this is the only flush point.
	// For done/stalled: ReleaseCard (done) or markCardStalled (stalled)
	// handles the flush.
	if stateChanged && card.State == board.StateNotPlanned {
		if err := s.flushDeferredCommit(id, ""); err != nil {
			slog.Warn("flush deferred commit after state change", "card_id", id, "state", card.State, "error", err)
		}
	}

	// Publish event
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

	// Auto-transition parent if child state changed
	if stateChanged {
		s.maybeTransitionParent(ctx, card)
	}

	s.enrichDependenciesMet(ctx, card)
	return card, nil
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
		if err := s.git.CommitAll(msg); err != nil {
			// File already deleted by store, just commit the change
			slog.Warn("git commit after delete", "error", err, "path", path)
		} else {
			s.notifyCommit()
		}
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Load card
	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
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
	if err := s.commitCardChange(project, id, entry.Agent, "log: "+entry.Action); err != nil {
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

// ReportUsage increments token usage counters on a card and recalculates cost.
func (s *CardService) ReportUsage(ctx context.Context, project, id string, input ReportUsageInput) (*board.Card, error) {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.TokenUsage == nil {
		card.TokenUsage = &board.TokenUsage{}
	}

	// Store the model name when provided
	if input.Model != "" {
		card.TokenUsage.Model = input.Model
	}

	card.TokenUsage.PromptTokens += input.PromptTokens
	card.TokenUsage.CompletionTokens += input.CompletionTokens

	// Calculate cost delta for this report and add to running total.
	// Warn when a model name is provided but not found in the cost map.
	if input.Model != "" {
		if rate, ok := s.tokenCosts[input.Model]; ok {
			deltaCost := float64(input.PromptTokens)*rate.Prompt + float64(input.CompletionTokens)*rate.Completion
			card.TokenUsage.EstimatedCostUSD += deltaCost
		} else {
			slog.Warn("unknown model in cost map, cost not calculated",
				"model", input.Model,
				"card_id", id,
			)
		}
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if err := s.commitCardChange(project, id, input.AgentID, "usage reported"); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// If the card has no active agent, flush immediately — there is no
	// subsequent ReleaseCard call to flush deferred paths (e.g. report_usage
	// called after complete_task).
	if card.AssignedAgent == "" {
		if err := s.flushDeferredCommit(id, input.AgentID); err != nil {
			slog.Warn("flush deferred commit on post-release usage report", "card_id", id, "error", err)
		}
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUsageReported,
		Project:   project,
		CardID:    id,
		Agent:     input.AgentID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"prompt_tokens":     input.PromptTokens,
			"completion_tokens": input.CompletionTokens,
			"model":             input.Model,
		},
	})

	s.enrichDependenciesMet(ctx, card)
	return card, nil
}

// AggregateUsage returns total token usage across all cards in a project.
func (s *CardService) AggregateUsage(ctx context.Context, project string) (*ProjectUsage, error) {
	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	usage := &ProjectUsage{}
	for _, card := range cards {
		if card.TokenUsage != nil {
			usage.PromptTokens += card.TokenUsage.PromptTokens
			usage.CompletionTokens += card.TokenUsage.CompletionTokens
			usage.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
			usage.CardCount++
		}
	}
	return usage, nil
}

// RecalculateCosts recomputes estimated costs for cards that have non-zero token
// counts but a zero estimated cost (e.g. because the model was not provided when
// usage was first reported). Only cards that match this condition are updated;
// cards that already have a non-zero estimated cost are left untouched.
//
// defaultModel is used when card.TokenUsage.Model is empty.  If neither the
// card's stored model nor defaultModel is in the cost map the card is skipped.
func (s *CardService) RecalculateCosts(ctx context.Context, project, defaultModel string) (*RecalculateCostsResult, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	result := &RecalculateCostsResult{}
	var updatedPaths []string

	for _, card := range cards {
		if card.TokenUsage == nil {
			continue
		}
		if card.TokenUsage.PromptTokens == 0 && card.TokenUsage.CompletionTokens == 0 {
			continue
		}
		if card.TokenUsage.EstimatedCostUSD != 0 {
			continue // already has a cost — don't double-count
		}

		model := card.TokenUsage.Model
		if model == "" {
			model = defaultModel
		}

		rate, ok := s.tokenCosts[model]
		if !ok {
			slog.Warn("recalculate_costs: model not in cost map, skipping card",
				"model", model,
				"card_id", card.ID,
			)
			continue
		}

		cost := float64(card.TokenUsage.PromptTokens)*rate.Prompt +
			float64(card.TokenUsage.CompletionTokens)*rate.Completion

		card.TokenUsage.EstimatedCostUSD = cost
		// Persist the effective model name so future recalculations are idempotent.
		if card.TokenUsage.Model == "" && model != "" {
			card.TokenUsage.Model = model
		}
		card.Updated = time.Now()

		if err := s.store.UpdateCard(ctx, project, card); err != nil {
			return nil, fmt.Errorf("update card %s: %w", card.ID, err)
		}

		updatedPaths = append(updatedPaths, s.cardPath(project, card.ID))
		result.CardsUpdated++
		result.TotalCostRecalculated += cost
	}

	// Batch-commit all recalculated cards in a single git commit.
	if s.gitAutoCommit && len(updatedPaths) > 0 {
		msg := fmt.Sprintf("[contextmatrix] %s: recalculated costs for %d cards", project, result.CardsUpdated)
		if err := s.git.CommitFiles(updatedPaths, msg); err != nil {
			return nil, fmt.Errorf("git commit recalculated costs: %w", err)
		}
		s.notifyCommit()
	}

	return result, nil
}

// GetDashboard computes aggregated dashboard data for a project.
func (s *CardService) GetDashboard(ctx context.Context, project string) (*DashboardData, error) {
	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	data := &DashboardData{
		StateCounts:  make(map[string]int),
		ActiveAgents: make([]ActiveAgent, 0),
		AgentCosts:   make([]AgentCost, 0),
		CardCosts:    make([]CardCost, 0),
	}

	agentCostMap := make(map[string]*AgentCost)

	for _, card := range cards {
		data.StateCounts[card.State]++

		// Active agents: cards with an assigned agent not in terminal states.
		if card.AssignedAgent != "" && card.State != board.StateDone && card.State != board.StateStalled && card.State != board.StateNotPlanned {
			aa := ActiveAgent{
				AgentID:   card.AssignedAgent,
				CardID:    card.ID,
				CardTitle: card.Title,
				Since:     card.Updated,
			}
			if card.LastHeartbeat != nil {
				aa.LastHeartbeat = *card.LastHeartbeat
				aa.Since = *card.LastHeartbeat
			}
			data.ActiveAgents = append(data.ActiveAgents, aa)
		}

		// Cards completed today.
		if card.State == board.StateDone && !card.Updated.Before(todayStart) {
			data.CardsCompletedToday++
		}

		// Cost aggregation.
		if card.TokenUsage != nil {
			data.TotalCostUSD += card.TokenUsage.EstimatedCostUSD

			data.CardCosts = append(data.CardCosts, CardCost{
				CardID:           card.ID,
				CardTitle:        card.Title,
				AssignedAgent:    card.AssignedAgent,
				PromptTokens:     card.TokenUsage.PromptTokens,
				CompletionTokens: card.TokenUsage.CompletionTokens,
				EstimatedCostUSD: card.TokenUsage.EstimatedCostUSD,
			})

			agent := card.AssignedAgent
			if agent == "" {
				agent = "unassigned"
			}
			ac, ok := agentCostMap[agent]
			if !ok {
				ac = &AgentCost{AgentID: agent}
				agentCostMap[agent] = ac
			}
			ac.PromptTokens += card.TokenUsage.PromptTokens
			ac.CompletionTokens += card.TokenUsage.CompletionTokens
			ac.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
			ac.CardCount++
		}
	}

	for _, ac := range agentCostMap {
		data.AgentCosts = append(data.AgentCosts, *ac)
	}

	return data, nil
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

// ClaimCard assigns a card to an agent.
// Flow: lock claim → store update → git commit → publish event.
func (s *CardService) ClaimCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Claim via lock manager (returns modified card)
	card, err := s.lock.Claim(ctx, project, id, agentID)
	if err != nil {
		return nil, fmt.Errorf("claim card: %w", err)
	}

	// Persist
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if err := s.commitCardChange(project, id, agentID, "claimed"); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardClaimed,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
	})

	return card, nil
}

// ReleaseCard removes an agent's claim on a card.
func (s *CardService) ReleaseCard(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Release via lock manager (returns modified card)
	card, err := s.lock.Release(ctx, project, id, agentID)
	if err != nil {
		return nil, fmt.Errorf("release card: %w", err)
	}

	// Persist
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if err := s.commitCardChange(project, id, agentID, "released"); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Flush any remaining deferred commits (release is the end of a work session)
	if err := s.flushDeferredCommit(id, agentID); err != nil {
		slog.Warn("flush deferred commit on release", "card_id", id, "error", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardReleased,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
	})

	return card, nil
}

// HeartbeatTimeout returns the configured heartbeat timeout duration.
func (s *CardService) HeartbeatTimeout() time.Duration {
	return s.lock.Timeout()
}

// HeartbeatCard updates the heartbeat timestamp for a claimed card.
func (s *CardService) HeartbeatCard(ctx context.Context, project, id, agentID string) error {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Heartbeat via lock manager (returns modified card)
	card, err := s.lock.Heartbeat(ctx, project, id, agentID)
	if err != nil {
		return fmt.Errorf("heartbeat card: %w", err)
	}

	// Persist
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer, silent, no event)
	if err := s.commitCardChange(project, id, agentID, "heartbeat"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// StartTimeoutChecker starts a background goroutine that periodically
// checks for stalled cards and transitions them to the "stalled" state.
// The goroutine stops when the context is cancelled.
func (s *CardService) StartTimeoutChecker(ctx context.Context, interval time.Duration) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("timeout checker panicked", "error", r)
			}
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("timeout checker stopped")
				return
			case <-ticker.C:
				if err := s.processStalled(ctx); err != nil {
					slog.Error("process stalled cards", "error", err)
				}
			}
		}
	}()

	slog.Info("timeout checker started", "interval", interval)
}

// processStalled finds and handles all stalled cards.
func (s *CardService) processStalled(ctx context.Context) error {
	stalled, err := s.lock.FindStalled(ctx)
	if err != nil {
		return fmt.Errorf("find stalled: %w", err)
	}

	for _, sc := range stalled {
		if err := s.markCardStalled(ctx, sc); err != nil {
			slog.Error("mark card stalled",
				"project", sc.Project,
				"card_id", sc.Card.ID,
				"error", err,
			)
			// Continue processing other cards
		}
	}

	return nil
}

// markCardStalled transitions a card to the "stalled" state.
func (s *CardService) markCardStalled(ctx context.Context, sc lock.StalledCard) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card := sc.Card
	previousAgent := card.AssignedAgent

	// Update card state
	card.State = board.StateStalled
	card.AssignedAgent = ""
	card.LastHeartbeat = nil
	card.Updated = time.Now()

	// Persist
	if err := s.store.UpdateCard(ctx, sc.Project, card); err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	// Git commit (or defer)
	if err := s.commitCardChange(sc.Project, card.ID, "", "stalled (heartbeat timeout)"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Flush any deferred commits since card is now in a final state
	if err := s.flushDeferredCommit(card.ID, previousAgent); err != nil {
		slog.Warn("flush deferred commit after stall", "card_id", card.ID, "error", err)
	}

	// Publish event
	s.bus.Publish(events.Event{
		Type:      events.CardStalled,
		Project:   sc.Project,
		CardID:    card.ID,
		Timestamp: card.Updated,
		Data: map[string]any{
			"previous_agent": previousAgent,
		},
	})

	slog.Info("card marked stalled",
		"project", sc.Project,
		"card_id", card.ID,
		"previous_agent", previousAgent,
	)

	return nil
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

// cardPath returns the relative path for a card file (for git operations).
// Paths are relative to the boards directory (which is the git repo root).
func (s *CardService) cardPath(project, id string) string {
	return filepath.Join(project, "tasks", id+".md")
}

// getConfig returns the cached project config, loading it if necessary.
func (s *CardService) getConfig(ctx context.Context, project string) (*board.ProjectConfig, error) {
	s.mu.RLock()
	cfg, ok := s.configs[project]
	s.mu.RUnlock()

	if ok {
		return cfg, nil
	}

	// Load from store
	cfg, err := s.store.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.configs[project] = cfg
	s.mu.Unlock()

	return cfg, nil
}

// getConfigLocked returns the project config, assumes caller holds s.mu.
// Always reloads from store to get latest NextID.
func (s *CardService) getConfigLocked(ctx context.Context, project string) (*board.ProjectConfig, error) {
	// Always reload to get current NextID for atomic ID generation
	cfg, err := s.store.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}

	s.configs[project] = cfg
	return cfg, nil
}

// getTemplates returns the cached templates for a project, loading them if necessary.
func (s *CardService) getTemplates(project string) (map[string]string, error) {
	s.mu.RLock()
	templates, ok := s.templates[project]
	s.mu.RUnlock()

	if ok {
		return templates, nil
	}

	// Load from filesystem
	projectDir := filepath.Join(s.boardsDir, project)
	templates, err := board.LoadTemplates(projectDir)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.templates[project] = templates
	s.mu.Unlock()

	return templates, nil
}

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
				Err:     board.ErrInvalidType,
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

// TransitionTo walks the shortest path of state transitions to reach targetState.
// Each intermediate transition goes through PatchCard (git commit + event per step).
// Returns the card in its final state, or an error if any step fails.
func (s *CardService) TransitionTo(ctx context.Context, project, cardID, targetState string) (*board.Card, error) {
	cardID = strings.ToUpper(cardID)
	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.State == targetState {
		return card, nil
	}

	cfg, err := s.getConfig(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("get project config: %w", err)
	}

	validator := s.validator
	path, err := validator.FindShortestPath(cfg, card.State, targetState)
	if err != nil {
		return nil, fmt.Errorf("find transition path: %w", err)
	}

	for _, state := range path {
		next := state
		card, err = s.PatchCard(ctx, project, cardID, PatchCardInput{State: &next})
		if err != nil {
			return nil, fmt.Errorf("transition to %s: %w", state, err)
		}
	}

	return card, nil
}

// commitCardChange either commits a card file immediately or records it for a
// deferred commit, depending on the gitDeferredCommit setting.
// Caller must hold writeMu.
func (s *CardService) commitCardChange(project, cardID, agentID, action string) error {
	if !s.gitAutoCommit {
		return nil
	}
	path := s.cardPath(project, cardID)
	if s.gitDeferredCommit {
		// Accumulate path for later flush; skip the git commit for now.
		s.deferredPaths[cardID] = append(s.deferredPaths[cardID], path)
		return nil
	}
	msg := commitMessage(agentID, cardID, action)
	if err := s.git.CommitFile(path, msg); err != nil {
		return err
	}
	s.notifyCommit()
	return nil
}

// flushDeferredCommit stages all accumulated deferred paths for cardID and
// produces a single commit. No-ops if there are no deferred paths.
// Caller must hold writeMu (or be in a context where no concurrent mutations occur).
func (s *CardService) flushDeferredCommit(cardID, agentID string) error {
	if !s.gitAutoCommit || !s.gitDeferredCommit {
		return nil
	}
	paths := s.deferredPaths[cardID]
	if len(paths) == 0 {
		return nil
	}
	// Deduplicate paths (same file may appear multiple times).
	seen := make(map[string]bool, len(paths))
	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	msg := commitMessage(agentID, cardID, "completed (deferred commit)")
	// Use shell git instead of go-git to avoid stale in-memory state after
	// shell-based push/rebase operations by the gitsync layer.
	if err := s.git.CommitFilesShell(context.Background(), unique, msg); err != nil {
		return err
	}
	// Delete paths only after a successful commit — prevents data loss if
	// the commit fails.
	delete(s.deferredPaths, cardID)
	// Refresh go-git's in-memory repo state so subsequent read operations
	// (e.g. GetLastCommitMessage) see the shell-git commit.
	if err := s.git.ReloadRepo(); err != nil {
		slog.Warn("reload repo after deferred flush", "card_id", cardID, "error", err)
	}
	s.notifyCommit()
	return nil
}

// maybeTransitionParent checks if a child's state change should trigger a
// parent state transition. Called after any child state change while writeMu
// is held. It does NOT acquire writeMu — callers must hold it.
//
// Rules:
//   - child moved to in_progress AND parent is in todo → transition parent to in_progress
//   - child moved to done AND ALL sibling subtasks are done → transition parent to review
func (s *CardService) maybeTransitionParent(ctx context.Context, child *board.Card) {
	if child.Parent == "" {
		return
	}

	parent, err := s.store.GetCard(ctx, child.Project, child.Parent)
	if err != nil {
		slog.Warn("parent auto-transition: get parent card",
			"parent_id", child.Parent,
			"child_id", child.ID,
			"error", err,
		)
		return
	}

	switch child.State {
	case board.StateInProgress:
		if parent.State == board.StateTodo {
			if err := s.transitionParentDirect(ctx, parent, board.StateInProgress); err != nil {
				slog.Warn("parent auto-transition: todo→in_progress",
					"parent_id", parent.ID,
					"error", err,
				)
			}
		}

	case board.StateDone:
		// Discover all children via store query (not parent.Subtasks, which may be empty
		// when children are created with parent field but parent's subtasks list is not updated).
		children, err := s.store.ListCards(ctx, child.Project, storage.CardFilter{Parent: child.Parent})
		if err != nil {
			slog.Warn("parent auto-transition: list children",
				"parent_id", child.Parent,
				"error", err,
			)
			return
		}

		// Guard: if no children found, never auto-transition
		if len(children) == 0 {
			return
		}

		// Check if all siblings are done
		allDone := true
		for _, sibling := range children {
			if sibling.ID == child.ID {
				continue // This child is already done (the one we just transitioned)
			}
			if sibling.State != board.StateDone {
				allDone = false
				break
			}
		}
		if allDone && parent.State != board.StateReview && parent.State != board.StateDone {
			if err := s.transitionParentDirect(ctx, parent, board.StateReview); err != nil {
				slog.Warn("parent auto-transition: in_progress→review",
					"parent_id", parent.ID,
					"error", err,
				)
			}
		}
	}
}

// transitionParentDirect transitions a parent card to the target state,
// persists it, commits to git, and publishes events. It walks the shortest
// valid transition path. Called while writeMu is held — does NOT re-acquire it.
func (s *CardService) transitionParentDirect(ctx context.Context, parent *board.Card, targetState string) error {
	if parent.State == targetState {
		return nil
	}

	cfg, err := s.getConfig(ctx, parent.Project)
	if err != nil {
		return fmt.Errorf("get project config: %w", err)
	}

	validator := s.validator
	path, err := validator.FindShortestPath(cfg, parent.State, targetState)
	if err != nil {
		return fmt.Errorf("find transition path from %s to %s: %w", parent.State, targetState, err)
	}

	for _, state := range path {
		oldState := parent.State
		parent.State = state
		parent.Updated = time.Now()

		if err := s.store.UpdateCard(ctx, parent.Project, parent); err != nil {
			return fmt.Errorf("persist parent card: %w", err)
		}

		if err := s.commitCardChange(parent.Project, parent.ID, "", "auto-transitioned to "+state); err != nil {
			slog.Warn("git commit for parent auto-transition", "parent_id", parent.ID, "error", err)
		}

		s.bus.Publish(events.Event{
			Type:      events.CardStateChanged,
			Project:   parent.Project,
			CardID:    parent.ID,
			Timestamp: parent.Updated,
			Data: map[string]any{
				"old_state": oldState,
				"new_state": state,
			},
		})

		slog.Info("parent auto-transitioned",
			"parent_id", parent.ID,
			"old_state", oldState,
			"new_state", state,
		)
	}

	// Flush deferred commits for the parent card
	if err := s.flushDeferredCommit(parent.ID, ""); err != nil {
		slog.Warn("flush deferred commit for parent auto-transition",
			"parent_id", parent.ID, "error", err)
	}

	return nil
}

// commitMessage formats a commit message with optional agent prefix.
func commitMessage(agentID, cardID, action string) string {
	if agentID != "" {
		return fmt.Sprintf("[agent:%s] %s: %s", agentID, cardID, action)
	}
	return fmt.Sprintf("[contextmatrix] %s: %s", cardID, action)
}

// branchNameSlugPattern matches anything that's not lowercase alphanumeric.
var branchNameSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

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

// ErrProtectedBranch is returned when an operation targets a protected branch (main/master).
var ErrProtectedBranch = fmt.Errorf("pushing to main/master is never allowed")

// isProtectedBranch returns true if the branch name resolves to main or master.
func isProtectedBranch(branch string) bool {
	normalized := strings.ToLower(strings.TrimSpace(branch))
	normalized = strings.TrimPrefix(normalized, "refs/heads/")
	return normalized == "main" || normalized == "master"
}

// ErrInvalidPRUrl is returned when a PR URL does not use http:// or https://.
var ErrInvalidPRUrl = fmt.Errorf("pr_url must use http or https scheme")

// RecordPush records a git push event on a card, updating PRUrl if provided and
// adding an activity log entry. All mutations are atomic under a single lock.
// Returns ErrProtectedBranch if the branch is main/master.
func (s *CardService) RecordPush(ctx context.Context, project, id, agentID, branch, prURL string) (*board.Card, error) {
	id = strings.ToUpper(id)

	// Service-layer branch protection — defense in depth.
	if isProtectedBranch(branch) {
		return nil, ErrProtectedBranch
	}

	// Validate PR URL scheme before acquiring the lock.
	if prURL != "" && !strings.HasPrefix(prURL, "https://") && !strings.HasPrefix(prURL, "http://") {
		return nil, ErrInvalidPRUrl
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != agentID {
		return nil, lock.ErrAgentMismatch
	}

	// Update PR URL if provided.
	if prURL != "" {
		card.PRUrl = prURL
	}

	// Append activity log entry.
	msg := "Pushed to branch " + branch
	if prURL != "" {
		msg += "; PR: " + prURL
	}
	entry := board.ActivityEntry{
		Agent:     agentID,
		Action:    "pushed",
		Message:   msg,
		Timestamp: time.Now(),
	}
	card.ActivityLog = append(card.ActivityLog, entry)
	if len(card.ActivityLog) > maxActivityLogEntries {
		card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}
	if err := s.commitCardChange(project, id, agentID, "pushed to "+branch); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Publish events after releasing the lock is not possible with defer,
	// so publish here (still under lock — acceptable for in-process bus).
	s.bus.Publish(events.Event{
		Type:      events.CardUpdated,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
		Data: map[string]any{
			"action": "pushed",
			"branch": branch,
			"pr_url": prURL,
		},
	})

	return card, nil
}

// ErrReviewAttemptsCapped is returned when the review_attempts counter has reached its limit.
var ErrReviewAttemptsCapped = fmt.Errorf("review attempts limit reached")

// ErrRunnerDisabled is returned when runner operations are attempted but the runner is not enabled.
var ErrRunnerDisabled = fmt.Errorf("remote execution is not enabled")

// IncrementReviewAttempts atomically increments the review_attempts counter on a card.
// Returns lock.ErrAgentMismatch if the caller is not the assigned agent, and
// ErrReviewAttemptsCapped if the counter has reached maxReviewAttempts.
func (s *CardService) IncrementReviewAttempts(ctx context.Context, project, id, agentID string) (*board.Card, error) {
	id = strings.ToUpper(id)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	// Verify agent ownership.
	if card.AssignedAgent != agentID {
		return nil, lock.ErrAgentMismatch
	}

	if card.ReviewAttempts >= maxReviewAttempts {
		return nil, fmt.Errorf("review attempts capped at %d: %w", maxReviewAttempts, ErrReviewAttemptsCapped)
	}

	card.ReviewAttempts++
	card.Updated = time.Now()
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}
	if err := s.commitCardChange(project, id, agentID, "review_attempts incremented"); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	s.bus.Publish(events.Event{
		Type:      events.CardUpdated,
		Project:   project,
		CardID:    id,
		Agent:     agentID,
		Timestamp: time.Now(),
		Data: map[string]any{
			"action":          "review_attempts_incremented",
			"review_attempts": card.ReviewAttempts,
		},
	})

	return card, nil
}

// UpdateRunnerStatus sets the runner_status field on a card.
func (s *CardService) UpdateRunnerStatus(ctx context.Context, project, cardID, status, message string) (*board.Card, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.validator.ValidateRunnerStatus(status); err != nil {
		return nil, err
	}

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	card.RunnerStatus = status
	card.Updated = time.Now()

	// Clear agent claim on terminal runner statuses.
	if status == "failed" || status == "killed" || status == "completed" {
		card.AssignedAgent = ""
		card.LastHeartbeat = nil
	}
	// On completed, also clear runner_status since the run is over.
	if status == "completed" {
		card.RunnerStatus = ""
	}

	if message != "" {
		card.ActivityLog = append(card.ActivityLog, board.ActivityEntry{
			Agent:     "runner",
			Timestamp: time.Now(),
			Action:    "runner_status",
			Message:   message,
		})
		if len(card.ActivityLog) > maxActivityLogEntries {
			card.ActivityLog = card.ActivityLog[len(card.ActivityLog)-maxActivityLogEntries:]
		}
	}

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}
	if err := s.commitCardChange(project, cardID, "runner", "runner_status: "+status); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Flush deferred commits only on terminal runner statuses (completed,
	// failed, killed). These occur after the agent has released the card, so
	// there is no subsequent flush point. Non-terminal statuses (queued,
	// running) happen during active work and should continue to defer.
	if status == "failed" || status == "killed" || status == "completed" {
		if err := s.flushDeferredCommit(cardID, "runner"); err != nil {
			slog.Warn("flush deferred commit on runner status update", "card_id", cardID, "error", err)
		}
	}

	var eventType events.EventType
	switch status {
	case "queued":
		eventType = events.RunnerTriggered
	case "running":
		eventType = events.RunnerStarted
	case "failed":
		eventType = events.RunnerFailed
	case "killed":
		eventType = events.RunnerKilled
	default:
		eventType = events.CardUpdated
	}
	s.bus.Publish(events.Event{
		Type:      eventType,
		Project:   project,
		CardID:    cardID,
		Timestamp: time.Now(),
		Data:      map[string]any{"runner_status": status},
	})

	return card, nil
}
