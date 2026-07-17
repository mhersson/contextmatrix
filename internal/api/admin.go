package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
)

// adminHandlers maps auth.Service admin operations onto /api/admin/*.
// Every handler starts with requireAdmin - the session guard has already
// authenticated the caller; this adds the role check.
type adminHandlers struct {
	svc *auth.Service
	// listProjectConfigs backs deleteCredential's bound-project guard; wired
	// from cfg.Service.ListProjects in NewRouter. nil when no card service is
	// configured (narrow-scope test routers) - the guard is then skipped
	// rather than dereferencing a nil *service.CardService.
	listProjectConfigs func(ctx context.Context) ([]board.ProjectConfig, error)
}

// requireAdmin returns the acting admin, or writes 403 and returns nil.
func requireAdmin(w http.ResponseWriter, r *http.Request) *authstore.User {
	user := sessionUserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "admin access required", "")

		return nil
	}

	return user
}

type adminUserResponse struct {
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	IsAdmin     bool       `json:"is_admin"`
	Disabled    bool       `json:"disabled"`
	HasPassword bool       `json:"has_password"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func toAdminUser(u *authstore.User) adminUserResponse {
	return adminUserResponse{
		Username:    u.Username,
		DisplayName: u.DisplayName,
		IsAdmin:     u.IsAdmin,
		Disabled:    u.Disabled,
		HasPassword: u.PasswordHash != nil,
		LastLoginAt: u.LastLoginAt,
	}
}

type inviteResponse struct {
	Token     string    `json:"token"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expires_at"`
}

// listUsers handles GET /api/admin/users.
func (h *adminHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	out := make([]adminUserResponse, len(users))
	for i, u := range users {
		out[i] = toAdminUser(u)
	}

	writeJSON(w, http.StatusOK, out)
}

// createUser handles POST /api/admin/users - creates the account and returns
// the invite token (the UI composes the /auth/token/<raw> URL; the server
// does not know its public address).
func (h *adminHandlers) createUser(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		IsAdmin     bool   `json:"is_admin"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	user, invite, err := h.svc.CreateUserWithInvite(r.Context(), req.Username, req.DisplayName, req.IsAdmin)
	if err != nil {
		writeAdminUserError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user": toAdminUser(user),
		"invite": inviteResponse{
			Token:     invite,
			Purpose:   string(authstore.TokenPurposeInvite),
			ExpiresAt: time.Now().UTC().Add(auth.OneTimeTokenTTL),
		},
	})
}

// patchUser handles PATCH /api/admin/users/{username}. Pointer fields: only
// keys present in the body are applied.
func (h *adminHandlers) patchUser(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	username := r.PathValue("username")

	var req struct {
		DisplayName *string `json:"display_name"`
		IsAdmin     *bool   `json:"is_admin"`
		Disabled    *bool   `json:"disabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	// Evaluate the whole patch before applying any field, so a mid-patch
	// last-admin refusal cannot leave earlier fields persisted.
	if (req.IsAdmin != nil && !*req.IsAdmin) || (req.Disabled != nil && *req.Disabled) {
		target, err := h.svc.UserByUsername(r.Context(), username)
		if err != nil {
			writeAdminUserError(w, r, err)

			return
		}

		if target.IsAdmin && !target.Disabled {
			if err := h.svc.CheckNotLastAdmin(r.Context()); err != nil {
				writeAdminUserError(w, r, err)

				return
			}
		}
	}

	apply := func(err error) bool {
		if err != nil {
			writeAdminUserError(w, r, err)

			return false
		}

		return true
	}

	if req.DisplayName != nil && !apply(h.svc.SetUserDisplayName(r.Context(), username, *req.DisplayName)) {
		return
	}

	if req.IsAdmin != nil && !apply(h.svc.SetUserAdmin(r.Context(), username, *req.IsAdmin)) {
		return
	}

	if req.Disabled != nil && !apply(h.svc.SetUserDisabled(r.Context(), username, *req.Disabled)) {
		return
	}

	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	for _, u := range users {
		if u.Username == authstore.NormalizeUsername(username) {
			writeJSON(w, http.StatusOK, toAdminUser(u))

			return
		}
	}

	writeError(w, http.StatusNotFound, ErrCodeUserNotFound, "user not found", "")
}

// regenerateLink handles POST /api/admin/users/{username}/invite.
func (h *adminHandlers) regenerateLink(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	raw, purpose, err := h.svc.RegenerateLink(r.Context(), r.PathValue("username"))
	if err != nil {
		writeAdminUserError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, inviteResponse{
		Token:     raw,
		Purpose:   string(purpose),
		ExpiresAt: time.Now().UTC().Add(auth.OneTimeTokenTTL),
	})
}

// writeAdminUserError maps admin-user operation errors.
func writeAdminUserError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, ErrCodeValidationError, "cannot remove the last admin", "")
	case errors.Is(err, authstore.ErrDuplicate):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "username already taken", "")
	case errors.Is(err, authstore.ErrInvalidUsername):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid username", "1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation")
	case errors.Is(err, authstore.ErrNotFound):
		writeError(w, http.StatusNotFound, ErrCodeUserNotFound, "user not found", "")
	default:
		handleServiceError(w, r, err)
	}
}
