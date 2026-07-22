package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Card list pagination bounds. Enforced in listCards; clients that exceed the
// max receive 400. Default is on the generous side because the web UI's board
// view currently fetches everything at once - raising it shifts this endpoint
// to cursor-based paging only when clients opt in.
const (
	defaultCardPageLimit = 500
	maxCardPageLimit     = 2000
)

// listCardsResponse is the envelope returned by GET /api/projects/{project}/cards.
//
// Items is always emitted; NextCursor is omitted when no more pages exist;
// Total is populated only on the first page (cursor == "") so subsequent
// pages do not pay the O(n) unfiltered-count query.
type listCardsResponse struct {
	Items      []*board.Card `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Total      *int          `json:"total,omitempty"`
}

// cardHandlers contains handlers for card-related endpoints.
type cardHandlers struct {
	svc        *service.CardService
	taskSkills *taskSkillsLister
	// bestOfNMax is the configured config.BestOfNConfig.MaxCandidates, wired
	// from RouterConfig. Bounds the accepted best_of_n range (0 or 2..max).
	bestOfNMax int
	// mob is the configured mob session block, wired from RouterConfig. Bounds
	// mob_participants (0 or 2..MaxParticipants) and supplies the guest
	// registry names for mob_guests validation.
	mob config.MobConfig
}

// createCardRequest is the JSON body for creating a card.
type createCardRequest struct {
	Title             string              `json:"title"`
	Type              string              `json:"type"`
	Priority          string              `json:"priority"`
	Labels            []string            `json:"labels"`
	Parent            string              `json:"parent"`
	Body              string              `json:"body"`
	Source            *board.Source       `json:"source"`
	Autonomous        bool                `json:"autonomous"`
	CreatePR          *bool               `json:"create_pr"`
	BaseBranch        string              `json:"base_branch"`
	Vetted            bool                `json:"vetted"`
	Skills            *[]string           `json:"skills,omitempty"`
	ModelOrchestrator string              `json:"model_orchestrator,omitempty"`
	ModelCoder        string              `json:"model_coder,omitempty"`
	ModelReviewer     string              `json:"model_reviewer,omitempty"`
	BestOfN           int                 `json:"best_of_n"`
	MobParticipants   int                 `json:"mob_participants"`
	MobPhases         []string            `json:"mob_phases"`
	MobGuests         []string            `json:"mob_guests"`
	Verify            *board.VerifyConfig `json:"verify,omitempty"`
}

// updateCardRequest is the JSON body for full card updates.
// All fields use value types to match PUT's full-replacement semantics.
type updateCardRequest struct {
	Title             string         `json:"title"`
	Type              string         `json:"type"`
	State             string         `json:"state"`
	Priority          string         `json:"priority"`
	Labels            []string       `json:"labels"`
	Parent            string         `json:"parent"`
	Subtasks          []string       `json:"subtasks"`
	DependsOn         []string       `json:"depends_on"`
	Context           []string       `json:"context"`
	Custom            map[string]any `json:"custom"`
	Body              string         `json:"body"`
	Autonomous        bool           `json:"autonomous"`
	CreatePR          bool           `json:"create_pr"`
	Vetted            bool           `json:"vetted"`
	Skills            *[]string      `json:"skills,omitempty"`
	Phase             *string        `json:"phase,omitempty"`
	ModelOrchestrator string         `json:"model_orchestrator,omitempty"`
	ModelCoder        string         `json:"model_coder,omitempty"`
	ModelReviewer     string         `json:"model_reviewer,omitempty"`
	BestOfN           int            `json:"best_of_n"`
	MobParticipants   int            `json:"mob_participants"`
	MobPhases         []string       `json:"mob_phases"`
	MobGuests         []string       `json:"mob_guests"`
}

// patchCardRequest is the JSON body for partial card updates.
//
// SkillsClear is the explicit "clear" sentinel: pure JSON cannot
// distinguish an omitted `skills` field from an explicit `null`
// (Go decodes both as a nil pointer), so the UI sends
// `{"skills_clear": true}` to mean "set Skills back to nil so the
// project default applies again". This sits alongside the normal
// `skills` field used for explicit list / explicit empty.
type patchCardRequest struct {
	Title             *string   `json:"title,omitempty"`
	Type              *string   `json:"type,omitempty"`
	State             *string   `json:"state,omitempty"`
	Priority          *string   `json:"priority,omitempty"`
	Labels            []string  `json:"labels,omitempty"`
	Body              *string   `json:"body,omitempty"`
	Autonomous        *bool     `json:"autonomous,omitempty"`
	CreatePR          *bool     `json:"create_pr,omitempty"`
	Vetted            *bool     `json:"vetted,omitempty"`
	BaseBranch        *string   `json:"base_branch,omitempty"`
	Skills            *[]string `json:"skills,omitempty"`
	SkillsClear       bool      `json:"skills_clear,omitempty"`
	Phase             *string   `json:"phase,omitempty"`
	ModelOrchestrator *string   `json:"model_orchestrator,omitempty"`
	ModelCoder        *string   `json:"model_coder,omitempty"`
	ModelReviewer     *string   `json:"model_reviewer,omitempty"`
	BestOfN           *int      `json:"best_of_n,omitempty"`
	// Mob session fields: MobParticipants nil = don't change; the two slices
	// follow the Labels convention (nil = don't change, [] = clear).
	MobParticipants *int     `json:"mob_participants,omitempty"`
	MobPhases       []string `json:"mob_phases,omitempty"`
	MobGuests       []string `json:"mob_guests,omitempty"`
	// Verify replaces the whole struct: omitting it preserves the card's
	// override; a present object replaces it (zero value clears it).
	Verify *board.VerifyConfig `json:"verify,omitempty"`
}

// validateCardSkills validates that each skill name in `skills` exists in
// the configured task-skills directory, and (when the project has a
// non-nil default_skills) is a subset of that. Returns nil for nil or
// empty skills slices - those are always valid (mount full set / mount
// nothing respectively).
func (h *cardHandlers) validateCardSkills(r *http.Request, projectName string, skills *[]string) error {
	if skills == nil || len(*skills) == 0 {
		return nil
	}

	ctx := r.Context()

	available, err := h.taskSkills.Names(ctx)
	if err != nil {
		return err
	}

	if err := validateSkillsAgainstAvailable(*skills, available); err != nil {
		return err
	}

	project, err := h.svc.GetProject(ctx, projectName)
	if err != nil {
		return err
	}

	return validateSkillsAgainstProjectDefault(*skills, project.DefaultSkills)
}

// isNonHumanAgent returns true if the request has an agent ID that is not a human user.
// A bare "human:" header is treated as non-human - see board.IsHumanAgentID.
func isNonHumanAgent(r *http.Request) bool {
	agentID := extractAgentID(r)

	return agentID != "" && !board.IsHumanAgentID(agentID)
}

// validateAgentOwnership checks if the requesting agent can mutate a claimed card.
// Returns an error message if unauthorized, empty string if allowed.
// Unclaimed cards can be mutated by anyone.
func validateAgentOwnership(r *http.Request, card *board.Card) string {
	if card.AssignedAgent == "" {
		return "" // Unclaimed cards can be mutated by anyone
	}

	agentID := extractAgentID(r)
	if agentID == "" {
		return "X-Agent-ID header required to modify claimed card"
	}

	if agentID != card.AssignedAgent {
		return "card is claimed by " + card.AssignedAgent
	}

	return ""
}

// validBestOfN reports whether v is an accepted best_of_n value: 0 (off) or
// in the inclusive range 2..maxCandidates. 1 is always rejected - racing a
// single candidate is meaningless.
func validBestOfN(v, maxCandidates int) bool {
	return v == 0 || (v >= 2 && v <= maxCandidates)
}

// mobPhaseAllowed is the closed set of phases a card may request
// discussions in. "execute" is accepted at write time even while the
// server-side mob.execute_checkpoints_enabled flag is off - the trigger
// path drops it with a warning then, so flipping the flag later needs no
// card rewrite.
var mobPhaseAllowed = map[string]bool{"plan": true, "review": true, "execute": true}

// validMob validates the card-level mob session fields against the
// configured bounds, mirroring validBestOfN: participants must be 0 (off) or
// 2..cfg.MaxParticipants; phases must be a duplicate-free subset of
// {plan, review, execute}; guests require participants >= 2 and every name
// must be registered in cfg.Guests. Returns nil when valid.
func validMob(cfg config.MobConfig, participants int, phases, guests []string) error {
	if participants != 0 && (participants < 2 || participants > cfg.MaxParticipants) {
		return fmt.Errorf("mob_participants must be 0 or 2..%d", cfg.MaxParticipants)
	}

	seen := make(map[string]bool, len(phases))

	for _, p := range phases {
		if !mobPhaseAllowed[p] {
			return fmt.Errorf("invalid mob_phases entry %q: must be one of plan, review, execute", p)
		}

		if seen[p] {
			return fmt.Errorf("duplicate mob_phases entry %q", p)
		}

		seen[p] = true
	}

	if len(guests) == 0 {
		return nil
	}

	if participants < 2 {
		return errors.New("mob_guests requires mob_participants >= 2")
	}

	registered := make(map[string]bool, len(cfg.Guests))
	for _, g := range cfg.Guests {
		registered[g.Name] = true
	}

	for _, name := range guests {
		if !registered[name] {
			return fmt.Errorf("unknown mob_guests entry %q: not in the mob.guests registry", name)
		}
	}

	return nil
}

// listCards handles GET /api/projects/{project}/cards.
func (h *cardHandlers) listCards(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	// Build filter from query params
	var vettedFilter *bool

	if v := r.URL.Query().Get("vetted"); v != "" {
		b := v == "true"
		vettedFilter = &b
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	priority := strings.TrimSpace(r.URL.Query().Get("priority"))

	// Validate enum filter values against the project config.
	if state != "" || typ != "" || priority != "" {
		cfg, err := h.svc.GetProject(r.Context(), projectName)
		if err != nil {
			handleServiceError(w, r, err)

			return
		}

		if state != "" && !slices.Contains(cfg.States, state) {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
				"invalid state filter: "+state, "")

			return
		}

		if typ != "" && !slices.Contains(cfg.Types, typ) && typ != "subtask" {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
				"invalid type filter: "+typ, "")

			return
		}

		if priority != "" && !slices.Contains(cfg.Priorities, priority) {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
				"invalid priority filter: "+priority, "")

			return
		}
	}

	filter := storage.CardFilter{
		State:         state,
		Type:          typ,
		Priority:      priority,
		AssignedAgent: r.URL.Query().Get("agent"),
		Label:         r.URL.Query().Get("label"),
		Parent:        r.URL.Query().Get("parent"),
		ExternalID:    r.URL.Query().Get("external_id"),
		Vetted:        vettedFilter,
	}

	// Pagination: parse limit and cursor from query string. Both are optional;
	// defaults mirror the pre-pagination behaviour (one page of up to
	// defaultCardPageLimit cards). Out-of-range limit / malformed cursor
	// produce 400 before any service work.
	limit, ok := parseCardPageLimit(w, r.URL.Query().Get("limit"))
	if !ok {
		return
	}

	cursor := r.URL.Query().Get("cursor")

	page, err := h.svc.ListCardsPage(r.Context(), projectName, filter, service.PageOpts{
		Limit:  limit,
		Cursor: cursor,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid cursor", "")

			return
		}

		handleServiceError(w, r, err)

		return
	}

	resp := listCardsResponse{
		Items:      page.Items,
		NextCursor: page.NextCursor,
	}
	if page.HasTotal {
		total := page.Total
		resp.Total = &total
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseCardPageLimit reads the ?limit= query parameter, enforces bounds, and
// writes a 400 error to w if the value is invalid. Returns (limit, true) on
// success or (0, false) if the caller should abort - in which case the
// response has already been written.
func parseCardPageLimit(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return defaultCardPageLimit, true
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"invalid limit", "limit must be an integer")

		return 0, false
	}

	if n < 1 || n > maxCardPageLimit {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"limit out of range",
			"limit must be between 1 and "+strconv.Itoa(maxCardPageLimit))

		return 0, false
	}

	return n, true
}

// createCard handles POST /api/projects/{project}/cards.
func (h *cardHandlers) createCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project name required", "")

		return
	}

	var req createCardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "title is required", "")

		return
	}

	// Autonomous and model-pin fields can only be set by human users (UI),
	// never by agents - mirrors the update and patch guards. Pins set at
	// create time flow onto the card and reach the agent via get_task_context.
	if isNonHumanAgent(r) && (req.Autonomous || req.CreatePR != nil || req.BaseBranch != "" || req.Vetted ||
		req.ModelOrchestrator != "" || req.ModelCoder != "" || req.ModelReviewer != "" ||
		req.BestOfN != 0 || req.MobParticipants != 0 || len(req.MobPhases) > 0 || len(req.MobGuests) > 0 ||
		req.Verify != nil) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, create_pr, base_branch, vetted, model pins, best_of_n, mob fields, and verify can only be set via the UI")

		return
	}

	if err := h.validateCardSkills(r, projectName, req.Skills); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeValidationError, err.Error(), "")

		return
	}

	if !validBestOfN(req.BestOfN, h.bestOfNMax) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"invalid best_of_n", fmt.Sprintf("must be 0 or 2..%d", h.bestOfNMax))

		return
	}

	if err := validMob(h.mob, req.MobParticipants, req.MobPhases, req.MobGuests); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid mob fields", err.Error())

		return
	}

	input := service.CreateCardInput{
		Title:             req.Title,
		Type:              req.Type,
		Priority:          req.Priority,
		Labels:            req.Labels,
		Parent:            req.Parent,
		Body:              req.Body,
		Source:            req.Source,
		Autonomous:        req.Autonomous,
		CreatePR:          req.CreatePR,
		BaseBranch:        req.BaseBranch,
		Vetted:            req.Vetted,
		Skills:            req.Skills,
		ModelOrchestrator: req.ModelOrchestrator,
		ModelCoder:        req.ModelCoder,
		ModelReviewer:     req.ModelReviewer,
		BestOfN:           req.BestOfN,
		MobParticipants:   req.MobParticipants,
		MobPhases:         req.MobPhases,
		MobGuests:         req.MobGuests,
		Verify:            req.Verify,
	}

	card, err := h.svc.CreateCard(r.Context(), projectName, input)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, card)
}

// getCard handles GET /api/projects/{project}/cards/{id}.
func (h *cardHandlers) getCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	card, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// updateCard handles PUT /api/projects/{project}/cards/{id}.
func (h *cardHandlers) updateCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req updateCardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)

		return
	}

	// Autonomous and model-pin fields can only be changed by human users (UI), never by agents.
	// For PUT semantics, compare against existing values to catch both setting AND clearing.
	if isNonHumanAgent(r) && (req.Autonomous != existingCard.Autonomous ||
		req.CreatePR != existingCard.CreatePR ||
		req.Vetted != existingCard.Vetted ||
		req.ModelOrchestrator != existingCard.ModelOrchestrator ||
		req.ModelCoder != existingCard.ModelCoder ||
		req.ModelReviewer != existingCard.ModelReviewer ||
		req.BestOfN != existingCard.BestOfN ||
		req.MobParticipants != existingCard.MobParticipants ||
		!slices.Equal(req.MobPhases, existingCard.MobPhases) ||
		!slices.Equal(req.MobGuests, existingCard.MobGuests)) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, create_pr, vetted, model pins, best_of_n, and mob fields can only be changed via the UI")

		return
	}

	if !validBestOfN(req.BestOfN, h.bestOfNMax) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"invalid best_of_n", fmt.Sprintf("must be 0 or 2..%d", h.bestOfNMax))

		return
	}

	if err := validMob(h.mob, req.MobParticipants, req.MobPhases, req.MobGuests); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid mob fields", err.Error())

		return
	}

	if err := h.validateCardSkills(r, projectName, req.Skills); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeValidationError, err.Error(), "")

		return
	}

	input := service.UpdateCardInput{
		Title:             req.Title,
		Type:              req.Type,
		State:             req.State,
		Priority:          req.Priority,
		Labels:            req.Labels,
		Parent:            req.Parent,
		Subtasks:          req.Subtasks,
		DependsOn:         req.DependsOn,
		Context:           req.Context,
		Custom:            req.Custom,
		Body:              req.Body,
		ImmediateCommit:   existingCard.AssignedAgent == "",
		Autonomous:        req.Autonomous,
		CreatePR:          req.CreatePR,
		Vetted:            req.Vetted,
		Skills:            req.Skills,
		Phase:             req.Phase,
		ModelOrchestrator: req.ModelOrchestrator,
		ModelCoder:        req.ModelCoder,
		ModelReviewer:     req.ModelReviewer,
		BestOfN:           req.BestOfN,
		MobParticipants:   req.MobParticipants,
		MobPhases:         req.MobPhases,
		MobGuests:         req.MobGuests,
	}

	card, err := h.svc.UpdateCard(r.Context(), projectName, cardID, input)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// patchCard handles PATCH /api/projects/{project}/cards/{id}.
func (h *cardHandlers) patchCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	var req patchCardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Autonomous and model-pin fields can only be set by human users (UI), never by agents.
	if isNonHumanAgent(r) && (req.Autonomous != nil ||
		req.CreatePR != nil ||
		req.Vetted != nil ||
		req.BaseBranch != nil ||
		req.ModelOrchestrator != nil ||
		req.ModelCoder != nil ||
		req.ModelReviewer != nil ||
		req.BestOfN != nil ||
		req.MobParticipants != nil ||
		req.MobPhases != nil ||
		req.MobGuests != nil ||
		req.Verify != nil) {
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"forbidden", "autonomous, create_pr, vetted, base_branch, model pins, best_of_n, mob fields, and verify can only be set via the UI")

		return
	}

	if req.BestOfN != nil && !validBestOfN(*req.BestOfN, h.bestOfNMax) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest,
			"invalid best_of_n", fmt.Sprintf("must be 0 or 2..%d", h.bestOfNMax))

		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)

		return
	}

	if req.MobParticipants != nil || req.MobPhases != nil || req.MobGuests != nil {
		// Validate the RESULTING state, not the patch in isolation.
		effParticipants := existingCard.MobParticipants
		if req.MobParticipants != nil {
			effParticipants = *req.MobParticipants
		}

		effPhases := existingCard.MobPhases
		if req.MobPhases != nil {
			effPhases = req.MobPhases
		}

		effGuests := existingCard.MobGuests
		if req.MobGuests != nil {
			effGuests = req.MobGuests
		}

		if err := validMob(h.mob, effParticipants, effPhases, effGuests); err != nil {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid mob fields", err.Error())

			return
		}
	}

	if err := h.validateCardSkills(r, projectName, req.Skills); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeValidationError, err.Error(), "")

		return
	}

	input := service.PatchCardInput{
		Title:             req.Title,
		Type:              req.Type,
		State:             req.State,
		Priority:          req.Priority,
		Labels:            req.Labels,
		Body:              req.Body,
		ImmediateCommit:   existingCard.AssignedAgent == "",
		Autonomous:        req.Autonomous,
		CreatePR:          req.CreatePR,
		Vetted:            req.Vetted,
		BaseBranch:        req.BaseBranch,
		Skills:            req.Skills,
		SkillsClear:       req.SkillsClear,
		Phase:             req.Phase,
		ModelOrchestrator: req.ModelOrchestrator,
		ModelCoder:        req.ModelCoder,
		ModelReviewer:     req.ModelReviewer,
		BestOfN:           req.BestOfN,
		MobParticipants:   req.MobParticipants,
		MobPhases:         req.MobPhases,
		MobGuests:         req.MobGuests,
		Verify:            req.Verify,
	}

	card, err := h.svc.PatchCard(r.Context(), projectName, cardID, input)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, card)
}

// deleteCard handles DELETE /api/projects/{project}/cards/{id}.
func (h *cardHandlers) deleteCard(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	cardID := r.PathValue("id")

	if projectName == "" || cardID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project and card ID required", "")

		return
	}

	// Check agent ownership for claimed cards
	existingCard, err := h.svc.GetCard(r.Context(), projectName, cardID)
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	if errMsg := validateAgentOwnership(r, existingCard); errMsg != "" {
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent mismatch", errMsg)

		return
	}

	if err := h.svc.DeleteCard(r.Context(), projectName, cardID); err != nil {
		handleServiceError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
