package playbookrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type env struct {
	cards    *fakeCards
	pbs      *fakePlaybooks
	launcher *recordingLauncher
	stopper  *recordingStopper
	clk      *clock.FakeClock
	runner   *Runner
}

func newEnv(t *testing.T) *env {
	t.Helper()

	cards := newFakeCards()
	pbs := newFakePlaybooks(cards)
	clk := clock.Fake(time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC))
	launcher := &recordingLauncher{}
	stopper := &recordingStopper{}

	r := New(Config{Playbooks: pbs, Lister: pbs, Cards: cards, Bus: events.NewBus(), Clock: clk, Instance: "lap-a", Tick: 30 * time.Second})
	r.SetLauncher(launcher.launch)
	r.SetStopper(stopper.stop)

	return &env{cards: cards, pbs: pbs, launcher: launcher, stopper: stopper, clk: clk, runner: r}
}

// activeRun seeds a running run owned by lap-a on entry.
func (e *env) activeRun(p *board.Playbook, entry string) {
	now := e.clk.Now()
	p.Run = &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a", StartedBy: "human:alice", StartedAt: now, UpdatedAt: now, Entry: entry}
}

func TestPass_LaunchesTheFrontierCard(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	p.BaseBranch = "main"
	e.activeRun(p, "")
	e.pbs.add(p)

	done := e.runner.pass(context.Background(), "rollout")
	require.False(t, done)

	require.Equal(t, 1, e.launcher.count())
	call := e.launcher.calls[0]
	assert.Equal(t, "alpha", call.project)
	assert.Equal(t, "ALPHA-1", call.card)
	assert.True(t, call.opts.CreateBaseBranch)
	assert.Equal(t, "main", call.opts.BaseBranchFrom)

	assert.Equal(t, []string{"ALPHA-1"}, e.cards.forced, "settings re-asserted before launch")
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusRunning, run.Status)
	assert.Equal(t, "e1", run.Entry)
	assert.Empty(t, run.Reason)
}

func TestPass_SkipsTerminalAndWaitsOnRunning(t *testing.T) {
	e := newEnv(t)
	done := todoCard("alpha", "ALPHA-1")
	done.State = board.StateDone
	e.cards.add(done)

	running := todoCard("alpha", "ALPHA-2")
	running.State = board.StateInProgress
	running.WorkerStatus = "running"
	e.cards.add(running)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 0, e.launcher.count())
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusRunning, run.Status)
	assert.Equal(t, "e2", run.Entry)

	// Idempotent: nothing changed, nothing written.
	writes := e.pbs.runWrites()
	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, writes, e.pbs.runWrites())
}

func TestPass_ClaimedElsewhereCountsAsRunning(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.AssignedAgent = "agent-1"
	e.cards.add(c)
	e.cards.elsewhere[cardKey("alpha", "ALPHA-1")] = true
	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 0, e.launcher.count())
	assert.Equal(t, board.RunStatusRunning, e.pbs.lastRun().Status)
}

func TestPass_NeedsHumanOnTriggeredEntry(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *board.Card)
		reason string
	}{
		{"failed", func(c *board.Card) { c.WorkerStatus = "failed" }, "worker failed"},
		{"killed", func(c *board.Card) { c.WorkerStatus = "killed" }, "worker killed"},
		{"parked", func(c *board.Card) {
			c.State = board.StateReview
			c.WorkerStatus = "parked"
			c.ActivityLog = []board.ActivityEntry{{Agent: "agent-1", Action: "parked", Message: "merge refused by GitHub"}}
		}, "parked: merge refused by GitHub"},
		{"stalled", func(c *board.Card) { c.State = board.StateStalled }, "stalled"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			c := todoCard("alpha", "ALPHA-1")
			tc.mutate(c)
			e.cards.add(c)

			p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
			e.activeRun(p, "e1") // this run already triggered e1
			e.pbs.add(p)

			require.False(t, e.runner.pass(context.Background(), "rollout"))
			assert.Equal(t, 0, e.launcher.count(), "never re-launch on its own")
			run := e.pbs.lastRun()
			assert.Equal(t, board.RunStatusWaiting, run.Status)
			assert.Equal(t, "e1", run.Entry)
			assert.Contains(t, run.Reason, tc.reason)
		})
	}
}

