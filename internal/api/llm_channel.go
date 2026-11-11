package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// LLMChannelState is the holder surface the LLM channel endpoints need.
// Implemented by llmchannel.Holder.
type LLMChannelState interface {
	Mode() llmchannel.Mode
	Switchable() bool
	Set(ctx context.Context, mode llmchannel.Mode, by string) error
	Updated() (time.Time, string)
	EndpointType() string
	ClaudeCodeConfigured() bool
	Supported(kind llmchannel.Kind) bool
}

// llmChannelStore is the ops.db surface behind in-flight counts and usage.
// Implemented by opstore/sqlite.Store.
type llmChannelStore interface {
	OpenCardRuns(ctx context.Context) ([]sqlite.CardRun, error)
	CloseCardRun(ctx context.Context, project, cardID string, startedNotAfter, at time.Time) error
	CountLiveSessionsByMode(ctx context.Context) (map[string]map[string]int, error)
	SubscriptionUsage(ctx context.Context) ([]sqlite.SubscriptionUsageRow, error)
}

// llmChannelCards reads a card's worker status. Implemented by
// service.CardService.
type llmChannelCards interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
}

// LLMChannelStore is the exported name RouterConfig uses for llmChannelStore.
type LLMChannelStore = llmChannelStore

// LLMChannelCards is the exported name RouterConfig uses for llmChannelCards.
type LLMChannelCards = llmChannelCards

type llmChannelHandlers struct {
	state       LLMChannelState
	store       llmChannelStore
	cards       llmChannelCards
	backends    []string // backend kinds configured under backends:
	authEnabled bool
	now         func() time.Time
}

type llmChannelEndpoint struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

type llmChannelCounts struct {
	Cards      int `json:"cards"`
	Chats      int `json:"chats"`
	Blueprints int `json:"blueprints"`
}

// llmChannelRunsOnNone is an exception's runs_on when no endpoint is configured.
const llmChannelRunsOnNone = "none"

type llmChannelException struct {
	Backend string `json:"backend"`
	RunsOn  string `json:"runs_on"` // endpoint, or none
}

type llmChannelUsage struct {
	Window      string  `json:"window"`
	Utilization float64 `json:"utilization"`
	Status      string  `json:"status"`
	ResetsAt    string  `json:"resets_at"`
	SeenAt      string  `json:"seen_at"`
	Stale       bool    `json:"stale"`
}

type llmChannelResponse struct {
	Mode       string                      `json:"mode"`
	Switchable bool                        `json:"switchable"`
	Endpoint   *llmChannelEndpoint         `json:"endpoint"`
	ClaudeCode bool                        `json:"claude_code"`
	InFlight   map[string]llmChannelCounts `json:"in_flight"`
	Exceptions []llmChannelException       `json:"exceptions"`
	Usage      []llmChannelUsage           `json:"usage"`
	UpdatedAt  string                      `json:"updated_at,omitempty"`
	UpdatedBy  string                      `json:"updated_by,omitempty"`
}

type llmChannelPutRequest struct {
	Mode string `json:"mode"`
}

