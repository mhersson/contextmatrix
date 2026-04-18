package sessionlog

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// DefaultMaxSessions is the default cap on concurrent active upstream sessions.
	DefaultMaxSessions = 64
	// DefaultSessionTTL is the default idle-sweeper TTL for sessions that have
	// never been explicitly stopped.
	DefaultSessionTTL = 2 * time.Hour

	// EventTypeTerminal is sent to subscribers when a session ends (Stop or error).
	EventTypeTerminal = "terminal"

	// retryBackoffBase is the initial retry delay for upstream reconnects.
	retryBackoffBase = 250 * time.Millisecond
	// retryBackoffCap is the maximum retry delay.
	retryBackoffCap = 4 * time.Second
	// maxUpstreamRetries is the number of reconnect attempts before marking a
	// session errored and closing.
	maxUpstreamRetries = 5

	// subscriberChanBuf is the channel buffer size for each subscriber.
	subscriberChanBuf = 256
)

// subscriber holds a channel that receives live events for one watcher.
type subscriber struct {
	id uint64
	ch chan Event
}

// activeSession tracks the upstream connection and subscriber fan-out for one
// card session.
type activeSession struct {
	cancel    context.CancelFunc
	startTime time.Time
	subs      []*subscriber
	done      chan struct{} // closed when the pump goroutine exits
}

// nextSubID is an atomic counter used to give each subscriber a unique ID.
var nextSubID atomic.Uint64

// WithMaxSessions sets the cap on concurrent active upstream sessions.
func WithMaxSessions(n int) Option {
	return func(m *Manager) { m.maxSessions = n }
}

// WithSessionTTL sets the idle-sweeper TTL.  Sessions running longer than this
// without an explicit Stop are force-closed.
func WithSessionTTL(d time.Duration) Option {
	return func(m *Manager) { m.sessionTTL = d }
}

// WithRunnerConfig sets the runner base URL and API key used for HMAC-signed
// upstream SSE connections.
func WithRunnerConfig(runnerURL, apiKey string) Option {
	return func(m *Manager) {
		m.runnerURL = runnerURL
		m.runnerAPIKey = apiKey
	}
}

// ensureActiveSessions lazily initialises the activeSessions and pendingSubs maps.
// Must be called with m.mu held.
func (m *Manager) ensureActiveSessions() {
	if m.activeSessions == nil {
		m.activeSessions = make(map[string]*activeSession)
	}
	if m.pendingSubs == nil {
		m.pendingSubs = make(map[string][]*subscriber)
	}
}

