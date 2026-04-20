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
	"slices"
	"strconv"
	"strings"
	"sync"
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
//
// Ordering invariant: all snapshot events must be delivered before any live
// event. This is enforced via the primed flag:
//   - While primed is false, the pump queues incoming live events in pending
//     instead of writing directly to ch.
//   - The snapshot goroutine in Subscribe writes snapshot events to ch first,
//     then acquires m.mu, drains pending into ch, and sets primed = true.
//   - Once primed, the pump writes directly to ch as normal.
//
// Lifecycle channels:
//   - done: closed by unsub or Stop/terminal-error to signal the snapshot
//     goroutine that it should exit early.  Guarded by doneOnce so that both
//     unsub and Stop can safely close it without a double-close panic.
//   - snapDone: closed by the snapshot goroutine (via defer) when it exits.
//     Stop/terminal-error waits on snapDone before sending the terminal event
//     and closing ch, which eliminates the close-of-in-flight-send panic race.
type subscriber struct {
	id       uint64
	ch       chan Event
	primed   bool    // true once snapshot drain is complete
	pending  []Event // live events buffered while snapshot is draining
	done     chan struct{}
	doneOnce sync.Once
	snapDone chan struct{}
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

// ensureActiveSessions lazily initialises the activeSessions, pendingSubs, and
// failedSessions maps. Must be called with m.mu held.
func (m *Manager) ensureActiveSessions() {
	if m.activeSessions == nil {
		m.activeSessions = make(map[string]*activeSession)
	}

	if m.pendingSubs == nil {
		m.pendingSubs = make(map[string][]*subscriber)
	}

	if m.failedSessions == nil {
		m.failedSessions = make(map[string]struct{})
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

	// Clear any permanent-failure flag so new subscribers won't be fast-pathed.
	delete(m.failedSessions, cardID)

	// Drain any subscribers that registered before this session was started.
	if pending := m.pendingSubs[cardID]; len(pending) > 0 {
		sess.subs = append(sess.subs, pending...)

		delete(m.pendingSubs, cardID)
	}

	m.activeSessions[cardID] = sess

	go m.runPump(ctx, cardID, project, sess)

	return nil
}

// snapDoneTimeout is the maximum time to wait for a snapshot goroutine to exit
// after its done channel is closed. This is a belt-and-suspenders guard only;
// in practice the goroutine exits as soon as it sees the done signal.
const snapDoneTimeout = time.Second

// closeSubscriber signals each subscriber's snapshot goroutine to exit, waits
// for it to finish (up to snapDoneTimeout), sends a terminal event, then closes
// the subscriber's channel.
//
// Waiting on snapDone before close(ch) eliminates the close-of-in-flight-send
// panic race: the snapshot goroutine may be mid-send on ch when Stop/terminal-
// error is triggered, and closing ch concurrently with a send panics.
func closeSubscriber(subs []*subscriber, terminal Event) {
	for _, s := range subs {
		// Signal the snapshot goroutine to exit (idempotent via doneOnce).
		s.doneOnce.Do(func() { close(s.done) })
		// Wait for the snapshot goroutine to finish before touching ch.
		select {
		case <-s.snapDone:
		case <-time.After(snapDoneTimeout):
			// Belt-and-suspenders: should never happen, but don't block forever.
		}
		// Now it is safe to send on ch and close it.
		select {
		case s.ch <- terminal:
		default:
		}

		close(s.ch)
	}
}

// Stop cancels the upstream connection for cardID, sends a terminal event to
// all subscribers, and clears the buffer.
//
// Stop is idempotent: if no session is running for cardID it returns immediately.
func (m *Manager) Stop(cardID string) {
	m.mu.Lock()

	sess, ok := m.activeSessions[cardID]
	if !ok {
		// No active session, but still clear any permanent-failure flag so that
		// a future Start can restart the session cleanly.
		delete(m.failedSessions, cardID)
		m.mu.Unlock()

		return
	}

	delete(m.activeSessions, cardID)
	delete(m.failedSessions, cardID)
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

	subs = append(subs, pendingSubs...)
	closeSubscriber(subs, terminal)

	m.Clear(cardID)
}

// Subscribe returns a channel that first delivers a snapshot of all buffered
// events and then delivers live events as they arrive from the upstream pump.
// The second return value is an unsubscribe function; calling it removes this
// subscriber.  The channel is closed either by a terminal Stop/error or by the
// unsubscribe func (whichever comes first).
//
// Ordering guarantee: every snapshot event (events already buffered at the
// moment Subscribe is called) is delivered before any live event, with no
// duplicates.  This is enforced by the primed-flag protocol: the pump queues
// live events in sub.pending while sub.primed is false, and the snapshot
// goroutine flips sub.primed to true (under m.mu) only after draining both the
// snapshot and the accumulated pending slice into sub.ch.
//
// Full-snapshot delivery: the snapshot goroutine blocks on each channel send
// rather than dropping events when the channel is full. If the subscriber is
// slow, the goroutine will block until the subscriber reads from the channel or
// until cancellation is signalled via the sub.done channel.
//
// Cancellation: calling the returned unsub function closes sub.done, which
// causes the snapshot goroutine to exit promptly. Stop and terminal upstream
// errors also close sub.done, then wait on sub.snapDone before sending the
// terminal event and closing the channel — this eliminates any
// close-of-in-flight-send panic race.
//
// If no session is running for cardID, the snapshot channel is still returned
// (possibly empty); live events will begin arriving once Start is called.
func (m *Manager) Subscribe(cardID string) (<-chan Event, func()) {
	id := nextSubID.Add(1)
	ch := make(chan Event, subscriberChanBuf)
	// primed starts false: pump will stage live events in sub.pending until the
	// snapshot goroutine below has finished draining the snapshot.
	sub := &subscriber{
		id:       id,
		ch:       ch,
		primed:   false,
		done:     make(chan struct{}),
		snapDone: make(chan struct{}),
	}

	m.mu.Lock()
	m.ensureActiveSessions()

	// Fast-path: if the session has permanently failed and no new session is
	// running, close this subscriber inline without starting a snapshot goroutine.
	// This avoids a hang where closeSubscriber would wait on sub.snapDone, which
	// would never be closed because the snapshot goroutine was never launched.
	if _, failed := m.failedSessions[cardID]; failed {
		if m.activeSessions[cardID] == nil {
			// Mark primed so no pump staging races can occur.
			sub.primed = true
			// Close snapDone inline — no snapshot goroutine will ever be launched.
			close(sub.snapDone)
			// Close done via doneOnce (idempotent).
			sub.doneOnce.Do(func() { close(sub.done) })

			terminal := Event{
				Seq:       0,
				Timestamp: time.Now(),
				Type:      EventTypeTerminal,
				Payload:   []byte("upstream error: session permanently failed"),
			}
			// Non-blocking send so we never block under the lock.
			select {
			case ch <- terminal:
			default:
			}

			close(ch)
			m.mu.Unlock()

			return ch, func() {} // noop unsub — channel already closed
		}
	}

	// Capture snapshot under the lock (call internal buffer directly to avoid a
	// re-entrant lock acquire) and register the subscriber atomically so that no
	// live event can slip between snapshot and registration without being staged.
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

	// Sort the snapshot by Seq so that events with lower sequence numbers are
	// always delivered before events with higher ones.  The buffer may contain
	// pump events (Seq > threshold) interleaved with events added via direct
	// Append calls (Seq ≤ threshold).  Stable sort preserves insertion order
	// among events with the same Seq.
	slices.SortStableFunc(snap, func(a, b Event) int {
		if a.Seq < b.Seq {
			return -1
		}

		if a.Seq > b.Seq {
			return 1
		}

		return 0
	})

	// Deliver snapshot then flip primed, all without blocking the caller.
	//
	// Order of operations:
	//   1. Write snapshot events (sorted by Seq) to ch, blocking on each send.
	//      The pump only touches sub.pending while primed is false.
	//      If sub.done is closed (unsub/Stop called), exit immediately.
	//   2. Acquire m.mu, drain sub.pending (live events queued by pump during
	//      snapshot delivery) into ch (also blocking with done-select), set
	//      sub.primed = true.
	//   3. From here on the pump writes directly to ch.
	//   4. Always close sub.snapDone on exit so Stop/terminal-error can safely
	//      close ch after the snapshot goroutine has finished.
	go func() {
		defer close(sub.snapDone)

		for _, evt := range snap {
			select {
			case ch <- evt:
			case <-sub.done:
				// Cancelled by unsub or Stop — exit before setting primed.
				// The pump will stop staging events once the subscriber is
				// removed from sess.subs (done by unsub/Stop before closing done).
				return
			}
		}

		// Acquire lock to atomically drain pending and flip primed.
		//
		// Use a head-pop loop instead of `for _, evt := range sub.pending` so
		// that we re-read len(sub.pending) after every lock re-acquire.  The
		// pump may append to sub.pending while we are unlocked (blocking on a
		// full-channel send); a range loop captures the slice length at the
		// start and would silently miss those late-appended events.
		m.mu.Lock()
		for len(sub.pending) > 0 {
			evt := sub.pending[0]
			sub.pending = sub.pending[1:]
			// We must release the lock before blocking, so use a non-blocking
			// attempt first; if the channel is full, unlock, block with
			// cancellation support, then re-acquire.
			select {
			case ch <- evt:
				// Sent without blocking — continue under the lock.
			default:
				m.mu.Unlock()

				select {
				case ch <- evt:
				case <-sub.done:
					// sub.pending not fully drained; primed stays false.
					// That is safe: the subscriber is already removed from
					// sess.subs before done is closed, so the pump won't
					// stage more events for it.
					return
				}

				m.mu.Lock()
			}
		}
		// Do NOT set sub.pending = nil here.  The pump may have appended to
		// sub.pending while we were unlocked; the head-pop loop above has
		// already consumed all of them, so the slice is now empty.  Setting
		// nil would race with a concurrent append that arrived after the last
		// iteration's lock re-acquire but before we reach this line.
		sub.primed = true
		m.mu.Unlock()
	}()

	unsub := func() {
		m.mu.Lock()
		// Remove from active session if present.
		if s, ok := m.activeSessions[cardID]; ok {
			for i, candidate := range s.subs {
				if candidate.id == id {
					s.subs = append(s.subs[:i], s.subs[i+1:]...)
					m.mu.Unlock()
					// Signal the snapshot goroutine to exit. Done after
					// removing from sess.subs so the pump won't stage more
					// events for this subscriber after done is closed.
					sub.doneOnce.Do(func() { close(sub.done) })

					return
				}
			}
		}
		// Also remove from pending subs if session has not started yet.
		if pending, ok := m.pendingSubs[cardID]; ok {
			for i, candidate := range pending {
				if candidate.id == id {
					m.pendingSubs[cardID] = append(pending[:i], pending[i+1:]...)
					m.mu.Unlock()
					sub.doneOnce.Do(func() { close(sub.done) })

					return
				}
			}
		}
		m.mu.Unlock()
	}

	return ch, unsub
}

// projectKey returns the internal map key used to namespace a project-scoped
// session so it cannot collide with card IDs (e.g. "project:alpha").
func projectKey(project string) string {
	return "project:" + project
}

// StartProject opens a long-lived authenticated SSE connection to the runner's
// /logs endpoint for the given project.  All events for the project are
// buffered and fanned out to subscribers, regardless of which card they
// belong to.
//
// StartProject is idempotent: if a project session is already running it
// returns nil immediately.
//
// Snapshot-then-live ordering and cancellation guarantees are identical to
// Start/Subscribe for card-scoped sessions.  The internal key is prefixed with
// "project:" so project sessions cannot collide with card IDs in the shared
// maps.
func (m *Manager) StartProject(ctx context.Context, project string) error {
	key := projectKey(project)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureActiveSessions()

	if _, ok := m.activeSessions[key]; ok {
		return nil
	}

	if len(m.activeSessions) >= m.maxSessions {
		return fmt.Errorf("sessionlog: session cap (%d) reached, cannot start project session for %s",
			m.maxSessions, project)
	}

	pumpCtx, cancel := context.WithCancel(ctx)
	sess := &activeSession{
		cancel:    cancel,
		startTime: time.Now(),
		done:      make(chan struct{}),
	}

	// Clear any permanent-failure flag so new subscribers won't be fast-pathed.
	delete(m.failedSessions, key)

	if pending := m.pendingSubs[key]; len(pending) > 0 {
		sess.subs = append(sess.subs, pending...)

		delete(m.pendingSubs, key)
	}

	m.activeSessions[key] = sess

	go m.runProjectPump(pumpCtx, project, key, sess)

	return nil
}

// StopProject cancels the upstream connection for the project, sends a
// terminal event to all subscribers, and clears the project buffer.
//
// StopProject is idempotent: if no session is running for the project it
// returns immediately.
func (m *Manager) StopProject(project string) {
	m.Stop(projectKey(project))
}

// SubscribeProject returns a channel that first delivers a snapshot of all
// buffered project events and then delivers live events as they arrive.
// The second return value is an unsubscribe function; calling it removes this
// subscriber.  The channel is closed either by a terminal StopProject/error or
// by the unsubscribe func (whichever comes first).
//
// Ordering guarantee: every snapshot event (events already buffered at the
// moment SubscribeProject is called) is delivered before any live event, with
// no duplicates.  This is the same guarantee as Subscribe — the primed-flag
// protocol is shared.
//
// Cancellation: calling the returned unsub function causes the snapshot
// goroutine to exit promptly.  StopProject and terminal upstream errors also
// close the channel cleanly with no panic.
//
// If no project session is running, the snapshot channel is still returned
// (possibly empty); live events will begin arriving once StartProject is called.
func (m *Manager) SubscribeProject(project string) (<-chan Event, func()) {
	return m.Subscribe(projectKey(project))
}

// runProjectPump is the upstream SSE pump goroutine for a project-scoped
// session.  It is identical to runPump except it uses readProjectUpstream,
// which accepts all events for the project rather than filtering by card ID.
func (m *Manager) runProjectPump(ctx context.Context, project, key string, sess *activeSession) {
	defer close(sess.done)

	attempt := 0

	for {
		if ctx.Err() != nil {
			return
		}

		delivered, err := m.readProjectUpstream(ctx, project, key, sess)
		if ctx.Err() != nil {
			return
		}

		if delivered {
			attempt = 0
		}

		attempt++
		if attempt >= maxUpstreamRetries {
			slog.Error("sessionlog: project upstream permanently failed, closing session",
				"project", project,
				"error", err,
				"attempts", attempt,
			)
			// Remove from active sessions, collect pending subs, and mark as failed.
			// Do NOT call closeSubscriber while holding m.mu — it waits on snapDone.
			m.mu.Lock()
			delete(m.activeSessions, key)

			subs := sess.subs
			sess.subs = nil
			// Drain any subscribers that raced between Subscribe and StartProject.
			pendingSubs := m.pendingSubs[key]
			delete(m.pendingSubs, key)
			m.failedSessions[key] = struct{}{}
			m.mu.Unlock()

			terminal := Event{
				Seq:       0,
				Timestamp: time.Now(),
				Type:      EventTypeTerminal,
				Payload:   fmt.Appendf(nil, "upstream error: %v", err),
			}

			subs = append(subs, pendingSubs...)
			closeSubscriber(subs, terminal)
			// Clear the event buffer only; leave failedSessions intact.
			m.mu.Lock()
			delete(m.sessions, key)
			m.mu.Unlock()

			return
		}

		backoff := backoffDuration(attempt)
		slog.Warn("sessionlog: project upstream error, retrying",
			"project", project,
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

// readProjectUpstream connects to the runner /logs endpoint for the given
// project and reads SSE frames until the connection closes or ctx is
// cancelled.
//
// Unlike readUpstream (which filters by card ID), this function accepts every
// event for the project and buffers them under the project key.  This allows
// SubscribeProject to replay all project events to reconnecting clients.
// readProjectUpstream returns (delivered, err) where delivered is true if at
// least one frame was successfully buffered and fanned out during this
// connection.  Callers use the delivered flag to reset the retry-attempt
// counter so that transient disconnects after successful frames do not
// accumulate toward the permanent-failure threshold.
func (m *Manager) readProjectUpstream(ctx context.Context, project, key string, sess *activeSession) (bool, error) {
	upstreamURL := m.runnerURL + "/logs"
	if project != "" {
		upstreamURL += "?project=" + url.QueryEscape(project)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}

	sigHeader, tsHeader := signSSERequest(m.runnerAPIKey)
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		return false, fmt.Errorf("upstream connect: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)

	var delivered bool

	for scanner.Scan() {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
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
		// Preserve the originating card ID so project-scoped subscribers can
		// populate the card_id field in SSE frames sent to the browser.
		evt.CardID = evtCardID

		// Buffer the event under the project key and fan out to all project
		// subscribers.  No card-ID filter: all project events are accepted.
		m.mu.Lock()
		m.getOrCreate(key).append(evt)

		shouldWarn := false

		for _, s := range sess.subs {
			if !s.primed {
				s.pending = append(s.pending, evt)
			} else {
				select {
				case s.ch <- evt:
				default:
					// Slow subscriber — record drop and notify.
					if m.notifyDrop(s) {
						shouldWarn = true
					}
				}
			}
		}
		m.mu.Unlock()

		// Mark that at least one frame was successfully delivered.
		delivered = true

		if shouldWarn {
			slog.Warn("sessionlog: slow subscriber, event dropped",
				"project", project,
				"dropped_total", m.DroppedEvents(),
			)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
		}

		return delivered, fmt.Errorf("scanner: %w", err)
	}

	return delivered, fmt.Errorf("upstream closed connection")
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

		delivered, err := m.readUpstream(ctx, cardID, project, sess)
		if ctx.Err() != nil {
			// Cancelled externally (Stop called) — clean exit without retrying.
			return
		}

		if delivered {
			attempt = 0
		}

		attempt++
		if attempt >= maxUpstreamRetries {
			slog.Error("sessionlog: upstream permanently failed, closing session",
				"card_id", cardID,
				"error", err,
				"attempts", attempt,
			)
			// Remove from active sessions, collect pending subs, and mark as failed.
			// Do NOT call closeSubscriber while holding m.mu — it waits on snapDone.
			m.mu.Lock()
			delete(m.activeSessions, cardID)

			subs := sess.subs
			sess.subs = nil
			// Drain any subscribers that raced between Subscribe and Start.
			pendingSubs := m.pendingSubs[cardID]
			delete(m.pendingSubs, cardID)
			m.failedSessions[cardID] = struct{}{}
			m.mu.Unlock()

			terminal := Event{
				Seq:       0,
				Timestamp: time.Now(),
				Type:      EventTypeTerminal,
				Payload:   fmt.Appendf(nil, "upstream error: %v", err),
			}

			subs = append(subs, pendingSubs...)
			closeSubscriber(subs, terminal)
			// Clear the event buffer only; leave failedSessions intact so that
			// any concurrent or future Subscribe call can take the fast-path.
			m.mu.Lock()
			delete(m.sessions, cardID)
			m.mu.Unlock()

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
//
// It returns (delivered, err) where delivered is true if at least one frame
// was successfully buffered and fanned out.  Callers use this to reset the
// retry-attempt counter so transient disconnects after successful frames do
// not accumulate toward the permanent-failure threshold.
func (m *Manager) readUpstream(ctx context.Context, cardID, project string, sess *activeSession) (bool, error) {
	upstreamURL := m.runnerURL + "/logs"
	if project != "" {
		upstreamURL += "?project=" + url.QueryEscape(project)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}

	sigHeader, tsHeader := signSSERequest(m.runnerAPIKey)
	req.Header.Set("X-Signature-256", sigHeader)
	req.Header.Set("X-Webhook-Timestamp", tsHeader)

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		return false, fmt.Errorf("upstream connect: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)

	var delivered bool

	for scanner.Scan() {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
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

		// Buffer the event and fan out to subscribers under a single lock so
		// that a concurrent Subscribe cannot observe the event in the buffer
		// without also seeing it in the pump's fan-out.  This prevents the
		// duplicate-delivery race where an event is captured in the snapshot
		// AND also staged in sub.pending.
		m.mu.Lock()
		m.getOrCreate(cardID).append(evt)

		shouldWarn := false

		for _, s := range sess.subs {
			if !s.primed {
				// Snapshot goroutine has not finished draining yet.  Stage the
				// live event so it arrives after all snapshot events.
				s.pending = append(s.pending, evt)
			} else {
				select {
				case s.ch <- evt:
				default:
					// Slow subscriber — record drop and notify.
					if m.notifyDrop(s) {
						shouldWarn = true
					}
				}
			}
		}
		m.mu.Unlock()

		// Mark that at least one frame was successfully delivered.
		delivered = true

		if shouldWarn {
			slog.Warn("sessionlog: slow subscriber, event dropped",
				"card_id", cardID,
				"dropped_total", m.DroppedEvents(),
			)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
		}

		return delivered, fmt.Errorf("scanner: %w", err)
	}

	return delivered, fmt.Errorf("upstream closed connection")
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

// notifyDrop records a fan-out drop for subscriber s and returns true if the
// caller should emit a slog.Warn after releasing m.mu.
//
// It must be called with m.mu held. It:
//  1. Increments the Manager-wide droppedEvents counter.
//  2. Attempts a non-blocking send of a drop-marker event to s.ch.
//     in-band drop marker from buffer eviction carries an 8-byte count payload;
//     fan-out drop marker carries nil payload — callers that parse drop counts
//     must guard len(Payload) >= 8.
//  3. Uses lastDropWarn with CompareAndSwap to throttle warn signals to at most
//     1 per second globally across all sessions.
func (m *Manager) notifyDrop(s *subscriber) (shouldWarn bool) {
	m.droppedEvents.Add(1)

	// Non-blocking send of a nil-payload drop marker to the subscriber channel.
	select {
	case s.ch <- Event{Type: EventTypeDropped, Seq: 0, Timestamp: time.Now()}:
	default:
		// Channel still full — silently discard the marker itself.
	}

	// Throttle: emit at most one warn per second globally.
	now := time.Now().UnixNano()
	old := m.lastDropWarn.Load()

	const warnIntervalNs = int64(time.Second)
	if now-old >= warnIntervalNs {
		if m.lastDropWarn.CompareAndSwap(old, now) {
			return true
		}
	}

	return false
}
