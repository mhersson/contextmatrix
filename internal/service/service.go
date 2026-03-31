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
)

// CreateCardInput contains the fields for creating a new card.
// Server-managed fields (id, created, updated, activity_log) are not included.
type CreateCardInput struct {
	Title    string
	Type     string
	Priority string
	Labels   []string
	Parent   string
	Body     string
	Source   *board.Source // Optional, immutable after creation
}

// UpdateCardInput contains all mutable fields for a full card update.
// Immutable fields (id, project, created, source) are not included.
type UpdateCardInput struct {
	Title     string
	Type      string
	State     string
	Priority  string
	Labels    []string
	Parent    string
	Subtasks  []string
	DependsOn []string
	Context   []string
	Custom    map[string]any
	Body      string
}

// PatchCardInput contains optional fields for partial card updates.
// Nil values mean "do not change".
type PatchCardInput struct {
	Title    *string
	State    *string
	Priority *string
	Labels   []string // nil = don't change, empty slice = clear
	Body     *string
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
	store      storage.Store
	git        *gitops.Manager
	lock       *lock.Manager
	bus        *events.Bus
	boardsDir  string
	tokenCosts map[string]ModelCost

	// writeMu serializes all card mutations (create, update, patch, delete,
	// claim, release, heartbeat, log). This prevents races like two agents
	// claiming the same card simultaneously.
	writeMu sync.Mutex

	// Per-project caches
	mu         sync.RWMutex
	validators map[string]*board.Validator
	configs    map[string]*board.ProjectConfig
	templates  map[string]map[string]string // project -> type -> template
}

