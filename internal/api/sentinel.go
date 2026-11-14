package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// SentinelStore is the ops.db side of the Sentinel endpoints.
type SentinelStore interface {
	SentinelSettings(ctx context.Context) (sentinel.Settings, time.Time, string, error)
	PutSentinelSettings(ctx context.Context, s sentinel.Settings, by string) error
	CardRunRow(ctx context.Context, project, cardID string) (*sentinel.Run, error)
	CardHeals(ctx context.Context, project, cardID string) ([]sentinel.Heal, error)
	CardFindings(ctx context.Context, project, cardID string) ([]sentinel.Finding, error)
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

type cardSentinelChange struct {
	SectionBefore string              `json:"section_before"`
	SectionAfter  string              `json:"section_after"`
	VerifyBefore  *board.VerifyConfig `json:"verify_before,omitempty"`
	VerifyAfter   *board.VerifyConfig `json:"verify_after,omitempty"`
	Retired       []string            `json:"retired"`
	Fresh         bool                `json:"fresh"`
}

type cardSentinelHeal struct {
	ID         int64               `json:"id"`
	Kind       string              `json:"kind"`
	Cause      string              `json:"cause"`
	Stage      string              `json:"stage"`
	Reason     string              `json:"reason"`
	Diagnosis  string              `json:"diagnosis"`
	SourceCard string              `json:"source_card,omitempty"`
	Change     *cardSentinelChange `json:"change,omitempty"`
	SpendUSD   float64             `json:"spend_usd"`
	CreatedAt  string              `json:"created_at"`
	UpdatedAt  string              `json:"updated_at"`
}

type cardSentinelFinding struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Evidence  string `json:"evidence"`
	CreatedAt string `json:"created_at"`
}

func healView(h sentinel.Heal) cardSentinelHeal {
	v := cardSentinelHeal{
		ID: h.ID, Kind: h.Kind, Cause: h.Cause, Stage: h.Stage, Reason: h.Reason,
		SourceCard: h.SourceCard, SpendUSD: h.SpendUSD,
		CreatedAt: h.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: h.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if h.Investigation != nil {
		v.Diagnosis = h.Investigation.Diagnosis
	}

	if c := h.Change; c != nil {
		v.Change = &cardSentinelChange{
			SectionBefore: c.SectionBefore, SectionAfter: c.SectionAfter,
			VerifyBefore: c.VerifyBefore, VerifyAfter: c.VerifyAfter,
			Retired: append([]string{}, c.Retired...), Fresh: c.Fresh,
		}
	}

	return v
}

// canSeeAdminNotes reports whether the caller may read project_verify findings:
// admins in multi mode, everyone in none mode.
func (h *sentinelHandlers) canSeeAdminNotes(r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	u := sessionUserFromContext(r.Context())

	return u != nil && u.IsAdmin
}

func (h *sentinelHandlers) cardRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	project, id := r.PathValue("project"), strings.ToUpper(r.PathValue("id"))

	run, err := h.store.CardRunRow(ctx, project, id)
	if err != nil {
		slog.Error("sentinel card run failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read sentinel run", "")

		return
	}

	heals, err := h.store.CardHeals(ctx, project, id)
	if err != nil {
		slog.Error("sentinel card heals failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read sentinel heals", "")

		return
	}

	found, err := h.store.CardFindings(ctx, project, id)
	if err != nil {
		slog.Error("sentinel card findings failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read sentinel findings", "")

		return
	}

	healViews := make([]cardSentinelHeal, 0, len(heals))
	for _, hl := range heals {
		healViews = append(healViews, healView(hl))
	}

	admin := h.canSeeAdminNotes(r)
	findings := make([]cardSentinelFinding, 0, len(found))

	for _, f := range found {
		if f.Kind == protocol.FindingProjectVerify && !admin {
			continue
		}

		findings = append(findings, cardSentinelFinding{
			Kind: f.Kind, Title: f.Title, Text: f.Text, Evidence: f.Evidence,
			CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	out := map[string]any{"run": nil, "heals": healViews, "findings": findings}

	if run != nil {
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

		out["run"] = view
	}

	writeJSON(w, http.StatusOK, out)
}
