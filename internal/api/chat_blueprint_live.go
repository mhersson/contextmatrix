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

	mu     sync.Mutex
	epoch  uint64
	relays map[string]*liveRelay
}

// liveRelay is one session's upstream; refs counts its chat SSE subscribers.
type liveRelay struct {
	refs   int
	cancel context.CancelFunc
	linger *time.Timer
}

func newBlueprintLive(up *architectUpstream, hub *chat.SSEHub) *blueprintLive {
	return &blueprintLive{
		up:           up,
		hub:          hub,
		linger:       liveLinger,
		backoffBase:  liveBackoffBase,
		backoffCap:   liveBackoffCap,
		healthyAfter: liveHealthyAfter,
		relays:       make(map[string]*liveRelay),
	}
}

// join counts a subscriber in and starts the session's upstream if none runs.
func (b *blueprintLive) join(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.relays[sessionID]
	if r == nil {
		// The relay outlives the request that started it; leave and the
		// linger end it.
		ctx, cancel := context.WithCancel(context.Background())
		b.epoch++
		r = &liveRelay{cancel: cancel}
		b.relays[sessionID] = r

		go b.run(ctx, sessionID, b.epoch)
	}

	if r.linger != nil {
		r.linger.Stop()
		r.linger = nil
	}

	r.refs++
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

	r.linger = time.AfterFunc(b.linger, func() { b.expire(sessionID, r) })
}

func (b *blueprintLive) expire(sessionID string, r *liveRelay) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.relays[sessionID] != r || r.refs > 0 {
		return
	}

	delete(b.relays, sessionID)
	r.cancel()
}

// run keeps the session's upstream open until ctx ends, reconnecting with
// backoff. A 404 stops it until the last subscriber leaves. Its cached
// frames go with it.
func (b *blueprintLive) run(ctx context.Context, sessionID string, epoch uint64) {
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

			return
		}

		if time.Since(started) >= b.healthyAfter {
			delay = b.backoffBase
		}

		slog.Info("blueprint live: upstream ended; reconnecting", "session_id", sessionID, "error", err, "retry_in", delay)

		t := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			t.Stop()

			return
		case <-t.C:
		}

		delay = min(delay*2, b.backoffCap)
	}
}

// stream holds one upstream connection; nil means the architect ended it.
func (b *blueprintLive) stream(ctx context.Context, sessionID string, epoch uint64) error {
	resp, err := b.up.get(ctx, "/blueprints/"+url.PathEscape(sessionID)+"/live", "text/event-stream")
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

	return readLiveFrames(resp.Body, func(frame string) { b.publish(sessionID, epoch, frame) })
}

// publish reads only the frame's type and hands the frame on unchanged.
func (b *blueprintLive) publish(sessionID string, epoch uint64, frame string) {
	var head struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal([]byte(frame), &head); err != nil {
		slog.Debug("blueprint live: dropped an unparseable frame", "session_id", sessionID, "error", err)

		return
	}

	b.hub.PublishBlueprint(sessionID, epoch, head.Type, frame)
}

// readLiveFrames calls onFrame with each SSE event's data, its data lines
// joined by newlines; comments and other fields are skipped, and an event cut
// off by the end of r is dropped. It returns nil when r ends.
func readLiveFrames(r io.Reader, onFrame func(string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLiveFrameBytes)

	var data []string

	for sc.Scan() {
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
