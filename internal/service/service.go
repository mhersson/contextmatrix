// Package service provides the CardService orchestration layer.
// It coordinates storage, git operations, lock management, event publishing,
// and state machine validation for all card mutations.
//
// The package is split across several files along domain axes:
//
//   - service.go           — CardService struct, constructor, lifecycle
//     accessors, TransitionTo orchestrator, HealthCheck.
//   - service_cards.go     — Card CRUD + applyCardMutation driver + helpers.
//   - service_projects.go  — Project CRUD + config/template accessors.
//   - service_locks.go     — Claim/Release/Heartbeat + stall detection.
//   - service_usage.go     — Token usage + cost aggregation/recalculation.
//   - service_runner.go    — Runner lifecycle (push, review attempts, status,
//     promote-to-autonomous).
//   - service_dashboard.go — GetDashboard.
//   - service_transitions.go — Parent auto-transitions + state-change side
//     effects shared between the card write path and TransitionTo.
//   - service_validation.go  — Dependency/reference validators.
//   - service_git.go         — commitCardChange / flushDeferredCommit / cardPath.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/refresh"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	// maxActivityLogEntries is the maximum number of entries kept in a card's activity log.
	// Older entries are dropped but preserved in git history.
	maxActivityLogEntries = 50

	// maxReviewAttempts caps the review_attempts counter as defense-in-depth.
	// The autonomous skill halts at 3 cycles (initial review + 2 rejections).
	// This server-side cap is higher to allow manual overrides while still
	// preventing runaway agents.
	maxReviewAttempts = 5

	// Field length limits to prevent abuse.
	maxTitleLen   = 500
	maxBodyLen    = 512 * 1024 // 512 KB
	maxLabelLen   = 100
	maxLabels     = 50
	maxAgentIDLen = 256
	maxLogMessage = 2000
	maxLogAction  = 200
)

// CardService orchestrates all card operations by coordinating
// storage, git, lock management, events, and validation.
type CardService struct {
	store             storage.Store
	git               *gitops.Manager
	commitQueue       *gitops.CommitQueue
	lock              *lock.Manager
	bus               *events.Bus
	boardsDir         string
	tokenCosts        map[string]ModelCost
	gitAutoCommit     bool
	gitDeferredCommit bool

	// writeMu serializes all card mutations (create, update, patch, delete,
	// claim, release, heartbeat, log). This prevents races like two agents
	// claiming the same card simultaneously. LockWrites / UnlockWrites expose
	// it for the gitsync layer to suspend mutations during pull+rebuild.
	writeMu sync.Mutex

	// deferredPaths tracks card file paths awaiting a deferred commit.
	// Key is card ID; value is the list of relative file paths modified.
	// Protected by writeMu (always held during card mutations).
	deferredPaths map[string][]string

	// onCommit is called after each successful git commit.
	// Used by the sync layer to trigger push-after-commit.
	onCommit func()

	validator *board.Validator

	// clk is the clock driving the timeout-checker ticker. Defaults to
	// clock.Real(); tests inject a fake clock via NewCardServiceWithClock.
	clk clock.Clock

	// sessionManager is optional; when non-nil it is notified of runner lifecycle
	// transitions (running → Start, terminal → Stop) so the per-card SSE buffer
	// stays in sync with actual execution state.
	sessionManager *sessionlog.Manager

	// stalledFn is called on each timeout-checker tick to process stalled cards.
	// Defaults to s.processStalled; overridable in tests to inject panics.
	stalledFn func(ctx context.Context) error

	// validateStalledCardFn validates the post-mutation card on the stall
	// path. Defaults to s.validator.ValidateCard(cfg, card); overridable in
	// tests to inject stricter card-level invariants (e.g. assigned_agent
	// must be empty when state==stalled).
	validateStalledCardFn func(cfg *board.ProjectConfig, card *board.Card) error

	// knowledgeCommitFn is the function called to commit knowledge doc writes.
	// Defaults to s.git.CommitFiles; overridable in tests to inject commit failures.
	knowledgeCommitFn func(ctx context.Context, paths []string, message string) error

	// refreshRegistry, when non-nil, receives a MarkCommitted call on every
	// successful Refresh-source WriteKnowledgeDocs. Optional: nil means no
	// in-flight job tracking (useful for tests and CLI tools that don't run
	// the full server).
	refreshRegistry *refresh.Registry

	// Per-project caches
	mu        sync.RWMutex
	configs   map[string]*board.ProjectConfig
	templates map[string]map[string]string // project -> type -> template
}

