package api

import "net/http"

type appConfigHandlers struct {
	theme   string
	version string
}

type appConfigResponse struct {
	Theme   string `json:"theme"`
	Version string `json:"version"`
}

func (h *appConfigHandlers) getAppConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, appConfigResponse{Theme: h.theme, Version: h.version})
}
