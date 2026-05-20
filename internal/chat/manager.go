package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"github.com/mhersson/contextmatrix/internal/chat/transcript"
	"github.com/mhersson/contextmatrix/internal/clock"
)

// ErrTooManyConcurrent is returned by OpenSession when the number of active
// or warm-idle sessions has reached the configured MaxConcurrent ceiling.
var ErrTooManyConcurrent = errors.New("chat: too many concurrent sessions")

// ErrRunnerSend is the sentinel wrapped around runner.SendChatMessage
// failures inside ClearContext. The API layer matches on errors.Is to
// route these to a 502 RUNNER_UNAVAILABLE rather than a 500 INTERNAL_ERROR,
// since the runtime cause is "the runner is unreachable", not a bug.
var ErrRunnerSend = errors.New("chat: runner send failed")

// ErrSessionNotRunning is returned by ClearContext when the target session is
// not in an active or warm-idle state (i.e. the runner container is not
// running). Clearing a cold or ending session has no runner to talk to.
// The API layer maps this to 409 RUNNER_NOT_RUNNING.
var ErrSessionNotRunning = errors.New("chat: session is not running")

// ErrRunnerSendPrimer is wrapped around primer-send failures inside
// ClearContext, in addition to the general ErrRunnerSend sentinel. The API
// layer uses errors.Is(err, ErrRunnerSendPrimer) to distinguish a "primer
// send failed after /clear succeeded" case (detail: "primer_failed") from a
// plain "/clear send failure" (detail: "clear_failed"). Both cases are still
// 502 RUNNER_UNAVAILABLE; the detail string is the differentiator.
var ErrRunnerSendPrimer = errors.New("chat: primer send failed after /clear succeeded")

// ContextClearedMarker is the canonical content string written to the
// system-role transcript row appended on Clear Context. The frontend uses
// this in conjunction with the persisted kind ("divider") to render a
// horizontal rule rather than a normal system message.
const ContextClearedMarker = "Context cleared"

// EventKindDivider is the persisted Message.Kind / SSE DataKind value that
// marks a structural divider in the transcript (currently used only for the
// Clear Context sentinel). Empty kind means "regular message".
const EventKindDivider = "divider"

// RunnerClient is the subset of the runner webhook surface that
// chat.Manager uses. The real implementation lives in internal/chat/runner.go;
// tests inject stubs.
type RunnerClient interface {
	StartChat(ctx context.Context, opts StartChatOpts) (containerID string, err error)
	EndChat(ctx context.Context, sessionID string) error
	SendChatMessage(ctx context.Context, sessionID, content, messageID string) error
	// StreamLogs opens a long-lived SSE subscription to the runner's
	// /logs?session_id=<id> endpoint and invokes onEntry for each parsed
	// LogEntry. Returns when ctx is cancelled or the stream closes.
	StreamLogs(ctx context.Context, sessionID string, onEntry func(LogEntry)) error
}

// StartChatOpts carries every input to RunnerClient.StartChat. Bundling the
// arguments lets us add fields (Model, Resume) without breaking the wire
// for tests with bespoke fakes.
type StartChatOpts struct {
	SessionID string
	Project   string
	RepoURL   string
	Model     string
	Resume    *ResumeContext
	// Primer is the chat-mode orientation text written to the container's
	// stdin as a stream-json user envelope before any rehydration priming.
	// Empty string means "no primer" — runner skips the write. Sourced
	// from workflow-skills/chat-mode.md on each cold open.
	Primer string
}

// Config carries Manager dependencies.
type Config struct {
	Store   Store
	Runner  RunnerClient
	Clock   clock.Clock
	IdleTTL time.Duration
	Logger  *slog.Logger
	// MaxConcurrent is the maximum number of sessions that may be active or
	// warm-idle at the same time. Zero means unlimited.
	MaxConcurrent int
	// Hub is the per-session SSE fan-out. When non-nil, SendUserMessage
	// publishes a user echo so the originator sees their own message in
	// the transcript without depending on a runner-side log round-trip.
	Hub *SSEHub
	// ResolveRepoURL returns the repo URL for a project, or "" if the
	// project has no repo. Caller wires this from service.CardService.GetProject.
	ResolveRepoURL func(ctx context.Context, project string) (string, error)

	// ResumeBudgetTokens caps the rehydration payload size passed to
	// transcript.Build on cold-reopen. Zero falls back to
	// transcript.DefaultBudgetTokens.
	ResumeBudgetTokens int

	// RehydrationTimeout is the upper bound on how long a session may
	// remain in the rehydration phase before the reaper forces it off.
	// Zero means "do not force off by timer" (user-message and
	// chat_rehydration_complete remain the only end signals). Production
	// wires this to chat.rehydration_timeout from config.
	RehydrationTimeout time.Duration

	// DefaultModel is used when a session row's model column is empty
	// (legacy rows, or callers that didn't pass a model on creation).
	// Production wires this to chat.default_model from config.
	DefaultModel string

	// PrimerPath is the on-disk path to the chat-mode orientation primer
	// file (typically <WorkflowSkillsDir>/chat-mode.md). Read on each cold
	// open and shipped to the runner as StartChatOpts.Primer. Empty path
	// or a missing/unreadable file is non-fatal: cold open proceeds with
	// an empty primer and a WARN log.
	PrimerPath string
}

// Manager orchestrates chat session lifecycle, transcript persistence,
// and runner-client coordination.
type Manager struct {
	store              Store
	runner             RunnerClient
	clk                clock.Clock
	idleTTL            time.Duration
	maxConcurrent      int
	logger             *slog.Logger
	hub                *SSEHub
	resolveRepoURL     func(ctx context.Context, project string) (string, error)
	resumeBudgetTokens int
	rehydrationTimeout time.Duration
	defaultModel       string
	primerPath         string

	mu        sync.Mutex
	seqMap    map[string]int64           // sessionID → last assigned seq
	titled    map[string]bool            // sessionID → auto-title work already completed
	consumers map[string]*consumerHandle // sessionID → runner-log consumer lifecycle
	// rehydrationActive mirrors chat_sessions.rehydration_active. Reads
	// from AppendMessage's hot path go through the cache to avoid an
	// extra DB round-trip per log entry; cache misses fall back to the
	// store and populate. setRehydrationActive updates both store and
	// cache atomically (under mu).
	rehydrationActive map[string]bool
	wg                sync.WaitGroup

	// openGroup deduplicates concurrent cold-open work per sessionID. Two
	// callers racing to open the same id share one runner.StartChat
	// round-trip; callers on *different* ids no longer serialise behind a
	// global mutex while a slow docker pull is in flight.
	openGroup singleflight.Group

	// clearGroup serialises concurrent ClearContext calls per sessionID.
	// Without this, two simultaneous clears on the same session could
	// interleave their /clear + primer pairs, leaving the transcript in an
	// ambiguous state. singleflight.Do is the right primitive here: the
	// first caller runs the body, and all concurrent callers on the same id
	// share the result (success or error).
	clearGroup singleflight.Group
	// openLimitMu guards the in-memory pendingActive reservation counter
	// used by openCold to bound MaxConcurrent without holding any lock
	// across the StartChat HTTP round-trip. Held only for the brief
	// count-check + increment / decrement windows; never across StartChat
	// or any other I/O. When MaxConcurrent is 0 the counter is not
	// consulted and the lock is never acquired.
	openLimitMu   sync.Mutex
	pendingActive int

	// appendLocks holds a per-session mutex used by AppendMessage to keep
	// the (seq-assign → store-write) window atomic for a given session
	// without coupling unrelated sessions through m.mu. The
	// UNIQUE(session_id, seq) index on disk is the final guarantor of seq
	// uniqueness. Lazily populated on first AppendMessage per session and
	// cleaned up by DeleteSession.
	appendLocksMu sync.Mutex
	appendLocks   map[string]*sync.Mutex

	// statusLocks holds a per-session mutex used to serialise
	// read-modify-write on sess.Status across all lifecycle callers
	// (MarkActive, MarkWarmIdle, OpenSession, openCold, EndSession) so
	// concurrent transitions cannot interleave inconsistently. Follows the
	// same lazy-allocation pattern as appendLocks. Cleaned up in
	// DeleteSession.
	statusLocksMu sync.Mutex
	statusLocks   map[string]*sync.Mutex

	closeOnce sync.Once
}

