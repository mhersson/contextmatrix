package playbookrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// clockTicker keeps the walker's ticker the clock package's type, so tests
// can drive passes with a fake clock.
type clockTicker = clock.Ticker

// RunnerAgent is the actor stamped on every run-state write the runner makes.
const RunnerAgent = "system:playbook-runner"

// defaultStopWait bounds Stop's walker wait and kills once the stop is
// persisted: above the trigger's worst case of about 39 s (three 10 s attempts
// plus their backoff).
const defaultStopWait = time.Minute

var (
	// ErrNoBackend is returned by Play when no task backend is configured.
	ErrNoBackend = errors.New("no execution backend is configured")
	// ErrRunInactive is returned by Stop when the playbook has no active run.
	ErrRunInactive = errors.New("playbook run is not active")
	// ErrStopWorker is returned by Stop when the run was marked stopped but
	// the current worker could not be killed.
	ErrStopWorker = errors.New("playbook run stopped but the worker could not be killed")
	// ErrRunChanged is the compare-and-swap refusal: the run block moved
	// between the read a pass decided from and its write, so the write was
	// dropped rather than allowed to overwrite the newer state.
	ErrRunChanged = errors.New("playbook run changed underneath")
)

// Launcher triggers one card the way the run endpoint does. The error text
// becomes the run's waiting reason, so it must be human-readable and free of
// secrets.
type Launcher func(ctx context.Context, project, cardID string, opts LaunchOptions) error

// Stopper kills one card's worker the way the stop endpoint does.
type Stopper func(ctx context.Context, project, cardID string) error

// Playbooks is the slice of the playbook service the runner uses. Every run
// write is guarded: the pass decides from an earlier read and writes through
// SetRunIf's compare-and-swap; Play and Stop decide inside the write.
type Playbooks interface {
	Get(ctx context.Context, id string) (*service.PlaybookDetail, error)
	SetRunIf(
		ctx context.Context, id string, guard func(current *board.PlaybookRun) error, run *board.PlaybookRun, agentID string,
	) (*service.PlaybookDetail, error)
	SetRunFunc(
		ctx context.Context, id string, next func(p *board.Playbook) (*board.PlaybookRun, error), agentID string,
	) (*service.PlaybookDetail, error)
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
	walkers map[string]*walker
	// watched is, per playbook with a walker, the card entries its last
	// pass read, keyed project/card. relevant filters card events against
	// it instead of resolving the playbook for every card event on the
	// board; the pass that a playbook's own event triggers refreshes it.
	watched map[string]map[string]struct{}
	// control holds one token per playbook. Play and Stop each hold it for
	// their whole call, so a Play never lands inside a Stop's wait and kill.
	control map[string]chan struct{}
	// exiting is, per playbook, the done channel of the walker Stop last
	// cancelled; the next walker waits on it before its first pass.
	exiting  map[string]chan struct{}
	stopWait time.Duration
	wg       sync.WaitGroup
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

	// The walkers' parent starts already cancelled, so an Ensure before
	// Start spawns a walker that exits at once instead of one nothing can
	// stop. Start replaces it with the real parent.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return &Runner{
		cfg: cfg, ctx: ctx, walkers: map[string]*walker{}, watched: map[string]map[string]struct{}{},
		control: map[string]chan struct{}{}, exiting: map[string]chan struct{}{}, stopWait: defaultStopWait,
	}
}

