package api

import (
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/service"
)

type playbookHandlers struct {
	svc *service.PlaybookService
}

type playbookEntryRequest struct {
	Type    string `json:"type"`
	Project string `json:"project,omitempty"`
	Card    string `json:"card,omitempty"`
	Text    string `json:"text,omitempty"`
	Note    string `json:"note,omitempty"`
}

type createPlaybookRequest struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	BoardsRepo  string                 `json:"boards_repo,omitempty"`
	Entries     []playbookEntryRequest `json:"entries,omitempty"`
}

type patchPlaybookRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

type patchPlaybookEntryRequest struct {
	Done     *bool   `json:"done,omitempty"`
	Note     *string `json:"note,omitempty"`
	Text     *string `json:"text,omitempty"`
	Position *int    `json:"position,omitempty"`
}

// playbookAgentID resolves attribution: session identity wins in multi
// mode, X-Agent-ID on machine channels, human:web as the UI fallback.
func playbookAgentID(r *http.Request) string {
	if id := extractAgentID(r); id != "" {
		return id
	}

	return "human:web"
}

// list handles GET /api/playbooks.
func (h *playbookHandlers) list(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.svc.List(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, summaries)
}

// create handles POST /api/playbooks.
func (h *playbookHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req createPlaybookRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "title is required", "")

		return
	}

	entries := make([]service.PlaybookEntryInput, len(req.Entries))
	for i, e := range req.Entries {
		entries[i] = service.PlaybookEntryInput{
			Type:    e.Type,
			Project: e.Project,
			Card:    e.Card,
			Text:    e.Text,
			Note:    e.Note,
		}
	}

	input := service.CreatePlaybookInput{
		Title:       req.Title,
		Description: req.Description,
		AgentID:     playbookAgentID(r),
		Entries:     entries,
		BoardsRepo:  req.BoardsRepo,
	}

	detail, err := h.svc.Create(r.Context(), input)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

// get handles GET /api/playbooks/{id}.
func (h *playbookHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	detail, err := h.svc.Get(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// patch handles PATCH /api/playbooks/{id}.
func (h *playbookHandlers) patch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req patchPlaybookRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	input := service.UpdatePlaybookInput{
		Title:       req.Title,
		Description: req.Description,
	}

	detail, err := h.svc.UpdateMeta(r.Context(), id, input, playbookAgentID(r))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// delete handles DELETE /api/playbooks/{id}.
func (h *playbookHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.svc.Delete(r.Context(), id, playbookAgentID(r)); err != nil {
		handleServiceError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// addEntry handles POST /api/playbooks/{id}/entries.
func (h *playbookHandlers) addEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req playbookEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	input := service.PlaybookEntryInput{
		Type:    req.Type,
		Project: req.Project,
		Card:    req.Card,
		Text:    req.Text,
		Note:    req.Note,
	}

	detail, err := h.svc.AddEntry(r.Context(), id, input, playbookAgentID(r))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

// patchEntry handles PATCH /api/playbooks/{id}/entries/{entryId}.
func (h *playbookHandlers) patchEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entryID := r.PathValue("entryId")

	var req patchPlaybookEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	input := service.UpdateEntryInput{
		Done:     req.Done,
		Note:     req.Note,
		Text:     req.Text,
		Position: req.Position,
	}

	detail, err := h.svc.UpdateEntry(r.Context(), id, entryID, input, playbookAgentID(r))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// deleteEntry handles DELETE /api/playbooks/{id}/entries/{entryId}.
func (h *playbookHandlers) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entryID := r.PathValue("entryId")

	detail, err := h.svc.RemoveEntry(r.Context(), id, entryID, playbookAgentID(r))
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, detail)
}