// NewManager constructs a Manager. Required: Store, Runner.
func NewManager(cfg Config) *Manager {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real()
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Manager{
		store:              cfg.Store,
		runner:             cfg.Runner,
		clk:                cfg.Clock,
		idleTTL:            cfg.IdleTTL,
		maxConcurrent:      cfg.MaxConcurrent,
		logger:             cfg.Logger,
		hub:                cfg.Hub,
		resolveRepoURL:     cfg.ResolveRepoURL,
		resumeBudgetTokens: cfg.ResumeBudgetTokens,
		rehydrationTimeout: cfg.RehydrationTimeout,
		defaultModel:       cfg.DefaultModel,
		primerPath:         cfg.PrimerPath,
		seqMap:             make(map[string]int64),
		titled:             make(map[string]bool),
		consumers:          make(map[string]*consumerHandle),
		rehydrationActive:  make(map[string]bool),
		appendLocks:        map[string]*sync.Mutex{},
		statusLocks:        map[string]*sync.Mutex{},
	}
}

// appendLock returns the per-session append mutex, creating it on first use.
// Lazily allocated so cold sessions don't pay the cost upfront. The map
// itself is guarded by appendLocksMu, which is independent of m.mu so the
// hot path of AppendMessage does not serialise on the same lock that
// guards shared session state.
func (m *Manager) appendLock(sessionID string) *sync.Mutex {
	m.appendLocksMu.Lock()
	defer m.appendLocksMu.Unlock()

	mu, ok := m.appendLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		m.appendLocks[sessionID] = mu
	}

	return mu
}

// statusLock returns the per-session status mutex, creating it on first use.
// Held across the read-modify-write window in MarkActive and MarkWarmIdle so
// a racing grace-timer goroutine and a new-subscriber OnSubscribe callback
// cannot interleave and produce an inconsistent final state. Lazily allocated
// on first use; cleaned up by DeleteSession.
func (m *Manager) statusLock(sessionID string) *sync.Mutex {
	m.statusLocksMu.Lock()
	defer m.statusLocksMu.Unlock()

	mu, ok := m.statusLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		m.statusLocks[sessionID] = mu
	}

	return mu
}

// withStatusLock acquires the per-session status mutex, runs fn, then
// releases it via defer. All five lifecycle callers (MarkActive,
// MarkWarmIdle, OpenSession, openCold, EndSession) route through this
// helper so the acquire/release pattern is uniform and an early return
// inside fn cannot leak the lock.
func (m *Manager) withStatusLock(sessionID string, fn func() error) error {
	mu := m.statusLock(sessionID)
	mu.Lock()
	defer mu.Unlock()

	return fn()
}

// publishSessionUpdate fans out a session-update event on the SSE hub.
// The publish runs in a goroutine so callers that hold a sessionHub lock
// (e.g. OnSubscribe, which fires inside SSEHub.Subscribe while sh.mu is
// held) do not deadlock when MarkActive calls back into the hub. Nil-hub
// guard is included so callers never need to check m.hub themselves.
// NOTE: handleUsageEntry does NOT use this helper — usage events are
// synchronous, non-status publishes that need no goroutine dispatch.
func (m *Manager) publishSessionUpdate(sessionID string, u SessionUpdate) {
	if m.hub == nil {
		return
	}

	go m.hub.PublishSessionUpdate(sessionID, u)
}

// consumerHandle is the per-session lifecycle handle for a runner-log consumer
// goroutine. done is closed when the goroutine returns, so stopConsumer can
// block until the cleanup defers have executed and the consumers map is
// guaranteed clean before a subsequent startConsumer runs.
type consumerHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// roleFromLogType maps a runner LogEntry.Type to a chat.Role.
// Unknown types fall back to RoleSystem so transcripts remain complete.
func roleFromLogType(typ string) Role {
	switch typ {
	case "text":
		return RoleAssistantText
	case "thinking":
		return RoleAssistantThinking
	case "tool_call":
		return RoleToolCall
	case "tool_result":
		return RoleToolResult
	case "stderr":
		return RoleStderr
	case "user":
		// User echoes are produced CM-side via SendUserMessage; the runner
		// re-emits them on the broadcaster as a courtesy. Ignore to avoid
		// duplicate transcript entries.
		return ""
	case "usage":
		// Usage entries are metadata; handled separately by handleUsageEntry
		// and do not become transcript rows.
		return ""
	default:
		return RoleSystem
	}
}

// handleUsageEntry processes a Claude stream-json usage block reported by the
// runner. The session row's context_tokens are updated and a session_updated
// SSE event is published so the UI header indicator refreshes in real time.
// Errors are non-fatal — usage is a UI niceness, not a correctness property.
func (m *Manager) handleUsageEntry(ctx context.Context, sessionID string, e LogEntry) {
	if e.Usage == nil {
		return
	}

	tokens := e.Usage.InputTokens + e.Usage.CacheReadTokens + e.Usage.CacheCreateTokens

	updatedAt := e.Timestamp
	if updatedAt.IsZero() {
		updatedAt = m.clk.Now().UTC()
	}

	if err := m.store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt); err != nil {
		// Session may have been deleted between the runner emitting the
		// event and CM consuming it — log at debug rather than warn.
		m.logger.Debug("chat: handleUsageEntry: update context_tokens failed",
			"session_id", sessionID, "error", err)

		return
	}

	if m.hub != nil {
		m.hub.PublishSessionUpdate(sessionID, SessionUpdate{
			ContextTokens:          tokens,
			ContextTokensUpdatedAt: updatedAt,
			Model:                  e.Model,
		})
	}
}