// NewCardService creates a new CardService with the given dependencies.
//
// CLOCK COUPLING (IMPORTANT):
//
// The service adopts lockMgr.Clock() as its own time source. This is not a
// cosmetic choice — stall detection, the timeout-checker ticker, and the
// lock manager's stall cutoff all compare timestamps against the same
// monotonic reading. If these subsystems ran on different clocks, a stall
// could be detected by the ticker but not by the lock manager (or vice
// versa) and cards would bounce between states.
//
// WARNING: Tests that mock time MUST construct the lock.Manager with their
// fake clock first — via lock.NewManagerWithClock(fake) — and then pass that
// manager into NewCardService. Passing a real-clock lock.Manager while
// expecting a fake clock elsewhere will silently produce non-deterministic
// timing. There is no type-level guard against this; the inferred-from-
// lockMgr pattern is deliberate to avoid an otherwise redundant parameter.
//
// On init we emit a slog.Debug line recording the clock type adopted so
// misconfigurations show up in logs at startup.
func NewCardService(
	store storage.Store,
	git *gitops.Manager,
	lockMgr *lock.Manager,
	bus *events.Bus,
	boardsDir string,
	tokenCosts map[string]ModelCost,
	gitAutoCommit bool,
	gitDeferredCommit bool,
) *CardService {
	clk := lockMgr.Clock()
	if clk == nil {
		clk = clock.Real()
	}

	slog.Debug("card service: adopting lock manager clock",
		"clock_type", fmt.Sprintf("%T", clk),
	)

	svc := &CardService{
		store:             store,
		git:               git,
		lock:              lockMgr,
		bus:               bus,
		boardsDir:         boardsDir,
		tokenCosts:        tokenCosts,
		gitAutoCommit:     gitAutoCommit,
		gitDeferredCommit: gitDeferredCommit,
		deferredPaths:     make(map[string][]string),
		validator:         board.NewValidator(),
		clk:               clk,
		configs:           make(map[string]*board.ProjectConfig),
		templates:         make(map[string]map[string]string),
	}
	svc.stalledFn = svc.processStalled
	svc.validateStalledCardFn = svc.validator.ValidateCard
	svc.knowledgeCommitFn = svc.git.CommitFiles

	return svc
}

// SetSessionManager registers the session manager used for runner lifecycle
// hooks.  Must be called before the server starts accepting requests.
// Passing nil disables lifecycle notifications.
func (s *CardService) SetSessionManager(m *sessionlog.Manager) {
	s.sessionManager = m
}

// SetCommitQueue registers a commit queue. When set, all write-path commits
// are routed through the queue so writeMu is only held across store writes
// plus job enqueue (not the go-git operation itself). Passing nil reverts to
// direct Manager.Commit* calls. Must be called before the server starts
// accepting requests.
func (s *CardService) SetCommitQueue(q *gitops.CommitQueue) {
	s.commitQueue = q
}

// CommitQueue returns the registered commit queue or nil.
func (s *CardService) CommitQueue() *gitops.CommitQueue {
	return s.commitQueue
}

// SetOnCommit registers a callback invoked after each successful git commit.
func (s *CardService) SetOnCommit(fn func()) {
	s.onCommit = fn
}

// SetRefreshRegistry wires in the in-flight refresh-job registry. Called
// once at server startup; nil-safe (a nil registry disables the
// MarkCommitted side effect on Refresh writes).
func (s *CardService) SetRefreshRegistry(r *refresh.Registry) {
	s.refreshRegistry = r
}

