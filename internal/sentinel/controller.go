package sentinel

import (
	"context"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const defaultTick = 30 * time.Second

// Store is the ops.db slice the controller reads and writes.
type Store interface {
	SentinelSettings(ctx context.Context) (Settings, time.Time, string, error)
	SubscriptionWindows(ctx context.Context) ([]protocol.SubscriptionUsage, error)
	RecordRunKindStatus(ctx context.Context, r RunKindReport) (healID int64, recorded bool, err error)
	AddFindings(ctx context.Context, healID int64, project, cardID string, fs []protocol.FrictionFinding, at time.Time) error
	// SaveHeal advances h.Version, or answers ErrHealChanged when the row was
	// written since h was read.
	SaveHeal(ctx context.Context, h *Heal) error
}

// Channel gives the LLM channel mode a fresh run of a backend kind starts in,
// and keeps the subscription windows workers report.
type Channel interface {
	Effective(kind llmchannel.Kind) llmchannel.Mode
	RecordUsage(ctx context.Context, u []protocol.SubscriptionUsage)
}

// Backend is the agent backend the controller dispatches run kinds to.
type Backend interface {
	RunKind(ctx context.Context, p protocol.RunKindPayload) error
	ListContainers(ctx context.Context) ([]backend.ContainerInfo, error)
	Health(ctx context.Context) (protocol.HealthResponse, error)
}

// Cards reads the cards and projects a heal works on.
type Cards interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
	ListCards(ctx context.Context, project string, f storage.CardFilter) ([]*board.Card, error)
	GetProject(ctx context.Context, project string) (*board.ProjectConfig, error)
}

// Playbooks reads a playbook with its run and resolved entries.
type Playbooks interface {
	Get(ctx context.Context, id string) (*service.PlaybookDetail, error)
}

// Config wires the controller. Clock nil means the real clock; Tick zero
// means 30 seconds. Pricer prices run-kind usage without a gateway cost and
// is required with a Store. Playbooks is nil when playbooks are off.
// Instance is this instance's ID as playbook runs record it.
type Config struct {
	Store     Store
	Channel   Channel
	Bus       *events.Bus
	Clock     clock.Clock
	Tick      time.Duration
	Pricer    Pricer
	Backend   Backend
	Cards     Cards
	Playbooks Playbooks
	Instance  string
}

// Controller is Sentinel's level-triggered loop: each pass decides from
// durable state, and the tick, bus events and nudges only shorten the wait.
type Controller struct {
	cfg   Config
	nudge chan struct{}
	wg    sync.WaitGroup

	mu              sync.Mutex
	last            statusInput // from the last pass that completed
	published       Status
	runKindBase     RunKindBase
	noRunKindsUntil time.Time // the agent answered that it has no run kinds
}

// New creates a controller; Start runs it. It panics on a Store without a
// Pricer, which would charge unpriced usage nothing.
func New(cfg Config) *Controller {
	if cfg.Store != nil && cfg.Pricer == nil {
		panic("sentinel: a controller with a store needs a pricer")
	}

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

// SetRunKindBase wires what fills a run kind's common payload fields.
func (c *Controller) SetRunKindBase(b RunKindBase) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.runKindBase = b
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
