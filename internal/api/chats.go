package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

const (
	ErrCodeChatNotFound = "CHAT_NOT_FOUND"
	ErrCodeTooManyChats = "TOO_MANY_CHATS"
	ErrCodeInvalidModel = "INVALID_MODEL"
)

// endpointModelCacheTTL is the lifetime of the cached endpoint model list.
// The picker calls listModels on page load and on re-focus; a 5-minute window
// absorbs repeated requests while remaining consistent with normal catalog churn.
const endpointModelCacheTTL = 5 * time.Minute

// cachedEndpointFetcher wraps a raw endpoint-model fetch function with a TTL
// cache and a last-good fallback: when a refresh fails but a prior successful
// fetch exists, the stale snapshot is returned rather than an error.
// Thread-safe.
type cachedEndpointFetcher struct {
	mu        sync.Mutex
	models    []chatModelEntry
	fetchedAt time.Time
	ttl       time.Duration
	raw       func(ctx context.Context) ([]chatModelEntry, error)
}

// newCachedEndpointFetcher returns a func that wraps raw with a TTL cache.
// The returned func is safe for concurrent calls and matches the type expected
// by chatHandlers.endpointModels.
func newCachedEndpointFetcher(
	raw func(ctx context.Context) ([]chatModelEntry, error),
	ttl time.Duration,
) func(context.Context) ([]chatModelEntry, error) {
	f := &cachedEndpointFetcher{raw: raw, ttl: ttl}

	return f.get
}

func (f *cachedEndpointFetcher) get(ctx context.Context) ([]chatModelEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.models != nil && time.Since(f.fetchedAt) < f.ttl {
		return f.models, nil
	}

	models, err := f.raw(ctx)
	if err != nil {
		// Last-good fallback: serve the stale snapshot rather than an error
		// when a prior successful fetch exists.
		if f.models != nil {
			return f.models, nil
		}

		return nil, err
	}

	f.models = models
	f.fetchedAt = time.Now()

	return models, nil
}

type chatHandlers struct {
	mgr *chat.Manager
	hub *chat.SSEHub

	// openRouter is true when the dedicated chat backend (contextmatrix-chat,
	// OpenRouter) serves chat. In that mode the chat.models allowlist does not
	// apply: the picker pulls the live OpenRouter catalog and slugs are accepted
	// as-is. False only in legacy configurations without a dedicated chat
	// backend.
	openRouter bool
	// orDefault is the default OpenRouter slug (backends.chat.default_model),
	// used to seed the picker and as the empty-model fallback in openRouter mode.
	orDefault string
	// endpointModels, when non-nil, retrieves the (cached) endpoint model list
	// for the picker. Returns (nil, err) when the upstream fetch fails and no
	// last-good snapshot exists. Set when llm_endpoint.type == "openai".
	endpointModels func(ctx context.Context) ([]chatModelEntry, error)
	// servedModels, when non-nil, returns the vendor-screened OpenRouter
	// catalog from CM's cached copy for the openrouter-mode picker.
	servedModels func(ctx context.Context) []chatModelEntry
	// validateModel, when non-nil, reports whether a slug is in the served
	// set. Fail-open on an empty catalog. Used by createChat in openrouter mode.
	validateModel func(ctx context.Context, slug string) bool
}

func newChatHandlers(mgr *chat.Manager, hub *chat.SSEHub, chatBackendCfg *config.ChatBackendConfig) *chatHandlers {
	// The chat backend is the active chat server exactly when it is enabled
	// and keyed — mirrors the route guard in router.go. IsEnabled is nil-safe
	// (an absent entry is nil → disabled), and the short-circuit keeps the
	// APIKey read from dereferencing nil.
	openRouter := chatBackendCfg.IsEnabled() && chatBackendCfg.APIKey != ""

	orDefault := ""
	if chatBackendCfg != nil {
		orDefault = chatBackendCfg.DefaultModel
	}

	return &chatHandlers{
		mgr:        mgr,
		hub:        hub,
		openRouter: openRouter,
		orDefault:  orDefault,
	}
}

// agentIDForChat returns the caller identity, defaulting to "human:web" when
// the X-Agent-ID header is absent (same fallback pattern used elsewhere in api/).
func agentIDForChat(r *http.Request) string {
	id := extractAgentID(r)
	if id == "" {
		return "human:web"
	}

	return id
}