// NewCardService creates a new CardService with the given dependencies.
func NewCardService(
	store storage.Store,
	git *gitops.Manager,
	lock *lock.Manager,
	bus *events.Bus,
	boardsDir string,
	tokenCosts map[string]ModelCost,
) *CardService {
	return &CardService{
		store:      store,
		git:        git,
		lock:       lock,
		bus:        bus,
		boardsDir:  boardsDir,
		tokenCosts: tokenCosts,
		validators: make(map[string]*board.Validator),
		configs:    make(map[string]*board.ProjectConfig),
		templates:  make(map[string]map[string]string),
	}
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
	msg := fmt.Sprintf("[contextmatrix] %s: project created", input.Name)
	if err := s.git.CommitAll(msg); err != nil {
		slog.Warn("git commit after project create", "error", err)
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
	path := filepath.Join(name, ".board.yaml")
	msg := fmt.Sprintf("[contextmatrix] %s: project updated", name)
	if err := s.git.CommitFile(path, msg); err != nil {
		slog.Warn("git commit after project update", "error", err)
	}

	// Invalidate caches so they rebuild with new config
	s.mu.Lock()
	s.configs[name] = cfg
	delete(s.validators, name)
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
	msg := fmt.Sprintf("[contextmatrix] %s: project deleted", name)
	if err := s.git.CommitAll(msg); err != nil {
		slog.Warn("git commit after project delete", "error", err)
	}

	// Purge all caches
	s.mu.Lock()
	delete(s.configs, name)
	delete(s.validators, name)
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

	// Build card
	now := time.Now()
	card := &board.Card{
		ID:       cardID,
		Title:    input.Title,
		Project:  project,
		Type:     input.Type,
		State:    cfg.States[0], // Default to first state
		Priority: input.Priority,
		Labels:   input.Labels,
		Parent:   input.Parent,
		Source:   input.Source,
		Created:  now,
		Updated:  now,
		Body:     input.Body,
	}

	// Validate card fields
	validator := s.getValidator(project)
	if err := validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Persist card
	if err := s.store.CreateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}

	// Git commit
	path := s.cardPath(project, cardID)
	msg := commitMessage("", cardID, "created")
	if err := s.git.CommitFile(path, msg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
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
		validator := s.getValidator(project)
		if err := validator.ValidateTransition(cfg, oldState, input.State); err != nil {
			return nil, fmt.Errorf("validate transition: %w", err)
		}
		// Block transition to in_progress if dependencies not met
		if input.State == "in_progress" {
			met, blockers := s.checkDependencies(ctx, project, input.DependsOn)
			if !met {
				return nil, dependencyError(input.State, blockers)
			}
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
	card.Updated = time.Now()

	// Validate updated card
	validator := s.getValidator(project)
	if err := validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Persist card
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit
	path := s.cardPath(project, id)
	msg := commitMessage("", id, "updated")
	if err := s.git.CommitFile(path, msg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
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

	s.enrichDependenciesMet(ctx, card)
	return card, nil
}

// PatchCard applies partial updates to a card.
// Only non-nil fields in the input are updated.
func (s *CardService) PatchCard(ctx context.Context, project, id string, input PatchCardInput) (*board.Card, error) {
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
			validator := s.getValidator(project)
			if err := validator.ValidateTransition(cfg, oldState, newState); err != nil {
				return nil, fmt.Errorf("validate transition: %w", err)
			}
			// Block transition to in_progress if dependencies not met
			if newState == "in_progress" {
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
	card.Updated = time.Now()

	// Validate updated card
	validator := s.getValidator(project)
	if err := validator.ValidateCard(cfg, card); err != nil {
		return nil, fmt.Errorf("validate card: %w", err)
	}

	// Persist card
	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// Git commit
	path := s.cardPath(project, id)
	msg := commitMessage("", id, "updated")
	if err := s.git.CommitFile(path, msg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
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

	s.enrichDependenciesMet(ctx, card)
	return card, nil
}

// DeleteCard removes a card from the project.
func (s *CardService) DeleteCard(ctx context.Context, project, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Verify card exists
	_, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	// Delete from store
	if err := s.store.DeleteCard(ctx, project, id); err != nil {
		return fmt.Errorf("delete card: %w", err)
	}

	// Git commit deletion
	path := s.cardPath(project, id)
	msg := commitMessage("", id, "deleted")
	if err := s.git.CommitAll(msg); err != nil {
		// File already deleted by store, just commit the change
		slog.Warn("git commit after delete", "error", err, "path", path)
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

	// Git commit
	path := s.cardPath(project, id)
	msg := commitMessage(entry.Agent, id, "log: "+entry.Action)
	if err := s.git.CommitFile(path, msg); err != nil {
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	card, err := s.store.GetCard(ctx, project, id)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.TokenUsage == nil {
		card.TokenUsage = &board.TokenUsage{}
	}

	card.TokenUsage.PromptTokens += input.PromptTokens
	card.TokenUsage.CompletionTokens += input.CompletionTokens

	// Calculate cost delta for this report and add to running total
	if rate, ok := s.tokenCosts[input.Model]; ok {
		deltaCost := float64(input.PromptTokens)*rate.Prompt + float64(input.CompletionTokens)*rate.Completion
		card.TokenUsage.EstimatedCostUSD += deltaCost
	}

	card.Updated = time.Now()

	if err := s.store.UpdateCard(ctx, project, card); err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	path := s.cardPath(project, id)
	msg := commitMessage(input.AgentID, id, "usage reported")
	if err := s.git.CommitFile(path, msg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
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
		if card.AssignedAgent != "" && card.State != "done" && card.State != "stalled" {
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
		if card.State == "done" && !card.Updated.Before(todayStart) {
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

	// Git commit
	path := s.cardPath(project, id)
	msg := commitMessage(agentID, id, "claimed")
	if err := s.git.CommitFile(path, msg); err != nil {
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

	// Git commit
	path := s.cardPath(project, id)
	msg := commitMessage(agentID, id, "released")
	if err := s.git.CommitFile(path, msg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
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

// HeartbeatCard updates the heartbeat timestamp for a claimed card.
func (s *CardService) HeartbeatCard(ctx context.Context, project, id, agentID string) error {
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

	// Git commit (silent, no event)
	path := s.cardPath(project, id)
	msg := commitMessage(agentID, id, "heartbeat")
	if err := s.git.CommitFile(path, msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// StartTimeoutChecker starts a background goroutine that periodically
// checks for stalled cards and transitions them to the "stalled" state.
// The goroutine stops when the context is cancelled.
func (s *CardService) StartTimeoutChecker(ctx context.Context, interval time.Duration) {
	go func() {
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
	card.State = "stalled"
	card.AssignedAgent = ""
	card.LastHeartbeat = nil
	card.Updated = time.Now()

	// Persist
	if err := s.store.UpdateCard(ctx, sc.Project, card); err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	// Git commit
	path := s.cardPath(sc.Project, card.ID)
	msg := commitMessage("", card.ID, "stalled (heartbeat timeout)")
	if err := s.git.CommitFile(path, msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
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

// getValidator returns the cached validator for a project, creating it if necessary.
func (s *CardService) getValidator(project string) *board.Validator {
	s.mu.RLock()
	v, ok := s.validators[project]
	s.mu.RUnlock()

	if ok {
		return v
	}

	// Create new validator (Validator is stateless, so we can share one per project)
	v = board.NewValidator()

	s.mu.Lock()
	s.validators[project] = v
	s.mu.Unlock()

	return v
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
		if dep.State != "done" {
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

	validator := s.getValidator(project)
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

// commitMessage formats a commit message with optional agent prefix.
func commitMessage(agentID, cardID, action string) string {
	if agentID != "" {
		return fmt.Sprintf("[agent:%s] %s: %s", agentID, cardID, action)
	}
	return fmt.Sprintf("[contextmatrix] %s: %s", cardID, action)
}
