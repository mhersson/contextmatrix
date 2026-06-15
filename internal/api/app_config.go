package api

import "net/http"

type appConfigHandlers struct {
	theme       string
	version     string
	taskBackend string
}

type appConfigResponse struct {
	Theme       string `json:"theme"`
	Version     string `json:"version"`
	TaskBackend string `json:"task_backend"`
}

func (h *appConfigHandlers) getAppConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, appConfigResponse{
		Theme:       h.theme,
		Version:     h.version,
		TaskBackend: h.taskBackend,
	})
}