// ownedSession loads the session for the {id} path value and enforces
// ownership when the caller has an authenticated identity (multi mode; in
// none mode there is no session identity and no scoping). Unknown IDs and
// foreign owners produce the identical 404 CHAT_NOT_FOUND response so
// existence is never leaked. Returns ok=false when a response has already
// been written.
func (h *chatHandlers) ownedSession(w http.ResponseWriter, r *http.Request) (chat.Session, bool) {
	sess, err := h.mgr.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		handleChatError(w, r, err)

		return chat.Session{}, false
	}

	if id, ok := sessionIdentity(r.Context()); ok && sess.CreatedBy != id {
		handleChatError(w, r, chat.ErrSessionNotFound)

		return chat.Session{}, false
	}

	return sess, true
}

// parseChatListFilter builds the SessionFilter shared by the user-facing
// and admin chat lists (project, status, limit with the default/max
// clamps). Returns ok=false after writing a 400 for invalid values.
func parseChatListFilter(w http.ResponseWriter, q url.Values) (chat.SessionFilter, bool) {
	f := chat.SessionFilter{
		Project:   q.Get("project"),
		CreatedBy: q.Get("created_by"),
		Limit:     listChatsDefaultLimit,
	}

	if st := q.Get("status"); st != "" {
		s, ok := chat.ParseStatus(st)
		if !ok {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid status", st)

			return chat.SessionFilter{}, false
		}

		f.Status = s
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid limit", v)

			return chat.SessionFilter{}, false
		}

		if n > listChatsMaxLimit {
			n = listChatsMaxLimit
		}

		f.Limit = n
	}

	return f, true
}

func (h *chatHandlers) listChats(w http.ResponseWriter, r *http.Request) {
	f, ok := parseChatListFilter(w, r.URL.Query())
	if !ok {
		return
	}

	// Multi mode: the list is always scoped to the caller — a client-
	// supplied created_by cannot widen or redirect it (silently
	// overridden; the UI never sends one). None mode keeps the param's
	// client-filter behavior.
	if id, ok := sessionIdentity(r.Context()); ok {
		f.CreatedBy = id
	}

	sessions, err := h.mgr.ListSessions(r.Context(), f)
	if err != nil {
		handleChatError(w, r, err)

		return
	}
	// Always return a slice — never nil — so JSON serializes as [] not null.
	if sessions == nil {
		sessions = []chat.Session{}
	}

	writeJSON(w, http.StatusOK, sessions)
}

func (h *chatHandlers) createChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title   string `json:"title"`
		Project string `json:"project"`
		Model   string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid body", err.Error())

		return
	}

	model := body.Model

	switch {
	case h.endpointModels != nil:
		if model == "" {
			model = h.orDefault
		}
		// Validate against the same cached list that feeds the picker; a
		// fetch error fails open so an upstream outage never blocks chat.
		if model != "" {
			if models, err := h.endpointModels(r.Context()); err == nil && !containsModelID(models, model) {
				writeError(w, http.StatusBadRequest, ErrCodeInvalidModel, "model not in catalog", model)

				return
			}
		}
	case h.openRouter:
		if model == "" {
			model = h.orDefault
		}
		// Validate against CM's vendor-screened catalog copy. validateModel
		// fails open on an empty catalog (cold start, OpenRouter outage).
		if model != "" && h.validateModel != nil && !h.validateModel(r.Context(), model) {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidModel, "model not in catalog", model)

			return
		}
	default:
		// No chat backend configured: accept the model untouched — sends
		// fail at open time via the disabled-backend stub, so there is no
		// catalog to validate against.
	}

	sess, err := h.mgr.CreateSession(r.Context(), chat.CreateInput{
		Title:     body.Title,
		Project:   body.Project,
		CreatedBy: agentIDForChat(r),
		Model:     model,
	})
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, sess)
}

// chatModelEntry is the picker-facing shape returned by listModels.
type chatModelEntry struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	MaxTokens int64  `json:"max_tokens"`
}

// containsModelID reports whether id is present in the picker model list.
func containsModelID(models []chatModelEntry, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}

	return false
}