// startConsumer ensures a goroutine is bridging runner /logs for sessionID
// into AppendMessage + hub.Publish. Idempotent: subsequent calls for the
// same session are no-ops while a consumer is already running.
func (m *Manager) startConsumer(sessionID string) {
	m.mu.Lock()

	if _, ok := m.consumers[sessionID]; ok {
		m.mu.Unlock()

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	handle := &consumerHandle{cancel: cancel, done: make(chan struct{})}
	m.consumers[sessionID] = handle
	m.mu.Unlock()

	m.wg.Add(1)

	go func() {
		defer m.wg.Done()
		// close(done) runs LAST so stopConsumer's <-done blocks until the
		// map-entry cleanup has executed; that guarantees a follow-on
		// startConsumer sees a clean slate and is not defeated by a stale
		// entry.
		defer close(handle.done)
		defer func() {
			m.mu.Lock()
			// Defensive identity check: stopConsumer may have already removed
			// the entry. Only delete if we still own it.
			if cur, ok := m.consumers[sessionID]; ok && cur == handle {
				delete(m.consumers, sessionID)
			}
			m.mu.Unlock()
		}()

		onEntry := func(e LogEntry) {
			if e.Type == "usage" {
				m.handleUsageEntry(ctx, sessionID, e)

				return
			}

			role := roleFromLogType(e.Type)
			if role == "" {
				return
			}

			msg, err := m.AppendMessage(ctx, sessionID, role, e.Content)
			if err != nil {
				m.logger.Warn("chat: consumer AppendMessage failed",
					"session_id", sessionID, "type", e.Type, "error", err)

				return
			}

			if m.hub != nil {
				m.hub.Publish(sessionID, SSEEvent{
					Seq:              msg.Seq,
					Role:             role,
					Content:          e.Content,
					RehydrationPhase: msg.RehydrationPhase,
				})
			}
		}

		m.logger.Info("chat: runner-log consumer started", "session_id", sessionID)

		// Exponential backoff retry loop. A bare StreamLogs failure used
		// to log WARN and exit, stranding the SSE bridge permanently
		// across a transient runner restart. Mirror the pattern from
		// internal/runner/sessionlog/manager.go runPump: retry with
		// exponential backoff, cap at 30s, exit cleanly on ctx
		// cancellation.
		const (
			backoffBase = 250 * time.Millisecond
			backoffCap  = 30 * time.Second
		)

		attempt := 0

		for {
			if ctx.Err() != nil {
				m.logger.Info("chat: runner-log consumer stopped", "session_id", sessionID)

				return
			}

			err := m.runner.StreamLogs(ctx, sessionID, onEntry)
			if err == nil || errors.Is(err, context.Canceled) {
				// Clean stream close or external cancellation. The
				// runner has explicitly signalled "no more events"
				// (typically because the session ended), so exit.
				m.logger.Info("chat: runner-log consumer stopped", "session_id", sessionID)

				return
			}

			if ctx.Err() != nil {
				m.logger.Info("chat: runner-log consumer stopped", "session_id", sessionID)

				return
			}

			attempt++

			shift := attempt - 1
			if shift > 16 {
				shift = 16
			}

			backoff := time.Duration(float64(backoffBase) * float64(int64(1)<<shift))
			if backoff > backoffCap {
				backoff = backoffCap
			}

			m.logger.Warn("chat: runner-log consumer stream error, retrying",
				"session_id", sessionID, "attempt", attempt,
				"backoff", backoff, "error", err)

			select {
			case <-ctx.Done():
				m.logger.Info("chat: runner-log consumer stopped", "session_id", sessionID)

				return
			case <-time.After(backoff):
			}
		}
	}()
}

// stopConsumer cancels the runner-log consumer for sessionID and blocks until
// the goroutine has exited. Synchronous teardown is required so a fast Reopen
// after EndSession is guaranteed to start a fresh consumer — an asynchronous
// cleanup defer would leave the map entry visible to startConsumer's
// idempotency check, dropping the new session's log bridge.
func (m *Manager) stopConsumer(sessionID string) {
	m.mu.Lock()

	handle, ok := m.consumers[sessionID]
	if ok {
		delete(m.consumers, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	handle.cancel()
	<-handle.done
}

// Close cancels every active runner-log consumer goroutine and waits for them
// to exit. The supplied ctx acts as a deadline: if consumers have not all
// stopped by ctx.Done(), Close returns an error wrapping ctx.Err(). Idempotent
// — subsequent calls are no-ops and return nil.
func (m *Manager) Close(ctx context.Context) error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		for id, handle := range m.consumers {
			handle.cancel()
			delete(m.consumers, id)
		}
		m.mu.Unlock()
	})

	done := make(chan struct{})

	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("chat: Close: timeout waiting for consumers: %w", ctx.Err())
	}
}

// CreateInput is the user-facing payload for creating a new session.
type CreateInput struct {
	Title     string
	Project   string
	CreatedBy string
	Model     string
}

// CreateSession inserts a cold-state session row. The container is
// not started until OpenSession is called.
func (m *Manager) CreateSession(ctx context.Context, in CreateInput) (Session, error) {
	now := m.clk.Now().UTC().Truncate(time.Second)

	sess := Session{
		ID:         NewID(),
		Title:      in.Title,
		Project:    in.Project,
		Status:     StatusCold,
		CreatedAt:  now,
		LastActive: now,
		CreatedBy:  in.CreatedBy,
		Model:      in.Model,
	}
	if err := m.store.CreateSession(ctx, sess); err != nil {
		return Session{}, fmt.Errorf("chat: CreateSession: %w", err)
	}

	return sess, nil
}

// buildResume loads the prior transcript and returns a ResumeContext for the
// runner, or nil when there's nothing worth resuming (fresh session, all
// messages filtered, or a DB error — the last case is logged and degrades to
// a fresh agent rather than refusing to open).
func (m *Manager) buildResume(ctx context.Context, sessionID string) *ResumeContext {
	const maxMessagesForBuild = 600 // matches transcript.MaxTurns + headroom

	msgs, err := m.store.ListMessagesTail(ctx, sessionID, maxMessagesForBuild)
	if err != nil {
		m.logger.Warn("chat: buildResume list messages failed; skipping rehydration",
			"session_id", sessionID, "error", err)

		return nil
	}

	tmsgs := make([]transcript.Message, len(msgs))
	for i, msg := range msgs {
		tmsgs[i] = transcript.Message{
			Seq:              msg.Seq,
			Role:             string(msg.Role),
			Content:          msg.Content,
			RehydrationPhase: msg.RehydrationPhase,
		}
	}

	return transcript.Build(tmsgs, transcript.BuildOpts{BudgetTokens: m.resumeBudgetTokens})
}

// resumeTurnCount is a small helper for structured logging — returns the
// number of turns in a Resume, or 0 when nil.
func resumeTurnCount(r *ResumeContext) int {
	if r == nil {
		return 0
	}

	return len(r.Turns)
}

// loadPrimer reads the chat-mode primer file from m.primerPath on each call.
// Returns an empty string and logs a WARN on any failure (missing file,
// permission error, unreadable bytes) — primer is a quality-of-life feature
// and must never block a cold open.
func (m *Manager) loadPrimer() string {
	if m.primerPath == "" {
		return ""
	}

	data, err := os.ReadFile(m.primerPath)
	if err != nil {
		m.logger.Warn("chat: primer load failed; cold open will start without primer",
			"path", m.primerPath, "error", err)

		return ""
	}

	return string(data)
}

// isRehydrationActive reports whether the session is currently in its
// rehydration phase. Reads go through the in-memory cache first; misses
// fall back to the store and populate. Errors fall back to false rather
// than blocking AppendMessage's hot path.
func (m *Manager) isRehydrationActive(ctx context.Context, sessionID string) bool {
	m.mu.Lock()

	if v, ok := m.rehydrationActive[sessionID]; ok {
		m.mu.Unlock()

		return v
	}

	m.mu.Unlock()

	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}

	m.mu.Lock()
	m.rehydrationActive[sessionID] = sess.RehydrationActive
	m.mu.Unlock()

	return sess.RehydrationActive
}