// Start opens a long-lived authenticated SSE connection to the runner's /logs
// endpoint for the given cardID/project pair.  Incoming events are written into
// the buffer and fanned out to all current subscribers.
//
// Start is idempotent: if a session is already running for cardID, it returns
// nil immediately.
//
// The provided ctx is used only as a root cancellation signal; the upstream
// pump goroutine runs with its own internal context so it can outlive the
// caller's request scope.
func (m *Manager) Start(_ context.Context, cardID, project string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureActiveSessions()

	// Idempotent: already running.
	if _, ok := m.activeSessions[cardID]; ok {
		return nil
	}

	if len(m.activeSessions) >= m.maxSessions {
		return fmt.Errorf("sessionlog: session cap (%d) reached, cannot start session for %s",
			m.maxSessions, cardID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &activeSession{
		cancel:    cancel,
		startTime: time.Now(),
		done:      make(chan struct{}),
	}

	// Drain any subscribers that registered before this session was started.
	if pending := m.pendingSubs[cardID]; len(pending) > 0 {
		sess.subs = append(sess.subs, pending...)
		delete(m.pendingSubs, cardID)
	}

	m.activeSessions[cardID] = sess

	go m.runPump(ctx, cardID, project, sess)

	return nil
}

// Stop cancels the upstream connection for cardID, sends a terminal event to
// all subscribers, and clears the buffer.
//
// Stop is idempotent: if no session is running for cardID it returns immediately.
func (m *Manager) Stop(cardID string) {
	m.mu.Lock()
	sess, ok := m.activeSessions[cardID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.activeSessions, cardID)
	m.mu.Unlock()

	// Cancel the pump goroutine and wait for it to finish before touching the
	// subscriber list to avoid races.
	sess.cancel()
	<-sess.done

	// Drain subscribers with a terminal event and close their channels.
	m.mu.Lock()
	subs := sess.subs
	sess.subs = nil
	// Also pick up any pending subscribers that registered but were never
	// transferred to a session (e.g., race between Subscribe and Stop).
	pendingSubs := m.pendingSubs[cardID]
	delete(m.pendingSubs, cardID)
	m.mu.Unlock()

	terminal := Event{
		Seq:       0,
		Timestamp: time.Now(),
		Type:      EventTypeTerminal,
	}
	allSubs := append(subs, pendingSubs...)
	for _, s := range allSubs {
		select {
		case s.ch <- terminal:
		default:
		}
		close(s.ch)
	}

	m.Clear(cardID)
}

// Subscribe returns a channel that first delivers a snapshot of all buffered
// events and then delivers live events as they arrive from the upstream pump.
// The second return value is an unsubscribe function; calling it removes this
// subscriber.  The channel is closed either by a terminal Stop/error or by the
// unsubscribe func (whichever comes first).
//
// If no session is running for cardID, the snapshot channel is still returned
// (possibly empty); live events will begin arriving once Start is called.
func (m *Manager) Subscribe(cardID string) (<-chan Event, func()) {
	id := nextSubID.Add(1)
	ch := make(chan Event, subscriberChanBuf)
	sub := &subscriber{id: id, ch: ch}

	m.mu.Lock()
	m.ensureActiveSessions()

	// Capture snapshot under the lock (call internal buffer directly to avoid a
	// re-entrant lock acquire) and register the subscriber atomically so that no
	// live event can slip between snapshot and registration.
	var snap []Event
	if b, ok := m.sessions[cardID]; ok {
		snap = b.snapshot()
	}

	sess := m.activeSessions[cardID]
	if sess != nil {
		sess.subs = append(sess.subs, sub)
	} else {
		// No session running yet — park in pending so Start can pick it up.
		m.pendingSubs[cardID] = append(m.pendingSubs[cardID], sub)
	}
	m.mu.Unlock()

	// Deliver snapshot asynchronously to avoid blocking the caller.
	go func() {
		for _, evt := range snap {
			select {
			case ch <- evt:
			default:
				// Channel full; drop remaining snapshot tail rather than block.
				return
			}
		}
	}()

	unsub := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// Remove from active session if present.
		if s, ok := m.activeSessions[cardID]; ok {
			for i, candidate := range s.subs {
				if candidate.id == id {
					s.subs = append(s.subs[:i], s.subs[i+1:]...)
					return
				}
			}
		}
		// Also remove from pending subs if session has not started yet.
		if pending, ok := m.pendingSubs[cardID]; ok {
			for i, candidate := range pending {
				if candidate.id == id {
					m.pendingSubs[cardID] = append(pending[:i], pending[i+1:]...)
					return
				}
			}
		}
	}

	return ch, unsub
}

// StartSweeper launches a background goroutine that periodically scans for
// sessions that have exceeded sessionTTL and force-closes them.  The goroutine
// exits when ctx is cancelled.
func (m *Manager) StartSweeper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(m.sessionTTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.sweepIdleSessions()
			}
		}
	}()
}

// sweepIdleSessions finds sessions older than sessionTTL and calls Stop on them.
func (m *Manager) sweepIdleSessions() {
	now := time.Now()
	m.mu.Lock()
	var stale []string
	for cardID, sess := range m.activeSessions {
		if now.Sub(sess.startTime) > m.sessionTTL {
			stale = append(stale, cardID)
		}
	}
	m.mu.Unlock()

	for _, cardID := range stale {
		slog.Warn("sessionlog: idle-sweeper force-closing stale session",
			"card_id", cardID,
			"ttl", m.sessionTTL,
		)
		m.Stop(cardID)
	}
}