// listModels exposes the chat model picker source for the frontend. The
// `source` field tells the picker which mode to render:
//   - "openrouter": the chat backend serves chat over OpenRouter; Models is
//     the vendor-screened OpenRouter catalog from CM's cached copy (empty
//     only when the catalog has not been fetched) and Default seeds it with
//     backends.chat.default_model.
//   - "endpoint": llm_endpoint.type is "openai"; Models comes from the
//     endpoint's /v1/models response and Default is
//     backends.chat.default_model. Also returned (with an empty list) when
//     no chat backend is configured — the picker renders nothing and new
//     chats fall back to the server default.
func (h *chatHandlers) listModels(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Source     string           `json:"source"`
		Models     []chatModelEntry `json:"models"`
		Default    string           `json:"default"`
		FetchError string           `json:"fetch_error,omitempty"`
	}

	if h.endpointModels != nil {
		models, err := h.endpointModels(r.Context())
		if err != nil {
			writeJSON(w, http.StatusOK, response{
				Source:     "endpoint",
				FetchError: err.Error(),
				Models:     []chatModelEntry{},
				Default:    h.orDefault,
			})

			return
		}

		if models == nil {
			models = []chatModelEntry{}
		}

		writeJSON(w, http.StatusOK, response{Source: "endpoint", Models: models, Default: h.orDefault})

		return
	}

	if h.openRouter {
		models := []chatModelEntry{}
		if h.servedModels != nil {
			models = h.servedModels(r.Context())
		}

		writeJSON(w, http.StatusOK, response{
			Source:  "openrouter",
			Models:  models,
			Default: h.orDefault,
		})

		return
	}

	// No chat backend configured: an empty endpoint-mode list renders
	// nothing in the picker.
	writeJSON(w, http.StatusOK, response{Source: "endpoint", Models: []chatModelEntry{}, Default: ""})
}

func (h *chatHandlers) getChat(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.ownedSession(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (h *chatHandlers) deleteChat(w http.ResponseWriter, r *http.Request) {
	// Ownership is enforced only for authenticated callers (multi mode) — and
	// there both missing and foreign IDs must 404 identically (a 204/404
	// split would leak existence). In none mode there is no identity to
	// scope on, so the check is skipped entirely; DeleteSession's own
	// not-found path (it loads the session before deleting) already 404s a
	// missing ID regardless of mode, so skipping the check here changes
	// nothing about that outcome — it only means none mode never scopes by
	// owner.
	if _, ok := sessionIdentity(r.Context()); ok {
		if _, ok := h.ownedSession(w, r); !ok {
			return
		}
	}

	if err := h.mgr.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		handleChatError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *chatHandlers) openChat(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	sess, err := h.mgr.OpenSession(r.Context(), r.PathValue("id"))
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (h *chatHandlers) endChat(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	id := r.PathValue("id")
	if err := h.mgr.EndSession(r.Context(), id); err != nil {
		handleChatError(w, r, err)

		return
	}

	sess, err := h.mgr.GetSession(r.Context(), id)
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, sess)
}

// clearChat wipes the worker's working memory in place (the worker re-orients
// its next epoch from its own embedded primer), marks prior transcript rows
// as rehydration_phase=true so they are excluded from future cold-open
// resume payloads, and appends a "Context cleared" divider row. The
// divider is published on the live SSE wire AND persisted with
// kind="divider" so a page reload still renders it as a horizontal rule.
//
// Request body is intentionally ignored — the operation has no per-user
// parameters and matches the empty-body convention used by endChat.
// agentIDForChat is intentionally not invoked here — ClearContext has no
// per-user effect on the no-auth trust model (see CLAUDE.md §Trust model).
//
// Errors are routed:
//   - chat.ErrSessionNotFound   → 404 (via handleChatError)
//   - chat.ErrSessionNotRunning → 409 WORKER_NOT_RUNNING
//   - chat.ErrBackendSend        → 502 BACKEND_UNAVAILABLE (detail: "clear_failed")
//   - everything else           → 500 (via handleChatError)
func (h *chatHandlers) clearChat(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	id := r.PathValue("id")
	if err := h.mgr.ClearContext(r.Context(), id); err != nil {
		if errors.Is(err, chat.ErrSessionNotRunning) {
			writeError(w, http.StatusConflict, ErrCodeWorkerNotRunning,
				"session is not running", "")

			return
		}

		if errors.Is(err, chat.ErrBackendSend) {
			writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable,
				"backend unavailable", "clear_failed")

			return
		}

		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (h *chatHandlers) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid body", err.Error())

		return
	}

	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "content is required", "")

		return
	}

	if len(body.Content) > 8192 {
		writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge, "message too long", "")

		return
	}

	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	msgID, err := h.mgr.SendUserMessage(r.Context(), r.PathValue("id"), body.Content)
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "message_id": msgID})
}

const (
	listChatsDefaultLimit = 500
	listChatsMaxLimit     = 5000

	listMessagesDefaultLimit = 200
	listMessagesMaxLimit     = 1000
)