func (h *llmChannelHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

func (h *llmChannelHandlers) get(w http.ResponseWriter, r *http.Request) {
	resp, err := h.build(r.Context())
	if err != nil {
		slog.Error("llm channel status failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read llm channel", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *llmChannelHandlers) put(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	var req llmChannelPutRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	by := extractAgentID(r)
	if by == "" {
		by = "human:web"
	}

	switch err := h.state.Set(r.Context(), llmchannel.Mode(req.Mode), by); {
	case errors.Is(err, llmchannel.ErrUnknownMode):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "unknown llm channel mode", "")

		return
	case errors.Is(err, llmchannel.ErrNotConfigured):
		writeError(w, http.StatusConflict, ErrCodeBadRequest, "llm channel mode is not configured", "")

		return
	case err != nil:
		slog.Error("set llm channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to switch llm channel", "")

		return
	}

	h.get(w, r)
}

func (h *llmChannelHandlers) build(ctx context.Context) (llmChannelResponse, error) {
	at, by := h.state.Updated()

	resp := llmChannelResponse{
		Mode:       string(h.state.Mode()),
		Switchable: h.state.Switchable(),
		ClaudeCode: h.state.ClaudeCodeConfigured(),
		Exceptions: []llmChannelException{},
		Usage:      []llmChannelUsage{},
		UpdatedBy:  by,
	}

	if !at.IsZero() {
		resp.UpdatedAt = at.UTC().Format(time.RFC3339)
	}

	if typ := h.state.EndpointType(); typ != "" {
		label := "Endpoint"
		if typ == config.LLMEndpointTypeOpenRouter {
			label = "OpenRouter"
		}

		resp.Endpoint = &llmChannelEndpoint{Type: typ, Label: label}
	}

	if resp.ClaudeCode {
		// Without an endpoint an unsupported backend gets no llm_endpoint and fails closed.
		runsOn := string(llmchannel.ModeEndpoint)
		if resp.Endpoint == nil {
			runsOn = llmChannelRunsOnNone
		}

		for _, kind := range h.backends {
			if !h.state.Supported(kind) {
				resp.Exceptions = append(resp.Exceptions, llmChannelException{Backend: kind, RunsOn: runsOn})
			}
		}
	}

	inFlight, err := h.inFlight(ctx)
	if err != nil {
		return resp, err
	}

	resp.InFlight = inFlight

	rows, err := h.store.SubscriptionUsage(ctx)
	if err != nil {
		return resp, err
	}

	now := h.now()

	for _, row := range rows {
		resp.Usage = append(resp.Usage, llmChannelUsage{
			Window:      row.Window,
			Utilization: row.Utilization,
			Status:      row.Status,
			ResetsAt:    row.ResetsAt.UTC().Format(time.RFC3339),
			SeenAt:      row.SeenAt.UTC().Format(time.RFC3339),
			Stale:       row.ResetsAt.Before(now),
		})
	}

	return resp, nil
}

// inFlight counts live work per mode. An open card run counts only while its
// card's worker is queued or running; any other run is closed here, which also
// covers a close event that was dropped or that raced the run row's creation.
func (h *llmChannelHandlers) inFlight(ctx context.Context) (map[string]llmChannelCounts, error) {
	out := map[string]llmChannelCounts{
		string(llmchannel.ModeEndpoint):   {},
		string(llmchannel.ModeClaudeCode): {},
	}

	sessions, err := h.store.CountLiveSessionsByMode(ctx)
	if err != nil {
		return nil, err
	}

	for mode, kinds := range sessions {
		if mode == "" {
			mode = string(llmchannel.ModeEndpoint)
		}

		c, ok := out[mode]
		if !ok {
			continue
		}

		c.Chats += kinds[config.BackendNameChat]
		c.Blueprints += kinds[config.BackendNameArchitect]
		out[mode] = c
	}

	if h.cards == nil {
		return out, nil
	}

	runs, err := h.store.OpenCardRuns(ctx)
	if err != nil {
		return nil, err
	}

	for _, run := range runs {
		card, err := h.cards.GetCard(ctx, run.Project, run.CardID)

		switch {
		case errors.Is(err, storage.ErrCardNotFound), errors.Is(err, storage.ErrProjectNotFound):
			h.closeRun(ctx, run)

			continue
		case err != nil:
			slog.Warn("read card for in-flight count failed",
				"project", run.Project, "card_id", run.CardID, "error", err)

			continue
		}

		if card.WorkerStatus != "queued" && card.WorkerStatus != "running" {
			h.closeRun(ctx, run)

			continue
		}

		c, ok := out[run.Mode]
		if !ok {
			continue
		}

		c.Cards++
		out[run.Mode] = c
	}

	return out, nil
}

func (h *llmChannelHandlers) closeRun(ctx context.Context, run sqlite.CardRun) {
	if err := h.store.CloseCardRun(ctx, run.Project, run.CardID, run.StartedAt, h.now()); err != nil {
		slog.Warn("close finished card run failed",
			"project", run.Project, "card_id", run.CardID, "error", err)
	}
}