// setRehydrationActive writes the flag to the store and mirrors it to the
// in-memory cache. Called from OpenSession (cold-resume → true),
// SendUserMessage (first user msg → false), CompleteRehydration (MCP tool
// → false), EndSession (cold transition → false), and the reaper sweep
// (timeout → false).
//
// Per-session serialisation is provided by the per-session statusLock so
// the persisted value and the cached value cannot diverge under concurrent
// flips on the same session. The global m.mu is held only across the
// trivial cache write — never across the SQL UPDATE — so a slow fsync on
// one session no longer stalls every unrelated session's AppendMessage
// hot path (which reads seqMap under m.mu).
//
// On store failure the cache is left untouched, so disk and cache stay
// aligned. Callers that already hold statusLock for this session MUST NOT
// call this function (sync.Mutex is not reentrant); the production call
// sites all run outside any withStatusLock callback.
func (m *Manager) setRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	sl := m.statusLock(sessionID)
	sl.Lock()
	defer sl.Unlock()

	// Pass the clock-sourced time so tests that inject a fake clock get a
	// deterministic rehydration_started_at rather than a real wall-clock value.
	if err := m.store.SetRehydrationActive(ctx, sessionID, active, m.clk.Now().UTC()); err != nil {
		return err
	}

	m.mu.Lock()
	m.rehydrationActive[sessionID] = active
	m.mu.Unlock()

	return nil
}

// CompleteRehydration ends the per-session rehydration phase: persists
// `summary` as a normal (non-phase) assistant_text message, flips the
// session flag off, and publishes the summary to the SSE hub. Idempotent:
// a second call with the flag already off returns success and no-ops.
func (m *Manager) CompleteRehydration(ctx context.Context, sessionID, summary string) error {
	if _, err := m.store.GetSession(ctx, sessionID); err != nil {
		return fmt.Errorf("chat: CompleteRehydration: %w", err)
	}

	if !m.isRehydrationActive(ctx, sessionID) {
		m.logger.Debug("chat: CompleteRehydration: already inactive, no-op",
			"session_id", sessionID)

		return nil
	}

	if err := m.setRehydrationActive(ctx, sessionID, false); err != nil {
		return fmt.Errorf("chat: CompleteRehydration: flip flag: %w", err)
	}

	msg, err := m.AppendMessage(ctx, sessionID, RoleAssistantText, summary)
	if err != nil {
		return fmt.Errorf("chat: CompleteRehydration: append summary: %w", err)
	}

	if m.hub != nil {
		m.hub.Publish(sessionID, SSEEvent{
			Seq:     msg.Seq,
			Role:    RoleAssistantText,
			Content: summary,
		})
	}

	m.logger.Info("chat: rehydration complete",
		"session_id", sessionID, "summary_len", len(summary))

	return nil
}

// ClearContext clears the runner's working memory in place: sends
// "/clear" then re-primes the session with the chat-mode primer, marks
// every prior transcript row with rehydration_phase=true so future
// rehydration payloads skip them, and appends a system-role divider row
// (content=ContextClearedMarker, kind=EventKindDivider) that the UI
// renders as a horizontal rule.
//
// Concurrent ClearContext calls for the same session are serialised via
// clearGroup (singleflight): the first caller executes the body and all
// concurrent callers on the same session share the result. This prevents
// /clear + primer pairs from interleaving across simultaneous requests.
//
// Failure semantics: a failure in the runner /clear or primer call wraps
// ErrRunnerSend so the API layer maps to 502. On runner failure we abort
// before marking the transcript or appending the divider — the transcript
// stays consistent with the runtime. If the /clear succeeds but the
// primer fails, a WARN log records that the runtime is "unoriented"; the
// transcript is still left untouched, so the user can retry.
func (m *Manager) ClearContext(ctx context.Context, sessionID string) error {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("chat: ClearContext: %w", err)
	}

	// Only active and warm-idle sessions have a live runner container. A cold
	// or ending session has nothing to /clear, so we fail fast here rather
	// than letting the runner call time out or produce a confusing error.
	if sess.Status != StatusActive && sess.Status != StatusWarmIdle {
		return ErrSessionNotRunning
	}

	_, err, _ = m.clearGroup.Do(sessionID, func() (any, error) {
		return struct{}{}, m.doClearContext(ctx, sessionID)
	})

	return err
}

// doClearContext is the serialised body of ClearContext, called under
// clearGroup.Do to prevent concurrent clears from interleaving on the same
// session. The session-not-running guard is checked before entering
// clearGroup, so this function can assume the session is active or warm-idle.
func (m *Manager) doClearContext(ctx context.Context, sessionID string) error {
	clearMsgID := NewID()
	if err := m.runner.SendChatMessage(ctx, sessionID, "/clear", clearMsgID); err != nil {
		return fmt.Errorf("chat: ClearContext: /clear: %w: %w", ErrRunnerSend, err)
	}

	primerPresent := false

	if primer := m.loadPrimer(); primer != "" {
		primerPresent = true
		primerMsgID := NewID()

		if err := m.runner.SendChatMessage(ctx, sessionID, primer, primerMsgID); err != nil {
			m.logger.Warn("chat: ClearContext: primer send failed after /clear succeeded; runtime is unoriented",
				"session_id", sessionID, "error", err)

			// Wrap with both ErrRunnerSendPrimer and ErrRunnerSend so callers
			// can use errors.Is to distinguish primer failure from /clear failure
			// while still matching the broader ErrRunnerSend sentinel.
			return fmt.Errorf("chat: ClearContext: primer: %w: %w: %w", ErrRunnerSendPrimer, ErrRunnerSend, err)
		}
	}

	// Build the divider message under the per-session append lock so the
	// seq is assigned atomically with the transcript mark + INSERT. The
	// ClearTranscriptAtomic call wraps both operations in a single SQL
	// transaction, preventing partial-failure states (rows marked but no
	// divider inserted, or vice-versa).
	sl := m.appendLock(sessionID)
	sl.Lock()

	// Seed the in-memory counter without holding m.mu across the MaxSeq
	// SQL round-trip — m.mu is the same mutex that gates seqMap reads on
	// the AppendMessage hot path for every session, so holding it across
	// I/O would stall unrelated appends. The per-session appendLock above
	// keeps the seq-assign → store-write window atomic for this session.
	m.mu.Lock()
	_, seeded := m.seqMap[sessionID]
	m.mu.Unlock()

	if !seeded {
		maxSeq, seedErr := m.store.MaxSeq(ctx, sessionID)
		if seedErr != nil {
			sl.Unlock()

			return fmt.Errorf("chat: ClearContext: seed seq: %w", seedErr)
		}

		m.mu.Lock()
		// Re-check under the lock: no caller for THIS session can have
		// raced — the per-session appendLock is held.
		if _, ok := m.seqMap[sessionID]; !ok {
			m.seqMap[sessionID] = maxSeq
		}
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.seqMap[sessionID]++
	seq := m.seqMap[sessionID]
	m.mu.Unlock()

	phase := m.isRehydrationActive(ctx, sessionID)

	divider := Message{
		SessionID:        sessionID,
		Seq:              seq,
		Role:             RoleSystem,
		Content:          ContextClearedMarker,
		Kind:             EventKindDivider,
		CreatedAt:        m.clk.Now().UTC().Truncate(time.Second),
		RehydrationPhase: phase,
	}

	markedCount, msg, err := m.store.ClearTranscriptAtomic(ctx, sessionID, divider)
	if err != nil {
		// Roll back the seq reservation so the next append re-uses it.
		m.mu.Lock()
		m.seqMap[sessionID]--
		m.mu.Unlock()
		sl.Unlock()

		return fmt.Errorf("chat: ClearContext: atomic transcript update: %w", err)
	}

	sl.Unlock()

	if m.hub != nil {
		m.hub.Publish(sessionID, SSEEvent{
			Seq:              msg.Seq,
			Role:             RoleSystem,
			Content:          msg.Content,
			DataKind:         EventKindDivider,
			RehydrationPhase: msg.RehydrationPhase,
		})
	}

	m.logger.Info("chat: context cleared",
		"session_id", sessionID,
		"marked_count", markedCount,
		"primer_present", primerPresent)

	return nil
}

// OpenSession transitions a session into the active state, starting a
// new container if cold or reattaching if warm-idle. Idempotent on
// already-active sessions.
func (m *Manager) OpenSession(ctx context.Context, id string) (Session, error) {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("chat: OpenSession: %w", err)
	}

	switch sess.Status {
	case StatusActive:
		// Idempotent for already-active sessions. Also ensure the runner-log
		// consumer is bridging /logs back into the SSE hub: a CM restart
		// strands that goroutine while the row stays active, and the only
		// recovery path used to be End → Reopen (which kills the container
		// and rehydrates a fresh one). startConsumer is a no-op when a
		// consumer for this session is already running.
		m.startConsumer(sess.ID)

		return sess, nil

	case StatusWarmIdle:
		// Hold the per-session status lock and re-read the session inside the
		// lock so a concurrent EndSession that committed between our initial
		// GetSession and here cannot be overwritten by stale warm-idle data
		// (including a dead ContainerID).
		var reattached Session

		if err := m.withStatusLock(id, func() error {
			fresh, err := m.store.GetSession(ctx, id)
			if err != nil {
				return fmt.Errorf("chat: warm reattach re-read: %w", err)
			}

			if fresh.Status != StatusWarmIdle {
				// Status drifted (e.g. EndSession raced and cleared the row).
				// Return the fresh row so the caller sees the current state.
				reattached = fresh

				return nil
			}

			fresh.Status = StatusActive
			fresh.LastActive = m.clk.Now().UTC().Truncate(time.Second)

			if err := m.store.UpdateSession(ctx, fresh); err != nil {
				return fmt.Errorf("chat: warm reattach: %w", err)
			}

			reattached = fresh

			return nil
		}); err != nil {
			return Session{}, err
		}

		if reattached.Status != StatusActive {
			// Status drifted; return the current row without publishing or
			// starting a consumer — the caller will observe the real state.
			return reattached, nil
		}

		m.logger.Info("chat: warm-idle reattached", "session_id", reattached.ID)
		m.startConsumer(reattached.ID)

		s := StatusActive
		m.publishSessionUpdate(reattached.ID, SessionUpdate{Status: &s})

		return reattached, nil

	case StatusCold:
		// Route the cold-start path through singleflight keyed on
		// sessionID so concurrent callers for the same id share one
		// runner.StartChat round-trip, and callers for *different* ids
		// no longer serialise on a global mutex behind a slow docker
		// run / image pull.
		v, err, _ := m.openGroup.Do(id, func() (any, error) {
			return m.openCold(ctx, id)
		})
		if err != nil {
			return Session{}, err
		}

		return v.(Session), nil

	case StatusEnding:
		return Session{}, fmt.Errorf("chat: session is ending")
	}

	return Session{}, fmt.Errorf("chat: unknown status %q", sess.Status)
}

