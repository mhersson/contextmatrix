package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// SentinelStore is the ops.db side of the Sentinel endpoints.
type SentinelStore interface {
	SentinelSettings(ctx context.Context) (sentinel.Settings, time.Time, string, error)
	PutSentinelSettings(ctx context.Context, s sentinel.Settings, by string) error
	CardRunRow(ctx context.Context, project, cardID string) (*sentinel.Run, error)
}

type sentinelHandlers struct {
	// ctl stays a concrete pointer: nil means no execution backend.
	ctl         *sentinel.Controller
	store       SentinelStore
	bus         *events.Bus
	authEnabled bool
}

type sentinelResponse struct {
	Status    sentinel.Status   `json:"status"`
	Settings  sentinel.Settings `json:"settings"`
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
}

type cardSentinelRun struct {
	Origin      string `json:"origin"`
	Autonomous  bool   `json:"autonomous"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	StartedAt   string `json:"started_at"`
	StopCause   string `json:"stop_cause,omitempty"`
	StoppedAt   string `json:"stopped_at,omitempty"`
	HumanStop   bool   `json:"human_stop"`
}

func (h *sentinelHandlers) build(ctx context.Context) (sentinelResponse, error) {
	set, at, by, err := h.store.SentinelSettings(ctx)
	if err != nil {
		return sentinelResponse{}, err
	}

	resp := sentinelResponse{Settings: set, UpdatedBy: by}
	if !at.IsZero() {
		resp.UpdatedAt = at.UTC().Format(time.RFC3339)
	}

	if h.ctl != nil {
		resp.Status = h.ctl.Status()
	} else {
		resp.Status = sentinel.NotRunningStatus(set)
	}

	return resp, nil
}

func (h *sentinelHandlers) get(w http.ResponseWriter, r *http.Request) {
	resp, err := h.build(r.Context())
	if err != nil {
		slog.Error("sentinel status failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read sentinel", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *sentinelHandlers) put(w http.ResponseWriter, r *http.Request) {
	if h.authEnabled && requireAdmin(w, r) == nil {
		return
	}

	var set sentinel.Settings
	if !decodeJSON(w, r, &set) {
		return
	}

	by := extractAgentID(r)
	if by == "" {
		by = "human:web"
	}

	switch err := h.store.PutSentinelSettings(r.Context(), set, by); {
	case errors.Is(err, sentinel.ErrInvalidSettings):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, err.Error(), "")

		return
	case err != nil:
		slog.Error("save sentinel settings failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to save sentinel settings", "")

		return
	}

	if h.ctl != nil {
		h.ctl.Nudge()
	}

	if h.bus != nil {
		h.bus.Publish(events.Event{Type: events.SentinelChanged, Agent: by, Timestamp: time.Now()})
	}

	h.get(w, r)
}

func (h *sentinelHandlers) cardRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.CardRunRow(r.Context(), r.PathValue("project"), strings.ToUpper(r.PathValue("id")))
	if err != nil {
		slog.Error("sentinel card run failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read sentinel run", "")

		return
	}

	if run == nil {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil})

		return
	}

	view := cardSentinelRun{
		Origin:      run.Launch.Origin,
		Autonomous:  run.Launch.Autonomous,
		Attempts:    run.Attempts,
		MaxAttempts: sentinel.MaxHealAttempts,
		StartedAt:   run.StartedAt.UTC().Format(time.RFC3339),
		StopCause:   run.StopCause,
		HumanStop:   run.HumanStop,
	}
	if run.StoppedAt != nil {
		view.StoppedAt = run.StoppedAt.UTC().Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, map[string]any{"run": view})
}
