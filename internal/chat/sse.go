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
	// the payload; zero-valued fields are omitted (omitempty) and the client
	// merges received fields into its local session view.
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

	// AssistantWorking flips the chat working indicator: true when a turn
	// starts (user send accepted, or a run-state frame from the worker),
	// false when the run goes idle. Pointer so omitempty distinguishes
	// "no change" from a deliberate transition.
	AssistantWorking *bool `json:"assistant_working,omitempty"`
	// AssistantWorkingSince accompanies AssistantWorking=true with the UTC
	// start of the in-flight turn. Nil on working=false.
	AssistantWorkingSince *time.Time `json:"assistant_working_since,omitempty"`
}

// EventKindBlueprint is the DataKind of an architect live frame relayed on
// the session SSE; Content is the LiveFrame JSON verbatim. Never persisted
// and never in the replay ring.
const EventKindBlueprint = "blueprint"

// blueprintDraftCap bounds the draft bytes cached after a snapshot.
const blueprintDraftCap = 256 << 10

// blueprintCache is the latest snapshot frame and the gapless drafts since
// it. epoch names the relay that filled it, so a stopped relay's late frame
// or reset never touches a newer relay's cache.
type blueprintCache struct {
	epoch    uint64
	snapshot string
	drafts   []string
	size     int
	// overflow stops draft caching until the next snapshot, so the cache
	// never holds drafts with a gap.
	overflow bool
}

func (c *blueprintCache) clear() {
	c.snapshot, c.drafts, c.size, c.overflow = "", nil, 0, false
}

func (c *blueprintCache) add(frameType, frame string) {
	switch frameType {
	case "snapshot":
		c.clear()
		c.snapshot = frame
	case "draft":
		if c.snapshot == "" || c.overflow {
			return
		}

		if c.size+len(frame) > blueprintDraftCap {
			c.drafts, c.size, c.overflow = nil, 0, true

			return
		}

		c.drafts = append(c.drafts, frame)
		c.size += len(frame)
	}
}

func (c *blueprintCache) events() []SSEEvent {
	if c.snapshot == "" {
		return nil
	}

	out := make([]SSEEvent, 0, 1+len(c.drafts))
	out = append(out, blueprintEvent(c.snapshot))

	for _, d := range c.drafts {
		out = append(out, blueprintEvent(d))
	}

	return out
}

func blueprintEvent(frame string) SSEEvent {
	return SSEEvent{Kind: SSEKindMessage, DataKind: EventKindBlueprint, Content: frame}
}

type subscriber struct {
	ch chan SSEEvent
}

type sessionHub struct {
	mu   sync.Mutex
	ring []SSEEvent
	cap  int
	subs map[*subscriber]struct{}
	bp   blueprintCache
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

// ReplayCap is the most events a session's replay ring holds.
func (h *SSEHub) ReplayCap() int { return h.bufCap }

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

// lookup returns the session's hub without creating one.
func (h *SSEHub) lookup(sessionID string) *sessionHub {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.perSess[sessionID]
}

// PublishBlueprint fans an architect live frame out to the session's
// subscribers and caches it for later ones. A session nobody subscribed to
// is skipped, so a relay outliving a deleted session never recreates its
// hub. A frame takes a subscriber's buffer only while it is under half
// full, so a draft burst never crowds out transcript events.
func (h *SSEHub) PublishBlueprint(sessionID string, epoch uint64, frameType, frame string) {
	sh := h.lookup(sessionID)
	if sh == nil {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if epoch < sh.bp.epoch {
		return
	}

	if epoch > sh.bp.epoch {
		sh.bp.clear()
		sh.bp.epoch = epoch
	}

	sh.bp.add(frameType, frame)

	e := blueprintEvent(frame)

	for s := range sh.subs {
		if len(s.ch) >= cap(s.ch)/2 {
			continue
		}

		select {
		case s.ch <- e:
		default:
		}
	}
}

// ResetBlueprint drops the frames the relay of epoch cached, so a subscriber
// arriving after that relay stopped gets no stale snapshot.
func (h *SSEHub) ResetBlueprint(sessionID string, epoch uint64) {
	sh := h.lookup(sessionID)
	if sh == nil {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if sh.bp.epoch == epoch {
		sh.bp.clear()
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
// events with Seq > sinceSeq, followed by the cached blueprint frames. The
// returned channel must be passed back to Unsubscribe to release resources.
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

	replay = append(replay, sh.bp.events()...)

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

// ClearReplay empties the session's replay ring and leaves its subscribers
// attached. The stream handler backfills a reconnect the ring no longer
// covers from the store.
func (h *SSEHub) ClearReplay(sessionID string) {
	sh := h.lookup(sessionID)
	if sh == nil {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

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
