package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/chat/transcript"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// errArchitectNotLive means a restart found the row cold or not an architect,
// so nothing is left for a consumer to serve.
var errArchitectNotLive = errors.New("chat: architect session is not live")

// ErrTooManyConcurrent is returned by OpenSession when the number of active
// or warm-idle sessions has reached the configured MaxConcurrent ceiling.
var ErrTooManyConcurrent = errors.New("chat: too many concurrent sessions")

// ErrBackendSend is the sentinel wrapped around Backend.SendChatMessage
// failures inside ClearContext. The API layer matches on errors.Is to
// route these to a 502 BACKEND_UNAVAILABLE rather than a 500 INTERNAL_ERROR,
// since the runtime cause is "the backend is unreachable", not a bug.
var ErrBackendSend = errors.New("chat: backend send failed")

// ErrSessionNotRunning is returned by ClearContext when the target session is
// not in an active or warm-idle state (i.e. the worker container is not
// running). Clearing a cold or ending session has no worker to talk to.
// The API layer maps this to 409 WORKER_NOT_RUNNING.
var ErrSessionNotRunning = errors.New("chat: session is not running")

// ErrKindUnavailable is returned when a session's kind has no configured
// backend (an architect session on an instance without an architect entry).
var ErrKindUnavailable = errors.New("chat: no backend configured for this session kind")

// ErrInvalidSessionInput wraps input a session cannot take, such as a
// CreateSession validation failure or a round reply or round chat sent to a
// chat session, so the API layer can map the whole family to one 400.
var ErrInvalidSessionInput = errors.New("chat: invalid session input")

// ContextClearedMarker is the canonical content string written to the
// system-role transcript row appended on Clear Context. The frontend uses
// this in conjunction with the persisted kind ("divider") to render a
// horizontal rule rather than a normal system message.
const ContextClearedMarker = "Context cleared"

// EventKindDivider is the persisted Message.Kind / SSE DataKind value that
// marks a structural divider in the transcript (currently used only for the
// Clear Context sentinel). Empty kind means "regular message".
const EventKindDivider = "divider"

// EventKindRoundChat is the persisted Message.Kind / SSE DataKind of a user
// message about an open question round. Its Content is the plain text, as on
// any user row; the panel shows it in the round's thread, not the transcript.
const EventKindRoundChat = "round_chat"

// EventKindRoundReply is the persisted Message.Kind / SSE DataKind of a user
// message that answers a question round. Its Content is the readable summary;
// the panel hides it when its round is loaded (the block is its record) and
// shows an orphan as a plain message.
const EventKindRoundReply = "round_reply"

// EventKindCommand is the persisted Message.Kind / SSE DataKind of a user
// message that is a session command (currently only `cap`). The panel never
// renders it; the architect answers with a system line of its own.
const EventKindCommand = "command"

// Backend is the chat-execution surface chat.Manager drives (the roadmap's
// ChatBackend): container start/end, message delivery, and the /logs SSE
// bridge. The webhook-backed implementation lives in
// internal/chat/backend_client.go; tests inject stubs.
type Backend interface {
	StartChat(ctx context.Context, opts StartChatOpts) (containerID string, err error)
	EndChat(ctx context.Context, sessionID string) error
	// SendChatMessage delivers one message; msg.Round is set only on a
	// question round's answers to an architect session.
	SendChatMessage(ctx context.Context, msg protocol.MessagePayload) error
	// StreamLogs opens a long-lived SSE subscription to the backend's
	// /logs?session_id=<id> endpoint and invokes onEntry for each parsed
	// LogEntry. Returns when ctx is cancelled or the stream closes.
	StreamLogs(ctx context.Context, sessionID string, onEntry func(LogEntry)) error
}

// StartChatOpts carries every input to Backend.StartChat. Bundling the
// arguments lets us add fields (Model, Resume) without breaking the wire
// for tests with bespoke fakes.
type StartChatOpts struct {
	SessionID string
	Project   string
	RepoURL   string
	// WorkerImage is the project's remote_execution.chat_worker_image - the
	// per-project toolchain image the chat worker container should run. Empty
	// means "use the chat backend's configured base_image".
	WorkerImage string
	Model       string
	Resume      *ResumeContext
	// LLMEndpoint is the CM-provisioned inference endpoint configuration
	// forwarded to the chat backend verbatim. Nil means "the backend falls
	// back to its own local llm_endpoint config" (pre-multi-user behavior).
	// Treat as a secret carrier: never log this value or the opts struct
	// as a whole - LLMEndpoint.APIKey rides along.
	LLMEndpoint *protocol.LLMEndpoint
	// GitCredentialsToken is the per-session bearer the worker presents to
	// CM's GET /api/worker/git-credentials endpoint to fetch per-repo git
	// credentials on demand. Empty when Manager.workerCredentialsToken is
	// nil (no chat-backend api_key configured, or wiring that predates
	// worker-fetched credentials). Treat as a secret carrier: never log this
	// value.
	GitCredentialsToken string
	// Architect is set for architect-kind sessions only: the blueprint's
	// target projects, mode and cost cap. Nil for chat sessions.
	Architect *protocol.ArchitectStart
}

// ProjectExecInfo carries the per-project execution inputs a cold open needs:
// the repo URL to clone and the worker image (toolchain) to run. Either field
// is empty when the project omits it.
type ProjectExecInfo struct {
	RepoURL     string
	WorkerImage string
}

// Config carries Manager dependencies.
type Config struct {
	Store   Store
	Backend Backend
	Clock   clock.Clock
	IdleTTL time.Duration
	Logger  *slog.Logger
	// MaxConcurrent is the maximum number of sessions that may be active or
	// warm-idle at the same time. Zero means unlimited.
	MaxConcurrent int
	// Hub is the per-session SSE fan-out. When non-nil, SendUserMessage
	// publishes a user echo so the originator sees their own message in
	// the transcript without depending on a backend-side log round-trip.
	Hub *SSEHub
	// ResolveProjectExec returns the execution inputs (repo URL and
	// per-project worker image) for a project in a single lookup per cold
	// open. Either field may be empty when the project omits it. Caller wires
	// this from service.CardService.GetProject.
	ResolveProjectExec func(ctx context.Context, project string) (ProjectExecInfo, error)

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

	// Pricer is used to compute token costs for chat sessions. Optional:
	// nil means cost computation is skipped.
	Pricer Pricer

	// LLMEndpoint is the CM-provisioned inference endpoint configuration
	// attached to every chat-start payload. Nil when llm_endpoint is
	// unconfigured; the wire field is then omitted and the chat backend fails
	// closed rather than using a local key. Production wires this from
	// config.LLMEndpoint in cmd/contextmatrix/main.go's wireChat.
	LLMEndpoint *protocol.LLMEndpoint

	// WorkerCredentialsToken mints the per-session bearer attached to every
	// chat-start payload as StartChatOpts.GitCredentialsToken. nil means "do
	// not attach a token" - the field stays empty and the worker falls back
	// to its own local git config (deprecated, pre-worker-fetched-credentials
	// behavior). Production wires this from a closure over the resolved
	// chat-backend api_key in cmd/contextmatrix/wire_chat.go - the same key
	// GET /api/worker/git-credentials verifies against, so mint and verify
	// never disagree.
	WorkerCredentialsToken func(sessionID string) string

	// ArchitectBackend serves architect-kind sessions. nil means no
	// architect backend is configured: CreateSession refuses the kind.
	ArchitectBackend Backend
	// ArchitectDefaultModel is the cold-open fallback for architect sessions
	// with an empty model, from backends.architect.default_model.
	ArchitectDefaultModel string
	// ArchitectWorkerCredentialsToken mints the per-session git-credentials
	// bearer for architect sessions, over the architect entry's api_key.
	// nil leaves the token empty.
	ArchitectWorkerCredentialsToken func(sessionID string) string
	// ArchitectPrice resolves the per-token price of a blueprint's model so
	// the start payload can carry it and the architect's cost cap binds. nil
	// or a false return leaves the price off the payload; the architect then
	// refuses capped runs. Production wires this over the card service's
	// ResolvedRate in cmd/contextmatrix/wire_chat.go.
	ArchitectPrice func(model string) (protocol.ModelPrice, bool)

	// ArchitectContextWindow returns a blueprint model's context length in
	// tokens from CM's model catalog, 0 when the catalog does not list it; the
	// architect then sizes compaction by its own default. nil sends 0.
	ArchitectContextWindow func(ctx context.Context, model string) int

	// ArchitectResearch is the research block of every architect start; nil
	// keeps research off. It carries the provider's API key: never log it.
	ArchitectResearch *protocol.ResearchStart

	// ArchitectSpend reads a blueprint's cost.spent_usd from the architect's
	// view; ok is false when the view carries none. nil disables the read on
	// an idle frame and the startup backfill.
	ArchitectSpend func(ctx context.Context, sessionID string) (spentUSD float64, ok bool, err error)
}

