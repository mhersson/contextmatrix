package api

import "net/http"

// appConfigHandlers holds handlers for the app config endpoint.
type appConfigHandlers struct {
	theme string
}

// appConfigResponse is the JSON response for GET /api/app/config.
type appConfigResponse struct {
	Theme string `json:"theme"`
}

// getAppConfig handles GET /api/app/config.
// Returns the active color palette name. Unauthenticated; safe for public read.
func (h *appConfigHandlers) getAppConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, appConfigResponse{Theme: h.theme})
}
