package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
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

type chatHandlers struct {
	mgr  *chat.Manager
	hub  *chat.SSEHub
	chat *config.ChatConfig
}

func newChatHandlers(mgr *chat.Manager, hub *chat.SSEHub, chatCfg *config.ChatConfig) *chatHandlers {
	return &chatHandlers{mgr: mgr, hub: hub, chat: chatCfg}
}

// agentIDForChat returns the caller identity, defaulting to "human:web" when
// the X-Agent-ID header is absent (same fallback pattern used elsewhere in api/).
func agentIDForChat(r *http.Request) string {
	id := r.Header.Get("X-Agent-ID")
	if id == "" {
		return "human:web"
	}

	return id
}

func (h *chatHandlers) listChats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	f := chat.SessionFilter{
		Project:   q.Get("project"),
		CreatedBy: q.Get("created_by"),
		Limit:     listChatsDefaultLimit,
	}
	if st := q.Get("status"); st != "" {
		s, ok := chat.ParseStatus(st)
		if !ok {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid status", st)

			return
		}

		f.Status = s
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid limit", v)

			return
		}

		if n > listChatsMaxLimit {
			n = listChatsMaxLimit
		}

		f.Limit = n
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
	if model == "" && h.chat != nil {
		model = h.chat.DefaultModel
	}

	if model != "" && h.chat != nil {
		if _, ok := h.chat.Models[model]; !ok {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidModel,
				"model not in allowlist", model)

			return
		}
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

// listModels exposes the configured chat model allowlist + default for the
// frontend picker. Response shape mirrors what NewChatDialog consumes.
func (h *chatHandlers) listModels(w http.ResponseWriter, _ *http.Request) {
	type response struct {
		Models  []chatModelEntry `json:"models"`
		Default string           `json:"default"`
	}

	if h.chat == nil {
		writeJSON(w, http.StatusOK, response{Models: []chatModelEntry{}, Default: ""})

		return
	}

	models := make([]chatModelEntry, 0, len(h.chat.Models))
	for id, m := range h.chat.Models {
		models = append(models, chatModelEntry{ID: id, Label: m.Label, MaxTokens: m.MaxTokens})
	}

	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })

	writeJSON(w, http.StatusOK, response{Models: models, Default: h.chat.DefaultModel})
}

func (h *chatHandlers) getChat(w http.ResponseWriter, r *http.Request) {
	sess, err := h.mgr.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (h *chatHandlers) deleteChat(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		handleChatError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *chatHandlers) openChat(w http.ResponseWriter, r *http.Request) {
	sess, err := h.mgr.OpenSession(r.Context(), r.PathValue("id"))
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (h *chatHandlers) endChat(w http.ResponseWriter, r *http.Request) {
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

// clearChat wipes the runner's working memory in place, re-primes the
// session with the chat-mode primer, marks prior transcript rows as
// rehydration_phase=true so they are excluded from future cold-open
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
//   - chat.ErrSessionNotRunning → 409 RUNNER_NOT_RUNNING
//   - chat.ErrRunnerSend        → 502 RUNNER_UNAVAILABLE (detail: "clear_failed" or "primer_failed")
//   - everything else           → 500 (via handleChatError)
func (h *chatHandlers) clearChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.mgr.ClearContext(r.Context(), id); err != nil {
		if errors.Is(err, chat.ErrSessionNotRunning) {
			writeError(w, http.StatusConflict, ErrCodeRunnerNotRunning,
				"session is not running", "")

			return
		}

		if errors.Is(err, chat.ErrRunnerSend) {
			// ErrRunnerSendPrimer is the more specific sentinel — check it first
			// so "primer_failed" takes precedence over the general "clear_failed".
			detail := "clear_failed"
			if errors.Is(err, chat.ErrRunnerSendPrimer) {
				detail = "primer_failed"
			}

			writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable,
				"runner unavailable", detail)

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
	if _, err := h.mgr.GetSession(r.Context(), id); err != nil {
		handleChatError(w, r, err)

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

	sess, err := h.mgr.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		handleChatError(w, r, err)

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

	// Validate the session exists before subscribing — the hub lazy-creates a
	// per-session ring buffer + subscriber set on first Subscribe, so an
	// unguarded handler would let any GET against an unknown id permanently
	// grow perSess.
	if _, err := h.mgr.GetSession(r.Context(), id); err != nil {
		handleChatError(w, r, err)

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
	ctxlog.Logger(r.Context()).Error("chat error", "error", err)

	switch {
	case errors.Is(err, chat.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, ErrCodeChatNotFound, "chat session not found", "")
	case errors.Is(err, chat.ErrTooManyConcurrent):
		writeError(w, http.StatusTooManyRequests, ErrCodeTooManyChats, "concurrent chat limit reached", "")
	default:
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal error", "")
	}
}
