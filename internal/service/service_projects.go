package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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
	DisplayName string
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
	GitHub      *board.GitHubImportConfig
	// GitHubCredential uses pointer-presence semantics (matches GitHub above):
	//   nil pointer   - preserve the existing value
	//   non-nil ""    - clear the binding (fall back to the instance credential)
	//   non-nil value - set the binding to this pool entry name
	GitHubCredential *string
	// DefaultSkills uses wholesale-PUT semantics (replaces existing):
	//   nil pointer       - clear (mount the full task-skills set)
	//   non-nil empty     - mount no skills
	//   non-nil populated - constrain to listed skills
	DefaultSkills *[]string
	// RemoteExecution uses field-level merge semantics (unlike DefaultSkills'
	// wholesale replace):
	//   nil pointer   - preserve the existing remote_execution config
	//   non-nil       - merge each set subfield into the existing config
	RemoteExecution *RemoteExecutionUpdate
	// Verify uses replace-whole-struct semantics:
	//   nil pointer   - preserve the existing verify config
	//   non-nil       - replace it wholesale, then normalize (zero value → nil)
	Verify *board.VerifyConfig
}

// RemoteExecutionUpdate carries per-field edits to a project's remote-execution
// config. Each pointer is applied independently: nil leaves the subfield
// untouched; non-nil sets it. A non-nil WorkerImage of "" clears the image.
// A non-nil ChatWorkerImage of "" clears the chat image.
type RemoteExecutionUpdate struct {
	WorkerImage     *string
	ChatWorkerImage *string
}

