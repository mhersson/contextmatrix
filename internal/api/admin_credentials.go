package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
)

type credentialResponse struct {
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Host           string     `json:"host"`
	APIBaseURL     string     `json:"api_base_url"`
	AppID          int64      `json:"app_id"`
	InstallationID int64      `json:"installation_id"`
	CreatedBy      string     `json:"created_by"`
	Disabled       bool       `json:"disabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
}

func toCredentialResponse(c *authstore.Credential) credentialResponse {
	return credentialResponse{
		Name: c.Name, Kind: string(c.Kind), Host: c.Host, APIBaseURL: c.APIBaseURL,
		AppID: c.AppID, InstallationID: c.InstallationID, CreatedBy: c.CreatedBy,
		Disabled: c.Disabled, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, LastUsedAt: c.LastUsedAt,
	}
}

// listCredentials handles GET /api/admin/credentials — metadata only.
func (h *adminHandlers) listCredentials(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	creds, err := h.svc.ListCredentials(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	out := make([]credentialResponse, len(creds))
	for i, c := range creds {
		out[i] = toCredentialResponse(c)
	}

	writeJSON(w, http.StatusOK, out)
}

// createCredential handles POST /api/admin/credentials — shape-checked,
// validated live against GitHub, encrypted, stored. The secret exists only
// in the request body.
func (h *adminHandlers) createCredential(w http.ResponseWriter, r *http.Request) {
	actor := requireAdmin(w, r)
	if actor == nil {
		return
	}

	var req struct {
		Name           string `json:"name"`
		Kind           string `json:"kind"`
		Host           string `json:"host"`
		APIBaseURL     string `json:"api_base_url"`
		AppID          int64  `json:"app_id"`
		InstallationID int64  `json:"installation_id"`
		Secret         string `json:"secret"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	in := auth.CredentialInput{
		Name: req.Name, Kind: authstore.CredentialKind(req.Kind), Host: req.Host,
		APIBaseURL: req.APIBaseURL, AppID: req.AppID, InstallationID: req.InstallationID, Secret: req.Secret,
	}

	if err := h.svc.CreateCredential(r.Context(), in, "human:"+actor.Username); err != nil {
		writeCredentialError(w, r, err)

		return
	}

	cred, err := h.svc.ListCredentials(r.Context())
	if err != nil {
		handleServiceError(w, r, err)

		return
	}

	for _, c := range cred {
		if c.Name == in.Name {
			writeJSON(w, http.StatusCreated, toCredentialResponse(c))

			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
}

// putCredential handles PUT /api/admin/credentials/{name}: secret → rotate;
// metadata fields → update; disabled → toggle. Fields compose in one call.
func (h *adminHandlers) putCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	name := r.PathValue("name")

	var req struct {
		Secret         *string `json:"secret"`
		Host           *string `json:"host"`
		APIBaseURL     *string `json:"api_base_url"`
		AppID          *int64  `json:"app_id"`
		InstallationID *int64  `json:"installation_id"`
		Disabled       *bool   `json:"disabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	if req.Host != nil || req.APIBaseURL != nil || req.AppID != nil || req.InstallationID != nil {
		current, err := h.currentCredential(r, name)
		if err != nil {
			writeCredentialError(w, r, err)

			return
		}

		host, apiBase, appID, instID := current.Host, current.APIBaseURL, current.AppID, current.InstallationID
		if req.Host != nil {
			host = *req.Host
		}

		if req.APIBaseURL != nil {
			apiBase = *req.APIBaseURL
		}

		if req.AppID != nil {
			appID = *req.AppID
		}

		if req.InstallationID != nil {
			instID = *req.InstallationID
		}

		if err := h.svc.UpdateCredentialMetadata(r.Context(), name, host, apiBase, appID, instID); err != nil {
			writeCredentialError(w, r, err)

			return
		}
	}

	if req.Secret != nil {
		if err := h.svc.RotateCredentialSecret(r.Context(), name, *req.Secret); err != nil {
			writeCredentialError(w, r, err)

			return
		}
	}

	if req.Disabled != nil {
		if err := h.svc.SetCredentialDisabled(r.Context(), name, *req.Disabled); err != nil {
			writeCredentialError(w, r, err)

			return
		}
	}

	cred, err := h.currentCredential(r, name)
	if err != nil {
		writeCredentialError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, toCredentialResponse(cred))
}

// deleteCredential handles DELETE /api/admin/credentials/{name}.
func (h *adminHandlers) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	name := r.PathValue("name")

	// Refuse to delete a credential that projects are bound to — the admin
	// rebinds them first. 409 lists the blockers by name.
	if h.listProjectConfigs != nil {
		configs, err := h.listProjectConfigs(r.Context())
		if err != nil {
			handleServiceError(w, r, err)

			return
		}

		var bound []string

		for _, cfg := range configs {
			if cfg.GitHubCredential == name {
				bound = append(bound, cfg.Name)
			}
		}

		if len(bound) > 0 {
			writeError(w, http.StatusConflict, ErrCodeValidationError,
				"credential is bound to projects", "rebind first: "+strings.Join(bound, ", "))

			return
		}
	}

	if err := h.svc.DeleteCredential(r.Context(), name); err != nil {
		writeCredentialError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *adminHandlers) currentCredential(r *http.Request, name string) (*authstore.Credential, error) {
	creds, err := h.svc.ListCredentials(r.Context())
	if err != nil {
		return nil, err
	}

	for _, c := range creds {
		if c.Name == name {
			return c, nil
		}
	}

	return nil, authstore.ErrNotFound
}

// writeCredentialError maps credential-pool errors.
func writeCredentialError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredential):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid credential", sanitizeErrorDetails(err))
	case errors.Is(err, auth.ErrCredentialRejected):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "credential rejected by GitHub", sanitizeErrorDetails(err))
	case errors.Is(err, authstore.ErrDuplicate):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "credential name already taken", "")
	case errors.Is(err, authstore.ErrNotFound):
		writeError(w, http.StatusNotFound, ErrCodeValidationError, "credential not found", "")
	case errors.Is(err, auth.ErrNoCredentialKey):
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "credential key not configured", "")
	default:
		handleServiceError(w, r, err)
	}
}
