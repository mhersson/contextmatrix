package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
)

// validatePathComponent rejects path components that could cause directory
// traversal (e.g. "..", "/", or names containing path separators).
func validatePathComponent(component string) error {
	if component == "" || component == "." || component == ".." ||
		strings.ContainsAny(component, "/\\") ||
		filepath.Clean(component) != component {
		return fmt.Errorf("%w: %q", ErrInvalidPath, component)
	}
	return nil
}

// cardIndex stores metadata for a single card for fast filtering.
type cardIndex struct {
	ID            string
	Title         string
	Project       string
	Type          string
	State         string
	Priority      string
	AssignedAgent string
	Parent        string
	Labels        []string
	ExternalID    string
	FilePath      string
}

// projectIndex stores all cards for a project.
type projectIndex struct {
	config *board.ProjectConfig
	cards  map[string]*cardIndex
}

// FilesystemStore implements Store using the local filesystem.
// Cards are stored as markdown files with YAML frontmatter.
// An in-memory index enables fast filtering without reading files.
type FilesystemStore struct {
	boardsDir string
	mu        sync.RWMutex
	projects  map[string]*projectIndex
}

// NewFilesystemStore creates a new FilesystemStore and loads the index.
func NewFilesystemStore(boardsDir string) (*FilesystemStore, error) {
	store := &FilesystemStore{
		boardsDir: boardsDir,
		projects:  make(map[string]*projectIndex),
	}

	if err := store.loadIndex(); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

	return store, nil
}

// loadIndex scans the boards directory and builds the in-memory index.
func (s *FilesystemStore) loadIndex() error {
	configs, err := board.DiscoverProjects(s.boardsDir)
	if err != nil {
		return fmt.Errorf("discover projects: %w", err)
	}

	for i := range configs {
		cfg := &configs[i]
		projectDir := filepath.Join(s.boardsDir, cfg.Name)

		idx := &projectIndex{
			config: cfg,
			cards:  make(map[string]*cardIndex),
		}

		tasksDir := filepath.Join(projectDir, "tasks")
		entries, err := os.ReadDir(tasksDir)
		if err != nil {
			if os.IsNotExist(err) {
				s.projects[cfg.Name] = idx
				continue
			}
			return fmt.Errorf("read tasks dir for %s: %w", cfg.Name, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			// Reject symlinks to prevent reads/writes outside the boards directory.
			if entry.Type()&os.ModeSymlink != 0 {
				slog.Warn("skipping symlink card file",
					"path", filepath.Join(tasksDir, entry.Name()),
				)
				continue
			}

			filePath := filepath.Join(tasksDir, entry.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Warn("skipping unreadable card file",
					"path", filePath,
					"error", err,
				)
				continue
			}

			card, err := board.ParseCard(data)
			if err != nil {
				slog.Warn("skipping unparseable card file",
					"path", filePath,
					"error", err,
				)
				continue
			}

			idx.cards[card.ID] = s.buildCardIndex(card, filePath)
		}

		s.projects[cfg.Name] = idx
	}

	return nil
}

// ReloadIndex rebuilds the in-memory index from disk.
// This is used after a git pull brings new/changed card files.
func (s *FilesystemStore) ReloadIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects = make(map[string]*projectIndex)
	return s.loadIndex()
}

// buildCardIndex creates a cardIndex from a Card.
func (s *FilesystemStore) buildCardIndex(card *board.Card, filePath string) *cardIndex {
	idx := &cardIndex{
		ID:            card.ID,
		Title:         card.Title,
		Project:       card.Project,
		Type:          card.Type,
		State:         card.State,
		Priority:      card.Priority,
		AssignedAgent: card.AssignedAgent,
		Parent:        card.Parent,
		Labels:        card.Labels,
		FilePath:      filePath,
	}

	if card.Source != nil {
		idx.ExternalID = card.Source.ExternalID
	}

	return idx
}

// cardPath returns the filesystem path for a card after validating that the
// project and card ID cannot escape the boards directory.
func (s *FilesystemStore) cardPath(project, id string) (string, error) {
	if err := validatePathComponent(project); err != nil {
		return "", fmt.Errorf("project name: %w", err)
	}
	if err := validatePathComponent(id); err != nil {
		return "", fmt.Errorf("card ID: %w", err)
	}
	return filepath.Join(s.boardsDir, project, "tasks", id+".md"), nil
}

// projectPath returns the filesystem path for a project directory after
// validating that the name cannot escape the boards directory.
func (s *FilesystemStore) projectPath(name string) (string, error) {
	if err := validatePathComponent(name); err != nil {
		return "", fmt.Errorf("project name: %w", err)
	}
	return filepath.Join(s.boardsDir, name), nil
}

// matchesFilter checks if a card index matches the filter criteria.
func (s *FilesystemStore) matchesFilter(idx *cardIndex, f CardFilter) bool {
	if f.State != "" && idx.State != f.State {
		return false
	}
	if f.Type != "" && idx.Type != f.Type {
		return false
	}
	if f.Priority != "" && idx.Priority != f.Priority {
		return false
	}
	if f.AssignedAgent != "" && idx.AssignedAgent != f.AssignedAgent {
		return false
	}
	if f.Parent != "" && idx.Parent != f.Parent {
		return false
	}
	if f.ExternalID != "" && idx.ExternalID != f.ExternalID {
		return false
	}
	if f.Label != "" && !slices.Contains(idx.Labels, f.Label) {
		return false
	}
	return true
}