func TestPass_ResumeRelaunchesAfterPlayClearedEntry(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateReview
	c.WorkerStatus = "parked"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "") // Play cleared the entry
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, []string{"ALPHA-1:review->todo"}, e.cards.transitions)
	assert.Equal(t, 1, e.launcher.count())
	assert.Equal(t, board.RunStatusRunning, e.pbs.lastRun().Status)
}

func TestPass_ManualGateWaitsThenAdvances(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", manualEntry("e1", "deploy"), cardEntry("e2", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusWaiting, run.Status)
	assert.Equal(t, "e1", run.Entry)
	assert.Equal(t, "awaiting check-off", run.Reason)
	assert.Equal(t, 0, e.launcher.count())

	p.Entries[0].Done = true

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 1, e.launcher.count())
	assert.Equal(t, "e2", e.pbs.lastRun().Entry)
}

func TestPass_MissingCardAndNoWorkerWait(t *testing.T) {
	e := newEnv(t)
	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "GONE-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusWaiting, run.Status)
	assert.Contains(t, run.Reason, "alpha/GONE-1")

	e2 := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress // claimed by hand, no worker
	e2.cards.add(c)

	p2 := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e2.activeRun(p2, "")
	e2.pbs.add(p2)

	require.False(t, e2.runner.pass(context.Background(), "rollout"))
	assert.Contains(t, e2.pbs.lastRun().Reason, "without a worker")
	assert.Equal(t, 0, e2.launcher.count())
}

func TestPass_LaunchErrorBecomesReason(t *testing.T) {
	e := newEnv(t)
	e.launcher.err = errors.New("failed to trigger backend task")
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusWaiting, run.Status)
	assert.Equal(t, "e1", run.Entry)
	assert.Contains(t, run.Reason, "failed to trigger backend task")

	// A waiting entry is never relaunched on its own: only Play, which
	// clears the entry, may trigger it again.
	writes := e.pbs.runWrites()
	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 1, e.launcher.count())
	assert.Equal(t, writes, e.pbs.runWrites())
	run = e.pbs.lastRun()
	assert.Equal(t, board.RunStatusWaiting, run.Status)
	assert.Equal(t, "e1", run.Entry)
}

func TestPass_RunningOnTodoEntryRelaunches(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1") // the crash between persist and trigger: still running, no worker yet
	e.pbs.add(p)

	ctx := context.Background()

	require.False(t, e.runner.pass(ctx, "rollout"))
	assert.Equal(t, 1, e.launcher.count())

	d, err := e.pbs.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusRunning, d.Run.Status)
}

func TestPass_CompletesWhenNothingIsLeft(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateDone
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), manualEntry("e2", "done"))
	p.Entries[1].Done = true
	e.activeRun(p, "e1")
	e.pbs.add(p)

	require.True(t, e.runner.pass(context.Background(), "rollout"))
	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusCompleted, run.Status)
	assert.Empty(t, run.Entry)
	require.NotNil(t, run.EndedAt)
	assert.Equal(t, e.clk.Now(), *run.EndedAt)
}

func TestPass_CompletionWriteFailureRetries(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateDone
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)
	e.pbs.failNextSetRun = errors.New("write failed")
	ctx := context.Background()

	require.False(t, e.runner.pass(ctx, "rollout"))
	d, err := e.pbs.Get(ctx, "rollout")
	require.NoError(t, err)
	require.NotNil(t, d.Run)
	assert.Equal(t, board.RunStatusRunning, d.Run.Status, "the failed write leaves the run as it was on disk")

	require.True(t, e.runner.pass(ctx, "rollout"))
	d, err = e.pbs.Get(ctx, "rollout")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusCompleted, d.Run.Status, "the retry succeeds once the write stops failing")
}

