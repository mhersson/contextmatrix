package playbookrun

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkerCount reads the walker map under the runner's lock, so a test never
// races a walker that is starting or exiting.
func (r *Runner) walkerCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.walkers)
}

// walkerReady reports whether exactly one walker is running and subscribed
// to the bus. Subscription is what makes a published event reach it, so
// tests that publish must wait for this and not merely for the map entry.
func (e *env) walkerReady() bool {
	return e.runner.walkerCount() == 1 && e.runner.cfg.Bus.SubscriberCount() == 1
}

func TestStart_ResumesOnlyOwnedActiveRuns(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))
	e.cards.add(todoCard("alpha", "ALPHA-2"))
	e.cards.add(todoCard("alpha", "ALPHA-3"))

	mine := runnablePlaybook("mine", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(mine, "")

	theirs := runnablePlaybook("theirs", cardEntry("e1", "alpha", "ALPHA-2"))
	e.activeRun(theirs, "")
	theirs.Run.Instance = "lap-b"
	idle := runnablePlaybook("idle", cardEntry("e1", "alpha", "ALPHA-3"))

	e.pbs.add(mine)
	e.pbs.add(theirs)
	e.pbs.add(idle)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)

	require.Eventually(t, func() bool { return e.launcher.count() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "ALPHA-1", e.launcher.call(0).card)

	require.Eventually(t, func() bool { return e.runner.walkerCount() == 1 }, time.Second, 5*time.Millisecond,
		"the walkers for the run owned elsewhere and the idle playbook exit; only the owned run keeps one")

	cancel()
	e.runner.Wait()
}

