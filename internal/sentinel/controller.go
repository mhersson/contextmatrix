package sentinel

import (
	"context"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

const defaultTick = 30 * time.Second

// Store is the ops.db slice the controller reads.
type Store interface {
	SentinelSettings(ctx context.Context) (Settings, time.Time, string, error)
	SubscriptionWindows(ctx context.Context) ([]protocol.SubscriptionUsage, error)
}

// Channel gives the LLM channel mode a fresh run of a backend kind starts in.
type Channel interface {
	Effective(kind llmchannel.Kind) llmchannel.Mode
}

// Config wires the controller. Clock nil means the real clock; Tick zero
// means 30 seconds.
type Config struct {
	Store   Store
	Channel Channel
	Bus     *events.Bus
	Clock   clock.Clock
	Tick    time.Duration
}

// Controller is Sentinel's level-triggered loop: each pass decides from
// durable state, and the tick, bus events and nudges only shorten the wait.
type Controller struct {
	cfg   Config
	nudge chan struct{}
	wg    sync.WaitGroup

	mu        sync.Mutex
	last      statusInput // from the last pass that completed
	published Status
}

// New creates a controller; Start runs it.
func New(cfg Config) *Controller {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real()
	}

	if cfg.Tick <= 0 {
		cfg.Tick = defaultTick
	}

	return &Controller{cfg: cfg, nudge: make(chan struct{}, 1)}
}

// Start runs a pass at once, then one per tick, wake-up event or nudge, until
// ctx ends.
func (c *Controller) Start(ctx context.Context) {
	ch, unsubscribe := c.cfg.Bus.Subscribe()
	ticker := c.cfg.Clock.NewTicker(c.cfg.Tick)

	c.wg.Go(func() {
		defer unsubscribe()
		defer ticker.Stop()

		for ctx.Err() == nil {
			c.safePass(ctx)

			if !c.wait(ctx, ch, ticker) {
				return
			}
		}
	})

	ctxlog.Logger(ctx).Info("sentinel controller started", "tick", c.cfg.Tick)
}

// Shutdown waits for the loop, which exits once Start's ctx ends.
func (c *Controller) Shutdown(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		defer close(done)

		c.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Nudge asks for a pass; nudges that arrive during a pass coalesce into one.
func (c *Controller) Nudge() {
	select {
	case c.nudge <- struct{}{}:
	default:
	}
}

// Status summarizes the last pass as of now, so a controller whose passes
// stopped landing reads as not running.
func (c *Controller) Status() Status {
	c.mu.Lock()
	in := c.last
	c.mu.Unlock()

	in.Now = c.cfg.Clock.Now()

	return summarize(in)
}

// safePass runs one pass. A panic or a failed read is logged and leaves the
// status as it was; the next wake-up tries again.
func (c *Controller) safePass(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			ctxlog.Logger(ctx).Error("sentinel: pass panicked", "panic", rec, "stack", string(debug.Stack()))
		}
	}()

	if err := c.pass(ctx); err != nil && ctx.Err() == nil {
		ctxlog.Logger(ctx).Warn("sentinel: pass failed", "error", err)
	}
}

// wait blocks until a nudge, a tick or a wake-up event, and reports false
// once ctx ends.
func (c *Controller) wait(ctx context.Context, ch <-chan events.Event, ticker clock.Ticker) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.nudge:
			return true
		case <-ticker.C():
			return true
		case ev, ok := <-ch:
			if !ok {
				return false
			}

			if wakes(ev.Type) {
				return true
			}
		}
	}
}

// wakes reports whether an event can change what a pass decides.
func wakes(t events.EventType) bool {
	switch t {
	case events.CardStalled, events.CardUpdated, events.CardStateChanged, events.LLMChannelChanged, events.PlaybookUpdated:
		return true
	}

	return strings.HasPrefix(string(t), "worker.")
}
