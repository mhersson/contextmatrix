package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// atomicWriteFile writes data to a file atomically by writing to a temporary
// file in the same directory, syncing to disk, then renaming over the target.
// This prevents partial writes from being visible to readers.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpName := tmp.Name()

	// Clean up the temp file on any failure path.
	success := false

	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	success = true

	return nil
}

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

// projectIndex holds the config and an in-memory copy of every card in a
// project. cards stores full *board.Card values so that reads never need to
// touch disk in steady state. paths maps card ID → file path so writes and
// cache-miss fallbacks can locate the on-disk file.
type projectIndex struct {
	config *board.ProjectConfig
	cards  map[string]*board.Card
	paths  map[string]string
}

// FilesystemStore implements Store using the local filesystem.
// Cards are stored as markdown files with YAML frontmatter.
// An in-memory cache of full cards enables reads without disk I/O.
type FilesystemStore struct {
	boardsDir string
	mu        sync.RWMutex
	projects  map[string]*projectIndex
}

// NewFilesystemStore creates a new FilesystemStore and loads the cache.
func NewFilesystemStore(boardsDir string) (*FilesystemStore, error) {
	store := &FilesystemStore{
		boardsDir: boardsDir,
		projects:  make(map[string]*projectIndex),
	}

	if err := store.loadIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

	store.updateCacheSizeMetric()

	return store, nil
}

// copyCard returns a deep copy of the card so that callers cannot mutate the
// cached value. All slice, map, and pointer fields are cloned.
func copyCard(c *board.Card) *board.Card {
	if c == nil {
		return nil
	}

	cp := *c

	if c.LastHeartbeat != nil {
		ts := *c.LastHeartbeat
		cp.LastHeartbeat = &ts
	}

	if c.Subtasks != nil {
		cp.Subtasks = slices.Clone(c.Subtasks)
	}

	if c.DependsOn != nil {
		cp.DependsOn = slices.Clone(c.DependsOn)
	}

	if c.DependenciesMet != nil {
		v := *c.DependenciesMet
		cp.DependenciesMet = &v
	}

	if c.Context != nil {
		cp.Context = slices.Clone(c.Context)
	}

	if c.Labels != nil {
		cp.Labels = slices.Clone(c.Labels)
	}

	if c.Source != nil {
		src := *c.Source
		cp.Source = &src
	}

	if c.Custom != nil {
		cp.Custom = make(map[string]any, len(c.Custom))
		for k, v := range c.Custom {
			cp.Custom[k] = v
		}
	}

	if c.TokenUsage != nil {
		tu := *c.TokenUsage
		cp.TokenUsage = &tu
	}

	if c.ActivityLog != nil {
		cp.ActivityLog = slices.Clone(c.ActivityLog)
	}

	return &cp
}

// loadIndex scans the boards directory and builds the in-memory cache.
// Callers must hold s.mu.Lock unless the store is still being constructed.
func (s *FilesystemStore) loadIndex(ctx context.Context) error {
	configs, err := board.DiscoverProjects(s.boardsDir)
	if err != nil {
		return fmt.Errorf("discover projects: %w", err)
	}

	for i := range configs {
		cfg := &configs[i]
		projectDir := filepath.Join(s.boardsDir, cfg.Name)

		idx := &projectIndex{
			config: cfg,
			cards:  make(map[string]*board.Card),
			paths:  make(map[string]string),
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
				ctxlog.Logger(ctx).Warn("skipping symlink card file",
					"path", filepath.Join(tasksDir, entry.Name()),
				)

				continue
			}

			filePath := filepath.Join(tasksDir, entry.Name())

			data, err := os.ReadFile(filePath)
			if err != nil {
				ctxlog.Logger(ctx).Warn("skipping unreadable card file",
					"path", filePath,
					"error", err,
				)

				continue
			}

			card, err := board.ParseCard(data)
			if err != nil {
				ctxlog.Logger(ctx).Warn("skipping unparseable card file",
					"path", filePath,
					"error", err,
				)

				continue
			}

			idx.cards[card.ID] = card
			idx.paths[card.ID] = filePath
		}

		s.projects[cfg.Name] = idx
	}

	return nil
}

// ReloadIndex rebuilds the in-memory cache from disk.
// This is used after a git pull brings new/changed card files.
func (s *FilesystemStore) ReloadIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.projects = make(map[string]*projectIndex)

	if err := s.loadIndex(ctx); err != nil {
		return err
	}

	s.updateCacheSizeMetric()

	return nil
}

