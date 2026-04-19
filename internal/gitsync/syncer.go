// Package gitsync provides automatic git pull/push synchronization for the
// boards repository. It uses shell-based git for all network operations
// (fetch, rebase, push) so that OpenSSH's full auth chain is available.
package gitsync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// SyncStatus reports the current state of the git sync system.
type SyncStatus struct {
	LastSyncTime  *time.Time `json:"last_sync_time"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
	Syncing       bool       `json:"syncing"`
	Enabled       bool       `json:"enabled"`
}

// Syncer manages automatic git pull/push for the boards repository.
type Syncer struct {
	git      *gitops.Manager
	store    *storage.FilesystemStore
	svc      *service.CardService
	bus      *events.Bus
	repoPath string
	interval time.Duration
	autoPull bool
	autoPush bool
	authMode string
	token    string

	mu            sync.RWMutex
	lastSyncTime  time.Time
	lastSyncError string
	syncing       bool

	pushCh chan struct{} // buffered(1), coalesces rapid commits
	wg     sync.WaitGroup

	// pullHook and pushHook are called instead of pullRebase/pushWithRetry
	// when set. Used in tests to inject panics or controlled errors.
	pullHook func(ctx context.Context, trigger string) error
	pushHook func(ctx context.Context) error
}

// NewSyncer creates a new Syncer. Returns nil if the repository has no remote
// configured or the git binary is not found — sync is silently disabled.
// authMode and token configure PAT authentication for shell git operations:
// use "ssh" (or "") to preserve the default environment, or "pat" to inject
// an Authorization Bearer header via GIT_CONFIG_* env vars.
func NewSyncer(
	git *gitops.Manager,
	store *storage.FilesystemStore,
	svc *service.CardService,
	bus *events.Bus,
	repoPath string,
	autoPull bool,
	autoPush bool,
	interval time.Duration,
	authMode string,
	token string,
) *Syncer {
	if !git.HasRemote() {
		slog.Info("git sync disabled: no remote configured")
		return nil
	}

	if _, err := exec.LookPath("git"); err != nil {
		slog.Warn("git sync disabled: git binary not found", "error", err)
		return nil
	}

	return &Syncer{
		git:      git,
		store:    store,
		svc:      svc,
		bus:      bus,
		repoPath: repoPath,
		interval: interval,
		autoPull: autoPull,
		autoPush: autoPush,
		authMode: authMode,
		token:    token,
		pushCh:   make(chan struct{}, 1),
	}
}

// PullOnStartup performs an initial pull+rebase. Errors are returned but
// should not abort startup — the caller decides.
func (s *Syncer) PullOnStartup(ctx context.Context) error {
	return s.pullRebase(ctx, "startup")
}

// Start launches background goroutines for periodic pull and push-after-commit.
// Both goroutines respect context cancellation for clean shutdown.
func (s *Syncer) Start(ctx context.Context) {
	if s.autoPull {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.periodicPull(ctx)
		}()
		slog.Info("git sync: periodic pull started", "interval", s.interval)
	}

	if s.autoPush {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.pushListener(ctx)
		}()
		slog.Info("git sync: push listener started")
	}
}

// Wait blocks until all background goroutines have stopped.
// Call after cancelling the context passed to Start.
func (s *Syncer) Wait() {
	s.wg.Wait()
}

// NotifyCommit signals that a new commit was made and should be pushed.
// Non-blocking: rapid commits are coalesced into a single push.
func (s *Syncer) NotifyCommit() {
	select {
	case s.pushCh <- struct{}{}:
	default:
		// Already queued, will be pushed on next iteration.
	}
}

// TriggerSync performs a manual sync: pull then push (if autoPush enabled).
func (s *Syncer) TriggerSync(ctx context.Context) error {
	if err := s.pullRebase(ctx, "manual"); err != nil {
		return err
	}
	if s.autoPush {
		return s.pushWithRetry(ctx)
	}
	return nil
}

// Status returns the current sync status.
func (s *Syncer) Status() SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := SyncStatus{
		Syncing: s.syncing,
		Enabled: true,
	}
	if !s.lastSyncTime.IsZero() {
		t := s.lastSyncTime
		status.LastSyncTime = &t
	}
	status.LastSyncError = s.lastSyncError
	return status
}

// pullRebase fetches from origin and rebases local commits on top.
// While running, card mutations are blocked via the service write lock.
func (s *Syncer) pullRebase(ctx context.Context, trigger string) error {
	s.setSyncing(true)
	defer s.setSyncing(false)

	start := time.Now()

	s.bus.Publish(events.Event{
		Type:      events.SyncStarted,
		Timestamp: start,
		Data:      map[string]any{"trigger": trigger},
	})

	// Lock writes to prevent mutations during pull+rebase+index rebuild.
	s.svc.LockWrites()
	defer s.svc.UnlockWrites()

	branch, err := s.git.CurrentBranch()
	if err != nil {
		s.setError(err)
		s.publishError(trigger, err)
		return fmt.Errorf("get current branch: %w", err)
	}

	// Fetch from origin.
	if _, err := runGit(ctx, s.repoPath, gitops.GitAuthEnv(s.authMode, s.token), "fetch", "origin"); err != nil {
		s.setError(err)
		s.publishError(trigger, err)
		return fmt.Errorf("git fetch: %w", err)
	}

	// Check if we need to rebase. Compare local HEAD with remote tracking ref.
	remote := "origin/" + branch
	behind, err := s.isBehind(ctx, branch, remote)
	if err != nil {
		// Remote tracking ref may not exist (e.g., first push hasn't happened).
		// This is not an error — just means nothing to pull.
		slog.Debug("git sync: cannot determine if behind", "error", err)
		s.setSuccess()
		s.publishCompleted(trigger, false, time.Since(start))
		return nil
	}

	if !behind {
		slog.Debug("git sync: already up to date")
		s.setSuccess()
		s.publishCompleted(trigger, false, time.Since(start))
		return nil
	}

	// Rebase local commits on top of remote. --autostash stashes any
	// uncommitted changes before the rebase and restores them after, so a
	// dirty worktree does not block the sync.
	authEnv := gitops.GitAuthEnv(s.authMode, s.token)
	if _, err := runGit(ctx, s.repoPath, authEnv, "rebase", "--autostash", remote); err != nil {
		// Rebase conflict — abort and report.
		slog.Error("git sync: rebase conflict, aborting", "error", err)
		_, _ = runGit(ctx, s.repoPath, nil, "rebase", "--abort")
		conflictErr := fmt.Errorf("rebase conflict: %w", err)
		s.setError(conflictErr)

		s.bus.Publish(events.Event{
			Type:      events.SyncConflict,
			Timestamp: time.Now(),
			Data:      map[string]any{"trigger": trigger, "error": conflictErr.Error()},
		})
		return conflictErr
	}

	// Refresh go-git's in-memory repository state after shell rebase so
	// that subsequent go-git read operations see the rebased history.
	if err := s.git.ReloadRepo(); err != nil {
		slog.Warn("git sync: failed to reload go-git repo after rebase", "error", err)
	}

	// Rebuild the in-memory index from disk (files changed by rebase).
	if err := s.store.ReloadIndex(); err != nil {
		s.setError(err)
		s.publishError(trigger, err)
		return fmt.Errorf("reload index after pull: %w", err)
	}

	// Clear cached validators/configs/templates.
	s.svc.ClearCaches()

	slog.Info("git sync: pull completed", "trigger", trigger, "duration", time.Since(start))
	s.setSuccess()
	s.publishCompleted(trigger, true, time.Since(start))
	return nil
}

// pushWithRetry attempts to push. On non-fast-forward failure, it performs a
// pull-rebase then retries once. Never force-pushes.
//
// Each call to git.Push is made while holding the service write lock so that
// push's shell git subprocess cannot race against pullRebase's shell fetch/rebase
// subprocess — both touch the same .git directory and can collide on
// .git/index.lock without this serialization. pullRebase acquires writeMu
// itself, so the lock must be released before calling it to avoid a deadlock.
func (s *Syncer) pushWithRetry(ctx context.Context) error {
	s.svc.LockWrites()
	err := s.git.Push(ctx)
	s.svc.UnlockWrites()
	if err == nil {
		return nil
	}

	// Check if the error is a non-fast-forward rejection.
	errStr := err.Error()
	if !strings.Contains(errStr, "non-fast-forward") && !strings.Contains(errStr, "fetch first") {
		slog.Error("git sync: push failed", "error", err)
		s.setError(err)
		s.publishError("push", err)
		return fmt.Errorf("push: %w", err)
	}

	// pullRebase acquires writeMu itself — must NOT be called under writeMu.
	slog.Info("git sync: push rejected (non-fast-forward), pulling first")
	if err := s.pullRebase(ctx, "push_retry"); err != nil {
		return fmt.Errorf("pull before push retry: %w", err)
	}

	s.svc.LockWrites()
	err = s.git.Push(ctx)
	s.svc.UnlockWrites()
	if err != nil {
		slog.Error("git sync: push failed after rebase", "error", err)
		s.setError(err)
		s.publishError("push", err)
		return fmt.Errorf("push after rebase: %w", err)
	}

	return nil
}

// periodicPull runs fetch+rebase at the configured interval.
func (s *Syncer) periodicPull(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("git sync: periodic pull stopped")
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("git sync: periodic pull panicked", "panic", r, "stack", string(debug.Stack()))
					}
				}()
				pull := s.pullHook
				if pull == nil {
					pull = s.pullRebase
				}
				if err := pull(ctx, "periodic"); err != nil {
					slog.Error("git sync: periodic pull failed", "error", err)
				}
			}()
		}
	}
}

// pushListener waits for commit notifications and pushes.
func (s *Syncer) pushListener(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("git sync: push listener stopped")
			return
		case <-s.pushCh:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("git sync: push listener panicked", "panic", r, "stack", string(debug.Stack()))
					}
				}()
				push := s.pushHook
				if push == nil {
					push = s.pushWithRetry
				}
				if err := push(ctx); err != nil {
					slog.Error("git sync: push failed", "error", err)
				}
			}()
		}
	}
}

// isBehind checks if the local branch is behind the remote tracking ref.
func (s *Syncer) isBehind(ctx context.Context, local, remote string) (bool, error) {
	// Count commits that exist in remote but not in local.
	// rev-list is a local operation (no network), so auth env is not needed.
	out, err := runGit(ctx, s.repoPath, nil, "rev-list", "--count", local+".."+remote)
	if err != nil {
		return false, err
	}
	count := strings.TrimSpace(out)
	return count != "0", nil
}

// setSyncing updates the syncing flag.
func (s *Syncer) setSyncing(syncing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncing = syncing
}

// setSuccess records a successful sync.
func (s *Syncer) setSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSyncTime = time.Now()
	s.lastSyncError = ""
}

// setError records a sync error.
func (s *Syncer) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSyncTime = time.Now()
	s.lastSyncError = err.Error()
}

// publishError emits a sync.error event.
func (s *Syncer) publishError(trigger string, err error) {
	s.bus.Publish(events.Event{
		Type:      events.SyncError,
		Timestamp: time.Now(),
		Data:      map[string]any{"trigger": trigger, "error": err.Error()},
	})
}

// publishCompleted emits a sync.completed event.
func (s *Syncer) publishCompleted(trigger string, changesPulled bool, duration time.Duration) {
	s.bus.Publish(events.Event{
		Type:      events.SyncCompleted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"trigger":        trigger,
			"changes_pulled": changesPulled,
			"duration_ms":    duration.Milliseconds(),
		},
	})
}

// runGit executes a git command in the given directory and returns its output.
// authEnv contains additional environment variables to inject (e.g.
// GIT_CONFIG_* entries for PAT auth). Pass nil to inherit the caller's env.
func runGit(ctx context.Context, dir string, authEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	if len(authEnv) > 0 {
		cmd.Env = append(os.Environ(), authEnv...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("git sync: running", "cmd", "git "+strings.Join(args, " "), "dir", dir)

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		if output == "" {
			output = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s: %s", err, output)
	}

	return stdout.String(), nil
}
