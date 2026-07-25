package api

import (
	"context"
	"net/http"

	"github.com/mhersson/contextmatrix/internal/authstore"
)

// UserLister is the consumer-side slice of auth.Service used by the user
// roster endpoint and (in a later task) assignee validation. *auth.Service
// satisfies this interface.
type UserLister interface {
	ListUsers(ctx context.Context) ([]*authstore.User, error)
	UserByUsername(ctx context.Context, username string) (*authstore.User, error)
}

// userSummary is the public roster projection of authstore.User - deliberately
// not adminUserResponse, which leaks has_password/last_login_at.
type userSummary struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// userHandlers contains handlers for the user-roster endpoint.
type userHandlers struct {
	users UserLister
}

// listUsers handles GET /api/users. Session-gated for any role - the
// assignee picker needs the roster for ordinary card work, so admin-gating
// here would over-gate (docs/architecture.md: "ordinary card work needs only
// a valid session").
func (h *userHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.ListUsers(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	out := make([]userSummary, 0, len(users))

	for _, u := range users {
		if u.Disabled {
			continue
		}

		out = append(out, userSummary{Username: u.Username, DisplayName: u.DisplayName})
	}

	writeJSON(w, http.StatusOK, out)
}
