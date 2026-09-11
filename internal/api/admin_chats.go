package api

import (
	"net/http"

	"github.com/mhersson/contextmatrix/internal/chat"
)

// adminChatHandlers maps chat-session management onto /api/admin/chats.
// Metadata and lifecycle only: there is deliberately no route here that
// returns transcript content (no messages, stream, or open) - "admins
// cannot read transcripts" is enforced by the routes not existing, not by
// a check. Transcripts remain readable to whoever holds the host (ops.db);
// this surface is interface-level privacy against the admin role.
type adminChatHandlers struct {
	mgr *chat.Manager
	// authEnabled mirrors "multi mode": every route then requires an admin
	// session. In none mode they are open, the same trust posture as the
	// model-selection admin routes: whoever reaches the API is trusted.
	authEnabled bool
}

func (h *adminChatHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// listChats handles GET /api/admin/chats - every session, no owner
// scoping. Session JSON carries metadata and cost totals only.
func (h *adminChatHandlers) listChats(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	f, ok := parseChatListFilter(w, r.URL.Query())
	if !ok {
		return
	}

	// The admin filter surface is project/status/limit only - created_by
	// is not honored here (the list is meant to show everything).
	f.CreatedBy = ""

	sessions, err := h.mgr.ListSessions(r.Context(), f)
	if err != nil {
		handleChatError(w, r, err)

		return
	}

	if sessions == nil {
		sessions = []chat.Session{}
	}

	writeJSON(w, http.StatusOK, sessions)
}

// endChat handles POST /api/admin/chats/{id}/end - force-ends any session
// regardless of owner. Mirrors the user-facing endChat semantics and
// error mapping; this is the remedy when a stuck active session holds a
// slot of the global concurrency cap.
func (h *adminChatHandlers) endChat(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
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

// deleteChat handles DELETE /api/admin/chats/{id} - deletes any session
// regardless of owner. Unknown IDs 404 (DeleteSession loads the session
// before deleting - existing manager semantics). DeleteSession preserves
// cost tombstones, so dashboard aggregates stay accurate.
func (h *adminChatHandlers) deleteChat(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	if err := h.mgr.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		handleChatError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
