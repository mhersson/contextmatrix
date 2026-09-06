package playbookrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
)

// RunnerAgent is the actor stamped on every run-state write the runner makes.
const RunnerAgent = "system:playbook-runner"

var (
	// ErrNoBackend is returned by Play when no task backend is configured.
	ErrNoBackend = errors.New("no execution backend is configured")
	// ErrRunInactive is returned by Stop when the playbook has no active run.
	ErrRunInactive = errors.New("playbook run is not active")
	// ErrStopWorker is returned by Stop when the run was marked stopped but
	// the current worker could not be killed.
	ErrStopWorker = errors.New("playbook run stopped but the worker could not be killed")
)

// Launcher triggers one card the way the run endpoint does. The error text
// becomes the run's waiting reason, so it must be human-readable and free of
// secrets.
type Launcher func(ctx context.Context, project, cardID string, opts LaunchOptions) error

// Stopper kills one card's worker the way the stop endpoint does.
type Stopper func(ctx context.Context, project, cardID string) error

// Playbooks is the slice of the playbook service the runner uses.
type Playbooks interface {
	Get(ctx context.Context, id string) (*service.PlaybookDetail, error)
	SetRun(ctx context.Context, id string, run *board.PlaybookRun, agentID string) (*service.PlaybookDetail, error)
}

// Lister lists raw playbooks; Start uses it to find runs to resume.
type Lister interface {
	List(ctx context.Context) ([]*board.Playbook, error)
}

// Cards is the slice of the card service the runner uses.
type Cards interface {
	GetCard(ctx context.Context, project, id string) (*board.Card, error)
	ClaimedElsewhere(card *board.Card) bool
	TransitionTo(ctx context.Context, project, cardID, targetState string) (*board.Card, error)
	ForcePlaybookSettings(ctx context.Context, project, id, playbookID, branch, agentID string) (*board.Card, error)
}

// Config wires the runner. Instance is this instance's name (empty on a
// private board) and decides which runs this process walks. Tick is the
// safety-net interval between passes; bus events only shorten the wait.
type Config struct {
	Playbooks Playbooks
	Lister    Lister
	Cards     Cards
	Bus       *events.Bus
	Clock     clock.Clock
	Instance  string
	Tick      time.Duration
}

// Runner drives every active playbook run this instance owns, one walker
// goroutine per playbook. It never imports the HTTP layer: the launcher and
// stopper are injected at wiring time.
type Runner struct {
	cfg Config

	mu      sync.Mutex
	launch  Launcher
	stop    Stopper
	ctx     context.Context //nolint:containedctx // the walkers' parent, set once by Start
	walkers map[string]context.CancelFunc
	wg      sync.WaitGroup //nolint:unused // wired by the walker goroutines added next
}

// New creates a runner. Clock nil defaults to the real clock; Tick zero
// defaults to 30 seconds.
func New(cfg Config) *Runner {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real()
	}

	if cfg.Tick <= 0 {
		cfg.Tick = 30 * time.Second
	}

	return &Runner{cfg: cfg, ctx: context.Background(), walkers: map[string]context.CancelFunc{}}
}

// SetLauncher wires the trigger path; nil means no task backend.
func (r *Runner) SetLauncher(l Launcher) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.launch = l
}

// SetStopper wires the kill path; nil means no task backend.
func (r *Runner) SetStopper(s Stopper) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stop = s
}

func (r *Runner) launcher() Launcher {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.launch
}

func (r *Runner) stopper() Stopper {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.stop
}

// Play starts or resumes a run. It writes the run block with entry and
// reason cleared, keeps started_at across a resume, and starts the walker.
func (r *Runner) Play(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	d, err := r.cfg.Playbooks.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if !d.Runnable {
		return nil, fmt.Errorf("%w: %s", service.ErrPlaybookNotRunnable, id)
	}

	if d.Run.Active() {
		return nil, fmt.Errorf("%w: %s is %s", service.ErrPlaybookRunActive, id, d.Run.Status)
	}

	if r.launcher() == nil {
		return nil, ErrNoBackend
	}

	now := r.cfg.Clock.Now().UTC()
	run := board.PlaybookRun{Status: board.RunStatusRunning, Instance: r.cfg.Instance, StartedBy: agentID, StartedAt: now, UpdatedAt: now}

	// A resume keeps the original start; a run after completion is new.
	if d.Run != nil && d.Run.Status != board.RunStatusCompleted && !d.Run.StartedAt.IsZero() {
		run.StartedAt = d.Run.StartedAt
	}

	d, err = r.cfg.Playbooks.SetRun(ctx, id, &run, agentID)
	if err != nil {
		return nil, err
	}

	r.Ensure(id)

	return d, nil
}

// Stop marks the run stopped, then kills the current entry's worker when one
// is in flight and owned by this instance. A kill failure is reported as
// ErrStopWorker with the stopped detail; the run stays stopped.
func (r *Runner) Stop(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	d, err := r.cfg.Playbooks.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if !d.Run.Active() {
		return nil, fmt.Errorf("%w: %s", ErrRunInactive, id)
	}

	now := r.cfg.Clock.Now().UTC()
	run := *d.Run
	run.Status = board.RunStatusStopped
	run.Reason = "stopped by " + agentID
	run.UpdatedAt = now
	run.EndedAt = &now

	d, err = r.cfg.Playbooks.SetRun(ctx, id, &run, agentID)
	if err != nil {
		return nil, err
	}

	entry := findEntry(d, run.Entry)
	if entry == nil || entry.Type != board.EntryTypeCard {
		return d, nil
	}

	card, err := r.cfg.Cards.GetCard(ctx, entry.Project, entry.Card)
	if err != nil {
		return d, nil //nolint:nilerr // a vanished card has no worker to kill
	}

	inFlight := card.WorkerStatus == "queued" || card.WorkerStatus == "running"
	if !inFlight || r.cfg.Cards.ClaimedElsewhere(card) {
		return d, nil
	}

	s := r.stopper()
	if s == nil {
		return d, fmt.Errorf("%w: %v", ErrStopWorker, ErrNoBackend)
	}

	if err := s(ctx, entry.Project, entry.Card); err != nil {
		return d, fmt.Errorf("%w: %v", ErrStopWorker, err)
	}

	return d, nil
}

// findEntry returns the entry with the given id, or nil.
func findEntry(d *service.PlaybookDetail, id string) *service.PlaybookEntryDetail {
	if id == "" {
		return nil
	}

	for i := range d.Entries {
		if d.Entries[i].ID == id {
			return &d.Entries[i]
		}
	}

	return nil
}

// Ensure starts the walker for id when none is running. Replaced by the
// walker implementation.
func (r *Runner) Ensure(_ string) {}