// openCold runs the cold→active transition for a single sessionID. It is
// invoked under singleflight by OpenSession so concurrent callers for the
// same id share one runner.StartChat round-trip; callers for *different*
// ids no longer serialise on a global lock when MaxConcurrent is 0.
//
// The MaxConcurrent ceiling is enforced via the in-memory pendingActive
// reservation counter: a slot is reserved under openLimitMu BEFORE the
// slow StartChat call and decremented in a deferred cleanup once the
// outcome is known. openLimitMu is held only for the brief count +
// increment / decrement windows, never across StartChat or any other
// I/O, so concurrent cold opens on different sessions no longer
// serialise at runner-latency timescale.
func (m *Manager) openCold(ctx context.Context, id string) (Session, error) {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("chat: OpenSession (re-read): %w", err)
	}

	if sess.Status != StatusCold {
		// Another caller raced ahead and opened this session. Treat as
		// already-active.
		return sess, nil
	}

	// reservedSlot tracks whether we currently hold a pendingActive slot.
	// The deferred releaseReservation drops it on any function exit
	// (success or failure): on success the persisted StatusActive row
	// becomes the accounting record so the in-memory slot is no longer
	// needed; on failure the rollback path releases the slot so it does
	// not leak.
	reservedSlot := false

	releaseReservation := func() {
		if !reservedSlot {
			return
		}

		m.openLimitMu.Lock()
		m.pendingActive--
		reservedSlot = false
		m.openLimitMu.Unlock()
	}
	defer releaseReservation()

	if m.maxConcurrent > 0 {
		// Brief lock window: count persisted active/warm rows + in-flight
		// reservations, then increment pendingActive if there's room.
		// Reservations cover the StartChat window so two callers can't
		// each see (active+warm < limit) and both proceed past StartChat.
		m.openLimitMu.Lock()

		n, err := m.store.CountSessionsByStatus(ctx, StatusActive, StatusWarmIdle)
		if err != nil {
			m.openLimitMu.Unlock()

			return Session{}, fmt.Errorf("chat: count active+warm: %w", err)
		}

		if n+m.pendingActive >= m.maxConcurrent {
			m.openLimitMu.Unlock()

			return Session{}, ErrTooManyConcurrent
		}

		m.pendingActive++
		reservedSlot = true
		m.openLimitMu.Unlock()
	}

	opts, err := m.coldPrep(ctx, sess)
	if err != nil {
		return Session{}, err
	}

	containerID, err := m.runner.StartChat(ctx, opts)
	if err != nil {
		return Session{}, fmt.Errorf("chat: start container: %w", err)
	}

	// Hold the per-session status lock, re-read the session, and verify
	// it is still cold before persisting the active state. A concurrent
	// EndSession or another OpenSession that raced the StartChat window
	// may have mutated the row; overwriting it would corrupt state (e.g.
	// restoring a dead ContainerID). If the status drifted, roll back the
	// just-started container and surface a clean error.
	var persistErr error

	if err := m.withStatusLock(sess.ID, func() error {
		fresh, err := m.store.GetSession(ctx, sess.ID)
		if err != nil {
			return fmt.Errorf("chat: openCold re-read: %w", err)
		}

		if fresh.Status != StatusCold {
			// Status drifted while StartChat was in flight. Roll back.
			m.rollbackContainer(ctx, "status drift during StartChat", sess.ID, containerID, nil)

			persistErr = fmt.Errorf("chat: openCold: session status changed during StartChat (now %q)", fresh.Status)

			return nil
		}

		fresh.Status = StatusActive
		fresh.ContainerID = containerID
		fresh.Model = opts.Model
		fresh.LastActive = m.clk.Now().UTC().Truncate(time.Second)

		if fresh.Project != "" && !slices.Contains(fresh.Workspace, fresh.Project) {
			fresh.Workspace = append(fresh.Workspace, fresh.Project)
		}

		if err := m.store.UpdateSession(ctx, fresh); err != nil {
			// Roll back the container start so we don't leak.
			m.rollbackContainer(ctx, "persist active failure", sess.ID, containerID, err)

			return fmt.Errorf("chat: persist active: %w", err)
		}

		sess = fresh

		return nil
	}); err != nil {
		return Session{}, err
	}

	if persistErr != nil {
		return Session{}, persistErr
	}

	if opts.Resume != nil {
		// Pre-arm the in-memory cache so concurrent log writes during
		// the persist window stamp rehydration_phase=TRUE even before
		// the DB write completes.
		// NOTE: if the setRehydrationActive persist below fails, the cache
		// is rolled back and the session is reset to cold. However, any
		// messages appended during this narrow window (between the pre-arm
		// and the rollback) will keep their rehydration_phase=TRUE stamp
		// permanently — they will be excluded from future resume payloads
		// by transcript.Build. In practice this window spans a single store
		// write and no user-driven AppendMessage can race here: the
		// runner-consumer goroutine is only spawned after a successful
		// OpenSession returns, so the risk is negligible.
		m.mu.Lock()
		m.rehydrationActive[sess.ID] = true
		m.mu.Unlock()

		if err := m.setRehydrationActive(ctx, sess.ID, true); err != nil {
			// Roll back: clear the cache, stop the container, and
			// reset the session row to cold so the next open retries
			// cleanly.
			m.mu.Lock()
			delete(m.rehydrationActive, sess.ID)
			m.mu.Unlock()

			m.rollbackContainer(ctx, "rehydration persist failure", sess.ID, containerID, err)

			// Rollback persist uses a fresh background context so a cancelled
			// caller ctx (e.g. the HTTP request that initiated OpenSession
			// hung up after StartChat succeeded) cannot leave the on-disk row
			// stuck in StatusActive. Bound by a short timeout so a wedged
			// store doesn't block the goroutine forever.
			rbCtx, rbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rbCancel()

			if err := m.withStatusLock(sess.ID, func() error {
				sess.Status = StatusCold
				sess.ContainerID = ""

				if err := m.store.UpdateSession(rbCtx, sess); err != nil {
					m.logger.Warn("chat: OpenSession: rollback reset to cold failed",
						"session_id", sess.ID, "error", err)
				}

				return nil
			}); err != nil {
				m.logger.Warn("chat: OpenSession: rollback lock failed",
					"session_id", sess.ID, "error", err)
			}

			return Session{}, fmt.Errorf("chat: OpenSession: persist rehydration_active: %w", err)
		}

		sess.RehydrationActive = true
	}

	m.logger.Info("chat: cold session active",
		"session_id", sess.ID, "container_id", containerID)
	m.startConsumer(sess.ID)

	s := StatusActive
	m.publishSessionUpdate(sess.ID, SessionUpdate{Status: &s})

	return sess, nil
}

