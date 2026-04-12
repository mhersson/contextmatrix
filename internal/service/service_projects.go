package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// CreateProjectInput contains the fields for creating a new project.
type CreateProjectInput struct {
	Name        string
	Prefix      string
	Repo        string
	States      []string
	Types       []string
	Priorities  []string
	Transitions map[string][]string
	Jira        *board.JiraEpicConfig
}

// UpdateProjectInput contains the mutable fields for updating a project.
// Name and Prefix are immutable and excluded.
type UpdateProjectInput struct {
	Repo        string
	States      []string
	Types       []string
	Priorities  []string
	Transitions map[string][]string
	GitHub      *board.GitHubImportConfig
	Jira        *board.JiraEpicConfig
}

// validProjectName matches safe directory names: alphanumeric, hyphens, underscores.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

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
		Jira:        input.Jira,
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

	// Git commit. CommitAll is not routed through the commit queue because
	// its path is "." (stage everything), which the queue would have to
	// special-case; for project-level events that fire at most once per
	// project-lifecycle, serializing via the manager's own mutex is fine.
	if s.gitAutoCommit {
		msg := fmt.Sprintf("[contextmatrix] %s: project created", input.Name)
		if err := s.git.CommitAll(ctx, msg); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}

		s.notifyCommit()
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

	// Deep-copy pre-update config so a failed git commit can restore the
	// store to its prior on-disk + cached state.
	snapshot := copyProjectConfig(cfg)

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
			// Skip built-in subtask type - it's auto-assigned when card has a parent
			if typ == board.SubtaskType {
				continue
			}

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
	if input.GitHub != nil {
		cfg.GitHub = input.GitHub
	}

	if input.Jira != nil {
		cfg.Jira = input.Jira
	}

	// SaveProject validates and persists
	if err := s.store.SaveProject(ctx, cfg); err != nil {
		return nil, fmt.Errorf("save project: %w", err)
	}

	// Git commit. Route through the commit queue when configured so ordering
	// is preserved with concurrent card commits; otherwise commit inline.
	if s.gitAutoCommit {
		path := filepath.Join(name, ".board.yaml")
		msg := fmt.Sprintf("[contextmatrix] %s: project updated", name)

		var commitErr error
		if s.commitQueue != nil {
			commitErr = <-s.commitQueue.Enqueue(gitops.CommitJob{
				Project: name,
				Kind:    gitops.CommitKindFile,
				Path:    path,
				Message: msg,
				Ctx:     ctx,
			})
		} else {
			commitErr = s.git.CommitFile(ctx, path, msg)
		}

		if commitErr != nil {
			return nil, s.rollbackProjectUpdateOnCommitFailure(ctx, name, snapshot, commitErr)
		}

		s.notifyCommit()
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

// rollbackProjectUpdateOnCommitFailure restores the project's on-disk config
// and cache to the pre-update snapshot after a failed git commit. Mirrors
// rollbackCardOnCommitFailure: the store write succeeded, the commit did not,
// and we must undo the store write so the cache + disk no longer describe a
// state that was never committed.
//
// Caller must hold writeMu.
func (s *CardService) rollbackProjectUpdateOnCommitFailure(
	ctx context.Context, name string, snapshot *board.ProjectConfig, commitErr error,
) error {
	if snapshot == nil {
		ctxlog.Logger(ctx).Error("project update commit failed without snapshot; cache/disk state unknown",
			"project", name, "error", commitErr)

		return fmt.Errorf("git commit (no snapshot for rollback): %w", commitErr)
	}

	if rollbackErr := s.store.SaveProject(ctx, snapshot); rollbackErr != nil {
		metrics.RollbackFailuresTotal.Inc()
		ctxlog.Logger(ctx).Error("project update commit failed and rollback failed; cache + disk inconsistent",
			"project", name,
			"committed", false,
			"rollback_failed", true,
			"commit_error", commitErr,
			"rollback_error", rollbackErr,
		)

		return errors.Join(
			fmt.Errorf("git commit (rollback failed, state inconsistent): %w", commitErr),
			fmt.Errorf("rollback: %w", rollbackErr),
		)
	}

	// Refresh the cache to match the restored on-disk config.
	s.mu.Lock()
	s.configs[name] = snapshot
	s.mu.Unlock()

	ctxlog.Logger(ctx).Warn("project update commit failed; rolled back cache + disk to pre-update config",
		"project", name,
	)

	return fmt.Errorf("git commit: %w", commitErr)
}

// DeleteProject removes a project. Requires zero cards.
//
// Commit-failure handling uses a journal-rollback strategy (approach b from
// the design doc): before asking the store to remove the directory, the
// project tree is snapshotted into an in-memory buffer. If the git commit
// fails, the snapshot is written back to disk and the store's on-disk view
// is reconciled with a targeted ReloadIndex so the project reappears in the
// cache. This is self-contained — no GitManager API change — and safe for a
// project being deleted because the invariant is zero cards, so the tree is
// small (.board.yaml plus any template files).
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

	// Snapshot the project directory tree before deletion so we can restore
	// it if the git commit fails. Must run before store.DeleteProject, which
	// is the destructive step.
	snapshot, snapErr := snapshotProjectDir(filepath.Join(s.boardsDir, name))
	if snapErr != nil {
		return fmt.Errorf("snapshot project dir for rollback: %w", snapErr)
	}

	// Delete from store (removes directory and index)
	if err := s.store.DeleteProject(ctx, name); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	// Git commit. CommitAll stages everything — the now-absent project dir
	// is recorded as deletions. Route through the commit queue when
	// configured so a failing committer injected by tests can exercise the
	// rollback path; fall back to an inline Manager call otherwise.
	if s.gitAutoCommit {
		msg := fmt.Sprintf("[contextmatrix] %s: project deleted", name)

		var commitErr error
		if s.commitQueue != nil {
			commitErr = <-s.commitQueue.Enqueue(gitops.CommitJob{
				Project: name,
				Kind:    gitops.CommitKindAll,
				Message: msg,
				Ctx:     ctx,
			})
		} else {
			commitErr = s.git.CommitAll(ctx, msg)
		}

		if commitErr != nil {
			return s.rollbackProjectDeleteOnCommitFailure(ctx, name, snapshot, commitErr)
		}

		s.notifyCommit()
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

// rollbackProjectDeleteOnCommitFailure restores a previously-deleted project
// directory from an in-memory snapshot when the git commit fails. After
// writing files back to disk, it asks the store to refresh its index so the
// resurrected project reappears in the cache.
//
// Caller must hold writeMu.
func (s *CardService) rollbackProjectDeleteOnCommitFailure(
	ctx context.Context, name string, snapshot *projectDirSnapshot, commitErr error,
) error {
	if snapshot == nil {
		ctxlog.Logger(ctx).Error("project delete commit failed without snapshot; cache/disk state unknown",
			"project", name, "error", commitErr)

		return fmt.Errorf("git commit (no snapshot for rollback): %w", commitErr)
	}

	projectDir := filepath.Join(s.boardsDir, name)

	if restoreErr := snapshot.restore(projectDir); restoreErr != nil {
		metrics.RollbackFailuresTotal.Inc()
		ctxlog.Logger(ctx).Error("project delete commit failed and restore failed; cache + disk inconsistent",
			"project", name,
			"committed", false,
			"rollback_failed", true,
			"commit_error", commitErr,
			"rollback_error", restoreErr,
		)

		return errors.Join(
			fmt.Errorf("git commit delete (rollback failed, state inconsistent): %w", commitErr),
			fmt.Errorf("rollback restore: %w", restoreErr),
		)
	}

	// Ask the store to re-pick up the restored project. ReloadIndex rebuilds
	// the full cache; acceptable here because delete-then-reload is rare.
	if reloadErr := s.reloadStoreIndex(ctx); reloadErr != nil {
		metrics.RollbackFailuresTotal.Inc()
		ctxlog.Logger(ctx).Error("project delete commit failed and store reload failed after disk restore",
			"project", name,
			"committed", false,
			"rollback_failed", true,
			"commit_error", commitErr,
			"rollback_error", reloadErr,
		)

		return errors.Join(
			fmt.Errorf("git commit delete (rollback reload failed, cache inconsistent): %w", commitErr),
			fmt.Errorf("rollback reload: %w", reloadErr),
		)
	}

	ctxlog.Logger(ctx).Warn("project delete commit failed; restored project tree and reloaded store",
		"project", name,
	)

	return fmt.Errorf("git commit delete: %w", commitErr)
}

// reloadStoreIndex invokes the store's ReloadIndex method when available.
// Used by the project-delete rollback to re-pick up a restored project. The
// storage.Store interface does not declare ReloadIndex, but the concrete
// FilesystemStore does — and that's the only implementation in production
// use. Tests using alternative Store fakes simply skip this step.
func (s *CardService) reloadStoreIndex(ctx context.Context) error {
	type reloader interface {
		ReloadIndex(ctx context.Context) error
	}

	if r, ok := s.store.(reloader); ok {
		return r.ReloadIndex(ctx)
	}

	return nil
}

// projectDirSnapshot captures a project directory tree in memory so it can
// be reconstructed after a failed git commit during DeleteProject. Only
// files and directories are recorded (symlinks are refused upstream by the
// store). Values are small for a zero-card project.
type projectDirSnapshot struct {
	// relPath -> snapshot entry, in discovery order so restore can create
	// parent directories before their children.
	entries []snapshotEntry
}

type snapshotEntry struct {
	relPath string
	isDir   bool
	mode    fs.FileMode
	data    []byte // nil when isDir
}

// snapshotProjectDir walks dir and records every file/directory (excluding
// symlinks) into a projectDirSnapshot. Missing dir yields an empty
// snapshot — the caller can still use it to "restore" a nonexistent dir
// (no-op).
func snapshotProjectDir(dir string) (*projectDirSnapshot, error) {
	snap := &projectDirSnapshot{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) && path == dir {
				// Dir is gone; nothing to snapshot.
				return fs.SkipAll
			}

			return walkErr
		}

		// Skip symlinks defensively — the store rejects them, but be safe.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if d.IsDir() {
			snap.entries = append(snap.entries, snapshotEntry{
				relPath: rel,
				isDir:   true,
				mode:    info.Mode().Perm(),
			})

			return nil
		}

		// Symlinks are rejected above; the path is therefore rooted under
		// the service's own boards directory and not attacker-controlled.
		data, err := os.ReadFile(path) //nolint:gosec // G304/G122: path is WalkDir-derived under our boards dir and symlinks are skipped
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		snap.entries = append(snap.entries, snapshotEntry{
			relPath: rel,
			isDir:   false,
			mode:    info.Mode().Perm(),
			data:    data,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return snap, nil
}

// restore writes the snapshot back out to targetDir, creating directories
// and files with their recorded mode bits. targetDir must not exist or must
// be empty; restore does not attempt to merge.
func (p *projectDirSnapshot) restore(targetDir string) error {
	// Iterate in recorded order; WalkDir yields parents before children so
	// directory creation order is safe.
	for _, e := range p.entries {
		dst := filepath.Join(targetDir, e.relPath)

		if e.isDir {
			mode := e.mode
			if mode == 0 {
				mode = 0o755
			}

			if err := os.MkdirAll(dst, mode); err != nil {
				return fmt.Errorf("mkdir %s: %w", dst, err)
			}

			continue
		}

		// Ensure parent exists (snapshot ordering should guarantee it, but
		// defend against filtered entries).
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir parent of %s: %w", dst, err)
		}

		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}

		if err := os.WriteFile(dst, e.data, mode); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}

	return nil
}

