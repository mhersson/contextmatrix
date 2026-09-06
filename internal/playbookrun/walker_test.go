package playbookrun

import (
	"context"
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
	assert.Equal(t, "ALPHA-1", e.launcher.calls[0].card)

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
	e.launcher.onLaunch = func(project, card string) {
		e.cards.mu.Lock()
		defer e.cards.mu.Unlock()

		queued := e.cards.cards[cardKey(project, card)]
		queued.State = board.StateInProgress
		queued.WorkerStatus = "queued"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.runner.Start(ctx)
	require.Eventually(t, e.walkerReady, time.Second, 5*time.Millisecond)

	// The first card finishes: a card event for it must launch the second.
	e.cards.mu.Lock()
	c.State = board.StateDone
	c.WorkerStatus = "completed"
	e.cards.mu.Unlock()

	e.runner.cfg.Bus.Publish(events.Event{Type: events.CardStateChanged, Project: "alpha", CardID: "ALPHA-1", Data: map[string]any{"new_state": "done"}})
	require.Eventually(t, func() bool { return e.launcher.count() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "ALPHA-2", e.launcher.calls[0].card)

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
	require.Eventually(t, e.walkerReady, time.Second, 5*time.Millisecond)

	before := e.pbs.runWrites()

	for range 10 {
		e.runner.cfg.Bus.Publish(events.Event{Type: events.CardUpdated, Project: "beta", CardID: "BETA-9"})
		e.runner.cfg.Bus.Publish(events.Event{Type: events.PlaybookUpdated, Data: map[string]any{"id": "other"}})
	}

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, before, e.pbs.runWrites(), "unrelated events do not write")

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
	e.launcher.onLaunch = func(project, card string) {
		close(entered)
		<-release

		e.cards.mu.Lock()
		defer e.cards.mu.Unlock()

		queued := e.cards.cards[cardKey(project, card)]
		queued.State = board.StateInProgress
		queued.WorkerStatus = "queued"
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

	select {
	case err := <-stopped:
		t.Fatalf("Stop returned while the launch was still in flight: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	require.NoError(t, <-stopped)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.calls, "the worker the launch queued is killed")
	assert.Equal(t, board.RunStatusStopped, e.pbs.lastRun().Status)
	assert.Equal(t, 1, e.launcher.count())

	cancel()
	e.runner.Wait()
}