// ListProjects returns all discovered projects.
func (s *FilesystemStore) ListProjects(_ context.Context) ([]board.ProjectConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make([]board.ProjectConfig, 0, len(s.projects))
	for _, idx := range s.projects {
		configs = append(configs, *idx.config)
	}
	return configs, nil
}

// GetProject returns the configuration for a specific project.
func (s *FilesystemStore) GetProject(_ context.Context, name string) (*board.ProjectConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.projects[name]
	if !ok {
		return nil, ErrProjectNotFound
	}

	cfg := *idx.config
	return &cfg, nil
}

// SaveProject persists a project configuration.
func (s *FilesystemStore) SaveProject(_ context.Context, cfg *board.ProjectConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectDir, err := s.projectPath(cfg.Name)
	if err != nil {
		return err
	}

	if err := board.SaveProjectConfig(projectDir, cfg); err != nil {
		return fmt.Errorf("save project config: %w", err)
	}

	if idx, ok := s.projects[cfg.Name]; ok {
		idx.config = cfg
	} else {
		s.projects[cfg.Name] = &projectIndex{
			config: cfg,
			cards:  make(map[string]*cardIndex),
		}
	}

	return nil
}

// DeleteProject removes a project and its directory from disk.
func (s *FilesystemStore) DeleteProject(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.projects[name]; !ok {
		return ErrProjectNotFound
	}

	projectDir, err := s.projectPath(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("remove project directory: %w", err)
	}

	delete(s.projects, name)

	return nil
}

// ProjectCardCount returns the number of cards in a project.
func (s *FilesystemStore) ProjectCardCount(_ context.Context, name string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.projects[name]
	if !ok {
		return 0, ErrProjectNotFound
	}

	return len(idx.cards), nil
}

// ListCards returns all cards in a project matching the filter.
// RLock is held for the entire operation (index scan + file reads) to prevent
// TOCTOU races where files are deleted between the index scan and the read.
func (s *FilesystemStore) ListCards(_ context.Context, project string, filter CardFilter) ([]*board.Card, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.projects[project]
	if !ok {
		return nil, ErrProjectNotFound
	}

	var paths []string
	for _, cardIdx := range idx.cards {
		if s.matchesFilter(cardIdx, filter) {
			paths = append(paths, cardIdx.FilePath)
		}
	}

	cards := make([]*board.Card, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		card, err := board.ParseCard(data)
		if err != nil {
			slog.Warn("skipping corrupt card file", "path", path, "error", err)
			continue
		}
		cards = append(cards, card)
	}

	return cards, nil
}

// GetCard returns a specific card.
func (s *FilesystemStore) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	s.mu.RLock()
	idx, ok := s.projects[project]
	if !ok {
		s.mu.RUnlock()
		return nil, ErrProjectNotFound
	}
	cardIdx, ok := idx.cards[id]
	if !ok {
		s.mu.RUnlock()
		return nil, ErrCardNotFound
	}
	filePath := cardIdx.FilePath
	s.mu.RUnlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCardNotFound
		}
		return nil, fmt.Errorf("read card file: %w", err)
	}

	card, err := board.ParseCard(data)
	if err != nil {
		return nil, fmt.Errorf("parse card: %w", err)
	}

	return card, nil
}

// CreateCard persists a new card.
func (s *FilesystemStore) CreateCard(_ context.Context, project string, card *board.Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.projects[project]
	if !ok {
		return ErrProjectNotFound
	}
	if _, exists := idx.cards[card.ID]; exists {
		return ErrCardExists
	}

	data, err := board.SerializeCard(card)
	if err != nil {
		return fmt.Errorf("serialize card: %w", err)
	}

	filePath, err := s.cardPath(project, card.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create tasks directory: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("write card file: %w", err)
	}

	idx.cards[card.ID] = s.buildCardIndex(card, filePath)

	return nil
}

// UpdateCard persists changes to an existing card.
func (s *FilesystemStore) UpdateCard(_ context.Context, project string, card *board.Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.projects[project]
	if !ok {
		return ErrProjectNotFound
	}
	cardIdx, ok := idx.cards[card.ID]
	if !ok {
		return ErrCardNotFound
	}
	filePath := cardIdx.FilePath

	data, err := board.SerializeCard(card)
	if err != nil {
		return fmt.Errorf("serialize card: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("write card file: %w", err)
	}

	idx.cards[card.ID] = s.buildCardIndex(card, filePath)

	return nil
}

// DeleteCard removes a card.
func (s *FilesystemStore) DeleteCard(_ context.Context, project, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.projects[project]
	if !ok {
		return ErrProjectNotFound
	}
	cardIdx, ok := idx.cards[id]
	if !ok {
		return ErrCardNotFound
	}
	filePath := cardIdx.FilePath

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrCardNotFound
		}
		return fmt.Errorf("remove card file: %w", err)
	}

	delete(idx.cards, id)

	return nil
}
