package chat

import (
	"fmt"
	"sync"
	"time"
)

// SSEEventKind is the discriminator on SSEEvent. The wire format uses
// SSE's "event:" header to route different kinds to different client-side
// listeners.
type SSEEventKind string

// maxSubscribersPerSession is the maximum number of concurrent SSE subscribers
// allowed per chat session. A single browser tab opens one subscriber; the
// cap is generous enough for legitimate concurrent tabs/devtools but prevents
// unbounded growth from leaky clients.
const maxSubscribersPerSession = 32

const (
	// SSEKindMessage is a transcript message append; Seq/Role/Content carry it.
	SSEKindMessage SSEEventKind = "message"
	// SSEKindSessionUpdate is a session metadata change. SessionUpdate carries
	// the payload. Fields: context_tokens, context_tokens_updated_at, model,
	// rehydration_active, status. Zero-valued fields are omitted (omitempty);
	// the client merges received fields into its local session view.
	SSEKindSessionUpdate SSEEventKind = "session_updated"
)

// SSEEvent is the shape pushed to browser subscribers. The default kind
// (empty) is treated as a message event for backwards compatibility with
// the original SSE wire (which had no kind discriminator).
//
// Kind is the SSE routing discriminator (off-wire, decides the SSE
// `event:` header). DataKind is the per-message payload marker that
// rides on the JSON itself (`"kind": "divider"`) so structural markers
// like the Clear Context divider survive both the live SSE wire and a
// REST-bootstrap reload.
type SSEEvent struct {
	Kind             SSEEventKind   `json:"-"`
	DataKind         string         `json:"kind,omitempty"`
	Seq              int64          `json:"seq,omitempty"`
	Role             Role           `json:"role,omitempty"`
	Content          string         `json:"content,omitempty"`
	RehydrationPhase bool           `json:"rehydration_phase,omitempty"`
	SessionUpdate    *SessionUpdate `json:"session_update,omitempty"`
}

