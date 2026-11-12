// Package llmchannel holds the global LLM channel: whether workers reach their
// model through the configured endpoint or through a Claude Code subscription.
package llmchannel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix-protocol/selection"

	"github.com/mhersson/contextmatrix/internal/events"
)

// Mode is the channel a worker's model calls travel over.
type Mode string

const (
	ModeEndpoint   Mode = "endpoint"
	ModeClaudeCode Mode = "claude_code"
)

// Kind is a backend kind: config.BackendNameAgent, Chat or Architect.
type Kind = string

var (
	// ErrUnknownMode is returned by Set for a mode that is not a Mode constant.
	ErrUnknownMode = errors.New("unknown llm channel mode")
	// ErrNotConfigured is returned by Set when the requested side has no
	// configuration to run on.
	ErrNotConfigured = errors.New("llm channel mode is not configured")
)

// Store persists the mode in ops.db.
type Store interface {
	// LLMChannel returns an empty mode when nothing is stored.
	LLMChannel(ctx context.Context) (mode string, at time.Time, by string, err error)
	PutLLMChannel(ctx context.Context, mode, by string) error
	RecordSubscriptionUsage(ctx context.Context, u []protocol.SubscriptionUsage, seen time.Time) error
}

// Publisher is the event bus.
type Publisher interface{ Publish(events.Event) }

// Config describes which sides of the channel are configured.
type Config struct {
	Endpoint     *protocol.LLMEndpoint // nil when llm_endpoint is unset
	OAuthToken   string                // "" when claude_code is unset
	Supported    []Kind                // claude_code.backends
	DefaultModel string                // claude_code.default_model
	Catalog      ClaudeCodeCatalog     // nil, or a catalog that is not built, checks IDs by form only
}

// ClaudeCodeCatalog reports membership in the built Claude Code catalog.
// Implemented by *modelcatalog.Builder; built is false while there is no
// catalog.
type ClaudeCodeCatalog interface {
	ClaudeCodeKnown(id string) (known, built bool)
}

// Holder is the runtime-switchable channel state.
type Holder struct {
	cfg   Config
	store Store
	bus   Publisher
	mode  atomic.Pointer[Mode]

	// setMu serializes Set so persist, swap, at/by and publish stay one unit.
	setMu sync.Mutex

	mu sync.RWMutex
	at time.Time
	by string
}

// New builds the holder from the stored mode. A stored mode whose side is no
// longer configured falls back to the side that is.
func New(ctx context.Context, cfg Config, store Store, bus Publisher) (*Holder, error) {
	h := &Holder{cfg: cfg, store: store, bus: bus}

	stored, at, by, err := store.LLMChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("load llm channel: %w", err)
	}

	used := h.initialMode(Mode(stored))
	if stored != "" && Mode(stored) != used {
		slog.Warn("stored llm channel mode is not usable, falling back",
			"stored_mode", stored, "used_mode", string(used))
	}

	h.mode.Store(&used)
	h.at, h.by = at, by

	return h, nil
}

func (h *Holder) initialMode(stored Mode) Mode {
	if h.configured(stored) {
		return stored
	}

	if h.configured(ModeEndpoint) {
		return ModeEndpoint
	}

	if h.configured(ModeClaudeCode) {
		return ModeClaudeCode
	}

	return ModeEndpoint
}

func (h *Holder) configured(m Mode) bool {
	switch m {
	case ModeEndpoint:
		return h.cfg.Endpoint != nil
	case ModeClaudeCode:
		return h.cfg.OAuthToken != ""
	default:
		return false
	}
}

// Mode returns the current mode.
func (h *Holder) Mode() Mode { return *h.mode.Load() }

// Switchable reports whether both sides are configured.
func (h *Holder) Switchable() bool {
	return h.configured(ModeEndpoint) && h.configured(ModeClaudeCode)
}