// copyProjectConfig deep-copies a ProjectConfig so a pre-mutation snapshot
// is safe to hand to rollback code. Pointer and map/slice fields are cloned.
func copyProjectConfig(cfg *board.ProjectConfig) *board.ProjectConfig {
	if cfg == nil {
		return nil
	}

	cp := *cfg

	if cfg.States != nil {
		cp.States = slices.Clone(cfg.States)
	}

	if cfg.Types != nil {
		cp.Types = slices.Clone(cfg.Types)
	}

	if cfg.Priorities != nil {
		cp.Priorities = slices.Clone(cfg.Priorities)
	}

	if cfg.Transitions != nil {
		cp.Transitions = make(map[string][]string, len(cfg.Transitions))
		for k, v := range cfg.Transitions {
			cp.Transitions[k] = slices.Clone(v)
		}
	}

	if cfg.RemoteExecution != nil {
		re := *cfg.RemoteExecution
		if cfg.RemoteExecution.Enabled != nil {
			v := *cfg.RemoteExecution.Enabled
			re.Enabled = &v
		}

		cp.RemoteExecution = &re
	}

	if cfg.GitHub != nil {
		gh := *cfg.GitHub
		if cfg.GitHub.Labels != nil {
			gh.Labels = slices.Clone(cfg.GitHub.Labels)
		}

		cp.GitHub = &gh
	}

	if cfg.Templates != nil {
		cp.Templates = make(map[string]string, len(cfg.Templates))
		for k, v := range cfg.Templates {
			cp.Templates[k] = v
		}
	}

	return &cp
}

// toSet converts a slice to a set for membership checks.
func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}

	return set
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
