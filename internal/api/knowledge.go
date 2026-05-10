package api

import (
	"encoding/json"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/service"
)

// knowledgeHandlers contains handlers for knowledge-base endpoints.
type knowledgeHandlers struct {
	svc *service.CardService
}

// listForProject returns the KB summary (repos + docs) for one project,
// or an empty shell when the project has no KB built yet.
func (h *knowledgeHandlers) listForProject(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	if _, err := h.svc.GetProject(r.Context(), project); err != nil {
		handleServiceError(w, r, err)

		return
	}

	bases, err := h.svc.ListKnowledgeBases(r.Context(), project)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if len(bases) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"project": project, "repos": []any{}})

		return
	}

	writeJSON(w, http.StatusOK, bases[0])
}

// getDoc returns the markdown content and meta for a single doc.
func (h *knowledgeHandlers) getDoc(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	repo := r.PathValue("repo")
	doc := r.PathValue("doc")

	out, err := h.svc.ReadKnowledgeDoc(r.Context(), project, repo, doc)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": out.Content,
		"meta":    out.Meta,
	})
}

// putDoc saves a hand-edited doc. Sets human_edited=true via WriteKnowledgeDocs(Edit).
func (h *knowledgeHandlers) putDoc(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	repo := r.PathValue("repo")
	doc := r.PathValue("doc")

	var body struct {
		Content *string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"invalid JSON body", sanitizeErrorDetails(err))

		return
	}

	if body.Content == nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"content field required", "")

		return
	}

	if *body.Content == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"content cannot be empty (use DELETE if you mean to remove the doc)", "")

		return
	}

	// REST KB writes are reachable only via the UI (CSRF gate enforces
	// X-Requested-With: contextmatrix in the request middleware). The UI is
	// always operated by a human, so we record "human:web" when the caller
	// hasn't supplied an explicit identity. Refresh writes go through MCP and
	// have their own service-layer human gate.
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = "human:web"
	}

	res, err := h.svc.WriteKnowledgeDocs(r.Context(), service.WriteKnowledgeDocsInput{
		Project: project,
		Repo:    repo,
		Docs:    map[string]string{doc: *body.Content},
		Source:  service.KnowledgeWriteSourceEdit,
		AgentID: agentID,
	})
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files_written": res.FilesWritten,
	})
}