// coldPrep gathers the inputs needed by runner.StartChat for a cold open.
// Returns the options struct and an error if any preparatory step failed.
func (m *Manager) coldPrep(ctx context.Context, sess Session) (StartChatOpts, error) {
	var repoURL string

	if sess.Project != "" && m.resolveRepoURL != nil {
		var err error

		repoURL, err = m.resolveRepoURL(ctx, sess.Project)
		if err != nil {
			return StartChatOpts{}, fmt.Errorf("chat: resolve repo for %q: %w", sess.Project, err)
		}
	}

	// Build the rehydration payload from the persisted transcript.
	// Errors here are non-fatal — fall back to "no resume" so we
	// never block the user from opening the chat.
	resume := m.buildResume(ctx, sess.ID)

	// Read the chat-mode primer on every cold open. Operators who edit
	// workflow-skills/chat-mode.md get hot-reload for free on the next
	// new container.
	primer := m.loadPrimer()

	model := sess.Model
	if model == "" {
		model = m.defaultModel
	}

	m.logger.Info("chat: opening cold session",
		"session_id", sess.ID, "project", sess.Project, "repo_url", repoURL,
		"model", model, "has_resume", resume != nil,
		"resume_turn_count", resumeTurnCount(resume),
		"has_primer", primer != "")

	return StartChatOpts{
		SessionID: sess.ID,
		Project:   sess.Project,
		RepoURL:   repoURL,
		Model:     model,
		Resume:    resume,
		Primer:    primer,
	}, nil
}

// rollbackContainer ends the runner container started during a cold open and
// logs the rollback reason. Should be called when openCold fails after the
// container has been provisioned.
func (m *Manager) rollbackContainer(_ context.Context, reason, sessID, containerID string, opErr error) {
	if rbErr := m.runner.EndChat(context.Background(), sessID); rbErr != nil {
		m.logger.Warn("chat: rollback EndChat failed",
			"reason", reason,
			"session_id", sessID,
			"container_id", containerID,
			"op_error", opErr,
			"error", rbErr)
	}
}

// maxMessageBytes caps a single persisted transcript entry. Verbose tool
// output (e.g. a tool_result containing a large file dump) would otherwise
// grow chats.db linearly without bound. The user-message path is already
// capped at the HTTP boundary (8192 bytes), so this cap mainly fires on
// runner-emitted entries.
const maxMessageBytes = 32 * 1024

// truncationMarker is appended to messages that exceeded maxMessageBytes.
const truncationMarker = "\n... [truncated]"

// truncateMessageContent caps content at maxMessageBytes and appends the
// truncation marker. Truncation respects UTF-8 rune boundaries so the marker
// is not appended in the middle of a multibyte sequence.
func truncateMessageContent(content string) string {
	if len(content) <= maxMessageBytes {
		return content
	}

	cut := maxMessageBytes - len(truncationMarker)
	// Back up to a rune start so we don't slice mid-rune.
	for cut > 0 && (content[cut]&0xC0) == 0x80 {
		cut--
	}

	return content[:cut] + truncationMarker
}

// AppendMessage persists a transcript entry with a monotonic seq.
// Seq is assigned server-side; the caller does not provide it. The
// rehydration_phase column on the persisted row is sourced from the
// in-memory cache (mirrors session.rehydration_active) so messages emitted
// during the rehydration phase are excluded from future resume payloads
// by transcript.Build.
func (m *Manager) AppendMessage(ctx context.Context, sessionID string, role Role, content string) (Message, error) {
	return m.appendMessageWithKind(ctx, sessionID, role, content, "")
}