// validWorkerImage is a hygiene-only screen for a per-project worker image
// reference: it must start with an alphanumeric and contain only characters
// that appear in OCI image references. Exact registry/tag/digest grammar is
// left to the container runtime. Empty passes (it means "clear the image").
var validWorkerImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`)

// maxWorkerImageLen caps the worker image reference length before it reaches
// .board.yaml. Hygiene only - well above any real image reference.
const maxWorkerImageLen = 512

// validProjectName matches safe directory names: alphanumeric, hyphens, underscores.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// nonAlphanumRun matches one or more characters that are not letters or digits.
var nonAlphanumRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyDisplayName converts a human-readable display name into a URL/filesystem-safe slug.
// Lowercase, collapses runs of non-alphanumeric characters to a single hyphen,
// strips leading and trailing hyphens.
func slugifyDisplayName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumRun.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	return s
}

func (s *CardService) ListProjects(ctx context.Context) ([]board.ProjectConfig, error) {
	return s.store.ListProjects(ctx)
}

func (s *CardService) GetProject(ctx context.Context, name string) (*board.ProjectConfig, error) {
	return s.store.GetProject(ctx, name)
}

// CreateProject creates a new project with directory structure and .board.yaml.
// On a shared board it runs inside a sync cycle so a name a peer took first is
// seen before the directory is written.
func (s *CardService) CreateProject(ctx context.Context, input CreateProjectInput) (*board.ProjectConfig, error) {
	if s.pushVerified() {
		return s.createProjectVerified(ctx, input)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cfg, err := s.createProjectLocked(ctx, input, func(ctx context.Context, name string) error {
		return s.git.CommitAll(ctx, projectCommitMessage(name, "created"))
	})
	if err != nil {
		return nil, err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectCreated,
		Project:   cfg.Name,
		Timestamp: s.clk.Now(),
	})

	return cfg, nil
}

// projectCommitMessage formats a project-level commit message.
func projectCommitMessage(name, action string) string {
	return fmt.Sprintf("[contextmatrix] %s: project %s", name, action)
}

// createProjectLocked is CreateProject with writeMu held by the caller and the
// commit path chosen by it. commit is handed the resolved project name, which
// the slug derivation above may have filled in. It does not publish; the
// caller does.
func (s *CardService) createProjectLocked(
	ctx context.Context, input CreateProjectInput, commit func(ctx context.Context, name string) error,
) (*board.ProjectConfig, error) {
	// Auto-derive slug from DisplayName when Name is not provided.
	if input.Name == "" && input.DisplayName != "" {
		input.Name = slugifyDisplayName(input.DisplayName)
	}

	// "playbooks" is the reserved top-level directory for cross-project
	// playbooks; a project with that name would collide with it on disk.
	if strings.EqualFold(input.Name, "playbooks") {
		return nil, fmt.Errorf("%w: %q is a reserved name", storage.ErrInvalidInput, input.Name)
	}

	if !validProjectName.MatchString(input.Name) {
		return nil, fmt.Errorf("invalid project name %q: must be alphanumeric with hyphens/underscores: %w", input.Name, board.ErrInvalidProjectConfig)
	}

	_, err := s.store.GetProject(ctx, input.Name)
	if err == nil {
		return nil, fmt.Errorf("project %q: %w", input.Name, storage.ErrProjectExists)
	}

	cfg := &board.ProjectConfig{
		Name:        input.Name,
		DisplayName: input.DisplayName,
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

	tasksDir := filepath.Join(s.boardsDir, input.Name, "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tasks directory: %w", err)
	}

	// Git commit. CommitAll is not routed through the commit queue because
	// its path is "." (stage everything), which the queue would have to
	// special-case; for project-level events that fire at most once per
	// project-lifecycle, serializing via the manager's own mutex is fine.
	if s.gitAutoCommit {
		if err := commit(ctx, input.Name); err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}

		s.notifyCommit()
	}

	s.mu.Lock()
	s.configs[input.Name] = cfg
	s.mu.Unlock()

	return cfg, nil
}

// createProjectVerified writes the project inside a sync cycle. The undo only
// fires when the .board.yaml on disk is still byte-for-byte what the write
// produced, so a merge that adopted a peer's project of the same name is left
// alone.
func (s *CardService) createProjectVerified(ctx context.Context, input CreateProjectInput) (*board.ProjectConfig, error) {
	var (
		cfg     *board.ProjectConfig
		written []byte
	)

	_, err := s.runVerified(ctx, "create project",
		func(ctx context.Context) error {
			c, err := s.createProjectLocked(ctx, input, func(ctx context.Context, name string) error {
				return s.commitAllReloaded(projectCommitMessage(name, "created"))(ctx)
			})
			if err != nil {
				return err
			}

			cfg = c
			written, _ = os.ReadFile(filepath.Join(s.boardsDir, c.Name, ".board.yaml"))

			return nil
		},
		func(ctx context.Context) error {
			if cfg == nil {
				return nil
			}

			cur, err := os.ReadFile(filepath.Join(s.boardsDir, cfg.Name, ".board.yaml"))
			if err != nil || !bytes.Equal(cur, written) {
				return nil // gone, or no longer what we wrote
			}

			if err := s.store.DeleteProject(ctx, cfg.Name); err != nil {
				return fmt.Errorf("delete project: %w", err)
			}

			s.mu.Lock()
			delete(s.configs, cfg.Name)
			s.mu.Unlock()

			return s.commitAllReloaded(projectCommitMessage(cfg.Name, "create undone: remote unreachable"))(ctx)
		})
	if err != nil {
		return nil, err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectCreated,
		Project:   cfg.Name,
		Timestamp: s.clk.Now(),
	})

	return cfg, nil
}

// UpdateProject updates a project's mutable configuration.
// Rejects removal of states, types, or priorities currently in use by cards.
// On a shared board it runs inside a sync cycle so the config the caller edits
// is the merged one.
func (s *CardService) UpdateProject(ctx context.Context, name string, input UpdateProjectInput) (*board.ProjectConfig, error) {
	if s.pushVerified() {
		return s.updateProjectVerified(ctx, name, input)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cfg, _, err := s.updateProjectLocked(ctx, name, input, func(ctx context.Context) error {
		return s.commitQueuedProjectConfig(ctx, name)
	})
	if err != nil {
		return nil, err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectUpdated,
		Project:   name,
		Timestamp: s.clk.Now(),
	})

	return cfg, nil
}

// commitQueuedProjectConfig commits a project's .board.yaml through the commit
// queue when configured, so ordering is preserved with concurrent card
// commits; otherwise it commits inline.
func (s *CardService) commitQueuedProjectConfig(ctx context.Context, name string) error {
	path := filepath.Join(name, ".board.yaml")
	msg := projectCommitMessage(name, "updated")

	if s.commitQueue != nil {
		return <-s.commitQueue.Enqueue(gitops.CommitJob{
			Project: name,
			Kind:    gitops.CommitKindFile,
			Path:    path,
			Message: msg,
			Ctx:     ctx,
		})
	}

	return s.git.CommitFile(ctx, path, msg)
}

// updateProjectLocked is UpdateProject with writeMu held by the caller and the
// commit path chosen by it. It returns the pre-update snapshot alongside the
// new config, and does not publish; the caller does.
func (s *CardService) updateProjectLocked(
	ctx context.Context, name string, input UpdateProjectInput, commit func(ctx context.Context) error,
) (updated, snapshot *board.ProjectConfig, err error) {
	cfg, err := s.store.GetProject(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("get project: %w", err)
	}

	// Deep-copy pre-update config so a failed git commit can restore the
	// store to its prior on-disk + cached state.
	snapshot = copyProjectConfig(cfg)

	// Check for in-use values that would be removed
	cards, err := s.store.ListCards(ctx, name, storage.CardFilter{})
	if err != nil {
		return nil, nil, fmt.Errorf("list cards: %w", err)
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
				return nil, nil, fmt.Errorf("cannot remove state %q: in use by cards: %w", state, board.ErrInvalidProjectConfig)
			}
		}

		newTypes := toSet(input.Types)

		for typ := range usedTypes {
			// Skip built-in subtask type - it's auto-assigned when card has a parent
			if typ == board.SubtaskType {
				continue
			}

			if !newTypes[typ] {
				return nil, nil, fmt.Errorf("cannot remove type %q: in use by cards: %w", typ, board.ErrInvalidProjectConfig)
			}
		}

		newPriorities := toSet(input.Priorities)
		for pri := range usedPriorities {
			if !newPriorities[pri] {
				return nil, nil, fmt.Errorf("cannot remove priority %q: in use by cards: %w", pri, board.ErrInvalidProjectConfig)
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

	if input.GitHubCredential != nil {
		cfg.GitHubCredential = *input.GitHubCredential
	}

	// DefaultSkills follows wholesale PUT semantics (matches cards' PUT
	// handling): nil pointer in the request clears the field, allowing
	// the UI to switch a project from constrained back to "mount full set".
	cfg.DefaultSkills = input.DefaultSkills

	// RemoteExecution merges field-by-field (nil pointer preserves the whole
	// config). Build a fresh struct rather than mutating cfg.RemoteExecution
	// in place: the store's GetProject returns a shallow copy, so that pointer
	// may still be shared with the cached config.
	if input.RemoteExecution != nil {
		re := board.RemoteExecutionConfig{}
		if cfg.RemoteExecution != nil {
			re = *cfg.RemoteExecution
		}

		if input.RemoteExecution.WorkerImage != nil {
			image := strings.TrimSpace(*input.RemoteExecution.WorkerImage)
			if err := validateWorkerImage("worker_image", image); err != nil {
				return nil, nil, err
			}

			re.WorkerImage = image
		}

		if input.RemoteExecution.ChatWorkerImage != nil {
			image := strings.TrimSpace(*input.RemoteExecution.ChatWorkerImage)
			if err := validateWorkerImage("chat_worker_image", image); err != nil {
				return nil, nil, err
			}

			re.ChatWorkerImage = image
		}

		// Normalize: drop a zero-value config so .board.yaml stays clean.
		if remoteExecutionIsZero(&re) {
			cfg.RemoteExecution = nil
		} else {
			cfg.RemoteExecution = &re
		}
	}

	// Verify replaces the whole struct (nil pointer preserves the existing
	// config). Validate then normalize so a zero-value config drops to nil and
	// .board.yaml stays clean.
	if input.Verify != nil {
		if err := validateProjectVerify(input.Verify); err != nil {
			return nil, nil, err
		}

		cfg.Verify = normalizeVerify(input.Verify)
	}

	// SaveProject validates and persists
	if err := s.store.SaveProject(ctx, cfg); err != nil {
		return nil, nil, fmt.Errorf("save project: %w", err)
	}

	if s.gitAutoCommit {
		if commitErr := commit(ctx); commitErr != nil {
			return nil, nil, s.rollbackProjectUpdateOnCommitFailure(ctx, name, snapshot, commitErr)
		}

		s.notifyCommit()
	}

	// Invalidate caches so they rebuild with new config
	s.mu.Lock()
	s.configs[name] = cfg
	s.mu.Unlock()

	return cfg, snapshot, nil
}

// updateProjectVerified edits the project inside a sync cycle. The undo only
// fires when the .board.yaml on disk is still byte-for-byte what the write
// produced, so an edit the merge has already reconciled is left alone.
func (s *CardService) updateProjectVerified(
	ctx context.Context, name string, input UpdateProjectInput,
) (*board.ProjectConfig, error) {
	var (
		cfg      *board.ProjectConfig
		snapshot *board.ProjectConfig
		written  []byte
	)

	configPath := filepath.Join(name, ".board.yaml")

	_, err := s.runVerified(ctx, "update project",
		func(ctx context.Context) error {
			c, snap, err := s.updateProjectLocked(ctx, name, input, func(ctx context.Context) error {
				return s.commitNow(ctx, []string{configPath}, projectCommitMessage(name, "updated"))
			})
			if err != nil {
				return err
			}

			cfg, snapshot = c, snap
			written, _ = os.ReadFile(filepath.Join(s.boardsDir, configPath))

			return nil
		},
		func(ctx context.Context) error {
			if snapshot == nil {
				return nil
			}

			cur, err := os.ReadFile(filepath.Join(s.boardsDir, configPath))
			if err != nil || !bytes.Equal(cur, written) {
				return nil // gone, or no longer what we wrote
			}

			if err := s.store.SaveProject(ctx, snapshot); err != nil {
				return fmt.Errorf("restore project: %w", err)
			}

			s.mu.Lock()
			s.configs[name] = snapshot
			s.mu.Unlock()

			return s.commitNow(ctx, []string{configPath}, projectCommitMessage(name, "update undone: remote unreachable"))
		})
	if err != nil {
		return nil, err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectUpdated,
		Project:   name,
		Timestamp: s.clk.Now(),
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
// cache. This is self-contained - no GitManager API change - and safe for a
// project being deleted because the invariant is zero cards, so the tree is
// small (.board.yaml plus any template files).
//
// On a shared board it runs inside a sync cycle, so a card a peer added is
// seen before the zero-card invariant is trusted.
func (s *CardService) DeleteProject(ctx context.Context, name string) error {
	if s.pushVerified() {
		return s.deleteProjectVerified(ctx, name)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if _, err := s.deleteProjectLocked(ctx, name, func(ctx context.Context) error {
		return s.commitQueuedProjectDelete(ctx, name)
	}); err != nil {
		return err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectDeleted,
		Project:   name,
		Timestamp: s.clk.Now(),
	})

	return nil
}

// commitQueuedProjectDelete stages the project's removal through the commit
// queue when configured so a failing committer injected by tests exercises the
// rollback path; otherwise it commits inline.
func (s *CardService) commitQueuedProjectDelete(ctx context.Context, name string) error {
	msg := projectCommitMessage(name, "deleted")

	if s.commitQueue != nil {
		return <-s.commitQueue.Enqueue(gitops.CommitJob{
			Project: name,
			Kind:    gitops.CommitKindAll,
			Message: msg,
			Ctx:     ctx,
		})
	}

	return s.git.CommitAll(ctx, msg)
}

// deleteProjectLocked is DeleteProject with writeMu held by the caller and the
// commit path chosen by it. It returns the pre-delete directory snapshot and
// does not publish; the caller does.
func (s *CardService) deleteProjectLocked(
	ctx context.Context, name string, commit func(ctx context.Context) error,
) (*projectDirSnapshot, error) {
	// Check exists
	if _, err := s.store.GetProject(ctx, name); err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}

	// Check no cards
	count, err := s.store.ProjectCardCount(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("count cards: %w", err)
	}

	if count > 0 {
		return nil, fmt.Errorf("project %q has %d cards: %w", name, count, storage.ErrProjectHasCards)
	}

	// Snapshot the project directory tree before deletion so we can restore
	// it if the git commit fails. Must run before store.DeleteProject, which
	// is the destructive step.
	snapshot, snapErr := snapshotProjectDir(filepath.Join(s.boardsDir, name))
	if snapErr != nil {
		return nil, fmt.Errorf("snapshot project dir for rollback: %w", snapErr)
	}

	// Delete from store (removes directory and index)
	if err := s.store.DeleteProject(ctx, name); err != nil {
		return nil, fmt.Errorf("delete project: %w", err)
	}

	// Git commit. CommitAll stages everything - the now-absent project dir
	// is recorded as deletions.
	if s.gitAutoCommit {
		if commitErr := commit(ctx); commitErr != nil {
			return nil, s.rollbackProjectDeleteOnCommitFailure(ctx, name, snapshot, commitErr)
		}

		s.notifyCommit()
	}

	// Purge all caches
	s.mu.Lock()
	delete(s.configs, name)
	delete(s.templates, name)
	s.mu.Unlock()

	return snapshot, nil
}

// deleteProjectVerified removes the project inside a sync cycle. The undo only
// restores the tree when the directory is still absent, so a peer's project
// the merge brought back is left alone.
func (s *CardService) deleteProjectVerified(ctx context.Context, name string) error {
	var snapshot *projectDirSnapshot

	projectDir := filepath.Join(s.boardsDir, name)

	_, err := s.runVerified(ctx, "delete project",
		func(ctx context.Context) error {
			snap, err := s.deleteProjectLocked(ctx, name, s.commitAllReloaded(projectCommitMessage(name, "deleted")))
			if err != nil {
				return err
			}

			snapshot = snap

			return nil
		},
		func(ctx context.Context) error {
			if snapshot == nil {
				return nil
			}

			if _, err := os.Stat(projectDir); err == nil {
				return nil // the merge brought a directory back; leave it alone
			}

			if err := snapshot.restore(projectDir); err != nil {
				return fmt.Errorf("restore project dir: %w", err)
			}

			if err := s.reloadStoreIndex(ctx); err != nil {
				return fmt.Errorf("reload store index: %w", err)
			}

			return s.commitAllReloaded(projectCommitMessage(name, "delete undone: remote unreachable"))(ctx)
		})
	if err != nil {
		return err
	}

	s.bus.Publish(events.Event{
		Type:      events.ProjectDeleted,
		Project:   name,
		Timestamp: s.clk.Now(),
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
// FilesystemStore does - and that's the only implementation in production
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
// snapshot - the caller can still use it to "restore" a nonexistent dir
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

		// Skip symlinks defensively - the store rejects them, but be safe.
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
		maps.Copy(cp.Templates, cfg.Templates)
	}

	if cfg.DefaultSkills != nil {
		clone := slices.Clone(*cfg.DefaultSkills)
		cp.DefaultSkills = &clone
	}

	return &cp
}

// validateWorkerImage screens a per-project worker image reference for hygiene
// only (length + allowed characters); exact OCI reference grammar is left to
// the container runtime. Empty is allowed - it clears the image. field names
// the offending key in the error. Wraps ErrInvalidProjectConfig so the API
// layer maps it to 422, matching the other project-config validation failures.
func validateWorkerImage(field, image string) error {
	if image == "" {
		return nil
	}

	if len(image) > maxWorkerImageLen {
		return fmt.Errorf("%w: %s exceeds %d bytes", board.ErrInvalidProjectConfig, field, maxWorkerImageLen)
	}

	if !validWorkerImage.MatchString(image) {
		return fmt.Errorf("%w: %s contains invalid characters", board.ErrInvalidProjectConfig, field)
	}

	return nil
}

// remoteExecutionIsZero reports whether a merged remote-execution config carries
// no operator intent and can be dropped so .board.yaml stays clean.
func remoteExecutionIsZero(re *board.RemoteExecutionConfig) bool {
	return re.WorkerImage == "" && re.ChatWorkerImage == ""
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