func (h *chatHandlers) listMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	q := r.URL.Query()

	var sinceSeq int64

	if v := q.Get("since_seq"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid since_seq", v)

			return
		}

		sinceSeq = n
	}

	limit := listMessagesDefaultLimit

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid limit", v)

			return
		}

		if n > listMessagesMaxLimit {
			n = listMessagesMaxLimit
		}

		limit = n
	}

	msgs, err := h.mgr.ListMessages(r.Context(), id, sinceSeq, limit)
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	if msgs == nil {
		msgs = []chat.Message{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (h *chatHandlers) patchChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title *string `json:"title,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid body", err.Error())

		return
	}

	sess, ok := h.ownedSession(w, r)
	if !ok {
		return
	}

	if body.Title != nil {
		sess.Title = *body.Title
	}

	if err := h.mgr.UpdateSessionMetadata(r.Context(), sess); err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (h *chatHandlers) streamChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Validate existence AND ownership before subscribing — the hub
	// lazy-creates a per-session ring buffer + subscriber set on first
	// Subscribe, so an unguarded handler would let any GET against an
	// unknown or foreign id permanently grow perSess.
	if _, ok := h.ownedSession(w, r); !ok {
		return
	}

	since, _ := strconv.ParseInt(r.URL.Query().Get("since_seq"), 10, 64)

	// Clear the server's WriteTimeout for this long-lived SSE connection;
	// without this the connection is severed after the global write deadline.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		ctxlog.Logger(r.Context()).Warn("chat SSE: could not clear write deadline; connection will drop on WriteTimeout",
			"error", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, replay, err := h.hub.Subscribe(id, since)
	if err != nil {
		writeError(w, http.StatusTooManyRequests, ErrCodeTooManyChats, err.Error(), "")

		return
	}

	defer h.hub.Unsubscribe(id, ch)

	flusher, _ := w.(http.Flusher)

	// Flush a connected comment immediately so EventSource.onopen fires
	// before any chat event is published. Critical for browsers behind
	// proxies that buffer until the first body byte.
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		return
	}

	if flusher != nil {
		flusher.Flush()
	}

	for _, e := range replay {
		writeChatSSEEvent(w, e)
	}

	if flusher != nil {
		flusher.Flush()
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}

			if flusher != nil {
				flusher.Flush()
			}
		case e, ok := <-ch:
			if !ok {
				return
			}

			writeChatSSEEvent(w, e)

			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// writeChatSSEEvent serialises one event onto the SSE wire. Different event
// kinds use the SSE "event:" header so the browser's EventSource can route
// to different listeners (transcript messages vs. session-state pushes).
// The default kind ("") is treated as the message wire (backwards-compatible
// with clients written before Wave 4 of the rehydration feature).
func writeChatSSEEvent(w http.ResponseWriter, e chat.SSEEvent) {
	switch e.Kind {
	case chat.SSEKindSessionUpdate:
		_, _ = w.Write([]byte("event: session_updated\n"))

		if e.SessionUpdate != nil {
			b, _ := json.Marshal(e.SessionUpdate)
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
		} else {
			_, _ = w.Write([]byte("data: {}\n\n"))
		}
	default:
		// Backwards-compatible default: emit data without an event: header
		// so older clients listening on the unnamed message stream keep
		// working. rehydration_phase is included so the UI can group
		// agent rehydration messages distinctly from normal turns. kind
		// (omitempty) carries structural markers like "divider" for the
		// Clear Context sentinel — empty for regular messages.
		b, _ := json.Marshal(struct {
			Seq              int64     `json:"seq"`
			Role             chat.Role `json:"role"`
			Content          string    `json:"content"`
			Kind             string    `json:"kind,omitempty"`
			RehydrationPhase bool      `json:"rehydration_phase,omitempty"`
		}{Seq: e.Seq, Role: e.Role, Content: e.Content, Kind: e.DataKind, RehydrationPhase: e.RehydrationPhase})
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
	}
}

func handleChatError(w http.ResponseWriter, r *http.Request, err error) {
	logger := ctxlog.Logger(r.Context())

	switch {
	case errors.Is(err, chat.ErrSessionNotFound):
		// Warn, not Error: in multi mode every foreign-owner probe lands
		// here by design (ownedSession maps foreign and missing IDs to the
		// same 404), so this path is routine client behavior, not a fault.
		logger.Warn("chat error", "error", err)
		writeError(w, http.StatusNotFound, ErrCodeChatNotFound, "chat session not found", "")
	case errors.Is(err, chat.ErrTooManyConcurrent):
		logger.Error("chat error", "error", err)
		writeError(w, http.StatusTooManyRequests, ErrCodeTooManyChats, "concurrent chat limit reached", "")
	default:
		logger.Error("chat error", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal error", "")
	}
}