// runPump is the upstream SSE pump goroutine.  It connects to the runner,
// reads events, appends them to the buffer, and fans them out to subscribers.
// On read error it retries with exponential backoff up to maxUpstreamRetries,
// then marks the session errored and closes.
func (m *Manager) runPump(ctx context.Context, cardID, project string, sess *activeSession) {
	defer close(sess.done)

	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}

		err := m.readUpstream(ctx, cardID, project, sess)
		if ctx.Err() != nil {
			// Cancelled externally (Stop called) — clean exit without retrying.
			return
		}

		attempt++
		if attempt >= maxUpstreamRetries {
			slog.Error("sessionlog: upstream permanently failed, closing session",
				"card_id", cardID,
				"error", err,
				"attempts", attempt,
			)
			// Remove from active sessions.
			m.mu.Lock()
			delete(m.activeSessions, cardID)
			subs := sess.subs
			sess.subs = nil
			m.mu.Unlock()

			terminal := Event{
				Seq:       0,
				Timestamp: time.Now(),
				Type:      EventTypeTerminal,
				Payload:   fmt.Appendf(nil, "upstream error: %v", err),
			}
			for _, s := range subs {
				select {
				case s.ch <- terminal:
				default:
				}
				close(s.ch)
			}
			m.Clear(cardID)
			return
		}

		backoff := backoffDuration(attempt)
		slog.Warn("sessionlog: upstream error, retrying",
			"card_id", cardID,
			"error", err,
			"attempt", attempt,
			"backoff", backoff,
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// readUpstream connects to the runner /logs endpoint and reads SSE frames until
// the connection closes or ctx is cancelled.
//
// The upstream URL is project-scoped only (no card_id query parameter) to
// maintain compatibility with current runner versions that stream all project
// events.  Events for other cards are filtered out before appending to the
// per-card buffer.
func (m *Manager) readUpstream(ctx context.Context, cardID, project string, sess *activeSession) error {
	upstreamURL := m.runnerURL + "/logs"
	if project != "" {
		upstreamURL += "?project=" + url.QueryEscape(project)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	sigHeader, tsHeader := signSSERequest(m.runnerAPIKey)
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("upstream connect: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}

		evt, evtCardID, ok := parseSSEPayload(raw)
		if !ok {
			continue
		}

		// Filter: only buffer and fan-out events that belong to this session's
		// card.  The runner streams all project events; other cards' events are
		// silently dropped here.
		if evtCardID != "" && evtCardID != cardID {
			continue
		}

		m.Append(cardID, evt)

		m.mu.Lock()
		for _, s := range sess.subs {
			select {
			case s.ch <- evt:
			default:
				// Slow subscriber — drop rather than block the pump.
			}
		}
		m.mu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("scanner: %w", err)
	}

	return fmt.Errorf("upstream closed connection")
}

// sseHTTPClient is a dedicated client for long-lived SSE upstream connections.
// Timeout 0 prevents the per-request deadline from terminating the stream.
var sseHTTPClient = &http.Client{Timeout: 0}

// sseJSONPayload is the JSON structure expected in SSE data frames from the runner.
type sseJSONPayload struct {
	Seq       uint64 `json:"seq"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	CardID    string `json:"card_id"`
}

// parseSSEPayload parses a JSON data value from an SSE frame into an Event.
// It also returns the card_id from the payload (may be empty if the runner
// did not include it), which callers use to filter cross-card events.
func parseSSEPayload(raw string) (Event, string, bool) {
	var p sseJSONPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return Event{}, "", false
	}
	ts := time.Now()
	if p.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, p.Timestamp); err == nil {
			ts = parsed
		}
	}
	return Event{
		Seq:       p.Seq,
		Timestamp: ts,
		Type:      p.Type,
		Payload:   []byte(p.Content),
	}, p.CardID, true
}

// signSSERequest computes HMAC-SHA256 auth headers for a GET SSE request.
// It signs "timestamp.body" where body is empty for GET requests, matching the
// pattern used by runner.SignRequestHeaders without creating an import cycle.
func signSSERequest(apiKey string) (sigHeader, tsHeader string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	// Empty body for GET.
	sig := hex.EncodeToString(mac.Sum(nil))
	return "sha256=" + sig, ts
}

// backoffDuration returns the exponential back-off delay for the given attempt
// number (1-based), capped at retryBackoffCap.
func backoffDuration(attempt int) time.Duration {
	d := time.Duration(float64(retryBackoffBase) * math.Pow(2, float64(attempt-1)))
	if d > retryBackoffCap {
		return retryBackoffCap
	}
	return d
}