// updateCacheSizeMetric recomputes card_cache_size from the in-memory state.
// Callers must hold s.mu (read or write).
func (s *FilesystemStore) updateCacheSizeMetric() {
	total := 0
	for _, idx := range s.projects {
		total += len(idx.cards)
	}

	metrics.CardCacheSize.Set(float64(total))
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

// matchesFilter checks if a cached card matches the filter criteria.
func (s *FilesystemStore) matchesFilter(c *board.Card, f CardFilter) bool {
	if f.State != "" && c.State != f.State {
		return false
	}

	if f.Type != "" && c.Type != f.Type {
		return false
	}

	if f.Priority != "" && c.Priority != f.Priority {
		return false
	}

	if f.AssignedAgent != "" && c.AssignedAgent != f.AssignedAgent {
		return false
	}

	if f.Parent != "" && c.Parent != f.Parent {
		return false
	}

	if f.ExternalID != "" {
		if c.Source == nil || c.Source.ExternalID != f.ExternalID {
			return false
		}
	}

	if f.Label != "" && !slices.Contains(c.Labels, f.Label) {
		return false
	}

	if f.Vetted != nil && c.Vetted != *f.Vetted {
		return false
	}

	return true
}

// ListProjects returns all discovered projects.
func (s *FilesystemStore) ListProjects(ctx context.Context) ([]board.ProjectConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make([]board.ProjectConfig, 0, len(s.projects))
	for _, idx := range s.projects {
		configs = append(configs, *idx.config)
	}

	return configs, nil
}

// GetProject returns the configuration for a specific project.
func (s *FilesystemStore) GetProject(ctx context.Context, name string) (*board.ProjectConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
func (s *FilesystemStore) SaveProject(ctx context.Context, cfg *board.ProjectConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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
			cards:  make(map[string]*board.Card),
			paths:  make(map[string]string),
		}
	}

	return nil
}

// DeleteProject removes a project and its directory from disk.
func (s *FilesystemStore) DeleteProject(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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
	s.updateCacheSizeMetric()

	return nil
}

// ProjectCardCount returns the number of cards in a project.
func (s *FilesystemStore) ProjectCardCount(ctx context.Context, name string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.projects[name]
	if !ok {
		return 0, ErrProjectNotFound
	}

	return len(idx.cards), nil
}

// ListCards returns all cards in a project matching the filter.
// Results are deep copies of the cached cards; mutating a returned card does
// not affect subsequent reads. No disk I/O is performed in steady state.
func (s *FilesystemStore) ListCards(ctx context.Context, project string, filter CardFilter) ([]*board.Card, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.projects[project]
	if !ok {
		return nil, ErrProjectNotFound
	}

	cards := make([]*board.Card, 0, len(idx.cards))

	for _, c := range idx.cards {
		if err := ctx.Err(); err != nil {
			return cards, err
		}

		if s.matchesFilter(c, filter) {
			cards = append(cards, copyCard(c))
		}
	}

	return cards, nil
}

// GetCard returns a deep copy of the requested card from the cache.
// On cache miss — a rare window during the boot scan or a concurrent reload —
// the canonical on-disk file is read as a fallback so callers never see a
// transient ErrCardNotFound for a card that exists on disk.
func (s *FilesystemStore) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()

	idx, ok := s.projects[project]
	if !ok {
		s.mu.RUnlock()

		return nil, ErrProjectNotFound
	}

	if cached, ok := idx.cards[id]; ok {
		card := copyCard(cached)

		s.mu.RUnlock()

		return card, nil
	}

	s.mu.RUnlock()

	// Cache miss: fall through to disk. This keeps read semantics robust
	// during the narrow window where the cache is being rebuilt after a
	// git rebase or boot scan.
	metrics.CardCacheMissTotal.Inc()
	ctxlog.Logger(ctx).Debug("card cache miss", "project", project, "id", id)

	filePath, err := s.cardPath(project, id)
	if err != nil {
		return nil, err
	}

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
func (s *FilesystemStore) CreateCard(ctx context.Context, project string, card *board.Card) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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

	if err := atomicWriteFile(filePath, data); err != nil {
		return fmt.Errorf("write card file: %w", err)
	}

	idx.cards[card.ID] = copyCard(card)
	idx.paths[card.ID] = filePath

	s.updateCacheSizeMetric()

	return nil
}

// UpdateCard persists changes to an existing card.
func (s *FilesystemStore) UpdateCard(ctx context.Context, project string, card *board.Card) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.projects[project]
	if !ok {
		return ErrProjectNotFound
	}

	if _, ok := idx.cards[card.ID]; !ok {
		return ErrCardNotFound
	}

	filePath, ok := idx.paths[card.ID]
	if !ok {
		// Should not happen — cards and paths are kept in sync — but fall
		// back to the canonical path so we don't lose the update.
		fp, err := s.cardPath(project, card.ID)
		if err != nil {
			return err
		}

		filePath = fp
	}

	data, err := board.SerializeCard(card)
	if err != nil {
		return fmt.Errorf("serialize card: %w", err)
	}

	if err := atomicWriteFile(filePath, data); err != nil {
		return fmt.Errorf("write card file: %w", err)
	}

	idx.cards[card.ID] = copyCard(card)
	idx.paths[card.ID] = filePath

	return nil
}

// DeleteCard removes a card.
func (s *FilesystemStore) DeleteCard(ctx context.Context, project, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.projects[project]
	if !ok {
		return ErrProjectNotFound
	}

	if _, ok := idx.cards[id]; !ok {
		return ErrCardNotFound
	}

	filePath, ok := idx.paths[id]
	if !ok {
		fp, err := s.cardPath(project, id)
		if err != nil {
			return err
		}

		filePath = fp
	}

	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("remove card file: %w", err)
		}
	}

	delete(idx.cards, id)
	delete(idx.paths, id)
	s.updateCacheSizeMetric()

	return nil
}
