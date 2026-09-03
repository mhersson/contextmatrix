// Package gitsync provides automatic git pull/push synchronization for the
// boards repository. It uses shell-based git for all network operations
// (fetch, rebase, push) so that OpenSSH's full auth chain is available.
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// maxResolutionRecords bounds the in-memory conflict-resolution log the
	// status endpoint exposes.
	maxResolutionRecords = 100

	// defaultSyncTimeout bounds every single network call made inside
	// Synced, so one unreachable remote cannot hold the write locks for the
	// full gitops.NetworkGitTimeout.
	defaultSyncTimeout = 10 * time.Second

	// defaultMaxAttempts bounds the fetch-integrate-push loop in Synced.
	defaultMaxAttempts = 5

	// defaultRetryBackoff is the base delay after a rejected push; the
	// delay grows with the attempt number and carries +-25% jitter.
	defaultRetryBackoff = 250 * time.Millisecond
)

// ErrRemoteUnreachable reports that a network git call inside Synced failed.
// The board is left consistent; the next tick retries.
var ErrRemoteUnreachable = errors.New("remote unreachable")

// ErrSyncContended reports that every push attempt was rejected because
// another instance pushed first. The local commits remain unpushed.
var ErrSyncContended = errors.New("sync contended")

// ResolutionRecord is one merge resolution with the time and trigger that
// produced it, kept for the status endpoint.
type ResolutionRecord struct {
	boardmerge.Resolution

	At      time.Time `json:"at"`
	Trigger string    `json:"trigger"`
}

// SyncStatus reports the current state of the git sync system.
type SyncStatus struct {
	LastSyncTime    *time.Time         `json:"last_sync_time"`
	LastSyncError   string             `json:"last_sync_error,omitempty"`
	Syncing         bool               `json:"syncing"`
	Enabled         bool               `json:"enabled"`
	Shared          bool               `json:"shared"`
	RemoteReachable *bool              `json:"remote_reachable,omitempty"`
	LastRemoteError string             `json:"last_remote_error,omitempty"`
	UnpushedCommits int                `json:"unpushed_commits"`
	Resolutions     []ResolutionRecord `json:"resolutions,omitempty"`
}

// SyncReport summarizes one shared sync cycle.
type SyncReport struct {
	ChangesPulled bool
	Pushed        bool
	Resolutions   []boardmerge.Resolution
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

	// clk drives the periodic-pull ticker. Defaults to clock.Real();
	// tests can inject a fake clock via SetClock before calling Start.
	clk clock.Clock

	// networkTimeout is the timeout for network git operations (fetch).
	// Defaults to gitops.NetworkGitTimeout; tests can inject a shorter
	// value via SetNetworkTimeout.
	networkTimeout time.Duration

	// shared switches the syncer to the merge-based cycle other instances
	// can write against; instance names this instance in merge audits.
	shared   bool
	instance string

	// syncTimeout bounds each network call inside Synced; maxAttempts
	// bounds its fetch-integrate-push loop and retryBackoff scales the
	// delay between its attempts.
	syncTimeout  time.Duration
	maxAttempts  int
	retryBackoff time.Duration

	mu            sync.RWMutex
	lastSyncTime  time.Time
	lastSyncError string
	syncing       bool

	remoteReachable *bool
	lastRemoteError string
	unpushed        int
	resolutions     []ResolutionRecord // ring of the last maxResolutionRecords

	pushCh chan struct{} // buffered(1), coalesces rapid commits
	wg     sync.WaitGroup

	// pullHook and pushHook are called instead of pullRebase/pushWithRetry
	// when set. Used in tests to inject panics or controlled errors.
	pullHook func(ctx context.Context, trigger string) error
	pushHook func(ctx context.Context) error

	// resolveHook resolves the unmerged paths left by a conflicted merge.
	// Nil uses resolveConflicts; tests set it to inject controlled errors.
	resolveHook func(ctx context.Context, branch string, oursChanged []string) ([]boardmerge.Resolution, error)

	// extraWritten holds the files resolveConflicts wrote for re-minted cards
	// while the current merge is unresolved, so a merge that fails after they
	// were written can delete them. Written and read only inside one integrate
	// call, under the write locks Synced holds.
	extraWritten []string

	// prePushHook runs immediately before each push attempt inside Synced.
	// Tests use it to advance the remote so the push is rejected as a
	// non-fast-forward. Never set in production.
	prePushHook func(attempt int)

	// playbooks quiesces playbook writes during pull+rebase and reloads the
	// playbook index afterwards. Nil when the playbooks subsystem is
	// disabled.
	playbooks playbookSync
}

