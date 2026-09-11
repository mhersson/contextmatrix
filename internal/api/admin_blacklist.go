package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

const ErrCodeModelNotBlacklisted = "MODEL_NOT_BLACKLISTED"

// Attribution for a manual add. In none mode there is no session to name;
// in multi mode the admin's username replaces it.
const (
	blacklistOperator      = "operator"
	blacklistDefaultReason = "blacklisted by operator"
)

// blacklistAdminStore is the op-store surface the admin model-blacklist
// endpoints need. Deliberately separate from blacklistReader
// (backend_handlers.go): that one is slug-only because runCard's trigger
// path never needs reasons or deletion, and widening it would force every
// consumer and test double of the narrow surface to grow methods it never
// calls. opstore/sqlite.Store implements both.
type blacklistAdminStore interface {
	BlacklistEntries(ctx context.Context) ([]sqlite.BlacklistEntry, error)
	DeleteBlacklistEntry(ctx context.Context, slug string) (bool, error)
	// InsertBlacklistEntry writes the same row the MCP report_incapable_model
	// tool does, but never overwrites one: a manual add on a listed slug
	// leaves the agent's reason and sample card alone.
	InsertBlacklistEntry(ctx context.Context, slug, reason, reportedBy string) (bool, error)
}

// blacklistAdminHandlers serves GET and POST /api/admin/model-blacklist and
// DELETE /api/admin/model-blacklist/{slug...}.
type blacklistAdminHandlers struct {
	store blacklistAdminStore
	// authEnabled mirrors "multi mode": when true, both endpoints require an
	// admin session. In none mode they are open, same trust posture as the
	// model-outcomes endpoints.
	authEnabled bool
}

// modelBlacklistResponse is the GET /api/admin/model-blacklist body.
type modelBlacklistResponse struct {
	Models []modelBlacklistEntry `json:"models"`
}

// modelBlacklistEntry is one blacklisted model. Timestamps are unix seconds.
type modelBlacklistEntry struct {
	Slug       string `json:"slug"`
	Reason     string `json:"reason"`
	SampleCard string `json:"sample_card,omitempty"`
	ReportedBy string `json:"reported_by"`
	FirstSeen  int64  `json:"first_seen"`
	LastSeen   int64  `json:"last_seen"`
}

func (h *blacklistAdminHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// modelBlacklistAddRequest is the POST /api/admin/model-blacklist body.
type modelBlacklistAddRequest struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// modelBlacklistAddResponse is the POST /api/admin/model-blacklist body.
// Created is false when the slug was already listed and nothing changed.
type modelBlacklistAddResponse struct {
	Slug    string `json:"slug"`
	Created bool   `json:"created"`
}

// add handles POST /api/admin/model-blacklist: an operator blacklisting a
// model by hand, as opposed to the agent reporting one incapable over MCP.
// A listed slug is left as it is (created:false), so a stale page can never
// clobber an agent report. The slug is not checked against the catalog, so
// a model that is not a candidate today is still excluded the day it
// becomes one.
func (h *blacklistAdminHandlers) add(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	var req modelBlacklistAddRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "slug is required", "")

		return
	}

	if detail := blacklistSlugProblem(slug); detail != "" {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid slug", detail)

		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = blacklistDefaultReason
	}

	reportedBy := blacklistOperator
	if u := sessionUserFromContext(r.Context()); u != nil {
		reportedBy = u.Username
	}

	created, err := h.store.InsertBlacklistEntry(r.Context(), slug, reason, reportedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to add blacklist entry", "")

		return
	}

	writeJSON(w, http.StatusOK, modelBlacklistAddResponse{Slug: slug, Created: created})
}

// blacklistSlugProblem says why a slug cannot be listed, or "" when it can.
// The delist route takes the slug as a raw path remainder (the web client
// sends it unencoded), so anything the router would decode, redirect or
// split differently must be refused up front or the row could only be
// removed with SQL.
func blacklistSlugProblem(slug string) string {
	if strings.ContainsAny(slug, " \t\r\n?#%") {
		return "slug must not contain whitespace, '?', '#' or '%'"
	}

	if strings.HasPrefix(slug, "/") || strings.HasSuffix(slug, "/") || strings.Contains(slug, "//") {
		return "slug must not start or end with '/' or contain '//'"
	}

	for seg := range strings.SplitSeq(slug, "/") {
		if seg == "." || seg == ".." {
			return "slug must not contain '.' or '..' path segments"
		}
	}

	return ""
}

// list handles GET /api/admin/model-blacklist.
func (h *blacklistAdminHandlers) list(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	entries, err := h.store.BlacklistEntries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read model blacklist", "")

		return
	}

	resp := modelBlacklistResponse{Models: make([]modelBlacklistEntry, 0, len(entries))}

	for _, e := range entries {
		resp.Models = append(resp.Models, modelBlacklistEntry{
			Slug:       e.Slug,
			Reason:     e.Reason,
			SampleCard: e.SampleCard,
			ReportedBy: e.ReportedBy,
			FirstSeen:  e.FirstSeen,
			LastSeen:   e.LastSeen,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// delist handles DELETE /api/admin/model-blacklist/{slug...}. The rest
// wildcard is required: model slugs contain a slash (z-ai/glm-5.2).
func (h *blacklistAdminHandlers) delist(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	slug := r.PathValue("slug")

	deleted, err := h.store.DeleteBlacklistEntry(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to delete blacklist entry", "")

		return
	}

	if !deleted {
		writeError(w, http.StatusNotFound, ErrCodeModelNotBlacklisted, "model is not blacklisted", slug)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": slug})
}