// Manager orchestrates chat session lifecycle, transcript persistence,
// and backend-client coordination.
type Manager struct {
	store              Store
	backend            Backend
	clk                clock.Clock
	idleTTL            time.Duration
	maxConcurrent      int
	logger             *slog.Logger
	hub                *SSEHub
	resolveProjectExec func(ctx context.Context, project string) (ProjectExecInfo, error)
	resumeBudgetTokens int
	rehydrationTimeout time.Duration
	defaultModel       string
	pricer             Pricer
	// llmEndpoint is the CM-provisioned inference endpoint configuration
	// forwarded to every chat-start payload. Treat as a secret carrier:
	// never log this value - llmEndpoint.APIKey rides along.
	llmEndpoint *protocol.LLMEndpoint
	// workerCredentialsToken mints the per-session bearer attached to every
	// chat-start payload as StartChatOpts.GitCredentialsToken. nil means "no
	// token" (no chat-backend api_key configured, or tests that don't
	// exercise this path).
	workerCredentialsToken func(sessionID string) string

	architectBackend                Backend
	architectDefaultModel           string
	architectWorkerCredentialsToken func(sessionID string) string
	architectPrice                  func(model string) (protocol.ModelPrice, bool)
	architectContextWindow          func(ctx context.Context, model string) int
	architectResearch               *protocol.ResearchStart
	architectSpend                  func(ctx context.Context, sessionID string) (float64, bool, error)

	mu           sync.Mutex
	seqMap       map[string]int64           // sessionID → last assigned seq
	titled       map[string]bool            // sessionID → auto-title work already completed
	consumers    map[string]*consumerHandle // sessionID → worker-log consumer lifecycle
	sendFailures map[string]int             // sessionID → consecutive backend-unreachable SendUserMessage failures
	// rehydrationActive mirrors chat_sessions.rehydration_active. Reads
	// from AppendMessage's hot path go through the cache to avoid an
	// extra DB round-trip per log entry; cache misses fall back to the
	// store and populate. setRehydrationActive updates both store and
	// cache atomically (under mu).
	rehydrationActive map[string]bool
	// working maps sessionID → the UTC time the in-flight turn started.
	// Absence means not working. Ephemeral indicator state: in-memory only,
	// lost on restart by design (a restarted CM has no turn in flight it
	// knows about).
	working map[string]time.Time
	wg      sync.WaitGroup

	// openGroup deduplicates concurrent cold-open work per sessionID. Two
	// callers racing to open the same id share one Backend.StartChat
	// round-trip; callers on *different* ids do not serialise behind a
	// global mutex while a slow docker pull is in flight.
	openGroup singleflight.Group

	// restartGroup shares one re-sent architect start per session between
	// the send path and the log consumer.
	restartGroup singleflight.Group

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

	// statusLocks holds a per-session mutex that every lifecycle path takes
	// around its read-modify-write of sess.Status, so concurrent transitions
	// cannot interleave. Lazily allocated like appendLocks; cleaned up in
	// DeleteSession.
	statusLocksMu sync.Mutex
	statusLocks   map[string]*sync.Mutex

	// transcriptLocks serialise an architect session's transcript writes with
	// its sends, so what the architect publishes inside /message lands after
	// the user's row. Lazily allocated; dropped in DeleteSession.
	transcriptLocksMu sync.Mutex
	transcriptLocks   map[string]*sync.Mutex

	// needLocks keep a blueprint's handed-over need to one post:
	// SendHandoffNeed holds one across its check, send and persist. Lazily
	// allocated; dropped in DeleteSession.
	needLocksMu sync.Mutex
	needLocks   map[string]*sync.Mutex

	closeOnce sync.Once

	// costCache holds the most-recently computed GetChatCostSummary result.
	// Guarded by mu. Zero value means "not yet computed."
	costCache chatCostCache
}