// SessionUpdate is the payload of an SSEKindSessionUpdate event. Zero-valued
// fields mean "unchanged" - the client merges these into its session view.
// Fields: ContextTokens, ContextTokensUpdatedAt, Model, RehydrationActive,
// Status, EstimatedCostUSD, PromptTokens, CompletionTokens, CacheReadTokens,
// CacheCreationTokens.
type SessionUpdate struct {
	ContextTokens          int64     `json:"context_tokens,omitempty"`
	ContextTokensUpdatedAt time.Time `json:"context_tokens_updated_at"`
	Model                  string    `json:"model,omitempty"`
	RehydrationActive      *bool     `json:"rehydration_active,omitempty"`
	// Status carries an explicit lifecycle transition. Use a pointer so that
	// omitempty can distinguish "no status change" from a deliberate value.
	Status *Status `json:"status,omitempty"`

	// Cost and token counters - new running totals after the most recent
	// IncrementSessionCost call. Zero means "no cost update in this event".
	EstimatedCostUSD    float64 `json:"estimated_cost_usd,omitempty"`
	PromptTokens        int64   `json:"prompt_tokens,omitempty"`
	CompletionTokens    int64   `json:"completion_tokens,omitempty"`
	CacheReadTokens     int64   `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64   `json:"cache_creation_tokens,omitempty"`
}

type subscriber struct {
	ch chan SSEEvent
}

type sessionHub struct {
	mu   sync.Mutex
	ring []SSEEvent
	cap  int
	subs map[*subscriber]struct{}
}

// SSEHub manages per-session ring buffers and subscriber fan-out.
type SSEHub struct {
	mu      sync.Mutex
	bufCap  int
	perSess map[string]*sessionHub

	// Callbacks (optional). Fired outside the hub's internal mutex but the
	// sessionHub mutex is held during the count check, so the callbacks see
	// a consistent snapshot of "last subscriber departed" / "first or Nth
	// subscriber arrived".
	OnLastUnsubscribe func(sessionID string)
	OnSubscribe       func(sessionID string)
}

// NewSSEHub creates an SSEHub with the given ring buffer capacity per session.
func NewSSEHub(bufCap int) *SSEHub {
	return &SSEHub{bufCap: bufCap, perSess: make(map[string]*sessionHub)}
}

func (h *SSEHub) hub(sessionID string) *sessionHub {
	h.mu.Lock()
	defer h.mu.Unlock()

	sh, ok := h.perSess[sessionID]
	if !ok {
		sh = &sessionHub{cap: h.bufCap, subs: make(map[*subscriber]struct{})}
		h.perSess[sessionID] = sh
	}

	return sh
}

// Publish appends an event to the session's ring buffer and pushes to all
// subscribers. Slow subscribers drop events rather than blocking the producer.
//
// The fan-out runs under the per-session lock so a concurrent Unsubscribe /
// Drop cannot close a subscriber channel between our copy-out and our send.
// Sends are non-blocking (buffered channels + default branch), so holding the
// lock here does not couple producer throughput to subscriber draining.
func (h *SSEHub) Publish(sessionID string, e SSEEvent) {
	if e.Kind == "" {
		e.Kind = SSEKindMessage
	}

	sh := h.hub(sessionID)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Only persistent transcript events go into the replay ring. Session
	// updates are pure state push - late subscribers should fetch fresh
	// state via GET /api/chats/{id} rather than seeing a stale update.
	if e.Kind == SSEKindMessage {
		sh.ring = append(sh.ring, e)
		if len(sh.ring) > sh.cap {
			sh.ring = sh.ring[len(sh.ring)-sh.cap:]
		}
	}

	for s := range sh.subs {
		select {
		case s.ch <- e:
		default:
			// slow subscriber - drop rather than block the producer
		}
	}
}

// PublishSessionUpdate fans out a session-metadata change event. Convenience
// wrapper around Publish that sets the kind discriminator.
func (h *SSEHub) PublishSessionUpdate(sessionID string, u SessionUpdate) {
	h.Publish(sessionID, SSEEvent{
		Kind:          SSEKindSessionUpdate,
		SessionUpdate: &u,
	})
}

// Subscribe returns a channel for live events and the replay slice of buffered
// events with Seq > sinceSeq. The returned channel must be passed back to
// Unsubscribe to release resources.
//
// Returns an error if the per-session subscriber count would exceed
// maxSubscribersPerSession. This caps memory and goroutine growth from leaky
// clients without affecting normal browser usage (one tab = one subscriber).
//
// The OnSubscribe callback fires inside the per-session lock so that the
// (Unsubscribe → OnLastUnsubscribe → Subscribe → OnSubscribe) sequence is
// strict - a fast resubscribe cannot race the prior unsubscribe's callback
// and leave a stale grace timer.
func (h *SSEHub) Subscribe(sessionID string, sinceSeq int64) (<-chan SSEEvent, []SSEEvent, error) {
	sh := h.hub(sessionID)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if len(sh.subs) >= maxSubscribersPerSession {
		return nil, nil, fmt.Errorf("chat: sse: session %q subscriber cap (%d) reached", sessionID, maxSubscribersPerSession)
	}

	s := &subscriber{ch: make(chan SSEEvent, 64)}
	sh.subs[s] = struct{}{}

	var replay []SSEEvent

	for _, e := range sh.ring {
		if e.Seq > sinceSeq {
			replay = append(replay, e)
		}
	}

	if h.OnSubscribe != nil {
		h.OnSubscribe(sessionID)
	}

	return s.ch, replay, nil
}

// Drop unconditionally releases the per-session ring buffer and closes every
// live subscriber. Used by Manager.DeleteSession so the hub's memory does not
// grow without bound across session churn. Idempotent: a second Drop, or a
// Drop on a never-seen session, is a no-op.
func (h *SSEHub) Drop(sessionID string) {
	h.mu.Lock()
	sh, ok := h.perSess[sessionID]

	if !ok {
		h.mu.Unlock()

		return
	}

	delete(h.perSess, sessionID)
	h.mu.Unlock()

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for s := range sh.subs {
		close(s.ch)
		delete(sh.subs, s)
	}

	sh.ring = nil
}

// Unsubscribe removes a subscriber and closes its channel.
//
// Lookup-only: if the session has already been Drop'd, the perSess entry is
// gone and this is a no-op. (The streamChat HTTP handler defers Unsubscribe
// after Subscribe; when a concurrent DeleteSession runs Drop, the handler's
// receive loop sees the channel close and returns, then the deferred
// Unsubscribe arrives - without this lookup-only guard it would resurrect
// the per-session entry and defeat Drop's cleanup.)
//
// OnLastUnsubscribe fires inside the per-session lock - see Subscribe for
// the ordering rationale.
func (h *SSEHub) Unsubscribe(sessionID string, ch <-chan SSEEvent) {
	h.mu.Lock()
	sh, ok := h.perSess[sessionID]
	h.mu.Unlock()

	if !ok {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for s := range sh.subs {
		if s.ch == ch {
			delete(sh.subs, s)
			close(s.ch)

			if len(sh.subs) == 0 && h.OnLastUnsubscribe != nil {
				h.OnLastUnsubscribe(sessionID)
			}

			return
		}
	}
}