func TestWalker_EventAndTickNudgeAPass(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)
	e.cards.add(todoCard("alpha", "ALPHA-2"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	// A launch queues the card, exactly as the trigger endpoint does. Without
	// it a pass that runs again before the worker starts would relaunch, and
	// the launch count would depend on how the walker interleaves with this
	// test's writes.
	e.launcher.onLaunch = func(_ context.Context, project, card string) error {
		e.cards.queue(project, card)

		return nil
	}

	ctx := t.Context()

	e.runner.Start(ctx)
	require.Eventually(t, e.walkerReady, time.Second, 5*time.Millisecond)

	// The first card finishes: a card event for it must launch the second.
	e.cards.mu.Lock()
	c.State = board.StateDone
	c.WorkerStatus = "completed"
	e.cards.mu.Unlock()

	e.runner.cfg.Bus.Publish(events.Event{Type: events.CardStateChanged, Project: "alpha", CardID: "ALPHA-1", Data: map[string]any{"new_state": "done"}})
	require.Eventually(t, func() bool { return e.launcher.count() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "ALPHA-2", e.launcher.call(0).card)

	// The second card finishes with no event at all: the tick completes the run.
	e.cards.mu.Lock()
	e.cards.cards[cardKey("alpha", "ALPHA-2")].State = board.StateDone
	e.cards.mu.Unlock()

	e.clk.Advance(e.runner.cfg.Tick)
	require.Eventually(t, func() bool { return e.pbs.lastRun().Status == board.RunStatusCompleted }, time.Second, 5*time.Millisecond)

	e.runner.Wait()
	assert.Equal(t, 0, e.runner.walkerCount(), "a completed run has no walker")
}

func TestWalker_IgnoresUnrelatedEvents(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	// The walker subscribes before its first pass, so wait for that pass's
	// read too before counting.
	require.Eventually(t, func() bool { return e.walkerReady() && e.pbs.getCount() >= 1 }, time.Second, 5*time.Millisecond)

	writes := e.pbs.runWrites()
	gets := e.pbs.getCount()

	for range 10 {
		e.runner.cfg.Bus.Publish(events.Event{Type: events.CardUpdated, Project: "beta", CardID: "BETA-9"})
		e.runner.cfg.Bus.Publish(events.Event{Type: events.PlaybookUpdated, Data: map[string]any{"id": "other"}})
	}

	// The bus delivers in order: once the pass this event triggers has read
	// the playbook, every unrelated event before it was already filtered.
	e.runner.cfg.Bus.Publish(events.Event{Type: events.PlaybookUpdated, Data: map[string]any{"id": "rollout"}})
	require.Eventually(t, func() bool { return e.pbs.getCount() > gets }, time.Second, 5*time.Millisecond)

	assert.Equal(t, gets+1, e.pbs.getCount(), "unrelated events are filtered without reading the playbook")
	assert.Equal(t, writes, e.pbs.runWrites(), "unrelated events do not write")

	cancel()
	e.runner.Wait()
}

// TestWalker_WatchesEntriesAddedMidRun pins that the entry set the walker
// filters on follows the playbook: an entry added while the run is active
// is watched from the pass its playbook event triggers.
func TestWalker_WatchesEntriesAddedMidRun(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)
	e.cards.add(todoCard("alpha", "ALPHA-2"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Eventually(t, func() bool { return e.walkerReady() && e.pbs.getCount() >= 1 }, time.Second, 5*time.Millisecond)

	// Before the add, a card event for ALPHA-2 is unrelated: no read.
	gets := e.pbs.getCount()
	e.runner.cfg.Bus.Publish(events.Event{Type: events.CardUpdated, Project: "alpha", CardID: "ALPHA-2"})
	e.runner.cfg.Bus.Publish(events.Event{Type: events.PlaybookUpdated, Data: map[string]any{"id": "rollout"}})
	require.Eventually(t, func() bool { return e.pbs.getCount() > gets }, time.Second, 5*time.Millisecond)
	assert.Equal(t, gets+1, e.pbs.getCount())

	// Add the entry the way the service does, then announce it.
	e.pbs.mu.Lock()

	p.Entries = append(p.Entries, cardEntry("e2", "alpha", "ALPHA-2"))
	p.NextEntryID = 3
	e.pbs.mu.Unlock()

	gets = e.pbs.getCount()
	e.runner.cfg.Bus.Publish(events.Event{Type: events.PlaybookUpdated, Data: map[string]any{"id": "rollout"}})
	require.Eventually(t, func() bool { return e.pbs.getCount() > gets }, time.Second, 5*time.Millisecond)

	// After it, a card event for ALPHA-2 triggers a pass.
	gets = e.pbs.getCount()
	e.runner.cfg.Bus.Publish(events.Event{Type: events.CardUpdated, Project: "alpha", CardID: "ALPHA-2"})
	require.Eventually(t, func() bool { return e.pbs.getCount() > gets }, time.Second, 5*time.Millisecond)

	cancel()
	e.runner.Wait()
}

func TestEnsure_ReportsWhetherItSpawned(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Eventually(t, e.walkerReady, time.Second, 5*time.Millisecond)

	assert.False(t, e.runner.Ensure("rollout"), "a running walker is nudged, not spawned")
	assert.True(t, e.runner.Ensure("unknown"), "no walker yet: one is spawned")

	cancel()
	e.runner.Wait()
}

// TestEnsure_PlayDuringExitingPassKeepsAWalker pins the window where a
// walker's pass has already read a run that Stop made inactive and a Play
// lands before that walker exits. Ensure must leave a walker behind, or the
// run stays running with nothing driving it until the next restart.
func TestEnsure_PlayDuringExitingPassKeepsAWalker(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	p.Run.Status = board.RunStatusStopped // Stop has already persisted
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Equal(t, 0, e.runner.walkerCount(), "a stopped run is not resumed")

	inPass := make(chan struct{})
	proceed := make(chan struct{})

	e.pbs.mu.Lock()
	e.pbs.afterGet = func() {
		close(inPass)
		<-proceed
	}
	e.pbs.mu.Unlock()

	e.runner.Ensure("rollout")
	<-inPass // the walker holds a detail that says the run is stopped

	// Play persists a fresh running run with the entry cleared and calls
	// Ensure while the pass that is about to report done still owns the
	// walker entry.
	e.pbs.mu.Lock()
	p.Run.Status = board.RunStatusRunning
	p.Run.Entry = ""
	p.Run.Reason = ""
	e.pbs.mu.Unlock()

	e.runner.Ensure("rollout")
	close(proceed)

	require.Eventually(t, func() bool { return e.launcher.count() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, e.runner.walkerCount(), "the Play left a walker behind")

	cancel()
	e.runner.Wait()
}

// TestStop_DropsTheWalker pins the second half of the Stop fix: the walker
// goes down with the run, so a pass already in flight unwinds on ctx.Err()
// instead of writing over the stop, and a later Play brings one back.
func TestStop_DropsTheWalker(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Eventually(t, e.walkerReady, time.Second, 5*time.Millisecond)

	_, err := e.runner.Stop(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, 0, e.runner.walkerCount(), "Stop takes the walker down with the run")

	_, err = e.runner.Play(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, 1, e.runner.walkerCount(), "Play brings one back")

	cancel()
	e.runner.Wait()
}

func TestShutdown_JoinsWalkersAndHonoursItsContext(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Hold the walker inside its first pass, then ask an already-expired
	// Shutdown to wait for it: it must give up rather than block.
	inPass := make(chan struct{})
	proceed := make(chan struct{})

	e.pbs.mu.Lock()
	e.pbs.afterGet = func() {
		close(inPass)
		<-proceed
	}
	e.pbs.mu.Unlock()

	e.runner.Start(ctx)
	<-inPass

	expired, expiredCancel := context.WithCancel(context.Background())
	expiredCancel()

	require.ErrorIs(t, e.runner.Shutdown(expired), context.Canceled)

	// Released and cancelled, the walker exits and Shutdown returns nil.
	close(proceed)
	cancel()

	require.NoError(t, e.runner.Shutdown(context.Background()))
	assert.Equal(t, 0, e.runner.walkerCount())
}

// TestStop_WaitsForAnInFlightLaunch pins the window between the run's
// running write and the card's queued write. A Stop that reads the card
// inside it sees todo, kills nothing, and the launch then queues a worker
// that outlives the stop. Stop must wait for the walker, whose launch is
// what queues the card, before deciding what to kill.
func TestStop_WaitsForAnInFlightLaunch(t *testing.T) {
	e := newEnv(t)

	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout") // already forced: no card writes in the way
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	entered := make(chan struct{})
	release := make(chan struct{})

	// The launch blocks mid-call, then queues the card the way the trigger
	// endpoint does.
	e.launcher.onLaunch = func(_ context.Context, project, card string) error {
		close(entered)
		<-release

		e.cards.queue(project, card)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	returnedEarly := false

	select {
	case <-stopped:
		returnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	require.False(t, returnedEarly, "Stop returned while the launch was still in flight")

	require.NoError(t, <-stopped)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.calls, "the worker the launch queued is killed")
	assert.Equal(t, board.RunStatusStopped, e.pbs.lastRun().Status)
	assert.Equal(t, 1, e.launcher.count())

	cancel()
	e.runner.Wait()
}

// seedOneCardRun seeds ALPHA-1, already forced, as the only entry of a
// running run with no entry yet, so the first pass launches it.
func (e *env) seedOneCardRun() {
	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout")
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)
}

// eventLog records launches and kills in order, and the most launches ever
// in flight at once.
type eventLog struct {
	mu                  sync.Mutex
	events              []string
	inFlight, maxFlight int
}

func (l *eventLog) add(ev string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, ev)
}

func (l *eventLog) enter() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.inFlight++
	l.maxFlight = max(l.maxFlight, l.inFlight)
}

func (l *eventLog) leave() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.inFlight--
}

func (l *eventLog) snapshot() ([]string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.events), l.maxFlight
}