// NewManager constructs a Manager. Required: Store, Backend.
func NewManager(cfg Config) *Manager {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real()
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Manager{
		store:                  cfg.Store,
		backend:                cfg.Backend,
		clk:                    cfg.Clock,
		idleTTL:                cfg.IdleTTL,
		maxConcurrent:          cfg.MaxConcurrent,
		logger:                 cfg.Logger,
		hub:                    cfg.Hub,
		resolveProjectExec:     cfg.ResolveProjectExec,
		resumeBudgetTokens:     cfg.ResumeBudgetTokens,
		rehydrationTimeout:     cfg.RehydrationTimeout,
		defaultModel:           cfg.DefaultModel,
		pricer:                 cfg.Pricer,
		llmEndpoint:            cfg.LLMEndpoint,
		workerCredentialsToken: cfg.WorkerCredentialsToken,

		architectBackend:                cfg.ArchitectBackend,
		architectDefaultModel:           cfg.ArchitectDefaultModel,
		architectWorkerCredentialsToken: cfg.ArchitectWorkerCredentialsToken,
		architectPrice:                  cfg.ArchitectPrice,
		architectContextWindow:          cfg.ArchitectContextWindow,
		architectResearch:               cfg.ArchitectResearch,
		architectSpend:                  cfg.ArchitectSpend,

		seqMap:            make(map[string]int64),
		titled:            make(map[string]bool),
		consumers:         make(map[string]*consumerHandle),
		sendFailures:      make(map[string]int),
		rehydrationActive: make(map[string]bool),
		working:           make(map[string]time.Time),
		appendLocks:       map[string]*sync.Mutex{},
		statusLocks:       map[string]*sync.Mutex{},
		transcriptLocks:   map[string]*sync.Mutex{},
		needLocks:         map[string]*sync.Mutex{},
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

// transcriptLock returns the architect session's transcript mutex, creating
// it on first use. Lock order: the transcript lock, then the append lock.
func (m *Manager) transcriptLock(sessionID string) *sync.Mutex {
	m.transcriptLocksMu.Lock()
	defer m.transcriptLocksMu.Unlock()

	mu, ok := m.transcriptLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		m.transcriptLocks[sessionID] = mu
	}

	return mu
}

// needLock returns the blueprint session's need mutex, creating it on first
// use. Lock order: the need lock, then the transcript lock.
func (m *Manager) needLock(sessionID string) *sync.Mutex {
	m.needLocksMu.Lock()
	defer m.needLocksMu.Unlock()

	mu, ok := m.needLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		m.needLocks[sessionID] = mu
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
// releases it via defer, so an early return inside fn cannot leak the lock.
// Every whole-row session write runs under it; a caller that must not undo a
// racing transition also re-reads the row inside fn.
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
// NOTE: handleUsageEntry does NOT use this helper - usage events are
// synchronous, non-status publishes that need no goroutine dispatch.
func (m *Manager) publishSessionUpdate(sessionID string, u SessionUpdate) {
	if m.hub == nil {
		return
	}

	go m.hub.PublishSessionUpdate(sessionID, u)
}

// setWorking flips the in-memory assistant-working flag for sessionID and
// publishes the transition as a session_updated event. Idempotent: asserting
// the current state again neither republishes nor resets the since timestamp
// (model_request re-asserts "working" at every turn start by design). The
// publish is synchronous like handleUsageEntry's - no sessionHub lock is held
// on any caller path. Reports whether it changed the state.
func (m *Manager) setWorking(sessionID string, working bool) bool {
	m.mu.Lock()

	since, was := m.working[sessionID]
	if working == was {
		m.mu.Unlock()

		return false
	}

	if working {
		since = m.clk.Now().UTC()
		m.working[sessionID] = since
	} else {
		delete(m.working, sessionID)
	}

	m.mu.Unlock()

	if m.hub == nil {
		return true
	}

	u := SessionUpdate{AssistantWorking: &working}
	if working {
		u.AssistantWorkingSince = &since
	}

	m.hub.PublishSessionUpdate(sessionID, u)

	return true
}

// handleStatusEntry processes a run-state frame from the backend's log bridge
// (Content "working" or "idle"). Status frames are ephemeral indicator state:
// never persisted, never a transcript row. Rehydration replay must not arm
// the indicator - the first real turn starts with the user's next message.
// A blueprint's idle frame re-reads its spend off the consumer goroutine, so
// the consumer never waits on the architect.
func (m *Manager) handleStatusEntry(ctx context.Context, sessionID string, kind SessionKind, e LogEntry) {
	switch e.Content {
	case "working":
		if m.isRehydrationActive(ctx, sessionID) {
			return
		}

		m.setWorking(sessionID, true)
	case "idle":
		m.setWorking(sessionID, false)

		if kind == KindArchitect && m.architectSpend != nil {
			m.wg.Go(func() { m.readBlueprintSpend(ctx, sessionID) })
		}
	default:
		m.logger.Debug("chat: unknown status frame content",
			"session_id", sessionID, "content", e.Content)
	}
}

// decorateWorking stamps the in-memory assistant-working state onto a session
// loaded from the store, so GET/list responses carry it without persistence.
func (m *Manager) decorateWorking(sess *Session) {
	m.mu.Lock()
	since, ok := m.working[sess.ID]
	m.mu.Unlock()

	if ok {
		sess.AssistantWorking = true
		t := since
		sess.AssistantWorkingSince = &t
	}
}

// consumerHandle is the per-session lifecycle handle for a worker-log consumer
// goroutine. done is closed when the goroutine returns, so stopConsumer can
// block until the cleanup defers have executed and the consumers map is
// guaranteed clean before a subsequent startConsumer runs.
type consumerHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	// wake cuts an architect consumer's backoff short. starts counts the
	// architect starts that succeeded while this consumer ran.
	wake   chan struct{}
	starts atomic.Uint64
}

// roleFromLogType maps a backend LogEntry.Type to a chat.Role.
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
		// User echoes are produced CM-side via SendUserMessage; the backend
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

// handleUsageEntry processes a per-turn usage entry reported by the
// backend. Each frame carries per-turn (per-assistant-message) token counts -
// NOT cumulative session totals. The values are passed directly to
// IncrementSessionCost, which atomically accumulates them into the session row.
// The session row's context_tokens are also updated and a session_updated SSE
// event is published so the UI header indicator refreshes in real time.
// Errors are non-fatal - usage is a UI niceness, not a correctness property.
//
// Invoked only from the per-session worker-log consumer goroutine.
// IncrementSessionCost is atomic so any future change weakening that invariant
// cannot lose updates.
// An architect row's model is never rewritten; see rowModel.
func (m *Manager) handleUsageEntry(ctx context.Context, sessionID, project, fallbackModel string, kind SessionKind, e LogEntry) {
	if e.Usage == nil {
		return
	}

	tokens := e.Usage.InputTokens + e.Usage.CacheReadTokens + e.Usage.CacheCreateTokens

	updatedAt := e.Timestamp
	if updatedAt.IsZero() {
		updatedAt = m.clk.Now().UTC()
	}

	if err := m.store.UpdateContextTokens(ctx, sessionID, tokens, updatedAt); err != nil {
		// Session may have been deleted between the backend emitting the
		// event and CM consuming it - log at debug rather than warn.
		m.logger.Debug("chat: handleUsageEntry: update context_tokens failed",
			"session_id", sessionID, "error", err)

		return
	}

	// Each frame carries per-turn token counts - pass them directly.
	// No snapshot subtraction: the backend emits one usage block per assistant
	// message (Anthropic Messages-API semantics), not cumulative session totals.
	prompt := e.Usage.InputTokens
	completion := e.Usage.OutputTokens
	cacheRead := e.Usage.CacheReadTokens
	cacheCreate := e.Usage.CacheCreateTokens

	// Resolve model: prefer the frame's model, else the session's fallback.
	model := e.Model
	if model == "" {
		model = fallbackModel
	}

	// The gateway's reported cost wins; the rate table prices only frames
	// without one, so the unknown-model signal counts table-priced frames.
	cost := e.Usage.CostUSD
	// The protocol says cost is never negative; a bad frame must not lower the total.
	if cost < 0 {
		cost = 0
	}

	if cost == 0 && m.pricer != nil {
		c, known := m.pricer.PriceTokens(model, prompt, cacheRead, cacheCreate, completion)
		if !known {
			m.logger.Warn("chat: handleUsageEntry: unknown model, cost recorded as $0",
				"session_id", sessionID, "model", model)
			metrics.ChatUsageUnknownModelTotal.WithLabelValues(model).Inc()
			// cost stays 0; token counters still advance.
		} else {
			cost = c
		}
	}

	// A blueprint's model is fixed once its first open persists it: it is the
	// next reopen's start model and price key, so a frame from a persona
	// override must not move it. An empty model keeps the column. Chat rows
	// take the echoed name.
	rowModel := model
	if kind == KindArchitect {
		rowModel = ""
	}

	newPrompt, newCompletion, newCacheRead, newCacheCreation, newCost, err := m.store.IncrementSessionCost(ctx, sessionID, prompt, completion, cacheRead, cacheCreate, cost, rowModel)
	if err != nil {
		m.logger.Debug("chat: handleUsageEntry: increment session cost failed",
			"session_id", sessionID, "error", err)

		return // Do NOT publish SessionUpdate - mirrors UpdateContextTokens early-return.
	}

	// Cost may be $0 (unknown model) - added anyway, matching the persisted
	// value; tokens skip zero kinds so no empty series appear.
	metrics.ChatCostUSDTotal.WithLabelValues(project, model).Add(cost)

	kinds := []struct {
		kind   string
		tokens int64
	}{
		{"prompt", prompt},
		{"completion", completion},
		{"cache_read", cacheRead},
		{"cache_creation", cacheCreate},
	}
	for _, k := range kinds {
		if k.tokens > 0 {
			metrics.ChatTokensTotal.WithLabelValues(project, model, k.kind).Add(float64(k.tokens))
		}
	}

	if m.hub != nil {
		m.hub.PublishSessionUpdate(sessionID, SessionUpdate{
			ContextTokens:          tokens,
			ContextTokensUpdatedAt: updatedAt,
			Model:                  model,
			EstimatedCostUSD:       newCost,
			PromptTokens:           newPrompt,
			CompletionTokens:       newCompletion,
			CacheReadTokens:        newCacheRead,
			CacheCreationTokens:    newCacheCreation,
		})
	}
}

// startConsumer ensures a goroutine is bridging backend /logs for sessionID
// into AppendMessage + hub.Publish. restart makes an architect consumer
// re-send the start before its first stream, for a consumer that replaces one
// CM lost. Idempotent: subsequent calls for the same session are no-ops while
// a consumer is already running.
func (m *Manager) startConsumer(sessionID string, restart bool) {
	m.mu.Lock()

	if _, ok := m.consumers[sessionID]; ok {
		m.mu.Unlock()

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	handle := &consumerHandle{cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	m.consumers[sessionID] = handle
	m.mu.Unlock()

	m.wg.Go(func() {
		// close(done) runs LAST so stopConsumer's <-done blocks until the
		// map-entry cleanup has executed; that guarantees a follow-on
		// startConsumer sees a clean slate and is not defeated by a stale
		// entry.
		defer close(handle.done)
		defer func() {
			m.setWorking(sessionID, false)

			m.mu.Lock()
			// Defensive identity check: stopConsumer may have already removed
			// the entry. Only delete if we still own it.
			if cur, ok := m.consumers[sessionID]; ok && cur == handle {
				delete(m.consumers, sessionID)
			}
			m.mu.Unlock()
		}()

		// Resolve the session once: the project for metric labels and the
		// model a usage frame without one is priced at. The frame's own
		// model wins; else the row's model (persisted when the session opens);
		// else the chat default.
		project := "none"
		kind := KindChat
		fallbackModel := m.defaultModel

		if sess, err := m.store.GetSession(ctx, sessionID); err == nil {
			if sess.Project != "" {
				project = sess.Project
			}

			kind = sess.Kind

			if sess.Model != "" {
				fallbackModel = sess.Model
			}
		}

		backend, err := m.backendFor(kind)
		if err != nil {
			m.logger.Error("chat: worker-log consumer has no backend", "session_id", sessionID, "error", err)

			return
		}

		onEntry := func(e LogEntry) {
			if e.Type == "status" {
				m.handleStatusEntry(ctx, sessionID, kind, e)

				return
			}

			if e.Type == "usage" {
				m.handleUsageEntry(ctx, sessionID, project, fallbackModel, kind, e)

				return
			}

			role := roleFromLogType(e.Type)
			if role == "" {
				return
			}

			// Waits out an architect send in flight: what the architect
			// published inside /message belongs after the user's row.
			if kind == KindArchitect {
				tl := m.transcriptLock(sessionID)
				tl.Lock()
				defer tl.Unlock()
			}

			content, dataKind := e.Content, ""

			// A marker is checked first, so a marker frame never becomes a prompt row.
			switch {
			case kind == KindArchitect && e.Marker != nil:
				env, err := markerContent(e.Content, e.Marker)
				if err == nil {
					content, dataKind = env, EventKindMarker
				} else {
					m.logger.Warn("chat: architect marker stored as plain text",
						"session_id", sessionID, "error", err)
				}
			case kind == KindArchitect && e.Type == "text" && e.Prompt != nil:
				env, err := promptContent(e.Content, e.Prompt)
				if err == nil {
					content, dataKind = env, EventKindPrompt
				} else {
					m.logger.Warn("chat: architect prompt stored as plain text",
						"session_id", sessionID, "error", err)
				}
			}

			msg, err := m.appendMessageWithKind(ctx, sessionID, role, content, dataKind)
			if err != nil {
				m.logger.Warn("chat: consumer AppendMessage failed",
					"session_id", sessionID, "type", e.Type, "error", err)

				return
			}

			if m.hub != nil {
				m.hub.Publish(sessionID, SSEEvent{
					Seq:              msg.Seq,
					Role:             role,
					Content:          content,
					DataKind:         dataKind,
					RehydrationPhase: msg.RehydrationPhase,
				})
			}
		}

		m.logger.Info("chat: worker-log consumer started", "session_id", sessionID)

		const (
			backoffBase = 250 * time.Millisecond
			backoffCap  = 30 * time.Second
			// A stream that lived this long was healthy; the next failure
			// starts the backoff again from the base.
			healthyStream = time.Minute
		)

		attempt := 0

		wait := func(err error) bool {
			attempt++

			shift := min(attempt-1, 16)
			backoff := min(time.Duration(float64(backoffBase)*float64(int64(1)<<shift)), backoffCap)

			m.logger.Warn("chat: worker-log consumer stream error, retrying",
				"session_id", sessionID, "attempt", attempt, "backoff", backoff, "error", err)

			select {
			case <-ctx.Done():
				return false
			case <-time.After(backoff):
				return true
			case <-handle.wake:
				// A successful restart proves the architect is live.
				attempt = 0

				return true
			}
		}

		// An architect that restarted no longer holds the session and its
		// /logs answers any id, so the start goes out before every stream
		// that follows a lost one.
		pendingStart := restart && kind == KindArchitect
		lost := handle.starts.Load()

		for {
			if ctx.Err() != nil {
				m.logger.Info("chat: worker-log consumer stopped", "session_id", sessionID)

				return
			}

			// A start that succeeded since the stream was lost already
			// restored the session. Re-sending it in the same second would
			// repeat a signed request and the replay cache would reject it.
			if pendingStart && handle.starts.Load() != lost {
				pendingStart = false
			}

			if pendingStart {
				if err := m.restartArchitect(ctx, sessionID); err != nil {
					if errors.Is(err, errArchitectNotLive) || errors.Is(err, ErrSessionNotFound) ||
						ctx.Err() != nil || !wait(err) {
						m.logger.Info("chat: worker-log consumer stopped", "session_id", sessionID)

						return
					}

					continue
				}

				pendingStart = false
			}

			began := time.Now()
			err := backend.StreamLogs(ctx, sessionID, onEntry)

			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				m.logger.Info("chat: worker-log consumer stopped", "session_id", sessionID)

				return
			}

			if err == nil && kind != KindArchitect {
				// A chat backend closes the stream when the session ended.
				m.logger.Info("chat: worker-log consumer stopped", "session_id", sessionID)

				return
			}

			if kind == KindArchitect {
				pendingStart = true
				lost = handle.starts.Load()

				if err == nil {
					err = errors.New("architect closed the log stream")
				}
			}

			if time.Since(began) >= healthyStream {
				attempt = 0
			}

			if !wait(err) {
				m.logger.Info("chat: worker-log consumer stopped", "session_id", sessionID)

				return
			}
		}
	})
}

// stopConsumer cancels the worker-log consumer for sessionID and blocks until
// the goroutine has exited. Synchronous teardown is required so a fast Reopen
// after EndSession is guaranteed to start a fresh consumer - an asynchronous
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

// Close cancels every active worker-log consumer goroutine and waits for them
// to exit. The supplied ctx acts as a deadline: if consumers have not all
// stopped by ctx.Done(), Close returns an error wrapping ctx.Err(). Idempotent
// - subsequent calls are no-ops and return nil.
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

// backendFor returns the backend that serves sessions of kind. An empty kind
// is a chat session (rows written before kinds existed read back as chat).
func (m *Manager) backendFor(kind SessionKind) (Backend, error) {
	switch kind {
	case KindChat, "":
		return m.backend, nil
	case KindArchitect:
		if m.architectBackend == nil {
			return nil, ErrKindUnavailable
		}

		return m.architectBackend, nil
	default:
		return nil, fmt.Errorf("%w: unknown session kind %q", ErrInvalidSessionInput, kind)
	}
}

// CreateInput is the user-facing payload for creating a new session.
type CreateInput struct {
	Title     string
	Project   string
	CreatedBy string
	Model     string
	// Kind selects the backend; empty means KindChat.
	Kind SessionKind
	// Projects, Mode, CostCapUSD, PlainLanguage and Unattended apply to KindArchitect only.
	Projects      []string
	Mode          string
	CostCapUSD    float64
	PlainLanguage bool
	Unattended    bool
	// OriginChat and OriginSeq name the chat and its handoff row this
	// blueprint starts from; KindArchitect only.
	OriginChat string
	OriginSeq  int64
}

// CreateSession inserts a cold-state session row. The container is
// not started until OpenSession is called.
func (m *Manager) CreateSession(ctx context.Context, in CreateInput) (Session, error) {
	kind := in.Kind
	if kind == "" {
		kind = KindChat
	}

	var (
		workspace     []string
		mode          string
		costCap       float64
		plainLanguage bool
		unattended    bool
	)

	switch kind {
	case KindChat:
	case KindArchitect:
		if m.architectBackend == nil {
			return Session{}, ErrKindUnavailable
		}

		if in.Project != "" {
			return Session{}, fmt.Errorf("%w: a blueprint session takes projects, not project", ErrInvalidSessionInput)
		}

		if len(in.Projects) == 0 {
			return Session{}, fmt.Errorf("%w: a blueprint session needs at least one project", ErrInvalidSessionInput)
		}

		for _, p := range in.Projects {
			if m.resolveProjectExec == nil {
				continue
			}

			if _, err := m.resolveProjectExec(ctx, p); err != nil {
				// The lookup error can name a filesystem path; it is logged
				// here and kept out of the 400 details.
				m.logger.Warn("chat: CreateSession: project lookup failed", "project", p, "error", err)

				return Session{}, fmt.Errorf("%w: unknown project %q", ErrInvalidSessionInput, p)
			}
		}

		mode = in.Mode
		if mode == "" {
			mode = protocol.ArchitectModeDefault
		}

		if !protocol.ValidArchitectMode(mode) {
			return Session{}, fmt.Errorf("%w: unknown mode %q", ErrInvalidSessionInput, in.Mode)
		}

		if in.CostCapUSD < 0 {
			return Session{}, fmt.Errorf("%w: cost cap must not be negative", ErrInvalidSessionInput)
		}

		costCap = in.CostCapUSD
		plainLanguage = in.PlainLanguage
		unattended = in.Unattended
		workspace = slices.Clone(in.Projects)
	default:
		return Session{}, fmt.Errorf("%w: unknown session kind %q", ErrInvalidSessionInput, in.Kind)
	}

	if err := m.checkOrigin(ctx, kind, in); err != nil {
		return Session{}, err
	}

	now := m.clk.Now().UTC().Truncate(time.Second)

	sess := Session{
		ID:            NewID(),
		Title:         in.Title,
		Project:       in.Project,
		Kind:          kind,
		Mode:          mode,
		CostCapUSD:    costCap,
		PlainLanguage: plainLanguage,
		Unattended:    unattended,
		Workspace:     workspace,
		Status:        StatusCold,
		CreatedAt:     now,
		LastActive:    now,
		CreatedBy:     in.CreatedBy,
		Model:         in.Model,
		OriginChat:    in.OriginChat,
		OriginSeq:     in.OriginSeq,
	}
	if err := m.store.CreateSession(ctx, sess); err != nil {
		return Session{}, fmt.Errorf("chat: CreateSession: %w", err)
	}

	return sess, nil
}

// buildResume loads the prior transcript and returns a ResumeContext for the
// backend, or nil when there's nothing worth resuming (fresh session, all
// messages filtered, or a DB error - the last case is logged and degrades to
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

// resumeTurnCount is a small helper for structured logging - returns the
// number of turns in a Resume, or 0 when nil.
func resumeTurnCount(r *ResumeContext) int {
	if r == nil {
		return 0
	}

	return len(r.Turns)
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
// trivial cache write - never across the SQL UPDATE - so a slow fsync on
// one session does not stall every unrelated session's AppendMessage
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

// ClearContext clears the worker's working memory in place: sends "/clear"
// (the worker re-orients its next epoch from its own embedded primer), marks
// every prior transcript row with rehydration_phase=true so future
// rehydration payloads skip them, and appends a system-role divider row
// (content=ContextClearedMarker, kind=EventKindDivider) that the UI
// renders as a horizontal rule.
//
// Concurrent ClearContext calls for the same session are serialised via
// clearGroup (singleflight): the first caller executes the body and all
// concurrent callers on the same session share the result.
//
// Failure semantics: a failure in the backend /clear call wraps ErrBackendSend
// so the API layer maps to 502. On backend failure we abort before marking the
// transcript or appending the divider - the transcript stays consistent with
// the runtime.
func (m *Manager) ClearContext(ctx context.Context, sessionID string) error {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("chat: ClearContext: %w", err)
	}

	// A blueprint worker would receive "/clear" as a user message.
	if sess.Kind == KindArchitect {
		return fmt.Errorf("%w: clear context is not supported for blueprint sessions", ErrInvalidSessionInput)
	}

	// Only active and warm-idle sessions have a live worker container. A cold
	// or ending session has nothing to /clear, so we fail fast here rather
	// than letting the backend call time out or produce a confusing error.
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
// Kept apart from appendMessageWithKind: ClearTranscriptAtomic marks and inserts in one transaction.
func (m *Manager) doClearContext(ctx context.Context, sessionID string) error {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("chat: ClearContext: %w", err)
	}

	backend, err := m.backendFor(sess.Kind)
	if err != nil {
		return fmt.Errorf("chat: ClearContext: %w", err)
	}

	clearMsgID := NewID()
	if err := backend.SendChatMessage(ctx, protocol.MessagePayload{SessionID: sessionID, Content: "/clear", MessageID: clearMsgID}); err != nil {
		return fmt.Errorf("chat: ClearContext: /clear: %w: %w", ErrBackendSend, err)
	}

	// Build the divider message under the per-session append lock so the
	// seq is assigned atomically with the transcript mark + INSERT. The
	// ClearTranscriptAtomic call wraps both operations in a single SQL
	// transaction, preventing partial-failure states (rows marked but no
	// divider inserted, or vice-versa).
	sl := m.appendLock(sessionID)
	sl.Lock()

	// Seed the in-memory counter without holding m.mu across the MaxSeq
	// SQL round-trip - m.mu is the same mutex that gates seqMap reads on
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
		// raced - the per-session appendLock is held.
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

	// Detach persistence from the caller context so a cancelled HTTP
	// request cannot race a committed autocommit INSERT in modernc.org/sqlite.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer persistCancel()

	markedCount, msg, err := m.store.ClearTranscriptAtomic(persistCtx, sessionID, divider)
	if err != nil {
		m.reseedSeq(ctx, sessionID, "ClearContext")

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
		"marked_count", markedCount)

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
		// Idempotent for already-active sessions. Also ensure the worker-log
		// consumer is bridging /logs back into the SSE hub: a CM restart
		// strands that goroutine while the row stays active, so reopening
		// restarts the consumer in place instead of forcing an End → Reopen
		// that kills the container and rehydrates a fresh one. startConsumer
		// is a no-op when a consumer for this session is already running.
		m.startConsumer(sess.ID, true)

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
			// starting a consumer - the caller will observe the real state.
			return reattached, nil
		}

		m.logger.Info("chat: warm-idle reattached", "session_id", reattached.ID)
		m.startConsumer(reattached.ID, true)

		s := StatusActive
		m.publishSessionUpdate(reattached.ID, SessionUpdate{Status: &s})

		return reattached, nil

	case StatusCold:
		// Route the cold-start path through singleflight keyed on
		// sessionID so concurrent callers for the same id share one
		// Backend.StartChat round-trip, and callers for *different* ids
		// do not serialise on a global mutex behind a slow docker
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
// same id share one Backend.StartChat round-trip; callers for *different*
// ids do not serialise on a global lock when MaxConcurrent is 0.
//
// The MaxConcurrent ceiling is enforced via the in-memory pendingActive
// reservation counter: a slot is reserved under openLimitMu BEFORE the
// slow StartChat call and decremented in a deferred cleanup once the
// outcome is known. openLimitMu is held only for the brief count +
// increment / decrement windows, never across StartChat or any other
// I/O, so concurrent cold opens on different sessions do not
// serialise at backend-latency timescale.
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

	// Architect sessions run no container and are not counted (see
	// CountSessionsByStatus).
	if m.maxConcurrent > 0 && sess.Kind != KindArchitect {
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

	backend, err := m.backendFor(sess.Kind)
	if err != nil {
		return Session{}, fmt.Errorf("chat: start container: %w", err)
	}

	containerID, err := backend.StartChat(ctx, opts)
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
			m.rollbackContainer(ctx, "status drift during StartChat", sess.Kind, sess.ID, containerID, nil)

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
			m.rollbackContainer(ctx, "persist active failure", sess.Kind, sess.ID, containerID, err)

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
		// permanently - they will be excluded from future resume payloads
		// by transcript.Build. In practice this window spans a single store
		// write and no user-driven AppendMessage can race here: the
		// worker-log consumer goroutine is only spawned after a successful
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

			m.rollbackContainer(ctx, "rehydration persist failure", sess.Kind, sess.ID, containerID, err)

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
	m.startConsumer(sess.ID, false)

	s := StatusActive
	m.publishSessionUpdate(sess.ID, SessionUpdate{Status: &s})

	return sess, nil
}

// waitPastSecond blocks until the manager clock is past the Unix second of t,
// or ctx ends.
func (m *Manager) waitPastSecond(ctx context.Context, t time.Time) error {
	d := time.Unix(t.Unix()+1, 0).Sub(m.clk.Now())
	if d <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait before architect re-send: %w", ctx.Err())
	case <-m.clk.After(d):
		return nil
	}
}

// restartArchitect re-sends /chat/start for an active or warm-idle architect
// session the architect lost. It sends the plain cold-open inputs, never a
// resume, and does not write the row; the architect no-ops a start for a
// session it holds. A row that is not a live architect session returns
// errArchitectNotLive. The status lock keeps the start from landing after
// EndSession's EndChat.
func (m *Manager) restartArchitect(ctx context.Context, sessionID string) error {
	_, err, _ := m.restartGroup.Do(sessionID, func() (any, error) {
		if err := m.withStatusLock(sessionID, func() error {
			sess, err := m.store.GetSession(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("chat: restart architect: %w", err)
			}

			if sess.Kind != KindArchitect || (sess.Status != StatusActive && sess.Status != StatusWarmIdle) {
				return errArchitectNotLive
			}

			opts, err := m.coldPrep(ctx, sess)
			if err != nil {
				return fmt.Errorf("chat: restart architect: %w", err)
			}

			backend, err := m.backendFor(sess.Kind)
			if err != nil {
				return fmt.Errorf("chat: restart architect: %w", err)
			}

			if _, err := backend.StartChat(ctx, opts); err != nil {
				return fmt.Errorf("chat: restart architect: %w", err)
			}

			m.logger.Info("chat: architect session restarted in place", "session_id", sessionID)

			return nil
		}); err != nil {
			return nil, err
		}

		// After the status lock is released; m.mu is held only for the map lookup.
		m.mu.Lock()
		if h, ok := m.consumers[sessionID]; ok {
			h.starts.Add(1)
		}
		m.mu.Unlock()

		return nil, nil
	})

	return err
}

// wakeConsumer cuts the session's consumer backoff short. Only the send path
// calls it: a wake from restartArchitect would leave the consumer's own
// restarts a token that shortens every later backoff, a spin on a stream that
// keeps ending.
func (m *Manager) wakeConsumer(sessionID string) {
	m.mu.Lock()
	h, ok := m.consumers[sessionID]
	m.mu.Unlock()

	if !ok {
		return
	}

	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// coldPrep gathers the inputs needed by Backend.StartChat for a cold open.
// Returns the options struct and an error if any preparatory step failed.
func (m *Manager) coldPrep(ctx context.Context, sess Session) (StartChatOpts, error) {
	if sess.Kind == KindArchitect {
		model := sess.Model
		if model == "" {
			model = m.architectDefaultModel
		}

		var token string
		if m.architectWorkerCredentialsToken != nil {
			token = m.architectWorkerCredentialsToken(sess.ID)
		}

		m.logger.Info("chat: opening cold blueprint session",
			"session_id", sess.ID, "projects", sess.Workspace, "mode", sess.Mode, "plain_language", sess.PlainLanguage, "unattended", sess.Unattended, "model", model,
			"research", m.architectResearch != nil)

		start := StartChatOpts{
			SessionID:           sess.ID,
			Model:               model,
			LLMEndpoint:         m.llmEndpoint,
			GitCredentialsToken: token,
			Architect: &protocol.ArchitectStart{
				Projects:      sess.Workspace,
				Mode:          sess.Mode,
				CostCapUSD:    sess.CostCapUSD,
				PlainLanguage: sess.PlainLanguage,
				Unattended:    sess.Unattended,
				Research:      m.architectResearch,
			},
		}

		if m.architectPrice != nil {
			if price, ok := m.architectPrice(model); ok {
				start.Architect.Price = &price
			} else {
				m.logger.Warn("chat: blueprint model has no price, the architect cannot bind its cost cap",
					"session_id", sess.ID, "model", model)
			}
		}

		if m.architectContextWindow != nil {
			start.Architect.ContextWindow = m.architectContextWindow(ctx, model)
		}

		return start, nil
	}

	var execInfo ProjectExecInfo

	if sess.Project != "" && m.resolveProjectExec != nil {
		var err error

		execInfo, err = m.resolveProjectExec(ctx, sess.Project)
		if err != nil {
			return StartChatOpts{}, fmt.Errorf("chat: resolve project exec for %q: %w", sess.Project, err)
		}
	}

	// Build the rehydration payload from the persisted transcript.
	// Errors here are non-fatal - fall back to "no resume" so we
	// never block the user from opening the chat.
	resume := m.buildResume(ctx, sess.ID)

	model := sess.Model
	if model == "" {
		model = m.defaultModel
	}

	// Mint the worker git-credentials bearer for this cold open. nil
	// workerCredentialsToken (no chat-backend api_key configured) leaves the
	// token empty, and the chat backend rejects the start fail-closed.
	var gitCredentialsToken string
	if m.workerCredentialsToken != nil {
		gitCredentialsToken = m.workerCredentialsToken(sess.ID)
	}

	m.logger.Info("chat: opening cold session",
		"session_id", sess.ID, "project", sess.Project, "repo_url", execInfo.RepoURL,
		"worker_image", execInfo.WorkerImage,
		"model", model, "has_resume", resume != nil,
		"resume_turn_count", resumeTurnCount(resume))

	return StartChatOpts{
		SessionID:           sess.ID,
		Project:             sess.Project,
		RepoURL:             execInfo.RepoURL,
		WorkerImage:         execInfo.WorkerImage,
		Model:               model,
		Resume:              resume,
		LLMEndpoint:         m.llmEndpoint,
		GitCredentialsToken: gitCredentialsToken,
	}, nil
}

// rollbackContainer ends the worker container started during a cold open and
// logs the rollback reason. Should be called when openCold fails after the
// container has been provisioned.
func (m *Manager) rollbackContainer(_ context.Context, reason string, kind SessionKind, sessID, containerID string, opErr error) {
	backend, err := m.backendFor(kind)
	if err != nil {
		m.logger.Warn("chat: rollback has no backend",
			"reason", reason, "session_id", sessID, "container_id", containerID, "error", err)

		return
	}

	if rbErr := backend.EndChat(context.Background(), sessID); rbErr != nil {
		m.logger.Warn("chat: rollback EndChat failed",
			"reason", reason,
			"session_id", sessID,
			"container_id", containerID,
			"op_error", opErr,
			"error", rbErr)
	}
}

// Timeouts for detached persistence and seq reseed on ambiguous failures.
// Persistence runs detached from the caller's context so a cancelled HTTP
// request cannot race a committed autocommit INSERT; reseed reads MAX(seq)
// from the store to heal the in-memory counter when a write outcome is
// uncertain.
const (
	reseedTimeout  = 10 * time.Second
	persistTimeout = 30 * time.Second
)

// reseedSeq re-reads MAX(seq) after a failed write and reseeds the in-memory
// counter from disk instead of decrementing it. Callers hold the session's
// append lock, so MAX(seq) is authoritative. The row may have committed
// despite the error: a decrement could land behind disk and collide on
// UNIQUE(session_id, seq) and wedge every later append. A gap is harmless.
func (m *Manager) reseedSeq(ctx context.Context, sessionID, op string) {
	reseedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reseedTimeout)
	defer cancel()

	maxSeq, err := m.store.MaxSeq(reseedCtx, sessionID)
	if err != nil {
		m.logger.Warn("chat: "+op+" failed and seq reseed failed; counter left ahead of disk",
			"session_id", sessionID, "error", err)

		return
	}

	m.mu.Lock()
	m.seqMap[sessionID] = maxSeq
	m.mu.Unlock()
}

// maxMessageBytes caps a single persisted transcript entry. Verbose tool
// output (e.g. a tool_result containing a large file dump) would otherwise
// grow the transcript store linearly without bound. The cap is for
// backend-emitted rows; user rows are bounded at the send and stored whole.
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
// Message.Kind on the persisted row: the log consumer's prompt and marker
// rows, and the send path's round chat rows.
func (m *Manager) appendMessageWithKind(ctx context.Context, sessionID string, role Role, content, kind string) (Message, error) {
	// promptContent bounds a prompt row itself, a round row may exceed
	// maxMessageBytes, a handoff row is bounded by PostHandoff, and every
	// user row passed the send limit.
	if kind != EventKindPrompt && kind != EventKindHandoff && role != RoleUser {
		content = truncateMessageContent(content)
	}

	phase := m.isRehydrationActive(ctx, sessionID)

	// Auto-title: if this is the first user message on a still-untitled session,
	// derive a title from the content (50-byte truncation with ellipsis). The
	// `titled` cache skips the SELECT+UPDATE round-trip once we've confirmed a
	// title exists for the session.
	//
	// Use store.UpdateSessionTitle - a targeted UPDATE that touches only
	// the title column - rather than GetSession + UpdateSession. A racing
	// OpenSession/MarkActive between the read and a wholesale UPDATE would
	// otherwise see its just-flipped ContainerID/Status/Workspace
	// overwritten by the stale pre-flip snapshot loaded here.
	if role == RoleUser && kind != EventKindCommand {
		m.mu.Lock()
		alreadyTitled := m.titled[sessionID]
		m.mu.Unlock()

		if !alreadyTitled {
			sess, err := m.store.GetSession(ctx, sessionID)
			if err == nil {
				if sess.Title == "" {
					title := content
					// Truncate at rune boundary, not byte boundary - slicing
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
	// One slow fsync on session A does not stall appends to session B.
	// SQLite serialises writes at the engine level and the
	// UNIQUE(session_id, seq) index is the final correctness guarantor.
	sl := m.appendLock(sessionID)
	sl.Lock()
	defer sl.Unlock()

	// seqMap is shared across sessions so the seq-assign window must take
	// m.mu briefly. The store write below runs outside m.mu - only the
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

	// Detach persistence from the caller context so a cancelled HTTP
	// request cannot race a committed autocommit INSERT in modernc.org/sqlite.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer persistCancel()

	if _, err := m.store.AppendMessage(persistCtx, msg); err != nil {
		m.reseedSeq(ctx, sessionID, "append")

		return Message{}, fmt.Errorf("chat: append: %w", err)
	}

	return msg, nil
}

// Reattach ensures the worker-log consumer is running for an active or
// warm-idle session and refreshes its LastActive timestamp so the idle
// reaper doesn't end it while the user is interacting with it. No-op on
// cold and ending sessions, which have no live worker container to
// bridge to.
//
// Status is intentionally left untouched; lifecycle promotion is
// MarkActive's job, called separately from OnSubscribe.
//
// Idempotent and safe for concurrent callers - startConsumer guards
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

	// Re-read under the status lock so a racing EndSession's cold row is never
	// overwritten by the snapshot above.
	if err := m.withStatusLock(sessionID, func() error {
		fresh, err := m.store.GetSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("chat: Reattach re-read: %w", err)
		}

		sess = fresh

		if fresh.Status != StatusActive && fresh.Status != StatusWarmIdle {
			return nil
		}

		sess.LastActive = m.clk.Now().UTC().Truncate(time.Second)
		if err := m.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("chat: Reattach: persist last-active: %w", err)
		}

		return nil
	}); err != nil {
		return Session{}, err
	}

	if sess.Status != StatusActive && sess.Status != StatusWarmIdle {
		return sess, nil
	}

	m.startConsumer(sess.ID, true)

	return sess, nil
}

// MarkWarmIdle transitions an active session to warm-idle. No-op if the
// session is not active. Tolerant of ErrSessionNotFound - a grace timer
// fired against a session that was already deleted is a benign race, not an
// error.
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
// is not warm-idle. Tolerant of ErrSessionNotFound - the same benign-race
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

// GetSession returns the persisted session by ID, decorated with the
// in-memory assistant-working state.
func (m *Manager) GetSession(ctx context.Context, id string) (Session, error) {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return Session{}, err
	}

	m.decorateWorking(&sess)

	return sess, nil
}

// SessionLiveness reports a session's kind and whether it is attached to its
// backend: a chat session that owns a worker container, or a blueprint whose
// architect session is open. active, warm-idle and ending are live; cold is
// not. An unknown id returns ErrSessionNotFound so the worker git-credentials
// endpoint can distinguish "does not exist" (404) from "exists but cold" (409).
func (m *Manager) SessionLiveness(ctx context.Context, sessionID string) (kind SessionKind, live bool, err error) {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", false, err
	}

	switch sess.Status {
	case StatusActive, StatusWarmIdle, StatusEnding:
		return sess.Kind, true, nil
	default:
		return sess.Kind, false, nil
	}
}