// RefreshRegistry returns the in-flight refresh-job registry. Returns nil
// if not wired (test or CLI contexts).
func (s *CardService) RefreshRegistry() *refresh.Registry {
	return s.refreshRegistry
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
// Exposed for the gitsync layer, which must suspend all writes during
// pull+rebuild to avoid interleaving with a rebase. If a commit queue is
// configured, it is also paused and drained so no async commit subprocess
// races against an external shell rebase/push.
func (s *CardService) LockWrites() {
	s.writeMu.Lock()

	if s.commitQueue != nil {
		s.commitQueue.Pause()
		// Best-effort drain: give in-flight commits a short window to
		// finish so the subsequent shell rebase/push does not collide
		// on .git/index.lock. The lock is already held so new writes
		// cannot enqueue fresh jobs while we wait.
		drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = s.commitQueue.AwaitIdle(drainCtx)

		cancel()
	}
}

// UnlockWrites releases the write mutex and resumes the commit queue.
// Paired with LockWrites.
func (s *CardService) UnlockWrites() {
	if s.commitQueue != nil {
		s.commitQueue.Resume()
	}

	s.writeMu.Unlock()
}

// HeartbeatTimeout returns the configured heartbeat timeout duration.
func (s *CardService) HeartbeatTimeout() time.Duration {
	return s.lock.Timeout()
}

// TransitionTo walks the shortest path of state transitions to reach targetState.
// Each step validates, persists, commits, and publishes an event. Validation,
// the in-memory mutation, the store write, and the commit enqueue all happen
// under writeMu; writeMu is then released before the commit is awaited so a
// slow commit cannot block other concurrent writers. Per-project commit
// ordering is preserved by the commit queue's per-project worker.
// Returns the card in its final state, or an error if any step fails.
func (s *CardService) TransitionTo(ctx context.Context, project, cardID, targetState string) (*board.Card, error) {
	cardID = strings.ToUpper(cardID)

	s.writeMu.Lock()
	unlocked := false

	defer func() {
		if !unlocked {
			s.writeMu.Unlock()
		}
	}()

	card, err := s.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.State == targetState {
		s.enrichDependenciesMet(ctx, card)

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

	for i, state := range path {
		// Re-load the card at the start of every iteration after the
		// first so concurrent writes that landed while writeMu was
		// released for the previous step's commit await are not silently
		// clobbered by our stale in-memory copy.
		if i > 0 {
			card, err = s.store.GetCard(ctx, project, cardID)
			if err != nil {
				return nil, fmt.Errorf("get card: %w", err)
			}
		}

		oldState := card.State

		if err := validator.ValidateTransition(cfg, oldState, state); err != nil {
			return nil, fmt.Errorf("validate transition: %w", err)
		}

		if state == board.StateInProgress {
			met, blockers := s.checkDependencies(ctx, project, card.DependsOn)
			if !met {
				return nil, dependencyError(state, blockers)
			}
		}

		// Snapshot the pre-step card for rollback on commit failure.
		// Earlier successful steps in the path are left committed (they
		// have their own git records); only the failing step rolls back.
		stepSnapshot, err := s.store.GetCard(ctx, project, cardID)
		if err != nil {
			return nil, fmt.Errorf("get card snapshot: %w", err)
		}

		card.State = state
		card.Updated = time.Now()

		appendStateChangeLog(card, oldState, state, "", card.Updated)

		// State-change invariants: release claim on not_planned, clear
		// runner_status on terminal states. Each step in the path is a state
		// change, so pass stateChanged=true.
		enforceTerminalStateInvariants(card, true)

		if err := validator.ValidateCard(cfg, card); err != nil {
			return nil, fmt.Errorf("validate card: %w", err)
		}

		if err := s.store.UpdateCard(ctx, project, card); err != nil {
			return nil, fmt.Errorf("update card: %w", err)
		}

		// Enqueue under writeMu so the per-project worker preserves order
		// with any other write that is racing for the same project.
		commitDone, notify := s.enqueueCardCommit(ctx, project, cardID, "", "transitioned to "+state)

		// Flush deferred commits on not_planned/review under writeMu so
		// the shared deferredPaths map stays serialized; the flush itself
		// is enqueued through the queue so its execution is ordered after
		// the main commit by the per-project worker.
		s.applyStateChangeSideEffects(ctx, card, true)

		// Release writeMu before awaiting so a slow commit does not stall
		// other writers. Per-project worker ordering still guarantees the
		// commit lands in enqueue order.
		s.writeMu.Unlock()

		unlocked = true

		if err := s.awaitCommit(commitDone, notify); err != nil {
			s.writeMu.Lock()
			unlocked = false
			rollbackErr := s.rollbackCardOnCommitFailure(ctx, project, stepSnapshot, err)

			return nil, rollbackErr
		}

		s.bus.Publish(events.Event{
			Type:      events.CardStateChanged,
			Project:   project,
			CardID:    cardID,
			Timestamp: card.Updated,
			Data: map[string]any{
				"old_state": oldState,
				"new_state": state,
			},
		})

		// Re-acquire writeMu for the next iteration (or the post-loop
		// parent auto-transition + dependency enrichment, both of which
		// need writeMu held).
		s.writeMu.Lock()
		unlocked = false
	}

	s.maybeTransitionParent(ctx, card)
	s.enrichDependenciesMet(ctx, card)

	return card, nil
}

// CheckResult holds the outcome of a single health check.
type CheckResult struct {
	Name string
	OK   bool
	Err  error
}

// HealthCheck runs a set of dependency checks and returns one CheckResult per check.
// All checks are always run regardless of individual failures.
func (s *CardService) HealthCheck(ctx context.Context) []CheckResult {
	results := make([]CheckResult, 0, 3)

	// Check 1: store — list projects to verify the filesystem store is accessible.
	_, err := s.store.ListProjects(ctx)
	results = append(results, CheckResult{
		Name: "store",
		OK:   err == nil,
		Err:  err,
	})

	// Check 2: git — verify git manager is configured and the repo is accessible.
	var gitErr error
	if s.git == nil {
		gitErr = fmt.Errorf("git manager not configured")
	} else {
		_, gitErr = s.git.CurrentBranch()
	}

	results = append(results, CheckResult{
		Name: "git",
		OK:   gitErr == nil,
		Err:  gitErr,
	})

	// Check 3: session_log — always ok; nil means runner is disabled (healthy),
	// non-nil means it is operational.
	results = append(results, CheckResult{
		Name: "session_log",
		OK:   true,
	})

	return results
}
