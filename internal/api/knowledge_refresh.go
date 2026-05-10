package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/refresh"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/service"
)

// knowledgeRefreshHandlers serves the per-repo plan/trigger and the
// project-scoped status endpoint. The trigger requires a human:-prefixed
// X-Agent-ID; reads do not.
type knowledgeRefreshHandlers struct {
	svc       *service.CardService
	registry  *refresh.Registry
	runner    *runner.Client // nil when runner is disabled
	mcpAPIKey string
}

func (h *knowledgeRefreshHandlers) getPlan(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	repo := r.PathValue("repo")

	plan, err := h.svc.BuildRefreshPlan(r.Context(), project, repo)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, plan)
}

type triggerRequest struct {
	OverwriteDocs []string `json:"overwrite_docs"`
}

func (h *knowledgeRefreshHandlers) trigger(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	repo := r.PathValue("repo")

	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"X-Agent-ID header required for refresh trigger", "")

		return
	}

	if !board.IsHumanAgentID(agentID) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"refresh is human-only; X-Agent-ID must start with \"human:\" and have a non-empty suffix", "")

		return
	}

	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled,
			"runner is not configured", "")

		return
	}

	if h.registry == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeRunnerDisabled,
			"refresh registry not initialised", "")

		return
	}

	var body triggerRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
				"invalid JSON body", "")

			return
		}
	}

	// Defence-in-depth: validate overwrite_docs at the CM boundary so
	// unknown doc names never reach the runner's prompt context. The
	// runner's webhook validator and commit_knowledge_docs both enforce
	// the same allowlist server-side; this just closes the leak window.
	if len(body.OverwriteDocs) > len(board.KnowledgeDocNames) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"overwrite_docs exceeds maximum length", "")

		return
	}

	for _, name := range body.OverwriteDocs {
		if !board.IsValidKnowledgeDoc(name) {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
				fmt.Sprintf("overwrite_docs contains invalid doc name: %q", name), "")

			return
		}
	}

	cfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	var repoURL string

	for _, rcfg := range cfg.EffectiveRepos() {
		if rcfg.Name == repo {
			repoURL = rcfg.URL

			break
		}
	}

	if repoURL == "" {
		writeError(w, http.StatusNotFound, ErrCodeProjectNotFound,
			"repo not found in project", repo)

		return
	}

	job, err := h.registry.Acquire(project, repo, agentID)
	if err != nil {
		if errors.Is(err, refresh.ErrJobInFlight) {
			writeError(w, http.StatusConflict, ErrCodeRunnerConflict,
				"refresh already in flight for this repo", "")

			return
		}

		handleServiceError(w, r, err)

		return
	}

	payload := runner.RefreshKnowledgePayload{
		Project:       project,
		Repo:          repo,
		RepoURL:       repoURL,
		AgentID:       agentID,
		OverwriteDocs: body.OverwriteDocs,
		MCPAPIKey:     h.mcpAPIKey,
	}

	if cfg.RemoteExecution != nil && cfg.RemoteExecution.RunnerImage != "" {
		payload.RunnerImage = cfg.RemoteExecution.RunnerImage
	}

	if err := h.runner.RefreshKnowledge(r.Context(), payload); err != nil {
		ctxlog.Logger(r.Context()).Error("runner refresh-knowledge webhook failed",
			"project", project, "repo", repo, "error", err)
		_ = h.registry.MarkTerminal(project, repo, refresh.StateFailed, "runner unreachable: "+err.Error())

		writeError(w, http.StatusBadGateway, ErrCodeRunnerUnavailable,
			"failed to trigger runner", "")

		return
	}

	// Optimistically transition to Running with DocsTotal from the plan.
	plan, planErr := h.svc.BuildRefreshPlan(r.Context(), project, repo)
	if planErr == nil && plan != nil {
		_ = h.registry.MarkRunning(project, repo, len(plan.Items))
	}

	writeJSON(w, http.StatusAccepted, jobToStatus(job))
}

func (h *knowledgeRefreshHandlers) status(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	repos := map[string]any{}

	if h.registry != nil {
		snap := h.registry.Snapshot(project)
		for repo, j := range snap {
			repos[repo] = jobToStatus(j)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// jobToStatus is a value-copy projection used by both trigger (initial
// state) and the status endpoint. Keeps the wire shape in one place.
func jobToStatus(j refresh.Job) map[string]any {
	out := map[string]any{
		"state":       string(j.State),
		"agent_id":    j.AgentID,
		"started_at":  j.StartedAt,
		"docs_total":  j.DocsTotal,
		"docs_done":   j.DocsDone,
		"current_doc": j.CurrentDoc,
		"error":       j.Error,
		"commit_sha":  j.CommitSHA,
	}

	if j.FinishedAt != nil {
		out["finished_at"] = *j.FinishedAt
	} else {
		out["finished_at"] = nil
	}

	return out
}