// EndSession transitions a session to cold, stopping the worker container.
// Idempotent on already-cold sessions and re-entrant against status=ending
// rows (which can result from a prior partial failure). Backend teardown and
// consumer stop are both idempotent, so calling EndSession on a wedged
// ending row safely completes the transition in a single store write.
//
// The per-session statusLock is acquired BEFORE the Backend.EndChat call and
// held across the cold persist. A racing OpenSession that gets a Status
// read between Backend.EndChat succeeding and the persist could otherwise
// observe status=active/warm-idle and reattach to a now-dead container.
// Holding statusLock across Backend.EndChat blocks racing status reads for
// the duration of an HTTP round-trip - acceptable cost given the
// transition is a one-shot terminal state.
func (m *Manager) EndSession(ctx context.Context, id string) error {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("chat: EndSession: %w", err)
	}

	if sess.Status == StatusCold {
		// Ending an already-cold session is a no-op.
		return nil
	}

	m.logger.Info("chat: ending session", "session_id", sess.ID,
		"from_status", string(sess.Status))

	// Stop the consumer outside the statusLock: it takes that lock to re-send
	// an architect start, and stopConsumer waits for it to exit.
	m.stopConsumer(sess.ID)

	if err := m.withStatusLock(sess.ID, func() error {
		// EndChat is idempotent on the backend side; re-entry from a
		// status=ending row is safe. Held under statusLock so no racing
		// OpenSession can re-read sess.Status=active/warm-idle and
		// reattach to the now-dead container.
		if backend, err := m.backendFor(sess.Kind); err != nil {
			m.logger.Warn("chat: no backend for session kind, marking cold anyway",
				"session_id", sess.ID, "error", err)
		} else if err := backend.EndChat(ctx, sess.ID); err != nil {
			m.logger.Warn("chat: backend end failed, marking cold anyway",
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
	// per-session statusLock - we MUST call it AFTER withStatusLock above
	// has released, otherwise sync.Mutex (non-reentrant) would deadlock.
	// Only include RehydrationActive in the SSE event if the persist
	// succeeded - if it failed the on-disk row still has
	// rehydration_active=true, and reporting false would contradict it.
	rehyErr := m.setRehydrationActive(ctx, sess.ID, false)
	if rehyErr != nil {
		m.logger.Warn("chat: EndSession: clear rehydration flag failed",
			"session_id", sess.ID, "error", rehyErr)
	}

	m.setWorking(sess.ID, false)

	// A cold session publishes nothing until it reopens, so its replay events
	// would otherwise sit in memory for good.
	if m.hub != nil {
		m.hub.ClearReplay(sess.ID)
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

// ListSessions returns sessions matching the filter, newest-active first,
// decorated with the in-memory assistant-working state.
func (m *Manager) ListSessions(ctx context.Context, f SessionFilter) ([]Session, error) {
	sessions, err := m.store.ListSessions(ctx, f)
	if err != nil {
		return nil, err
	}

	for i := range sessions {
		m.decorateWorking(&sessions[i])
	}

	return sessions, nil
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
	delete(m.sendFailures, id)
	delete(m.working, id)
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

	m.transcriptLocksMu.Lock()
	delete(m.transcriptLocks, id)
	m.transcriptLocksMu.Unlock()

	m.needLocksMu.Lock()
	delete(m.needLocks, id)
	m.needLocksMu.Unlock()

	return nil
}

// ensureRunningForSend opens a cold session and promotes a warm-idle one before a send.
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

// maxConsecutiveSendFailures is the number of consecutive
// backend-unreachable SendUserMessage failures tolerated before a session
// is auto-flipped to cold. This is the only self-healing path for an
// unreachable chat backend.
const maxConsecutiveSendFailures = 3

// recordSendFailure increments the consecutive-failure counter for
// sessionID when sendErr indicates the backend is unreachable. Reaching
// maxConsecutiveSendFailures flips the session to cold via EndSession,
// which preserves the transcript - the next SendUserMessage goes through
// ensureRunningForSend's StatusCold branch and triggers a cold-open resume.
func (m *Manager) recordSendFailure(ctx context.Context, sessionID string, sendErr error) {
	if !errors.Is(sendErr, ErrBackendUnreachable) {
		return
	}

	m.mu.Lock()
	m.sendFailures[sessionID]++
	n := m.sendFailures[sessionID]
	m.mu.Unlock()

	if n < maxConsecutiveSendFailures {
		return
	}

	m.logger.Warn("chat: backend unreachable on consecutive sends, auto-flipping session to cold",
		"session_id", sessionID, "consecutive_failures", n)

	m.mu.Lock()
	delete(m.sendFailures, sessionID)
	m.mu.Unlock()

	if endErr := m.EndSession(ctx, sessionID); endErr != nil {
		m.logger.Warn("chat: auto-recovery EndSession failed", "session_id", sessionID, "error", endErr)
	}
}

// clearSendFailures resets the consecutive-failure counter after a
// successful send.
func (m *Manager) clearSendFailures(sessionID string) {
	m.mu.Lock()
	delete(m.sendFailures, sessionID)
	m.mu.Unlock()
}

// endRehydrationForSend ends the session's rehydration phase before a user
// message is sent, as a safety net for agents that forget to call
// chat_rehydration_complete. It runs before the row is appended so the
// message itself is persisted as non-phase.
func (m *Manager) endRehydrationForSend(ctx context.Context, sessionID string) {
	if !m.isRehydrationActive(ctx, sessionID) {
		return
	}

	if err := m.setRehydrationActive(ctx, sessionID, false); err != nil {
		m.logger.Warn("chat: SendUserMessage: clear rehydration flag failed",
			"session_id", sessionID, "error", err)
	} else {
		m.logger.Info("chat: rehydration ended by user message",
			"session_id", sessionID)
	}
}

// reopenCold ends a chat session its backend no longer holds and opens it
// again through the cold-open resume.
func (m *Manager) reopenCold(ctx context.Context, sessionID string) error {
	m.logger.Warn("chat: backend does not hold the session, reopening it cold", "session_id", sessionID)

	if err := m.EndSession(ctx, sessionID); err != nil {
		return err
	}

	if _, err := m.OpenSession(ctx, sessionID); err != nil {
		return err
	}

	m.endRehydrationForSend(ctx, sessionID)

	return nil
}

// SendUserMessage forwards a user message to the backend first; only on a
// successful backend call is the message persisted and fanned out via the
// SSE hub, so a failed send leaves no row and the UI can retry. Cold-state
// sessions are opened first. Returns the generated message_id used for
// backend-side echo dedup.
//
// A backend that no longer holds the session gets it back once before the
// message is re-sent: an architect by a re-sent start, a chat backend by an
// end and a cold-open resume. The resume carries only persisted rows, so it
// never holds the message being sent.
func (m *Manager) SendUserMessage(ctx context.Context, sessionID, content string) (string, error) {
	return m.sendMessage(ctx, sessionID, content, userSend{})
}

// SendRoundReply forwards a question round's answers to a blueprint
// session's architect; content is the readable summary stored with the row.
func (m *Manager) SendRoundReply(ctx context.Context, sessionID, content string, round protocol.RoundReply) (string, error) {
	return m.sendMessage(ctx, sessionID, content, userSend{round: &round})
}

// SendMockupReply forwards the person's reply to the waiting mockup to a
// blueprint session unchanged; the stored row is an ordinary user message.
func (m *Manager) SendMockupReply(ctx context.Context, sessionID, content string, reply protocol.MockupReply) (string, error) {
	return m.sendMessage(ctx, sessionID, content, userSend{mockup: &reply})
}

// SendRoundChat forwards a message about the open question round promptID to
// a blueprint session's architect as a plain message, and stores it as a
// round chat row.
func (m *Manager) SendRoundChat(ctx context.Context, sessionID, content, promptID string) (string, error) {
	return m.sendMessage(ctx, sessionID, content, userSend{roundChat: promptID})
}

// MaxCostCapUSD is the largest cost cap SetCostCap accepts.
const MaxCostCapUSD = 1_000_000

// SetCostCap sends the architect a `cap` command and, once the send succeeds,
// records the new cap on the session row; 0 removes the cap. The amount is
// rounded to cents. The architect may still refuse a cap at or below its
// spend, which CM does not compare against.
func (m *Manager) SetCostCap(ctx context.Context, sessionID string, capUSD float64) (Session, error) {
	if math.IsNaN(capUSD) || math.IsInf(capUSD, 0) || capUSD > MaxCostCapUSD {
		return Session{}, fmt.Errorf("%w: cost cap must be at most %d USD", ErrInvalidSessionInput, MaxCostCapUSD)
	}

	if capUSD < 0 {
		return Session{}, fmt.Errorf("%w: cost cap must not be negative", ErrInvalidSessionInput)
	}

	cents := math.Round(capUSD * 100)
	if capUSD > 0 && cents == 0 {
		return Session{}, fmt.Errorf("%w: cost cap rounds to $0.00; use 0 to remove the cap", ErrInvalidSessionInput)
	}

	capUSD = cents / 100

	content := "cap off"
	if cents > 0 {
		content = "cap " + strconv.FormatFloat(capUSD, 'f', 2, 64)
		content = strings.TrimSuffix(strings.TrimSuffix(content, "0"), "0")
		content = strings.TrimSuffix(content, ".")
	}

	if _, err := m.sendMessage(ctx, sessionID, content, userSend{command: true}); err != nil {
		return Session{}, err
	}

	// Keeps CM's record of the cap in step with the architect's.
	if err := m.store.UpdateSessionCostCap(ctx, sessionID, capUSD); err != nil {
		return Session{}, fmt.Errorf("chat: record cost cap: %w", err)
	}

	return m.store.GetSession(ctx, sessionID)
}

// SetMode sends the architect a `mode` command and, once the send succeeds,
// records the mode on the session row, which every later start carries. The
// architect refuses a switch once emission has started, which CM does not
// check.
func (m *Manager) SetMode(ctx context.Context, sessionID, mode string) (Session, error) {
	if !protocol.ValidArchitectMode(mode) {
		return Session{}, fmt.Errorf("%w: unknown mode %q", ErrInvalidSessionInput, mode)
	}

	if _, err := m.sendMessage(ctx, sessionID, "mode "+mode, userSend{command: true}); err != nil {
		return Session{}, err
	}

	if err := m.store.UpdateSessionMode(ctx, sessionID, mode); err != nil {
		return Session{}, fmt.Errorf("chat: record mode: %w", err)
	}

	return m.store.GetSession(ctx, sessionID)
}

// userSend is what a send carries beside its content: a round reply's
// answers, the prompt id of the round a chat message is about, a mockup
// reply, a session command, or a handed-over need.
type userSend struct {
	round       *protocol.RoundReply
	roundChat   string
	mockup      *protocol.MockupReply
	command     bool
	handoffNeed bool
}

func (m *Manager) sendMessage(ctx context.Context, sessionID, content string, us userSend) (string, error) {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	if err := checkMessageSize(sess.Kind, len(content)); err != nil {
		return "", err
	}

	if sess.Kind != KindArchitect {
		switch {
		case us.round != nil:
			return "", fmt.Errorf("%w: round replies go to blueprint sessions only", ErrInvalidSessionInput)
		case us.roundChat != "":
			return "", fmt.Errorf("%w: round chat goes to blueprint sessions only", ErrInvalidSessionInput)
		case us.mockup != nil:
			return "", fmt.Errorf("%w: mockup replies go to blueprint sessions only", ErrInvalidSessionInput)
		case us.command:
			return "", fmt.Errorf("%w: session commands go to blueprint sessions only", ErrInvalidSessionInput)
		}
	}

	kind := ""

	switch {
	case us.command:
		kind = EventKindCommand
	case us.roundChat != "":
		kind = EventKindRoundChat
	case us.round != nil:
		kind = EventKindRoundReply
	case us.handoffNeed:
		kind = EventKindHandoffNeed
	}

	if err := m.ensureRunningForSend(ctx, sess); err != nil {
		return "", err
	}

	m.endRehydrationForSend(ctx, sessionID)

	msgID := NewID()
	payload := protocol.MessagePayload{SessionID: sessionID, Content: content, MessageID: msgID, Round: us.round, Mockup: us.mockup}

	logAttrs := []any{"session_id", sessionID, "message_id", msgID, "content_len", len(content)}
	if us.roundChat != "" {
		logAttrs = append(logAttrs, "round_chat", us.roundChat)
	}

	m.logger.Info("chat: forwarding user message to backend", logAttrs...)

	backend, err := m.backendFor(sess.Kind)
	if err != nil {
		return "", err
	}

	// An architect can answer inside /message. Its transcript lock, held from
	// the send to the user row's publish, keeps that answer after the row;
	// chat sessions never take it.
	var tl *sync.Mutex
	if sess.Kind == KindArchitect {
		tl = m.transcriptLock(sessionID)
		tl.Lock()
	}

	// held makes unlock idempotent, so the deferred call releases the lock on
	// a panic without ever unlocking an unlocked mutex (a fatal error).
	held := tl != nil

	unlock := func() {
		if held {
			held = false

			tl.Unlock()
		}
	}
	defer unlock()

	// Arm before the send. A backend that answers inside /message (the
	// architect, for a note or an abort) publishes its idle frame before
	// SendChatMessage returns; setWorking(false) on an unarmed session is a
	// no-op, so arming afterwards would leave the indicator on. A failed
	// send disarms only what it armed: an earlier turn may still be running.
	// A command starts no turn, so no idle frame would clear the indicator.
	armed := !us.command && m.setWorking(sessionID, true)

	err = backend.SendChatMessage(ctx, payload)

	// A restarted architect no longer holds the session: re-send its start
	// and the message once, with the same id.
	if err != nil && sess.Kind == KindArchitect && errors.Is(err, ErrSessionUnknown) {
		firstSend := m.clk.Now()

		// Released while the start and the wait run: only frames the reopened
		// architect delivers meanwhile precede this message; later ones can follow.
		unlock()

		rerr := m.restartArchitect(ctx, sessionID)
		if rerr == nil {
			m.wakeConsumer(sessionID)
			rerr = m.waitPastSecond(ctx, firstSend)
		}

		tl.Lock()

		held = true

		if rerr != nil {
			err = errors.Join(err, rerr)
		} else {
			// The architect's replay cache already holds the first send's
			// whole-second (timestamp, signature) pair despite the 404, so
			// the re-send must sign in a later second.
			err = backend.SendChatMessage(ctx, payload)
		}
	}

	// A chat backend that restarted, or whose worker exited, no longer holds
	// the session: reopen it cold and re-send once, with the same id.
	if err != nil && sess.Kind != KindArchitect && errors.Is(err, ErrSessionUnknown) {
		if rerr := m.reopenCold(ctx, sessionID); rerr != nil {
			err = errors.Join(err, rerr)
		} else {
			// The reopen's EndSession dropped the indicator this send armed.
			armed = m.setWorking(sessionID, true)
			err = backend.SendChatMessage(ctx, payload)
		}
	}

	if err != nil {
		// Before recordSendFailure: its EndSession waits for the consumer,
		// which may be waiting on this lock.
		unlock()

		if armed {
			m.setWorking(sessionID, false)
		}

		m.recordSendFailure(ctx, sessionID, err)

		return "", err
	}

	m.clearSendFailures(sessionID)

	// Backend accepted the message - now safe to persist + publish.
	msg, err := m.appendMessageWithKind(ctx, sessionID, RoleUser, content, kind)
	if err != nil {
		unlock()

		return "", err
	}

	if m.hub != nil {
		m.hub.Publish(sessionID, SSEEvent{
			DataKind: kind,
			Seq:      msg.Seq,
			Role:     RoleUser,
			Content:  content,
		})
	}

	unlock()

	return msgID, nil
}

// SetTitle sets only the title; it never touches last_active or status.
func (m *Manager) SetTitle(ctx context.Context, sessionID, title string) error {
	if err := m.store.UpdateSessionTitle(ctx, sessionID, title); err != nil {
		return fmt.Errorf("set title: %w", err)
	}

	return nil
}

// SetPinned sets only the pin; it never touches last_active or status.
func (m *Manager) SetPinned(ctx context.Context, sessionID string, pinned bool) error {
	if err := m.store.UpdateSessionPinned(ctx, sessionID, pinned); err != nil {
		return fmt.Errorf("set pinned: %w", err)
	}

	return nil
}

// RecordBlueprintSpend stores the architect's spend for a blueprint; it never
// touches last_active or status.
func (m *Manager) RecordBlueprintSpend(ctx context.Context, sessionID string, spentUSD float64) error {
	if err := m.store.UpdateSessionSpend(ctx, sessionID, spentUSD); err != nil {
		return fmt.Errorf("record blueprint spend: %w", err)
	}

	return nil
}

// spendBackfillConcurrency bounds the startup backfill's architect reads in flight.
const spendBackfillConcurrency = 4

// BackfillBlueprintSpend reads the architect's view once for every blueprint
// with no recorded spend and records what it finds. Best-effort: a failed or
// empty read leaves the row unrecorded until the next view read. It returns
// when the pass ends and starts no read once ctx is done.
func (m *Manager) BackfillBlueprintSpend(ctx context.Context) {
	if m.architectSpend == nil || ctx.Err() != nil {
		return
	}

	sessions, err := m.store.ListSessions(ctx, SessionFilter{Kind: KindArchitect})
	if err != nil {
		m.logger.Warn("chat: blueprint spend backfill: list failed", "error", err)

		return
	}

	var (
		wg         sync.WaitGroup
		candidates int
		recorded   atomic.Int64
	)

	sem := make(chan struct{}, spendBackfillConcurrency)

	for _, sess := range sessions {
		if sess.SpentUSD != nil {
			continue
		}

		candidates++

		if ctx.Err() != nil {
			continue
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			continue
		}

		wg.Go(func() {
			defer func() { <-sem }()

			if m.readBlueprintSpend(ctx, sess.ID) {
				recorded.Add(1)
			}
		})
	}

	wg.Wait()

	m.logger.Info("chat: blueprint spend backfill done",
		"recorded", recorded.Load(), "unrecorded", int64(candidates)-recorded.Load())
}

// readBlueprintSpend reads a blueprint's spend from the architect and records
// it. A failed or empty read leaves the row as it is; it reports whether a
// spend was recorded.
func (m *Manager) readBlueprintSpend(ctx context.Context, sessionID string) bool {
	spent, ok, err := m.architectSpend(ctx, sessionID)
	if err != nil {
		m.logger.Debug("chat: blueprint spend read failed", "session_id", sessionID, "error", err)

		return false
	}

	if !ok {
		m.logger.Debug("chat: blueprint view carries no spend", "session_id", sessionID)

		return false
	}

	if err := m.RecordBlueprintSpend(ctx, sessionID, spent); err != nil {
		m.logger.Debug("chat: blueprint spend not recorded", "session_id", sessionID, "error", err)

		return false
	}

	return true
}

// ListMessages returns the transcript slice (seq > sinceSeq, oldest-first,
// bounded by limit). Used by the REST bootstrap endpoint that backfills the
// browser ring buffer beyond what the SSE in-memory ring can replay.
func (m *Manager) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]Message, error) {
	return m.store.ListMessages(ctx, sessionID, sinceSeq, limit)
}

// ListMessagesTail returns the newest limit messages (ASC). Bootstrap page
// for the lazy-history REST endpoint (tail=1).
func (m *Manager) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	return m.store.ListMessagesTail(ctx, sessionID, limit)
}

// ListMessagesBefore returns the newest limit messages with seq < beforeSeq
// (ASC). Backward page for the lazy-history REST endpoint.
func (m *Manager) ListMessagesBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]Message, error) {
	return m.store.ListMessagesBefore(ctx, sessionID, beforeSeq, limit)
}