// playbookSync is the slice of PlaybookService the syncer needs: quiesce
// writes during pull+rebase and reload the index afterwards.
type playbookSync interface {
	LockWrites()
	UnlockWrites()
	Reload(ctx context.Context) error
}

// Option configures a Syncer at construction time.
type Option func(*Syncer)

// WithShared marks the board repository as shared with other ContextMatrix
// instances and names this one. Only a syncer built with it may call Synced.
func WithShared(instanceID string) Option {
	return func(s *Syncer) {
		s.shared = true
		s.instance = instanceID
	}
}

// WithSyncTimeout overrides the per-network-call timeout used inside Synced.
func WithSyncTimeout(d time.Duration) Option {
	return func(s *Syncer) {
		s.syncTimeout = d
	}
}

// NewSyncer creates a new Syncer. Returns nil if the repository has no remote
// configured or the git binary is not found - sync is silently disabled.
// Auth credentials are obtained at call time via the Manager's AuthEnv method.
func NewSyncer(
	git *gitops.Manager,
	store *storage.FilesystemStore,
	svc *service.CardService,
	bus *events.Bus,
	repoPath string,
	autoPull bool,
	autoPush bool,
	interval time.Duration,
	opts ...Option,
) *Syncer {
	if !git.HasRemote() {
		slog.Info("git sync disabled: no remote configured")

		return nil
	}

	if _, err := exec.LookPath("git"); err != nil {
		slog.Warn("git sync disabled: git binary not found", "error", err)

		return nil
	}

	s := &Syncer{
		git:            git,
		store:          store,
		svc:            svc,
		bus:            bus,
		repoPath:       repoPath,
		interval:       interval,
		autoPull:       autoPull,
		autoPush:       autoPush,
		clk:            clock.Real(),
		networkTimeout: gitops.NetworkGitTimeout,
		syncTimeout:    defaultSyncTimeout,
		maxAttempts:    defaultMaxAttempts,
		retryBackoff:   defaultRetryBackoff,
		pushCh:         make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SetClock overrides the clock used to drive the periodic-pull ticker.
// Must be called before Start; changing the clock after Start has no effect.
// Used by tests to deterministically fire pull ticks.
func (s *Syncer) SetClock(c clock.Clock) {
	if c == nil {
		c = clock.Real()
	}

	s.clk = c
}

// SetNetworkTimeout overrides the timeout for network git operations
// (fetch). Used by tests to shorten the bound; production code is not
// expected to call it.
func (s *Syncer) SetNetworkTimeout(d time.Duration) {
	s.networkTimeout = d
}

// SetPlaybooks wires the playbook service in. Must be called before Start.
// Nil (subsystem disabled) leaves sync behavior unchanged.
func (s *Syncer) SetPlaybooks(p playbookSync) {
	s.playbooks = p
}

// PullOnStartup performs an initial pull+rebase. Errors are returned but
// should not abort startup - the caller decides.
func (s *Syncer) PullOnStartup(ctx context.Context) error {
	return s.pullRebase(ctx, "startup")
}

// Start launches background goroutines for periodic pull and push-after-commit.
// Both goroutines respect context cancellation for clean shutdown.
func (s *Syncer) Start(ctx context.Context) {
	if s.autoPull {
		s.wg.Go(func() {
			s.periodicPull(ctx)
		})

		slog.Info("git sync: periodic pull started", "interval", s.interval)
	}

	if s.autoPush {
		s.wg.Go(func() {
			s.pushListener(ctx)
		})

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

func (s *Syncer) Status() SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := SyncStatus{
		Syncing:         s.syncing,
		Enabled:         true,
		Shared:          s.shared,
		LastRemoteError: s.lastRemoteError,
		UnpushedCommits: s.unpushed,
	}
	if !s.lastSyncTime.IsZero() {
		t := s.lastSyncTime
		status.LastSyncTime = &t
	}

	if s.remoteReachable != nil {
		reachable := *s.remoteReachable
		status.RemoteReachable = &reachable
	}

	if len(s.resolutions) > 0 {
		status.Resolutions = append([]ResolutionRecord(nil), s.resolutions...)
	}

	status.LastSyncError = s.lastSyncError

	return status
}

// setRemote records the outcome of a network call. A nil err marks the remote
// reachable and clears the last error.
func (s *Syncer) setRemote(reachable bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := reachable
	s.remoteReachable = &v

	if err != nil {
		s.lastRemoteError = err.Error()
	} else {
		s.lastRemoteError = ""
	}
}

func (s *Syncer) setUnpushed(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unpushed = n
}

// recordResolutions appends to the bounded resolution log.
func (s *Syncer) recordResolutions(trigger string, rs []boardmerge.Resolution) {
	if len(rs) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, r := range rs {
		s.resolutions = append(s.resolutions, ResolutionRecord{Resolution: r, At: now, Trigger: trigger})
	}

	if len(s.resolutions) > maxResolutionRecords {
		s.resolutions = s.resolutions[len(s.resolutions)-maxResolutionRecords:]
	}
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

	// The playbook lock MUST be acquired before the card lock. Card
	// LockWrites pauses the shared commit queue, and a playbook mutation
	// holds its write mutex while awaiting its queued commit - queue
	// Pause/AwaitIdle do not drain buffered jobs, so locking in the other
	// order deadlocks against a mutation enqueued around the pause. No
	// ABBA risk: playbook mutations never take the card mutex.
	if s.playbooks != nil {
		s.playbooks.LockWrites()
		defer s.playbooks.UnlockWrites()
	}

	// Lock writes to prevent mutations during pull+rebase+index rebuild.
	s.svc.LockWrites()
	defer s.svc.UnlockWrites()

	branch, err := s.git.CurrentBranch()
	if err != nil {
		s.setError(err)
		s.publishError(trigger, err)

		return fmt.Errorf("get current branch: %w", err)
	}

	// Obtain auth credentials once and reuse for all network operations.
	// A nil-provider error means SSH mode - proceed without injecting env
	// (the SSH agent handles auth). Any other error (e.g. token-mint failure)
	// is logged as a warning so the root cause is visible; the subsequent
	// network call will then fail with the real permission error.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, s.networkTimeout)
	defer fetchCancel()

	authEnv, authErr := s.git.AuthEnv(fetchCtx)
	if authErr != nil {
		slog.Warn("git sync: could not obtain auth env", "error", authErr)
	}

	if _, err := runGit(fetchCtx, s.repoPath, authEnv, "fetch", "origin"); err != nil {
		s.setError(err)
		s.publishError(trigger, err)

		return fmt.Errorf("git fetch: %w", err)
	}

	// Check if we need to rebase. Compare local HEAD with remote tracking ref.
	remote := "origin/" + branch

	behind, err := s.isBehind(ctx, branch, remote)
	if err != nil {
		// Remote tracking ref may not exist (e.g., first push hasn't happened).
		// This is not an error - just means nothing to pull.
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
	if _, err := runGit(ctx, s.repoPath, authEnv, "rebase", "--autostash", remote); err != nil {
		// Rebase conflict - abort and report.
		slog.Error("git sync: rebase conflict, aborting", "error", err)

		if abortErr := runGitAbort(ctx, s.repoPath); abortErr != nil {
			slog.Error("git sync: rebase --abort failed", "error", abortErr)
		}

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
	if err := s.git.ReloadRepo(ctx); err != nil {
		slog.Warn("git sync: failed to reload go-git repo after rebase", "error", err)
	}

	// Rebuild the in-memory index from disk (files changed by rebase).
	if err := s.store.ReloadIndex(ctx); err != nil {
		s.setError(err)
		s.publishError(trigger, err)

		return fmt.Errorf("reload index after pull: %w", err)
	}

	if s.playbooks != nil {
		if err := s.playbooks.Reload(ctx); err != nil {
			wrapped := fmt.Errorf("reload playbooks after pull: %w", err)
			s.setError(wrapped)
			s.publishError(trigger, wrapped)

			return wrapped
		}
	}

	// Clear cached validators/configs/templates.
	s.svc.ClearCaches()

	slog.Info("git sync: pull completed", "trigger", trigger, "duration", time.Since(start))
	s.setSuccess()
	s.publishCompleted(trigger, true, time.Since(start))

	return nil
}

// reNonFastForward matches non-fast-forward rejection messages from GitHub,
// GitLab, Gitea, and other common git hosting services.
var reNonFastForward = regexp.MustCompile(`(?i)(non-fast-forward|fetch first|cannot fast-forward|rejected.*non-fast|updates were rejected)`)

// pushWithRetry attempts to push. On non-fast-forward failure, it performs a
// pull-rebase then retries once. Never force-pushes.
//
// Each call to git.Push is made while holding the service write lock so that
// push's shell git subprocess cannot race against pullRebase's shell fetch/rebase
// subprocess - both touch the same .git directory and can collide on
// .git/index.lock without this serialization. pullRebase acquires writeMu
// itself, so the lock must be released before calling it to avoid a deadlock.
func (s *Syncer) pushWithRetry(ctx context.Context) error {
	s.svc.LockWrites()
	err := s.git.Push(ctx)
	s.svc.UnlockWrites()

	if err == nil {
		return nil
	}

	// Check if the error is a non-fast-forward rejection. Use a broad regex so
	// Gitea / GitLab variants are also caught.
	if !reNonFastForward.MatchString(err.Error()) {
		slog.Error("git sync: push failed", "error", err)
		s.setError(err)
		s.publishError("push", err)

		return fmt.Errorf("push: %w", err)
	}

	// Add ±25% jitter to the retry delay so concurrent CM instances do not
	// hammer the remote in lock-step.
	jitter := time.Duration(float64(500*time.Millisecond) * (0.75 + 0.5*rand.Float64())) //nolint:gosec // non-security jitter

	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		return ctx.Err()
	}

	// pullRebase acquires writeMu itself - must NOT be called under writeMu.
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

// Synced runs one shared-repo cycle: quiesce writes, commit anything dirty,
// fetch, integrate the remote by fast-forward or merge, run body, and push
// what the cycle produced. Only valid on a syncer built WithShared.
//
// body, when non-nil, runs under the write locks after the first integration
// and before the first push attempt, at most once per cycle: a push rejected
// as non-fast-forward re-integrates but does not re-run it. It must never
// touch the network.
//
// What body may do is narrower than it looks. Both write locks are held, and
// on a shared repository LockWrites drains the commit queue and leaves it
// paused for the whole cycle. A body that enqueues a commit and waits for it,
// or that calls any CardService write method, therefore blocks until
// UnlockWrites resumes the queue, which only happens after body returns. That
// is an unrecoverable deadlock with no timeout. Write files directly and
// commit them with gitops.Manager.CommitFilesShell instead.
//
// The repository is never left mid-merge: every failure after a merge started
// aborts it and returns an error, a merge left behind by an earlier cycle is
// aborted before this one touches the tree, and the next cycle retries from a
// clean tree.
func (s *Syncer) Synced(ctx context.Context, trigger string, body func(ctx context.Context) error) (SyncReport, error) {
	if !s.shared {
		return SyncReport{}, errors.New("shared sync requires a syncer built with WithShared")
	}

	s.setSyncing(true)
	defer s.setSyncing(false)

	start := time.Now()

	s.bus.Publish(events.Event{
		Type:      events.SyncStarted,
		Timestamp: start,
		Data:      map[string]any{"trigger": trigger},
	})

	// The playbook lock MUST be acquired before the card lock; see the same
	// comment in pullRebase for why the reverse order deadlocks.
	if s.playbooks != nil {
		s.playbooks.LockWrites()
		defer s.playbooks.UnlockWrites()
	}

	// Held across the whole cycle, body included, so no mutation lands
	// between the merge and the push it is meant to be part of.
	s.svc.LockWrites()
	defer s.svc.UnlockWrites()

	report, err := s.synced(ctx, trigger, body)
	if err != nil {
		s.setError(err)
		s.publishError(trigger, err)

		return report, err
	}

	s.recordResolutions(trigger, report.Resolutions)
	s.setSuccess()
	s.publishCompleted(trigger, report.ChangesPulled, time.Since(start))

	return report, nil
}

// synced is the body of Synced, run with both write locks held.
func (s *Syncer) synced(ctx context.Context, trigger string, body func(ctx context.Context) error) (SyncReport, error) {
	var report SyncReport

	// Must precede commitLeftovers: in a conflicted worktree the leftovers
	// are the conflict markers, and staging them concludes the merge.
	if err := s.clearStaleMerge(ctx); err != nil {
		return report, err
	}

	if err := s.commitLeftovers(ctx); err != nil {
		return report, err
	}

	branch, err := s.git.CurrentBranch()
	if err != nil {
		return report, fmt.Errorf("current branch: %w", err)
	}

	bodyRan := false

	for attempt := range s.maxAttempts {
		if err := s.fetch(ctx); err != nil {
			return report, err
		}

		pulled, res, err := s.integrate(ctx, trigger, branch)
		if err != nil {
			return report, err
		}

		report.ChangesPulled = report.ChangesPulled || pulled
		report.Resolutions = append(report.Resolutions, res...)

		if body != nil && !bodyRan {
			bodyRan = true

			if err := body(ctx); err != nil {
				return report, fmt.Errorf("sync body: %w", err)
			}
		}

		ahead, err := s.git.AheadCount(ctx, branch)
		if err != nil {
			return report, err
		}

		s.setUnpushed(ahead)

		if ahead == 0 {
			return report, nil
		}

		if s.prePushHook != nil {
			s.prePushHook(attempt)
		}

		pushCtx, cancel := context.WithTimeout(ctx, s.syncTimeout)
		pushErr := s.git.Push(pushCtx)

		cancel()

		if pushErr == nil {
			report.Pushed = true

			s.setUnpushed(0)
			s.setRemote(true, nil)

			return report, nil
		}

		// Anything but a non-fast-forward rejection is a network or auth
		// failure: the local history is fine, so report and let the next
		// cycle retry.
		if !reNonFastForward.MatchString(pushErr.Error()) {
			s.setRemote(false, pushErr)

			return report, fmt.Errorf("%w: push: %w", ErrRemoteUnreachable, pushErr)
		}

		slog.Info("git sync: push rejected, re-integrating", "attempt", attempt+1, "trigger", trigger)
		s.sleepJitter(ctx, time.Duration(attempt+1)*s.retryBackoff)

		// sleepJitter also returns on cancellation; report that as such
		// rather than letting the next fetch fail as an unreachable remote.
		if err := ctx.Err(); err != nil {
			return report, err
		}
	}

	return report, fmt.Errorf("%w: push rejected %d times", ErrSyncContended, s.maxAttempts)
}

// fetch updates the remote tracking refs under the per-call timeout.
func (s *Syncer) fetch(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(ctx, s.syncTimeout)
	defer cancel()

	// A nil-provider error means SSH mode - proceed without injecting env.
	authEnv, authErr := s.git.AuthEnv(fetchCtx)
	if authErr != nil {
		slog.Debug("git sync: no auth env", "error", authErr)
	}

	if _, err := runGit(fetchCtx, s.repoPath, authEnv, "fetch", "origin"); err != nil {
		s.setRemote(false, err)

		return fmt.Errorf("%w: fetch: %w", ErrRemoteUnreachable, err)
	}

	s.setRemote(true, nil)

	return nil
}

// clearStaleMerge undoes a merge an earlier cycle left in progress, which a
// crash or a container restart between Merge and CommitMerge produces. It has
// to run before anything else touches the tree: in a conflicted worktree
// git status reports the unmerged paths as ordinary dirty files, so
// commitLeftovers would stage the conflict markers, and because MERGE_HEAD is
// present the commit would conclude the merge and push marker-laden files to
// every other instance.
//
// Residual dirtiness after the abort is left to commitLeftovers, which exists
// for exactly that. What must not survive is merge state, so that is what is
// verified.
func (s *Syncer) clearStaleMerge(ctx context.Context) error {
	if !s.git.MergeInProgress() {
		return nil
	}

	slog.Warn("git sync: merge left in progress by an earlier cycle, aborting it")

	if err := s.git.MergeAbort(ctx); err != nil {
		return fmt.Errorf("abort stale merge: %w", err)
	}

	left, err := s.git.UnmergedPaths(ctx)
	if err != nil {
		return fmt.Errorf("verify stale merge abort: %w", err)
	}

	if len(left) > 0 || s.git.MergeInProgress() {
		return fmt.Errorf("stale merge still in progress after abort, %d unmerged paths", len(left))
	}

	return nil
}

// commitLeftovers commits anything dirty so nothing is ever stashed. A shared
// repository is merged, not rebased, and a merge needs a clean tree.
func (s *Syncer) commitLeftovers(ctx context.Context) error {
	clean, paths, err := s.git.IsClean(ctx)
	if err != nil {
		return fmt.Errorf("status before sync: %w", err)
	}

	if clean {
		return nil
	}

	slog.Warn("git sync: committing uncommitted changes before sync", "paths", paths)

	if err := s.git.CommitFilesShell(ctx, paths, "external edit"); err != nil {
		return fmt.Errorf("commit leftovers: %w", err)
	}

	return nil
}

// integrate brings the remote branch into HEAD, by fast-forward when the local
// branch has not diverged and by a merge commit when it has. It reports
// whether anything was pulled and which conflicts were resolved.
func (s *Syncer) integrate(ctx context.Context, trigger, branch string) (bool, []boardmerge.Resolution, error) {
	remote := "origin/" + branch

	behind, err := s.isBehind(ctx, branch, remote)
	if err != nil {
		// isBehind shells out to rev-list, which fails both when a ref is
		// missing and when the object store is broken. A branch that has
		// never been pushed is the one benign case - nothing to pull - so
		// only that one is swallowed.
		if s.hasRef(ctx, remote) {
			return false, nil, fmt.Errorf("compare with %s: %w", remote, err)
		}

		slog.Debug("git sync: no remote tracking ref yet", "remote", remote)

		return false, nil, nil
	}

	if !behind {
		return false, nil, nil
	}

	ffErr := s.git.MergeFastForward(ctx, remote)
	if ffErr == nil {
		return true, nil, s.reloadAfterPull(ctx)
	}

	// A refused fast-forward is the ordinary divergent case, so it is not an
	// error here - but log it, because a dirty tree or a broken ref reaches
	// this line the same way and the merge below reports it far less
	// specifically.
	slog.Debug("git sync: fast-forward refused, merging", "remote", remote, "error", ffErr)

	// Capture what our side changed since the fork point before the merge
	// overwrites the worktree.
	mergeBase, err := s.git.MergeBase(ctx, remote)
	if err != nil {
		return false, nil, err
	}

	oursChanged, err := s.git.DiffNames(ctx, mergeBase, "HEAD")
	if err != nil {
		return false, nil, err
	}

	mergeErr := s.git.Merge(ctx, remote)
	if mergeErr == nil {
		return true, nil, s.reloadAfterPull(ctx)
	}

	if !errors.Is(mergeErr, gitops.ErrMergeConflict) {
		return false, nil, s.abortAndWrap(ctx, fmt.Errorf("merge: %w", mergeErr))
	}

	resolve := s.resolveHook
	if resolve == nil {
		resolve = s.resolveConflicts
	}

	s.extraWritten = nil

	res, err := resolve(ctx, branch, oursChanged)
	if err != nil {
		return false, nil, s.abortAndWrap(ctx, fmt.Errorf("resolve conflicts: %w", err))
	}

	msg := fmt.Sprintf("merge: %s (%d conflicts resolved)", remote, len(res))
	if err := s.git.CommitMerge(ctx, msg); err != nil {
		return false, nil, s.abortAndWrap(ctx, err)
	}

	// The re-minted files are part of the merge commit now, so a later abort
	// must not treat them as leftovers to delete.
	s.extraWritten = nil

	left, err := s.git.UnmergedPaths(ctx)
	if err != nil {
		return false, nil, s.abortAndWrap(ctx, fmt.Errorf("verify merge result: %w", err))
	}

	if len(left) > 0 || s.git.MergeInProgress() {
		return false, nil, s.abortAndWrap(ctx, errors.New("merge left unmerged paths"))
	}

	s.bus.Publish(events.Event{
		Type:      events.SyncConflict,
		Timestamp: time.Now(),
		Data:      map[string]any{"resolved": len(res), "trigger": trigger},
	})

	return true, res, s.reloadAfterPull(ctx)
}

// abortAndWrap returns the worktree to HEAD and hands back cause. When the
// abort itself fails, the returned error also says the repository is still
// merging, so the sync.error event and the status field tell an operator that
// manual intervention is needed rather than reporting a routine conflict.
//
// Files written for re-minted cards are deleted here as well. Git's abort
// removes the ones it had staged, so this is the backstop for any it does not:
// left behind, the next cycle commits one as an external edit and the merge
// after that re-mints the same card a second time.
func (s *Syncer) abortAndWrap(ctx context.Context, cause error) error {
	if err := s.abortMerge(ctx); err != nil {
		return fmt.Errorf("%w; repository still merging: %w", cause, err)
	}

	s.removeExtras()

	return cause
}

// abortMerge returns the worktree to HEAD when a merge is in progress.
func (s *Syncer) abortMerge(ctx context.Context) error {
	if !s.git.MergeInProgress() {
		return nil
	}

	if err := s.git.MergeAbort(ctx); err != nil {
		slog.Error("git sync: merge --abort failed", "error", err)

		return err
	}

	return nil
}

// reloadAfterPull refreshes every in-memory view of the worktree that the
// integration just changed. Order matters: go-git first, then the card index
// and the playbook index, then the service caches.
func (s *Syncer) reloadAfterPull(ctx context.Context) error {
	if err := s.git.ReloadRepo(ctx); err != nil {
		slog.Warn("git sync: reload go-git repo", "error", err)
	}

	if err := s.store.ReloadIndex(ctx); err != nil {
		return fmt.Errorf("reload index: %w", err)
	}

	if s.playbooks != nil {
		if err := s.playbooks.Reload(ctx); err != nil {
			return fmt.Errorf("reload playbooks: %w", err)
		}
	}

	s.svc.ClearCaches()

	return nil
}

// hasRef reports whether ref resolves to a commit in the local object store.
func (s *Syncer) hasRef(ctx context.Context, ref string) bool {
	_, err := runGit(ctx, s.repoPath, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")

	return err == nil
}

// sleepJitter waits base with +-25% jitter so concurrent instances do not
// retry in lock-step.
func (s *Syncer) sleepJitter(ctx context.Context, base time.Duration) {
	d := time.Duration(float64(base) * (0.75 + 0.5*rand.Float64())) //nolint:gosec // non-security jitter

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// periodicPull runs fetch+rebase at the configured interval.
func (s *Syncer) periodicPull(ctx context.Context) {
	ticker := s.clk.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("git sync: periodic pull stopped")

			return
		case <-ticker.C():
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

func (s *Syncer) setSyncing(syncing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.syncing = syncing
}

func (s *Syncer) setSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastSyncTime = time.Now()
	s.lastSyncError = ""
}

func (s *Syncer) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastSyncTime = time.Now()

	if msg := err.Error(); msg != "" {
		s.lastSyncError = msg
	} else {
		s.lastSyncError = "unknown error"
	}
}

// runGitAbort runs "git rebase --abort" and returns any error.
// A separate function keeps the call site clean and makes logging uniform.
func runGitAbort(ctx context.Context, repoPath string) error {
	_, err := runGit(ctx, repoPath, nil, "rebase", "--abort")

	return err
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
	// Under `go test`, prepend one-shot config that disables GPG signing so the
	// suite is hermetic regardless of the developer's global commit.gpgsign /
	// tag.gpgsign settings (a working gpg-agent is not guaranteed in test
	// environments). Production builds skip this branch.
	if testing.Testing() {
		args = append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.WaitDelay = 3 * time.Second

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

		return "", fmt.Errorf("git %s: %w (%s)", args[0], err, output)
	}

	return stdout.String(), nil
}