// Set switches the mode, persists it and publishes a change event.
func (h *Holder) Set(ctx context.Context, mode Mode, by string) error {
	if mode != ModeEndpoint && mode != ModeClaudeCode {
		return ErrUnknownMode
	}

	if !h.configured(mode) {
		return ErrNotConfigured
	}

	h.setMu.Lock()
	defer h.setMu.Unlock()

	if err := h.store.PutLLMChannel(ctx, string(mode), by); err != nil {
		return fmt.Errorf("persist llm channel: %w", err)
	}

	h.mode.Store(&mode)

	h.mu.Lock()
	h.at, h.by = time.Now().UTC(), by
	h.mu.Unlock()

	h.bus.Publish(events.Event{
		Type:      events.LLMChannelChanged,
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"mode": string(mode)},
	})

	return nil
}

// RecordUsage stores the subscription windows a worker reported and tells the
// UIs. A window without a reset time is dropped; failures are logged, never
// returned.
func (h *Holder) RecordUsage(ctx context.Context, u []protocol.SubscriptionUsage) {
	kept := make([]protocol.SubscriptionUsage, 0, len(u))

	for _, w := range u {
		if w.ResetsAt.IsZero() {
			slog.Debug("dropping subscription window without a reset time", "window", w.Window)

			continue
		}

		kept = append(kept, w)
	}

	if len(kept) == 0 {
		return
	}

	now := time.Now().UTC()

	if err := h.store.RecordSubscriptionUsage(ctx, kept, now); err != nil {
		slog.Warn("record subscription usage failed", "error", err)

		return
	}

	h.bus.Publish(events.Event{
		Type:      events.LLMChannelChanged,
		Timestamp: now,
		Data:      map[string]any{"mode": string(h.Mode())},
	})
}

// EndpointType is the configured endpoint's type, or "" when none is set.
func (h *Holder) EndpointType() string {
	if h.cfg.Endpoint == nil {
		return ""
	}

	return h.cfg.Endpoint.Type
}

// ClaudeCodeConfigured reports whether a subscription token is configured.
func (h *Holder) ClaudeCodeConfigured() bool { return h.configured(ModeClaudeCode) }

// Supported reports whether claude_code is allowed for the backend kind.
func (h *Holder) Supported(kind Kind) bool {
	return slices.Contains(h.cfg.Supported, kind)
}

// Effective is the mode a fresh start of the kind runs in: the current mode,
// or endpoint when the kind does not support claude_code.
func (h *Holder) Effective(kind Kind) Mode {
	if m := h.Mode(); m != ModeClaudeCode || h.Supported(kind) {
		return m
	}

	return ModeEndpoint
}

// EndpointFor returns the endpoint payload and the mode it belongs to. A
// recorded mode that is still usable for the kind wins so a run keeps the
// channel it started on; otherwise the kind's effective mode applies. The
// endpoint is nil when the resolved mode is endpoint and none is configured.
func (h *Holder) EndpointFor(kind Kind, recorded Mode) (*protocol.LLMEndpoint, Mode) {
	m := recorded
	if !h.configured(m) || (m == ModeClaudeCode && !h.Supported(kind)) {
		m = h.Effective(kind)
	}

	if m == ModeClaudeCode {
		return &protocol.LLMEndpoint{
			Type:       protocol.LLMEndpointTypeClaudeCode,
			OAuthToken: h.cfg.OAuthToken,
		}, m
	}

	if h.cfg.Endpoint == nil {
		return nil, m
	}

	ep := *h.cfg.Endpoint

	return &ep, m
}

// ResolveModel maps a model name onto the mode's catalog. claude_code takes
// the mapped Claude Code ID, else the default; once a catalog is built, an ID
// outside it also falls back to the default. endpoint replaces a bare name
// with endpointDefault only when the built catalog knows it, so a gateway's
// own bare claude names keep working.
func (h *Holder) ResolveModel(mode Mode, model, endpointDefault string) string {
	id, ok := selection.ClaudeCodeModelID(model)

	var known, built bool
	if ok && h.cfg.Catalog != nil {
		known, built = h.cfg.Catalog.ClaudeCodeKnown(id)
	}

	if mode == ModeClaudeCode {
		if ok && (known || !built) {
			return id
		}

		return h.cfg.DefaultModel
	}

	if ok && known && !strings.Contains(model, "/") {
		return endpointDefault
	}

	return model
}

// Updated returns when and by whom the mode was last switched.
func (h *Holder) Updated() (time.Time, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.at, h.by
}