// appendMessageWithKind is the internal variant that stamps a non-empty
// Message.Kind on the persisted row. Public callers reach this via
// AppendMessage (kind="") or ClearContext (kind=EventKindDivider).
func (m *Manager) appendMessageWithKind(ctx context.Context, sessionID string, role Role, content, kind string) (Message, error) {
	content = truncateMessageContent(content)

	phase := m.isRehydrationActive(ctx, sessionID)

	// Auto-title: if this is the first user message on a still-untitled session,
	// derive a title from the content (50-byte truncation with ellipsis). The
	// `titled` cache skips the SELECT+UPDATE round-trip once we've confirmed a
	// title exists for the session.
	//
	// Use store.UpdateSessionTitle — a targeted UPDATE that touches only
	// the title column — rather than GetSession + UpdateSession. A racing
	// OpenSession/MarkActive between the read and a wholesale UPDATE would
	// otherwise see its just-flipped ContainerID/Status/Workspace
	// overwritten by the stale pre-flip snapshot loaded here.
	if role == RoleUser {
		m.mu.Lock()
		alreadyTitled := m.titled[sessionID]
		m.mu.Unlock()

		if !alreadyTitled {
			sess, err := m.store.GetSession(ctx, sessionID)
			if err == nil {
				if sess.Title == "" {
					title := content
					// Truncate at rune boundary, not byte boundary — slicing
					// bytes mid-UTF-8-rune produces invalid sequences that
					// JSON-marshal as U+FFFD.
					if utf8.RuneCountInString(title) > 50 {
						runes := []rune(title)
						title = string(runes[:50]) + "…"
					}

					if err := m.store.UpdateSessionTitle(ctx, sessionID, title); err != nil {
						m.logger.Warn("chat: auto-title persist failed",
							"session_id", sessionID, "error", err)
					}
				}

				m.mu.Lock()
				m.titled[sessionID] = true
				m.mu.Unlock()
			}
		}
	}

	// Per-session lock keeps the (seq-assign → store-write) window atomic
	// for this session without coupling unrelated sessions through m.mu.
	// One slow fsync on session A no longer stalls appends to session B.
	// SQLite serialises writes at the engine level and the
	// UNIQUE(session_id, seq) index is the final correctness guarantor.
	sl := m.appendLock(sessionID)
	sl.Lock()
	defer sl.Unlock()

	// seqMap is shared across sessions so the seq-assign window must take
	// m.mu briefly. The store write below runs outside m.mu — only the
	// per-session lock is held across the I/O.
	m.mu.Lock()

	// Lazy seed from the store if first call this process. Uses an indexed
	// MAX(seq) query so the seed cost is constant time even on long sessions.
	if _, ok := m.seqMap[sessionID]; !ok {
		maxSeq, err := m.store.MaxSeq(ctx, sessionID)
		if err != nil {
			m.mu.Unlock()

			return Message{}, fmt.Errorf("chat: seed seq: %w", err)
		}

		m.seqMap[sessionID] = maxSeq
	}

	m.seqMap[sessionID]++
	seq := m.seqMap[sessionID]
	m.mu.Unlock()

	msg := Message{
		SessionID:        sessionID,
		Seq:              seq,
		Role:             role,
		Content:          content,
		Kind:             kind,
		CreatedAt:        m.clk.Now().UTC().Truncate(time.Second),
		RehydrationPhase: phase,
	}

	if _, err := m.store.AppendMessage(ctx, msg); err != nil {
		// The seq was claimed but the store write failed. Roll back the
		// in-memory counter so the next append re-uses it; the UNIQUE
		// index on (session_id, seq) would otherwise reject a future
		// AppendMessage if the failed write somehow made it to disk. The
		// per-session lock is still held so the rollback is sequenced
		// before any other append on this session.
		m.mu.Lock()
		m.seqMap[sessionID]--
		m.mu.Unlock()

		return Message{}, fmt.Errorf("chat: append: %w", err)
	}

	return msg, nil
}

// Reattach ensures the runner-log consumer is running for an active or
// warm-idle session and refreshes its LastActive timestamp so the idle
// reaper doesn't end it while the user is interacting with it. No-op on
// cold and ending sessions, which have no live runner container to
// bridge to.
//
// Status is intentionally left untouched; lifecycle promotion is
// MarkActive's job, called separately from OnSubscribe.
//
// Idempotent and safe for concurrent callers — startConsumer guards
// against duplicate consumer goroutines internally.
//
// Returns the loaded Session so callers (e.g. OnSubscribe) can inspect
// the current status without a redundant GetSession round-trip.
func (m *Manager) Reattach(ctx context.Context, sessionID string) (Session, error) {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, fmt.Errorf("chat: Reattach: %w", err)
	}

	if sess.Status != StatusActive && sess.Status != StatusWarmIdle {
		return sess, nil
	}

	sess.LastActive = m.clk.Now().UTC().Truncate(time.Second)
	if err := m.store.UpdateSession(ctx, sess); err != nil {
		return Session{}, fmt.Errorf("chat: Reattach: persist last-active: %w", err)
	}

	m.startConsumer(sess.ID)

	return sess, nil
}

// MarkWarmIdle transitions an active session to warm-idle. No-op if the
// session is not active. Tolerant of ErrSessionNotFound — a grace timer
// fired against a session that was already deleted (DeleteSession,
// reconcile sweep) is a benign race, not an error.
func (m *Manager) MarkWarmIdle(ctx context.Context, sessionID string) error {
	// Hold the per-session status lock across the read-modify-write window
	// so a concurrent MarkActive (fired from OnSubscribe) serialises cleanly
	// rather than interleaving with last-write-wins semantics.
	var publish bool

	if err := m.withStatusLock(sessionID, func() error {
		sess, err := m.store.GetSession(ctx, sessionID)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				m.logger.Debug("chat: MarkWarmIdle: session not found, ignoring", "session_id", sessionID)

				return nil
			}

			return fmt.Errorf("chat: MarkWarmIdle: %w", err)
		}

		if sess.Status != StatusActive {
			return nil
		}

		sess.Status = StatusWarmIdle
		sess.LastActive = m.clk.Now().UTC().Truncate(time.Second)

		if err := m.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("chat: MarkWarmIdle persist: %w", err)
		}

		publish = true

		return nil
	}); err != nil {
		return err
	}

	if publish {
		s := StatusWarmIdle
		m.publishSessionUpdate(sessionID, SessionUpdate{Status: &s})
	}

	return nil
}

// MarkActive promotes a warm-idle session back to active. No-op if the session
// is not warm-idle. Tolerant of ErrSessionNotFound — the same benign-race
// reasoning as MarkWarmIdle.
func (m *Manager) MarkActive(ctx context.Context, sessionID string) error {
	// Hold the per-session status lock so this serialises with MarkWarmIdle.
	// The lock is intentionally taken before the store read so the
	// read-modify-write is atomic: a racing MarkWarmIdle that has already
	// committed its write will be visible to us (status will be warm-idle)
	// and we correctly overwrite it; a MarkWarmIdle that hasn't committed
	// yet waits and then sees status=active from our write and no-ops.
	var publish bool

	if err := m.withStatusLock(sessionID, func() error {
		sess, err := m.store.GetSession(ctx, sessionID)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				m.logger.Debug("chat: MarkActive: session not found, ignoring", "session_id", sessionID)

				return nil
			}

			return fmt.Errorf("chat: MarkActive: %w", err)
		}

		if sess.Status != StatusWarmIdle {
			return nil
		}

		sess.Status = StatusActive
		sess.LastActive = m.clk.Now().UTC().Truncate(time.Second)

		if err := m.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("chat: MarkActive persist: %w", err)
		}

		m.logger.Info("chat: session promoted to active", "session_id", sessionID)

		publish = true

		return nil
	}); err != nil {
		return err
	}

	if publish {
		s := StatusActive
		m.publishSessionUpdate(sessionID, SessionUpdate{Status: &s})
	}

	return nil
}

// GetSession returns the persisted session by ID.
func (m *Manager) GetSession(ctx context.Context, id string) (Session, error) {
	return m.store.GetSession(ctx, id)
}

