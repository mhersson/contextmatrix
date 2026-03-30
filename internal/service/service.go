// Package service provides the CardService orchestration layer.
// It coordinates storage, git operations, lock management, event publishing,
// and state machine validation for all card mutations.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
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

// CardContext contains a card with its project configuration and template.
type CardContext struct {
	Card     *board.Card
	Project  *board.ProjectConfig
	Template string // Template body for the card's type
}

// CardService orchestrates all card operations by coordinating
// storage, git, lock management, events, and validation.
type CardService struct {
	store     storage.Store
	git       *gitops.Manager
	lock      *lock.Manager
	bus       *events.Bus
	boardsDir string

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
) *CardService {
	return &CardService{
		store:      store,
		git:        git,
		lock:       lock,
		bus:        bus,
		boardsDir:  boardsDir,
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

// ListCards returns all cards in a project matching the filter.
func (s *CardService) ListCards(ctx context.Context, project string, filter storage.CardFilter) ([]*board.Card, error) {
	return s.store.ListCards(ctx, project, filter)
}

// GetCard returns a specific card.
func (s *CardService) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	return s.store.GetCard(ctx, project, id)
}

// CreateCard creates a new card in the project.
// Flow: generate ID → validate → store → git commit → publish event.
func (s *CardService) CreateCard(ctx context.Context, project string, input CreateCardInput) (*board.Card, error) {
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

	return card, nil
}

// UpdateCard performs a full update of a card's mutable fields.
// Immutable fields (id, project, created, source) are preserved.
func (s *CardService) UpdateCard(ctx context.Context, project, id string, input UpdateCardInput) (*board.Card, error) {
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

	return card, nil
}

// PatchCard applies partial updates to a card.
// Only non-nil fields in the input are updated.
func (s *CardService) PatchCard(ctx context.Context, project, id string, input PatchCardInput) (*board.Card, error) {
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

	return card, nil
}

// DeleteCard removes a card from the project.
func (s *CardService) DeleteCard(ctx context.Context, project, id string) error {
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

// commitMessage formats a commit message with optional agent prefix.
func commitMessage(agentID, cardID, action string) string {
	if agentID != "" {
		return fmt.Sprintf("[agent:%s] %s: %s", agentID, cardID, action)
	}
	return fmt.Sprintf("[contextmatrix] %s: %s", cardID, action)
}
