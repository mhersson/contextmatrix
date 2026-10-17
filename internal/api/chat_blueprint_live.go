package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/chat"
)

// errLiveNotServed means the architect answered the live stream with 404: an
// architect without the route, or a blueprint it does not hold.
var errLiveNotServed = errors.New("architect does not serve this blueprint's live stream")

// maxLiveFrameBytes bounds one live frame; a snapshot carries the whole draft.
const maxLiveFrameBytes = 16 << 20

const (
	liveLinger       = 5 * time.Second
	liveBackoffBase  = 500 * time.Millisecond
	liveBackoffCap   = 30 * time.Second
	liveHealthyAfter = 30 * time.Second
	// liveIdleTimeout is three of the architect's 15 s heartbeats.
	liveIdleTimeout = 45 * time.Second
)

// blueprintLive holds one architect live stream per architect session with
// a browser on its chat SSE and publishes each frame on that session's hub.
type blueprintLive struct {
	up  *architectUpstream
	hub *chat.SSEHub

	linger       time.Duration
	backoffBase  time.Duration
	backoffCap   time.Duration
	healthyAfter time.Duration
	idleTimeout  time.Duration

	mu     sync.Mutex
	epoch  uint64
	relays map[string]*liveRelay
}

// liveRelay is one session's upstream; refs counts its chat SSE subscribers.
type liveRelay struct {
	refs   int
	cancel context.CancelFunc
	linger *time.Timer
	// gen counts leaves that armed a linger, so a timer that fired before a
	// later join and leave replaced it cannot close the upstream early.
	gen uint64
	// epoch tags the running upstream's frames in the hub cache.
	epoch uint64
	// stopped is set when the architect answered 404 and run returned.
	stopped bool
}

func newBlueprintLive(up *architectUpstream, hub *chat.SSEHub) *blueprintLive {
	return &blueprintLive{
		up:           up,
		hub:          hub,
		linger:       liveLinger,
		backoffBase:  liveBackoffBase,
		backoffCap:   liveBackoffCap,
		healthyAfter: liveHealthyAfter,
		idleTimeout:  liveIdleTimeout,
		relays:       make(map[string]*liveRelay),
	}
}

// join counts a subscriber in and starts the session's upstream if none
// runs or the upstream stopped on 404.
func (b *blueprintLive) join(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.relays[sessionID]
	if r == nil {
		r = &liveRelay{}
		b.relays[sessionID] = r
		b.startLocked(sessionID, r)
	} else if r.stopped {
		b.startLocked(sessionID, r)
	}

	if r.linger != nil {
		r.linger.Stop()
		r.linger = nil
	}

	r.refs++
}

// startLocked runs r's upstream under a fresh context; the caller holds b.mu.
// The relay outlives the request that started it; leave and the linger end it.
func (b *blueprintLive) startLocked(sessionID string, r *liveRelay) {
	if r.cancel != nil {
		r.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.epoch++
	r.cancel = cancel
	r.epoch = b.epoch
	r.stopped = false

	go b.run(ctx, sessionID, b.epoch, r)
}

// leave counts a subscriber out; after the last one the upstream closes once
// the linger passes without a new subscriber.
func (b *blueprintLive) leave(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.relays[sessionID]
	if r == nil {
		return
	}

	r.refs--
	if r.refs > 0 {
		return
	}

	r.gen++
	gen := r.gen
	r.linger = time.AfterFunc(b.linger, func() { b.expire(sessionID, r, gen) })
}

// retry restarts a relay that stopped on 404 and still has subscribers; the
// caller knows the architect now holds the blueprint.
func (b *blueprintLive) retry(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.relays[sessionID]
	if r == nil || !r.stopped || r.refs == 0 {
		return
	}

	b.startLocked(sessionID, r)
}

func (b *blueprintLive) expire(sessionID string, r *liveRelay, gen uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.relays[sessionID] != r || r.refs > 0 || r.gen != gen {
		return
	}

	delete(b.relays, sessionID)
	r.cancel()
	// Cleared here, not only when run returns, so a subscriber arriving
	// before that never replays the closed stream's snapshot.
	b.hub.ResetBlueprint(sessionID, r.epoch)
}

// run keeps the session's upstream open until ctx ends, reconnecting with
// backoff. A 404 stops it until a subscriber joins or retry. Its cached
// frames go with it.
func (b *blueprintLive) run(ctx context.Context, sessionID string, epoch uint64, r *liveRelay) {
	defer b.hub.ResetBlueprint(sessionID, epoch)

	delay := b.backoffBase

	for {
		started := time.Now()

		err := b.stream(ctx, sessionID, epoch)
		if ctx.Err() != nil {
			return
		}

		if errors.Is(err, errLiveNotServed) {
			slog.Info("blueprint live: the architect does not serve this stream; not retrying", "session_id", sessionID)

			b.mu.Lock()
			if b.relays[sessionID] == r {
				r.stopped = true
			}
			b.mu.Unlock()

			return
		}

		wait, next := nextLiveDelay(delay, time.Since(started), b.backoffBase, b.backoffCap, b.healthyAfter)
		delay = next

		slog.Info("blueprint live: upstream ended; reconnecting", "session_id", sessionID, "error", err, "retry_in", wait)

		t := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			t.Stop()

			return
		case <-t.C:
		}
	}
}

// nextLiveDelay returns how long to wait before reconnecting and the delay
// after that; a stream that lived at least healthyAfter starts over at base.
func nextLiveDelay(delay, lived, base, ceiling, healthyAfter time.Duration) (wait, next time.Duration) {
	if lived >= healthyAfter {
		delay = base
	}

	return delay, min(delay*2, ceiling)
}

// stream holds one upstream connection; nil means the architect ended it.
func (b *blueprintLive) stream(ctx context.Context, sessionID string, epoch uint64) error {
	// A half-open connection never errors; cutting a silent one lets run
	// reconnect.
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	idle := time.AfterFunc(b.idleTimeout, cancel)
	defer idle.Stop()

	resp, err := b.up.get(sctx, "/blueprints/"+url.PathEscape(sessionID)+"/live", "text/event-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errLiveNotServed
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("architect live: status %d: %s", resp.StatusCode, body)
	}

	err = readLiveFrames(resp.Body, func(frame string) { b.publish(sessionID, epoch, frame) },
		func() { idle.Reset(b.idleTimeout) })

	if sctx.Err() != nil && ctx.Err() == nil {
		return errors.New("architect live: no data within the idle timeout")
	}

	return err
}

// publish reads only the frame's type and hands the frame on unchanged.
func (b *blueprintLive) publish(sessionID string, epoch uint64, frame string) {
	var head struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal([]byte(frame), &head); err != nil {
		slog.Warn("blueprint live: dropped an unparseable frame", "session_id", sessionID, "error", err)

		return
	}

	b.hub.PublishBlueprint(sessionID, epoch, head.Type, frame)
}

// readLiveFrames calls onFrame with each SSE event's data, its data lines
// joined by newlines; comments and other fields are skipped, and an event cut
// off by the end of r is dropped. onLine, when set, runs for every line read.
// It returns nil when r ends.
func readLiveFrames(r io.Reader, onFrame func(string), onLine func()) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLiveFrameBytes)

	var data []string

	for sc.Scan() {
		if onLine != nil {
			onLine()
		}

		line := sc.Text()
		if line == "" {
			if len(data) > 0 {
				onFrame(strings.Join(data, "\n"))
				data = data[:0]
			}

			continue
		}

		field, value, _ := strings.Cut(line, ":")
		if field == "data" {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("architect live: read: %w", err)
	}

	return nil
}