// EndSession transitions a session to cold, stopping the runner container.
// Idempotent on already-cold sessions and re-entrant against status=ending
// rows (which can result from a prior partial failure). Runner teardown and
// consumer stop are both idempotent, so calling EndSession on a wedged
// ending row safely completes the transition in a single store write.
//
// The per-session statusLock is acquired BEFORE the runner.EndChat call and
// held across the cold persist. A racing OpenSession that gets a Status
// read between runner.EndChat succeeding and the persist could otherwise
// observe status=active/warm-idle and reattach to a now-dead container.
// Holding statusLock across runner.EndChat blocks racing status reads for
// the duration of an HTTP round-trip — acceptable cost given the
// transition is a one-shot terminal state.
func (m *Manager) EndSession(ctx context.Context, id string) error {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("chat: EndSession: %w", err)
	}

	if sess.Status == StatusCold {
		return nil
	}

	m.logger.Info("chat: ending session", "session_id", sess.ID,
		"from_status", string(sess.Status))

	// Stop the consumer outside the statusLock — stopConsumer waits for the
	// consumer goroutine to exit, and the goroutine's AppendMessage path
	// touches m.mu / appendLock but never statusLock, so this is safe.
	m.stopConsumer(sess.ID)

	if err := m.withStatusLock(sess.ID, func() error {
		// EndChat is idempotent on the runner side; re-entry from a
		// status=ending row is safe. Held under statusLock so no racing
		// OpenSession can re-read sess.Status=active/warm-idle and
		// reattach to the now-dead container.
		if err := m.runner.EndChat(ctx, sess.ID); err != nil {
			m.logger.Warn("chat: runner end failed, marking cold anyway",
				"session_id", sess.ID, "error", err)
		}

		// Single store write: transition directly to cold without an
		// intermediate status=ending persist. Collapsing to one write
		// means a failure here leaves the row in its original state
		// (active/warm-idle/ending) rather than wedged in ending,
		// making the next EndSession call a clean retry.
		sess.Status = StatusCold
		sess.ContainerID = ""
		sess.LastActive = m.clk.Now().UTC().Truncate(time.Second)

		if err := m.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("chat: mark cold: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	// Reset any leftover rehydration flag so a subsequent reopen starts
	// from a clean state. setRehydrationActive is idempotent and tolerant
	// of an already-false value. setRehydrationActive itself acquires the
	// per-session statusLock — we MUST call it AFTER withStatusLock above
	// has released, otherwise sync.Mutex (non-reentrant) would deadlock.
	// Only include RehydrationActive in the SSE event if the persist
	// succeeded — if it failed the on-disk row still has
	// rehydration_active=true, and reporting false would contradict it.
	rehyErr := m.setRehydrationActive(ctx, sess.ID, false)
	if rehyErr != nil {
		m.logger.Warn("chat: EndSession: clear rehydration flag failed",
			"session_id", sess.ID, "error", rehyErr)
	}

	cold := StatusCold
	update := SessionUpdate{Status: &cold}

	if rehyErr == nil {
		falseVal := false
		update.RehydrationActive = &falseVal
	}

	m.publishSessionUpdate(sess.ID, update)

	m.logger.Info("chat: session cold", "session_id", sess.ID)

	return nil
}

// ListSessions returns sessions matching the filter, newest-active first.
func (m *Manager) ListSessions(ctx context.Context, f SessionFilter) ([]Session, error) {
	return m.store.ListSessions(ctx, f)
}

// DeleteSession ends the container if running, then deletes the row.
func (m *Manager) DeleteSession(ctx context.Context, id string) error {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return err
	}

	if sess.Status == StatusActive || sess.Status == StatusWarmIdle {
		if err := m.EndSession(ctx, id); err != nil {
			m.logger.Warn("chat: DeleteSession: EndSession failed, deleting anyway",
				"session_id", id, "error", err)
		}
	}

	m.stopConsumer(id)

	if err := m.store.DeleteSession(ctx, id); err != nil {
		return err
	}

	// Release the SSE hub's per-session ring buffer + subscriber set so the
	// hub doesn't grow without bound across session churn.
	if m.hub != nil {
		m.hub.Drop(id)
	}

	m.logger.Info("chat: session deleted", "session_id", id)

	// Drop the seq cache entry so a future session that happens to reuse
	// the ID (or an accidental Append after delete) does not leak memory.
	m.mu.Lock()
	delete(m.seqMap, id)
	delete(m.titled, id)
	delete(m.rehydrationActive, id)
	m.mu.Unlock()

	// Drop the per-session append lock entry. Held under appendLocksMu
	// rather than m.mu so the AppendMessage hot path's appendLock() call
	// does not serialise on the same lock that guards shared session state.
	m.appendLocksMu.Lock()
	delete(m.appendLocks, id)
	m.appendLocksMu.Unlock()

	// Drop the per-session status lock entry.
	m.statusLocksMu.Lock()
	delete(m.statusLocks, id)
	m.statusLocksMu.Unlock()

	return nil
}

// SendUserMessage forwards a user message to the runner first; only on a
// successful runner call is the message persisted and fanned out via the
// SSE hub. If the runner is unreachable the caller gets an error and the
// UI can retry — the alternative (snappy echo, then runner failure) used
// to leave the user staring at their own message with no reply path.
// Cold-state sessions are opened first. Returns the generated message_id
// used for runner-side echo dedup.
//
// If the session is currently in its rehydration phase, the user typing
// ends the phase as a belt-and-suspenders safety net for agents that
// forget to call chat_rehydration_complete. The flag is flipped BEFORE
// AppendMessage so the user's message itself is persisted as non-phase.
// ensureRunningForSend promotes a cold or warm-idle session to running so a
// user message can be dispatched. Cold sessions are opened via OpenSession;
// warm-idle sessions are flipped to active via MarkActive. Active sessions
// are a no-op. Returns nil when the session is ready to receive messages.
func (m *Manager) ensureRunningForSend(ctx context.Context, sess Session) error {
	switch sess.Status {
	case StatusCold:
		if _, err := m.OpenSession(ctx, sess.ID); err != nil {
			return err
		}
	case StatusWarmIdle:
		if err := m.MarkActive(ctx, sess.ID); err != nil {
			m.logger.Warn("chat: SendUserMessage: promote warm-idle to active failed",
				"session_id", sess.ID, "error", err)
		}
	}

	return nil
}

func (m *Manager) SendUserMessage(ctx context.Context, sessionID, content string) (string, error) {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	if err := m.ensureRunningForSend(ctx, sess); err != nil {
		return "", err
	}

	if m.isRehydrationActive(ctx, sessionID) {
		if err := m.setRehydrationActive(ctx, sessionID, false); err != nil {
			m.logger.Warn("chat: SendUserMessage: clear rehydration flag failed",
				"session_id", sessionID, "error", err)
		} else {
			m.logger.Info("chat: rehydration ended by user message",
				"session_id", sessionID)
		}
	}

	msgID := NewID()

	m.logger.Info("chat: forwarding user message to runner",
		"session_id", sessionID, "message_id", msgID, "content_len", len(content))

	if err := m.runner.SendChatMessage(ctx, sessionID, content, msgID); err != nil {
		return "", err
	}

	// Runner accepted the message — now safe to persist + publish.
	msg, err := m.AppendMessage(ctx, sessionID, RoleUser, content)
	if err != nil {
		return "", err
	}

	if m.hub != nil {
		m.hub.Publish(sessionID, SSEEvent{
			Seq:     msg.Seq,
			Role:    RoleUser,
			Content: content,
		})
	}

	return msgID, nil
}

// UpdateSessionMetadata writes session metadata changes (title, last_active).
func (m *Manager) UpdateSessionMetadata(ctx context.Context, s Session) error {
	return m.store.UpdateSession(ctx, s)
}

// ListMessages returns the transcript slice (seq > sinceSeq, oldest-first,
// bounded by limit). Used by the REST bootstrap endpoint that backfills the
// browser ring buffer beyond what the SSE in-memory ring can replay.
func (m *Manager) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]Message, error) {
	return m.store.ListMessages(ctx, sessionID, sinceSeq, limit)
}