// chatCostCacheTTL is the minimum duration between re-computations of the
// chat cost summary. Dashboard fan-out (one call per project) would otherwise
// amplify a single server-wide computation N× on every poll cycle.
const chatCostCacheTTL = 30 * time.Second

// chatCostCache holds the most-recently computed chat cost summary values so
// GetChatCostSummary can serve cache hits without a SQL round-trip.
type chatCostCache struct {
	last30d    float64
	prior30d   float64
	series30d  []float64
	computedAt time.Time
}

// GetChatCostSummary returns server-wide chat cost aggregates aligned on UTC
// midnight. The result is cached for chatCostCacheTTL to prevent N× SQL
// amplification when multiple projects fan out concurrent dashboard requests.
//
// Errors are not cached: a transient store error causes the next caller to
// retry immediately. The returned values are zero on error; callers should
// log the error and emit a metric but continue rendering the dashboard.
func (m *Manager) GetChatCostSummary(ctx context.Context) (last30d, prior30d float64, series30d []float64, err error) {
	now := m.clk.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := todayStart.Add(24 * time.Hour)
	since := todayStart.AddDate(0, 0, -29)

	// Check cache under lock.
	m.mu.Lock()
	cached := m.costCache
	m.mu.Unlock()

	if !cached.computedAt.IsZero() && now.Sub(cached.computedAt) < chatCostCacheTTL {
		// Return a defensive copy so the caller cannot mutate the cached backing
		// array. The cache entry itself is never exposed - copy-on-read.
		return cached.last30d, cached.prior30d, append([]float64(nil), cached.series30d...), nil
	}

	// Cache miss or expiry: re-compute.
	last30d, prior30d, series30d, err = m.store.AggregateCost(ctx, since, until)
	if err != nil {
		// Do NOT cache errors: next caller should retry immediately.
		return 0, 0, nil, fmt.Errorf("chat: GetChatCostSummary: %w", err)
	}

	m.mu.Lock()
	m.costCache = chatCostCache{
		last30d:    last30d,
		prior30d:   prior30d,
		series30d:  series30d,
		computedAt: now,
	}
	m.mu.Unlock()

	// Return a defensive copy here too: the value returned to this first caller
	// must not share backing array with the freshly-cached entry. Copy-on-read
	// is the chosen invariant - the cache entry is always private to the cache.
	return last30d, prior30d, append([]float64(nil), series30d...), nil
}