// lockControl takes id's control token, waiting for a Play or Stop that
// holds it, and returns its release. It gives up when ctx ends.
func (r *Runner) lockControl(ctx context.Context, id string) (func(), error) {
	r.mu.Lock()

	token, ok := r.control[id]
	if !ok {
		token = make(chan struct{}, 1)
		r.control[id] = token
	}

	r.mu.Unlock()

	select {
	case token <- struct{}{}:
		return func() { <-token }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for playbook %s: %w", id, ctx.Err())
	}
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

// Play starts or resumes a run. It writes the run block with the reason
// cleared and the entry on a live worker (the stopped run's own first), keeps
// started_at across a resume, and starts the walker. It waits for a Stop of the
// same playbook that is still in progress.
func (r *Runner) Play(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	release, err := r.lockControl(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()

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

	// The pre-read above gives the early answers; the run is decided inside
	// the write, from the stored entries, under the lock a hand run's queued
	// write takes. It never names a removed entry and sees every live worker.
	next := func(p *board.Playbook) (*board.PlaybookRun, error) {
		if p.RunActive() {
			return nil, fmt.Errorf("%w: %s is %s", service.ErrPlaybookRunActive, id, p.Run.Status)
		}

		run := &board.PlaybookRun{
			Status: board.RunStatusRunning, Instance: r.cfg.Instance, StartedBy: agentID, StartedAt: now, UpdatedAt: now,
		}

		prefer := ""

		if p.Run != nil {
			prefer = p.Run.Entry

			// A resume keeps the original start; a run after completion is new.
			if p.Run.Status != board.RunStatusCompleted && !p.Run.StartedAt.IsZero() {
				run.StartedAt = p.Run.StartedAt
			}
		}

		run.Entry = r.liveEntry(ctx, id, p.Entries, prefer)

		return run, nil
	}

	d, err = r.cfg.Playbooks.SetRunFunc(ctx, id, next, agentID)
	if err != nil {
		return nil, err
	}

	r.Ensure(id)

	return d, nil
}

// Stop marks the run stopped, then kills the workers this instance owns that
// were live on the run's cards at the stop write, and the current entry's,
// which a launch in flight may be queuing. A worker started after the write,
// such as a hand run, is left alone. A kill failure is reported as
// ErrStopWorker with the stopped detail; the run stays stopped. Once the stop
// is persisted, the wait and kills outlive ctx, bounded by stopWait.
func (r *Runner) Stop(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	release, err := r.lockControl(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()

	d, err := r.cfg.Playbooks.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if !d.Run.Active() {
		return nil, fmt.Errorf("%w: %s", ErrRunInactive, id)
	}

	now := r.cfg.Clock.Now().UTC()

	var targets []board.PlaybookEntry

	// Decided from the stored run, under the lock a hand run's queued write
	// takes: the workers it records are the run's, and none started later.
	next := func(p *board.Playbook) (*board.PlaybookRun, error) {
		if !p.RunActive() {
			return nil, fmt.Errorf("%w: %s", ErrRunInactive, id)
		}

		run := p.Run.Clone()
		run.Status = board.RunStatusStopped
		run.Reason = "stopped by " + agentID
		run.UpdatedAt = now
		run.EndedAt = &now

		// An entry that vanished under the run would fail validation.
		if p.FindEntry(run.Entry) < 0 {
			run.Entry = ""
		}

		targets = r.stopTargets(ctx, id, p.Entries, run.Entry)

		return run, nil
	}

	d, err = r.cfg.Playbooks.SetRunFunc(ctx, id, next, agentID)
	if err != nil {
		// A pass may have seen the stop before its failed commit was rolled
		// back, and let its walker go.
		if !errors.Is(err, ErrRunInactive) {
			r.Ensure(id)
		}

		return nil, err
	}

	// Shutdown joins the wait and kills, so the process cannot exit between
	// a trigger and its kill.
	r.wg.Add(1)
	defer r.wg.Done()

	// The stop is persisted, so a client that hangs up must not skip the kills.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.stopWait)
	defer cancel()

	// Take the walker down and wait for it. A launch in flight runs to its
	// outcome, so the card below reads queued and is killed, or its webhook
	// failed and the launcher reverted the card. Reading the card before the
	// walker exits could see todo and kill nothing.
	if done := r.cancelWalker(id); done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return d, fmt.Errorf("%w: %v", ErrStopWorker, ctx.Err())
		}
	}

	return d, r.killLiveWorkers(ctx, d.ID, targets)
}

// stopTargets returns the card entries Stop may kill: those whose card is not
// terminal and has a worker queued or running on this instance, plus the run's
// entry, which the walker may be queuing right now.
func (r *Runner) stopTargets(ctx context.Context, playbookID string, entries []board.PlaybookEntry, runEntry string) []board.PlaybookEntry {
	var targets []board.PlaybookEntry

	for _, e := range entries {
		if e.Type != board.EntryTypeCard {
			continue
		}

		if e.ID == runEntry {
			targets = append(targets, e)

			continue
		}

		card, err := r.cfg.Cards.GetCard(ctx, e.Project, e.Card)
		if err != nil {
			if !errors.Is(err, storage.ErrCardNotFound) && !errors.Is(err, storage.ErrProjectNotFound) {
				ctxlog.Logger(ctx).Warn("playbook stop: card read failed, worker not killed",
					"playbook", playbookID, "project", e.Project, "card", e.Card, "error", err)
			}

			continue
		}

		if !board.IsTerminalState(card.State) && workerInFlight(card) && !r.cfg.Cards.ClaimedElsewhere(card) {
			targets = append(targets, e)
		}
	}

	return targets
}

// killLiveWorkers kills the given entries' workers that are still queued or
// running on this instance, on cards that are not terminal. A failed kill
// does not spare the others.
func (r *Runner) killLiveWorkers(ctx context.Context, playbookID string, targets []board.PlaybookEntry) error {
	var failed []string

	for _, e := range targets {
		card, err := r.cfg.Cards.GetCard(ctx, e.Project, e.Card)
		if err != nil {
			// The reconcile sweep reaps the container of a card deleted under its worker.
			ctxlog.Logger(ctx).Warn("playbook stop: card read failed, worker not killed",
				"playbook", playbookID, "project", e.Project, "card", e.Card, "error", err)

			continue
		}

		if board.IsTerminalState(card.State) || !workerInFlight(card) || r.cfg.Cards.ClaimedElsewhere(card) {
			continue
		}

		s := r.stopper()
		if s == nil {
			return fmt.Errorf("%w: %v", ErrStopWorker, ErrNoBackend)
		}

		if err := s(ctx, e.Project, e.Card); err != nil {
			failed = append(failed, card.ID+": "+err.Error())
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("%w: %s", ErrStopWorker, strings.Join(failed, "; "))
	}

	return nil
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

// parent returns the walkers' parent context, the one Start recorded.
func (r *Runner) parent() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.ctx
}

// Start records the walkers' parent context and resumes every active run
// this instance owns. Returns immediately; walkers stop when ctx ends.
// Until it is called the parent is an already-cancelled context, so a Play
// before Start spawns a walker that exits at once rather than one that
// outlives the process's own context.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()

	playbooks, err := r.cfg.Lister.List(ctx)
	if err != nil {
		ctxlog.Logger(ctx).Error("playbook runner: list failed; no runs resumed", "error", err)

		return
	}

	resumed := 0

	for _, p := range playbooks {
		if p.Runnable && p.Run.Active() && p.Run.Instance == r.cfg.Instance && r.Ensure(p.ID) {
			resumed++
		}
	}

	ctxlog.Logger(ctx).Info("playbook runner started", "resumed_runs", resumed, "tick", r.cfg.Tick)
}

// Ensure starts a walker for id when none is running and reports whether
// it did. When one is running it is nudged instead: the nudge is what keeps
// a Play that lands while a walker is mid-pass on a run Stop just ended
// from being lost.
func (r *Runner) Ensure(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, running := r.walkers[id]; running {
		select {
		case w.nudge <- struct{}{}:
		default: // one pending nudge is enough; the next pass sees the run
		}

		return false
	}

	ctx, cancel := context.WithCancel(r.ctx)
	w := &walker{cancel: cancel, nudge: make(chan struct{}, 1), done: make(chan struct{})}
	r.walkers[id] = w

	r.wg.Add(1)

	go r.walk(ctx, id, w, r.exiting[id])

	return true
}

// setWatched records the card entries of d for relevant.
func (r *Runner) setWatched(id string, d *service.PlaybookDetail) {
	set := make(map[string]struct{}, len(d.Entries))

	for _, e := range d.Entries {
		if e.Type == board.EntryTypeCard {
			set[entryKey(e.Project, e.Card)] = struct{}{}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.watched[id] = set
}

// Wait blocks until every walker has exited.
func (r *Runner) Wait() {
	r.wg.Wait()
}

// Shutdown waits for every walker to exit, and for the wait and kills of any
// Stop past its run write, bounded by ctx: it returns
// ctx.Err() when ctx ends first, leaving the walkers running. Cancelling the
// context given to Start is what makes them stop; this only waits, so the
// caller must cancel before calling it.
func (r *Runner) Shutdown(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		defer close(done)

		r.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
