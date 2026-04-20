package api

import (
	"context"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/service"
)

type readinessHandlers struct {
	svc *service.CardService
}

// checkJSON is the per-check JSON representation for /readyz responses.
// CheckResult.Err is an error interface; we convert it to a string for JSON output.
type checkJSON struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type readyzResponse struct {
	Status string      `json:"status"`
	Checks []checkJSON `json:"checks"`
}

func (h *readinessHandlers) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	defer cancel()

	results := h.svc.HealthCheck(ctx)

	allOK := true
	checks := make([]checkJSON, 0, len(results))

	for _, c := range results {
		cj := checkJSON{
			Name: c.Name,
			OK:   c.OK,
		}
		if c.Err != nil {
			cj.Error = c.Err.Error()
		}

		if !c.OK {
			allOK = false
		}

		checks = append(checks, cj)
	}

	statusCode := http.StatusOK
	statusStr := "ok"

	if !allOK {
		statusCode = http.StatusServiceUnavailable
		statusStr = "degraded"
	}

	writeJSON(w, statusCode, readyzResponse{Status: statusStr, Checks: checks})
}
