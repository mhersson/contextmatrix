package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/attention"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// AttentionLister answers the waiting list a viewer sees.
type AttentionLister interface {
	List(ctx context.Context, viewer string, ownOnly bool) ([]attention.Item, error)
}

type attentionHandlers struct {
	attn AttentionLister
}

// list handles GET /api/attention, scoped like GET /api/chats: a session
// limits blueprints to its own user.
func (h *attentionHandlers) list(w http.ResponseWriter, r *http.Request) {
	_, ownOnly := sessionIdentity(r.Context())

	items, err := h.attn.List(r.Context(), extractAgentID(r), ownOnly)
	if err != nil {
		ctxlog.Logger(r.Context()).Error("list attention", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read the waiting list", "")

		return
	}

	if items == nil {
		items = []attention.Item{}
	}

	writeJSON(w, http.StatusOK, items)
}
