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
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const defaultTick = 30 * time.Second

// Store is the ops.db slice the controller reads and writes.
type Store interface {
	SentinelSettings(ctx context.Context) (state.Settings, time.Time, string, error)
	SubscriptionWindows(ctx context.Context) ([]protocol.SubscriptionUsage, error)
	RecordRunKindStatus(ctx context.Context, r state.RunKindReport) (healID int64, recorded bool, err error)
	AddFindings(ctx context.Context, healID int64, project, cardID string, fs []protocol.FrictionFinding, at time.Time) error
	// SaveHeal advances h.Version, or answers ErrHealChanged when the row was
	// written since h was read.
	SaveHeal(ctx context.Context, h *state.Heal) error
	// OpenHeal also marks a heal's run decided.
	OpenHeal(ctx context.Context, h state.Heal) (int64, error)
	OpenHeals(ctx context.Context) ([]state.Heal, error)
	// InFlightRows returns every row of every kind with a run kind in flight,
	// whatever its stage, oldest first.
	InFlightRows(ctx context.Context) ([]state.Heal, error)
	// CardHeals returns the card's heal and forward rows, newest first.
	CardHeals(ctx context.Context, project, cardID string) ([]state.Heal, error)
	// LatestHeals returns each card's newest heal or forward row in the project.
	LatestHeals(ctx context.Context, project string) (map[string]state.Heal, error)
	UndecidedRuns(ctx context.Context) ([]state.Run, error)
	MarkRunDecided(ctx context.Context, project, cardID string, started time.Time) error
	// UnqueuedRuns returns the runs with an ID that are not yet past the
	// analysis queue.
	UnqueuedRuns(ctx context.Context) ([]state.Run, error)
	// QueueAnalysis stores an analysis row and marks its run queued; a run ID
	// already queued is no error.
	QueueAnalysis(ctx context.Context, h state.Heal) error
	MarkAnalysisQueued(ctx context.Context, project, cardID string, started time.Time) error
	// OpenAnalyses returns every analysis row at stage analyse, newest first.
	OpenAnalyses(ctx context.Context) ([]state.Heal, error)
	// RunHeal answers nil when the run that started at started has no heal.
	RunHeal(ctx context.Context, project, cardID string, started time.Time) (*state.Heal, error)
	// CardRunRow answers nil when this instance never launched the card.
	CardRunRow(ctx context.Context, project, cardID string) (*state.Run, error)
	RefreshEditSnapshot(ctx context.Context, project, cardID string, snap board.EditSnapshot) error
	IncrementAttempts(ctx context.Context, project, cardID string) error
	IncrementUsageReruns(ctx context.Context, project, cardID string) error
	Charge(ctx context.Context, healID int64, mode string, usd float64, at time.Time) error
	SpentSince(ctx context.Context, mode string, since time.Time) (float64, error)
	HealedSince(ctx context.Context, since time.Time) (int, error)
	// QueuedFindings returns the findings waiting to be routed, oldest first.
	QueuedFindings(ctx context.Context) ([]state.Finding, error)
	// ProjectGuidance returns the project's guidance findings that are pending,
	// proposed, merged or rejected.
	ProjectGuidance(ctx context.Context, project string) ([]state.Finding, error)
	// ProposedFindings returns the project's guidance findings in state proposed.
	ProposedFindings(ctx context.Context, project string) ([]state.Finding, error)
	// StandingPRs returns every project's standing PR record.
	StandingPRs(ctx context.Context) ([]state.StandingPR, error)
	// StandingPR answers nil when the project has no record.
	StandingPR(ctx context.Context, project string) (*state.StandingPR, error)
	// PutStandingPR replaces the project's record.
	PutStandingPR(ctx context.Context, p state.StandingPR) error
	// CardedSince counts the project's findings that became cards at or after since.
	CardedSince(ctx context.Context, project string, since time.Time) (int, error)
	// CardedFindings returns the IDs of the project's cards made from a finding.
	CardedFindings(ctx context.Context, project string) (map[string]bool, error)
	SetFindingOutcome(ctx context.Context, id int64, state, reason, output string, revisionID int64, at time.Time) error
	// PendingFindings returns the project's guidance findings in state pending,
	// oldest first.
	PendingFindings(ctx context.Context, project string) ([]state.Finding, error)
	// OpenRevision returns the project's open revision row, opening one at at
	// when there is none; a project has at most one.
	OpenRevision(ctx context.Context, project string, at time.Time) (state.Heal, error)
	// OpenRevisions returns every revision row at stage revise, oldest first.
	OpenRevisions(ctx context.Context) ([]state.Heal, error)
	// LastRefusedRevision answers nil when the project has no refused revision
	// created after since.
	LastRefusedRevision(ctx context.Context, project string, since time.Time) (*state.Heal, error)
	// LastFailedRevision answers nil when the project has no revision that
	// ended revision_failed or revision_stale.
	LastFailedRevision(ctx context.Context, project string) (*state.Heal, error)
	// LastRevisedRevision answers nil when the project has no revision that
	// ended revised.
	LastRevisedRevision(ctx context.Context, project string) (*state.Heal, error)
	// Findings returns the findings with the given IDs, whatever their state.
	Findings(ctx context.Context, ids []int64) ([]state.Finding, error)
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

// Cards reads and writes the cards and projects a heal works on.
type Cards interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
	ListCards(ctx context.Context, project string, f storage.CardFilter) ([]*board.Card, error)
	ClaimedElsewhere(card *board.Card) bool
	PatchCard(ctx context.Context, project, id string, in service.PatchCardInput) (*board.Card, error)
	TransitionToAs(ctx context.Context, project, id, state, agentID string, expectUpdated time.Time) (*board.Card, error)
	AddSystemLogEntry(ctx context.Context, project, id string, e board.ActivityEntry) (*board.Card, error)
	GetProject(ctx context.Context, project string) (*board.ProjectConfig, error)
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)
	CreateCard(ctx context.Context, project string, in service.CreateCardInput) (*board.Card, error)
}