func TestPass_IgnoresRunsItDoesNotOwn(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	p.Run.Instance = "lap-b"
	e.pbs.add(p)

	require.True(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 0, e.launcher.count())
	assert.Equal(t, 0, e.pbs.runWrites())

	// Unknown playbook: done, nothing written.
	require.True(t, e.runner.pass(context.Background(), "nope"))
}

func TestPlay_StartsAndResumes(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.pbs.add(p)

	ctx := context.Background()

	d, err := e.runner.Play(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	require.NotNil(t, d.Run)
	assert.Equal(t, board.RunStatusRunning, d.Run.Status)
	assert.Equal(t, "lap-a", d.Run.Instance)
	assert.Equal(t, "human:alice", d.Run.StartedBy)
	assert.Empty(t, d.Run.Entry)
	started := d.Run.StartedAt

	_, err = e.runner.Play(ctx, "rollout", "human:alice")
	require.ErrorIs(t, err, service.ErrPlaybookRunActive)

	// Stopped, then resumed: started_at survives, entry and reason are cleared.
	e.clk.Advance(time.Minute)
	d, err = e.runner.Stop(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusStopped, d.Run.Status)
	require.NotNil(t, d.Run.EndedAt)

	e.clk.Advance(time.Minute)
	d, err = e.runner.Play(ctx, "rollout", "human:bob")
	require.NoError(t, err)
	assert.Equal(t, started, d.Run.StartedAt)
	assert.Equal(t, "human:bob", d.Run.StartedBy)
	assert.Nil(t, d.Run.EndedAt)

	// Completed, then played again: a fresh started_at.
	p.Run.Status = board.RunStatusCompleted

	e.clk.Advance(time.Minute)
	d, err = e.runner.Play(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, e.clk.Now(), d.Run.StartedAt)
}

func TestPlay_Refusals(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	p.Runnable = false
	e.pbs.add(p)

	ctx := context.Background()

	_, err := e.runner.Play(ctx, "rollout", "human:alice")
	require.ErrorIs(t, err, service.ErrPlaybookNotRunnable)

	p.Runnable = true

	e.runner.SetLauncher(nil)
	_, err = e.runner.Play(ctx, "rollout", "human:alice")
	require.ErrorIs(t, err, ErrNoBackend)

	_, err = e.runner.Stop(ctx, "rollout", "human:alice")
	require.ErrorIs(t, err, ErrRunInactive)
}

func TestStop_KillsTheCurrentWorker(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	d, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusStopped, d.Run.Status)
	assert.Equal(t, []string{"ALPHA-1"}, e.stopper.calls)

	// A stopper failure keeps the run stopped and surfaces as ErrStopWorker.
	e2 := newEnv(t)
	e2.stopper.err = errors.New("kill webhook failed")
	c2 := todoCard("alpha", "ALPHA-1")
	c2.State = board.StateInProgress
	c2.WorkerStatus = "queued"
	e2.cards.add(c2)

	p2 := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e2.activeRun(p2, "e1")
	e2.pbs.add(p2)

	d, err = e2.runner.Stop(context.Background(), "rollout", "human:alice")
	require.ErrorIs(t, err, ErrStopWorker)
	require.NotNil(t, d)
	assert.Equal(t, board.RunStatusStopped, d.Run.Status)

	// No worker in flight: no stopper call.
	e3 := newEnv(t)
	e3.cards.add(todoCard("alpha", "ALPHA-1"))

	p3 := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e3.activeRun(p3, "e1")
	e3.pbs.add(p3)

	_, err = e3.runner.Stop(context.Background(), "rollout", "human:alice")
	require.NoError(t, err)
	assert.Empty(t, e3.stopper.calls)
}