// holdFirstLaunch blocks the first launch inside the launcher until release
// is called, then queues its card; later launches queue at once. Kills mark
// the card killed. release is also a cleanup, so a failing test never leaves
// a walker blocked.
func (e *env) holdFirstLaunch(t *testing.T) (<-chan struct{}, func(), *eventLog) {
	t.Helper()

	entered := make(chan struct{})
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)

	log := &eventLog{}

	var held atomic.Bool

	e.launcher.onLaunch = func(_ context.Context, project, card string) error {
		log.enter()
		defer log.leave()

		log.add("launch " + card)

		if held.CompareAndSwap(false, true) {
			close(entered)
			<-gate
		}

		e.cards.queue(project, card)

		return nil
	}

	e.stopper.onStop = func(project, card string) {
		log.add("kill " + card)
		e.cards.set(project, card, func(c *board.Card) { c.WorkerStatus = "killed" })
	}

	return entered, release, log
}

// TestPlay_DuringStopWaitsForTheStop: a Play that lands while Stop waits for
// its walker's in-flight launch must wait for that Stop's kill. Otherwise the
// new run holds on the card the old launch queued and the old Stop kills it,
// silently undoing the Play.
func TestPlay_DuringStopWaitsForTheStop(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()
	entered, release, log := e.holdFirstLaunch(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	require.Eventually(t, func() bool { return e.runner.walkerCount() == 0 }, time.Second, 5*time.Millisecond,
		"Stop has cancelled the walker and is waiting for it")

	played := make(chan error, 1)

	go func() {
		_, err := e.runner.Play(context.Background(), "rollout", "human:alice")
		played <- err
	}()

	select {
	case err := <-played:
		t.Fatalf("Play returned while Stop was still waiting for the launch: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	assert.Equal(t, 1, e.launcher.count())

	release()

	require.NoError(t, <-stopped)
	require.NoError(t, <-played)
	require.Eventually(t, func() bool { return e.launcher.count() == 2 }, time.Second, 5*time.Millisecond,
		"the resumed run relaunches the killed card")

	events, maxFlight := log.snapshot()
	assert.Equal(t, []string{"launch ALPHA-1", "kill ALPHA-1", "launch ALPHA-1"}, events)
	assert.Equal(t, 1, maxFlight, "never two launches at once")
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.called(), "only the Stop killed a worker")

	cancel()
	e.runner.Wait()
}

// TestPlay_GivesUpWhenItsContextEndsDuringAStop: a Play waiting for a Stop
// returns when its own context ends and writes nothing, so a retry is clean.
func TestPlay_GivesUpWhenItsContextEndsDuringAStop(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()
	entered, release, _ := e.holdFirstLaunch(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	require.Eventually(t, func() bool { return e.runner.walkerCount() == 0 }, time.Second, 5*time.Millisecond)

	writes := e.pbs.runWrites()

	playCtx, cancelPlay := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelPlay()

	_, err := e.runner.Play(playCtx, "rollout", "human:alice")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, writes, e.pbs.runWrites(), "a Play that gave up wrote nothing")

	release()
	require.NoError(t, <-stopped)

	cancel()
	e.runner.Wait()
}

// TestEnsure_NewWalkerWaitsForTheCancelledOne: Stop gives up waiting once
// stopWait runs out, so a Play can follow while the cancelled walker is still
// inside its launch. The new walker must not pass until that walker exits.
func TestEnsure_NewWalkerWaitsForTheCancelledOne(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()
	entered, release, _ := e.holdFirstLaunch(t)
	e.runner.stopWait = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
	require.ErrorIs(t, err, ErrStopWorker, "Stop gave up waiting for the walker")

	_, err = e.runner.Play(context.Background(), "rollout", "human:alice")
	require.NoError(t, err)

	assert.Never(t, func() bool { return e.launcher.count() > 1 }, 50*time.Millisecond, 5*time.Millisecond,
		"the new walker passed while the cancelled one was inside its launch")

	release()

	require.Eventually(t, func() bool {
		run := e.pbs.lastRun()

		return run.Status == board.RunStatusRunning && run.Entry == "e1"
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, e.launcher.count(), "the new run waits on the worker the old launch queued")

	cancel()
	e.runner.Wait()
}

// TestStop_FinishesItsKillsAfterItsRequestEnds: the request behind a Stop can
// end while Stop waits out a caught launch (a reload, a closed tab). The stop
// is already persisted, so Stop must still kill the worker that launch queues.
func TestStop_FinishesItsKillsAfterItsRequestEnds(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()
	entered, release, _ := e.holdFirstLaunch(t)

	// Shaped like the real kill: a call whose context has ended fails.
	e.runner.SetStopper(func(ctx context.Context, project, card string) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		return e.stopper.stop(ctx, project, card)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(stopCtx, "rollout", "human:alice")
		stopped <- err
	}()

	require.Eventually(t, func() bool { return e.runner.walkerCount() == 0 }, time.Second, 5*time.Millisecond,
		"Stop has cancelled the walker and is waiting for it")

	stopCancel()

	assert.Never(t, func() bool { return len(stopped) > 0 }, 50*time.Millisecond, 5*time.Millisecond,
		"Stop returned when its request ended, before the launch it caught had queued")

	release()

	require.NoError(t, <-stopped)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.called(), "the worker the launch queued is killed")

	card, err := e.cards.GetCard(context.Background(), "alpha", "ALPHA-1")
	require.NoError(t, err)
	assert.Equal(t, "killed", card.WorkerStatus)

	cancel()
	e.runner.Wait()
}

// TestStop_LetsACaughtLaunchFinish: Stop cancels the walker while its launch
// is mid-call. The launch still runs to its outcome, so the card reads queued
// and Stop kills it; aborted, it would revert the card to failed while the
// backend may already have started the worker.
func TestStop_LetsACaughtLaunchFinish(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()

	entered := make(chan struct{})
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)

	// Shaped like the real launch: a call whose context ends mid-send fails
	// and reverts the card.
	e.launcher.onLaunch = func(ctx context.Context, project, card string) error {
		close(entered)
		<-gate

		if ctx.Err() != nil {
			e.cards.set(project, card, func(c *board.Card) { c.WorkerStatus = "failed" })

			return errors.New("failed to trigger backend task")
		}

		e.cards.queue(project, card)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	require.Eventually(t, func() bool { return e.runner.walkerCount() == 0 }, time.Second, 5*time.Millisecond,
		"Stop has cancelled the walker")

	release()

	require.NoError(t, <-stopped)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.called(), "the worker the launch queued is killed")

	cancel()
	e.runner.Wait()
}

// TestStop_SparesAHandRunStartedDuringItsWait: once the stop is written a
// human may run another card by hand while Stop still waits for its walker.
// That worker is not the run's and must outlive the stop.
func TestStop_SparesAHandRunStartedDuringItsWait(t *testing.T) {
	e := newEnv(t)

	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout")
	e.cards.add(c)
	e.cards.add(todoCard("alpha", "ALPHA-2"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	e.activeRun(p, "")
	e.pbs.add(p)

	entered, release, _ := e.holdFirstLaunch(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	<-entered

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	require.Eventually(t, func() bool { return e.pbs.lastRun().Status == board.RunStatusStopped },
		time.Second, 5*time.Millisecond, "the stop is written")

	e.cards.queue("alpha", "ALPHA-2")
	release()

	require.NoError(t, <-stopped)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.called(), "only the run's launch is killed")

	cancel()
	e.runner.Wait()
}

// TestStop_RestartsTheWalkerWhenItsWriteFails: a pass can see the stop before
// its failed commit is rolled back and let its walker go, leaving the still
// running run with nothing walking it.
func TestStop_RestartsTheWalkerWhenItsWriteFails(t *testing.T) {
	e := newEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Started before the run is seeded, so no walker is resumed.
	e.runner.Start(ctx)

	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout")
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	commitErr := errors.New("commit failed")

	e.pbs.mu.Lock()
	e.pbs.failNextSetRun = commitErr
	e.pbs.mu.Unlock()

	_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
	require.ErrorIs(t, err, commitErr)

	require.Eventually(t, func() bool { return e.launcher.count() == 1 }, 2*time.Second, 10*time.Millisecond,
		"a walker walks the run that is still running")

	cancel()
	e.runner.Wait()
}

// TestLaunchEntry_SendsNothingOnceStopped: a Stop that lands between the
// running write and the trigger has cancelled the walker; no trigger goes out.
func TestLaunchEntry_SendsNothingOnceStopped(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()

	stopped := make(chan error, 1)

	e.pbs.mu.Lock()
	e.pbs.afterSetRun = func(ctx context.Context) {
		go func() {
			_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
			stopped <- err
		}()

		// This runs on the walker: its pass context ends when Stop cancels it.
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
	}
	e.pbs.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)

	require.NoError(t, <-stopped)
	assert.Equal(t, 0, e.launcher.count(), "no trigger after the stop was persisted")
	assert.Empty(t, e.stopper.called())

	cancel()
	e.runner.Wait()
}

// TestLaunchEntry_ShutdownCancelsTheTrigger: cancelling the runner's context
// aborts a trigger in flight, so Shutdown returns instead of waiting it out.
func TestLaunchEntry_ShutdownCancelsTheTrigger(t *testing.T) {
	e := newEnv(t)
	e.seedOneCardRun()

	entered := make(chan struct{})

	e.launcher.onLaunch = func(ctx context.Context, _, _ string) error {
		close(entered)
		<-ctx.Done()

		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the trigger never started")
	}

	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer scancel()

	require.NoError(t, e.runner.Shutdown(sctx))
}

// TestShutdown_JoinsAStopsKills: Shutdown waits for the kills a Stop runs
// after its detached wait, so the process cannot exit between them.
func TestShutdown_JoinsAStopsKills(t *testing.T) {
	e := newEnv(t)

	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout")
	e.cards.add(c)
	e.cards.queue("alpha", "ALPHA-1")

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	entered := make(chan struct{})
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)

	e.stopper.onStop = func(_, _ string) {
		close(entered)
		<-gate
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Eventually(t, e.walkerReady, 2*time.Second, 5*time.Millisecond)

	stopped := make(chan error, 1)

	go func() {
		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		stopped <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the kill never started")
	}

	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer scancel()

	require.ErrorIs(t, e.runner.Shutdown(sctx), context.DeadlineExceeded)

	release()
	require.NoError(t, <-stopped)

	sctx2, scancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer scancel2()

	require.NoError(t, e.runner.Shutdown(sctx2))
}