// Playbooks reads a playbook with its run and resolved entries.
type Playbooks interface {
	Get(ctx context.Context, id string) (*service.PlaybookDetail, error)
}

// PlaybookRunner stops and plays a playbook's run, as a human's Stop and Run do.
type PlaybookRunner interface {
	Stop(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error)
	Play(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error)
}

// Config wires the controller. Clock nil means the real clock; Tick zero
// means 30 seconds. Pricer prices run-kind usage without a gateway cost and
// is required with a Store. Playbooks and Runner are nil when playbooks are
// off. Instance is this instance's ID as playbook runs record it.
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
	Runner    PlaybookRunner
	Instance  string
	// Secrets returns the secret values CM holds; may be nil.
	Secrets func() []string
}

// Controller is Sentinel's level-triggered loop: each pass decides from
// durable state, and the tick, bus events and nudges only shorten the wait.
type Controller struct {
	cfg   Config
	nudge chan struct{}
	wg    sync.WaitGroup

	mu              sync.Mutex
	started         time.Time
	settings        state.Settings // as the last pass read them, landed or not
	last            statusInput    // from the last pass that completed
	published       Status
	runKindBase     RunKindBase
	github          GitHub
	guidanceBase    GuidanceBase
	relauncher      func(ctx context.Context, project, cardID string) error
	noRunKindsUntil time.Time            // the agent answered that it has no run kinds
	heldKinds       map[string]time.Time // run kinds the agent does not know, held until then
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

	c.mu.Lock()
	c.started = c.cfg.Clock.Now()
	c.mu.Unlock()

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

// PullState is what a standing guidance PR looks like on GitHub.
type PullState struct {
	Open, Merged bool
	HeadSHA      string
	MergeSHA     string
}

// GitHub reads a project's standing guidance PR and the file it carries.
type GitHub interface {
	PullState(ctx context.Context, project, prURL string) (PullState, error)
	FileAt(ctx context.Context, project, prURL, path, ref string) (content string, found bool, err error)
}

// GuidanceBase fills a guidance_pr run's payload fields for a project: repo,
// worker image and a token that can push and open pull requests.
type GuidanceBase func(ctx context.Context, project string) (protocol.RunKindPayload, error)

// SetGitHub wires the reader of standing guidance PRs.
func (c *Controller) SetGitHub(g GitHub) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.github = g
}

// SetGuidanceBase wires what fills a guidance_pr run's payload fields.
func (c *Controller) SetGuidanceBase(b GuidanceBase) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.guidanceBase = b
}

// SetRelauncher wires what reruns a card outside a playbook run.
func (c *Controller) SetRelauncher(r func(ctx context.Context, project, cardID string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.relauncher = r
}

// Nudge asks for a pass; nudges that arrive during a pass coalesce into one.
func (c *Controller) Nudge() {
	select {
	case c.nudge <- struct{}{}:
	default:
	}
}

// Status summarizes the last pass that landed as of now, under the settings
// a pass last read, so a controller whose passes stopped landing, or never
// landed since Start, reads as not running once Sentinel is on.
func (c *Controller) Status() Status {
	c.mu.Lock()
	in, set, started := c.last, c.settings, c.started
	c.mu.Unlock()

	in.Settings, in.Now = set, c.cfg.Clock.Now()
	if in.LastPass.IsZero() {
		in.LastPass = started
	}

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
