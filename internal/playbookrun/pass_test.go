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

	// Start is deliberately not called: until it is, the runner's walker
	// parent is already cancelled, so a walker a Play spawns exits at once
	// instead of racing the test's own writes. Tests that want live walkers
	// call Start with their own context.
	e := &env{cards: cards, pbs: pbs, launcher: launcher, stopper: stopper, clk: clk, runner: r}

	t.Cleanup(func() { e.runner.Wait() })

	return e
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

// TestPass_StopMidPassIsNotOverwritten pins the compare-and-swap. The pass
// decides from a run it read as running; a human presses Stop before the
// write lands. Without the guard the pass would restore running and trigger
// an autonomous container after the human pressed the escape hatch.
func TestPass_StopMidPassIsNotOverwritten(t *testing.T) {
	e := newEnv(t)

	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout") // already forced: no card writes in the way
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	// The Stop lands after the pass has its detail and before its write.
	e.pbs.mu.Lock()
	e.pbs.afterGet = func() {
		e.pbs.mu.Lock()
		defer e.pbs.mu.Unlock()

		p.Run.Status = board.RunStatusStopped
		p.Run.Reason = "stopped by human:alice"
	}
	e.pbs.mu.Unlock()

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 0, e.launcher.count(), "a stopped run never triggers a card")
	assert.Equal(t, 0, e.pbs.runWrites(), "the refused write never reached the store")

	d, err := e.pbs.Get(context.Background(), "rollout")
	require.NoError(t, err)
	require.NotNil(t, d.Run)
	assert.Equal(t, board.RunStatusStopped, d.Run.Status, "the stop survives the pass")
}

func TestPass_SkipsForcingWhenTheCardAlreadyHasTheSettings(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.ApplyPlaybookSettings("playbook/rollout")
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 1, e.launcher.count())
	assert.Empty(t, e.cards.forced, "no card write when the settings are already there")
}

func TestStop_DeclinesTheStopperWhenTheCardIsClaimedElsewhere(t *testing.T) {
	e := newEnv(t)
	c := todoCard("alpha", "ALPHA-1")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"
	e.cards.add(c)
	e.cards.elsewhere[cardKey("alpha", "ALPHA-1")] = true

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "e1")
	e.pbs.add(p)

	d, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusStopped, d.Run.Status)
	assert.Empty(t, e.stopper.calls, "another instance owns that container")
}

func importedCard(project, id string) *board.Card {
	c := todoCard(project, id)
	c.Source = &board.Source{System: "jira", ExternalID: "PROJ-1", ExternalURL: "https://acme.atlassian.net/browse/PROJ-1"}

	return c
}

func TestPass_UnvettedCardWaitsUntilVetted(t *testing.T) {
	e := newEnv(t)
	c := importedCard("alpha", "ALPHA-1")
	e.cards.add(c)

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	ctx := context.Background()

	require.False(t, e.runner.pass(ctx, "rollout"))
	assert.Equal(t, 0, e.launcher.count(), "an unvetted card is never launched")
	assert.Empty(t, e.cards.forced, "settings are not forced on a card the run cannot launch")

	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusWaiting, run.Status)
	assert.Empty(t, run.Entry, "the entry is not marked triggered")
	assert.Contains(t, run.Reason, "ALPHA-1")

	// A second pass with nothing changed writes nothing.
	writes := e.pbs.runWrites()
	require.False(t, e.runner.pass(ctx, "rollout"))
	assert.Equal(t, writes, e.pbs.runWrites())

	// A human vets the card: the next pass launches it without a Play.
	c.Vetted = true

	require.False(t, e.runner.pass(ctx, "rollout"))
	assert.Equal(t, 1, e.launcher.count())
	assert.Equal(t, "e1", e.pbs.lastRun().Entry)
	assert.Equal(t, board.RunStatusRunning, e.pbs.lastRun().Status)
}

func TestPass_SourcelessUnvettedCardLaunches(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1")) // Vetted false, Source nil

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"))
	e.activeRun(p, "")
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 1, e.launcher.count())
}

// liveCard is ALPHA-2 in progress with a running worker, the run's entry e2
// in the hold tests.
func liveCard() *board.Card {
	c := todoCard("alpha", "ALPHA-2")
	c.State = board.StateInProgress
	c.WorkerStatus = "running"

	return c
}

// TestPass_HoldsOnTheLiveWorker simulates what a refusal that lost a race,
// a peer's merge or a card delete leaves behind: an entry ahead of the run's
// live card turned incomplete. Acting on it would start a second worker or
// point run.entry away from the live one, and Stop kills only run.entry's
// worker. Once that worker ends, the entry ahead is handled as it is today.
func TestPass_HoldsOnTheLiveWorker(t *testing.T) {
	ahead := []struct {
		name  string
		entry board.PlaybookEntry
		card  func() *board.Card // e1's card; nil for a manual entry or a missing card
		// resumed checks the pass after e2's worker ended.
		resumed func(t *testing.T, e *env)
	}{
		{
			name: "reopened card", entry: cardEntry("e1", "alpha", "ALPHA-1"),
			card: func() *board.Card { return todoCard("alpha", "ALPHA-1") },
			resumed: func(t *testing.T, e *env) {
				t.Helper()
				require.Equal(t, 1, e.launcher.count())
				assert.Equal(t, "ALPHA-1", e.launcher.call(0).card)
				run := e.pbs.lastRun()
				assert.Equal(t, board.RunStatusRunning, run.Status)
				assert.Equal(t, "e1", run.Entry)
			},
		},
		{
			name: "unvetted import", entry: cardEntry("e1", "alpha", "ALPHA-1"),
			card: func() *board.Card { return importedCard("alpha", "ALPHA-1") },
			resumed: func(t *testing.T, e *env) {
				t.Helper()
				assert.Equal(t, 0, e.launcher.count())
				run := e.pbs.lastRun()
				assert.Equal(t, board.RunStatusWaiting, run.Status)
				assert.Empty(t, run.Entry)
				assert.Equal(t, "ALPHA-1 not vetted", run.Reason)
			},
		},
		{
			name: "unchecked manual entry", entry: manualEntry("e1", "deploy"),
			resumed: func(t *testing.T, e *env) {
				t.Helper()
				assert.Equal(t, 0, e.launcher.count())
				run := e.pbs.lastRun()
				assert.Equal(t, board.RunStatusWaiting, run.Status)
				assert.Equal(t, "e1", run.Entry)
				assert.Equal(t, "awaiting check-off", run.Reason)
			},
		},
		{
			name: "missing card", entry: cardEntry("e1", "alpha", "GONE-1"),
			resumed: func(t *testing.T, e *env) {
				t.Helper()
				assert.Equal(t, 0, e.launcher.count())
				run := e.pbs.lastRun()
				assert.Equal(t, board.RunStatusWaiting, run.Status)
				assert.Equal(t, "e1", run.Entry)
				assert.Contains(t, run.Reason, "alpha/GONE-1")
			},
		},
	}

	live := []struct {
		name string
		make func(e *env, c *board.Card)
	}{
		{"running", func(_ *env, c *board.Card) { c.WorkerStatus = "running" }},
		{"queued", func(_ *env, c *board.Card) { c.WorkerStatus = "queued" }},
		{"claimed elsewhere", func(e *env, c *board.Card) {
			c.WorkerStatus = ""
			c.AssignedAgent = "agent-1"
			e.cards.elsewhere[cardKey("alpha", "ALPHA-2")] = true
		}},
	}

	ended := []struct {
		name string
		end  func(c *board.Card)
	}{
		{"card done", func(c *board.Card) { c.State = board.StateDone }},
		{"worker status cleared", func(c *board.Card) { c.WorkerStatus = "" }},
	}

	setup := func(t *testing.T, i int) (*env, *board.Card) {
		t.Helper()

		e := newEnv(t)
		if a := ahead[i]; a.card != nil {
			e.cards.add(a.card())
		}

		c := liveCard()
		e.cards.add(c)

		p := runnablePlaybook("rollout", ahead[i].entry, cardEntry("e2", "alpha", "ALPHA-2"))
		e.activeRun(p, "e2")
		e.pbs.add(p)

		return e, c
	}

	for i, a := range ahead {
		for _, l := range live {
			t.Run(a.name+"/"+l.name, func(t *testing.T) {
				e, c := setup(t, i)
				l.make(e, c)

				require.False(t, e.runner.pass(context.Background(), "rollout"))
				assert.Equal(t, 0, e.launcher.count(), "no second worker")
				assert.Empty(t, e.cards.transitions)
				assert.Empty(t, e.cards.forced)
				assert.Equal(t, 0, e.pbs.runWrites(), "already running on e2: nothing to write")

				d, err := e.pbs.Get(context.Background(), "rollout")
				require.NoError(t, err)
				assert.Equal(t, board.RunStatusRunning, d.Run.Status)
				assert.Equal(t, "e2", d.Run.Entry, "Stop still targets the live worker")
				assert.Empty(t, d.Run.Reason)
			})
		}

		for _, end := range ended {
			t.Run(a.name+"/after the worker ends: "+end.name, func(t *testing.T) {
				e, c := setup(t, i)

				require.False(t, e.runner.pass(context.Background(), "rollout"))
				require.Equal(t, 0, e.launcher.count())

				e.cards.mu.Lock()
				end.end(c)
				e.cards.mu.Unlock()

				require.False(t, e.runner.pass(context.Background(), "rollout"))
				a.resumed(t, e)
			})
		}
	}
}

// TestPass_HoldOnLiveWorkerResumesRunning: a run left waiting on its entry
// goes back to running, with the reason cleared, once that entry's card has a
// live worker, whatever is incomplete ahead of it.
func TestPass_HoldOnLiveWorkerResumesRunning(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))

	c := liveCard()
	c.WorkerStatus = ""
	c.AssignedAgent = "agent-1"
	e.cards.add(c)
	e.cards.elsewhere[cardKey("alpha", "ALPHA-2")] = true

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	e.activeRun(p, "e2")
	p.Run.Status = board.RunStatusWaiting
	p.Run.Reason = "ALPHA-2 is in in_progress without a worker"
	e.pbs.add(p)

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	assert.Equal(t, 0, e.launcher.count())

	run := e.pbs.lastRun()
	assert.Equal(t, board.RunStatusRunning, run.Status)
	assert.Equal(t, "e2", run.Entry)
	assert.Empty(t, run.Reason)
}

// TestPass_RunCardReadFailureWalksInOrder: when the run's card cannot be read
// the pass falls back to the stored-order walk.
func TestPass_RunCardReadFailureWalksInOrder(t *testing.T) {
	e := newEnv(t)
	e.cards.add(todoCard("alpha", "ALPHA-1"))
	e.cards.add(liveCard())

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	e.activeRun(p, "e2")
	e.pbs.add(p)

	// The card vanishes between the detail read and the pass's own read.
	e.pbs.mu.Lock()
	e.pbs.afterGet = func() {
		e.cards.mu.Lock()
		defer e.cards.mu.Unlock()

		delete(e.cards.cards, cardKey("alpha", "ALPHA-2"))
	}
	e.pbs.mu.Unlock()

	require.False(t, e.runner.pass(context.Background(), "rollout"))
	require.Equal(t, 1, e.launcher.count())
	assert.Equal(t, "ALPHA-1", e.launcher.call(0).card)
	assert.Equal(t, "e1", e.pbs.lastRun().Entry)
}

// TestPlay_WaitsOnAWorkerThatOutlivedStop: Stop leaves a worker alive when
// its kill fails or the card is claimed through another instance. A reopen is
// allowed once the run is stopped, so without Play resuming on the live
// worker the first pass would launch the reopened card beside it and Stop
// would then target the wrong one.
func TestPlay_WaitsOnAWorkerThatOutlivedStop(t *testing.T) {
	modes := []struct {
		name    string
		survive func(e *env, c *board.Card)
		stopErr error
	}{
		{"the kill fails", func(e *env, _ *board.Card) {
			e.stopper.err = errors.New("kill webhook failed")
		}, ErrStopWorker},
		{"the card is claimed elsewhere", func(e *env, c *board.Card) {
			c.AssignedAgent = "agent-1"
			e.cards.elsewhere[cardKey("alpha", "ALPHA-2")] = true
		}, nil},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()

			first := todoCard("alpha", "ALPHA-1")
			first.State = board.StateDone
			e.cards.add(first)

			c := liveCard()
			e.cards.add(c)
			m.survive(e, c)

			p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
			e.activeRun(p, "e2")
			e.pbs.add(p)

			_, err := e.runner.Stop(ctx, "rollout", "human:alice")
			if m.stopErr != nil {
				require.ErrorIs(t, err, m.stopErr)
			} else {
				require.NoError(t, err)
			}

			// The run is stopped, so the reopen is allowed.
			e.cards.mu.Lock()
			first.State = board.StateTodo
			e.cards.mu.Unlock()

			d, err := e.runner.Play(ctx, "rollout", "human:alice")
			require.NoError(t, err)
			assert.Equal(t, "e2", d.Run.Entry, "the resume names the surviving worker")

			require.False(t, e.runner.pass(ctx, "rollout"))
			assert.Equal(t, 0, e.launcher.count(), "no second worker")

			got, err := e.pbs.Get(ctx, "rollout")
			require.NoError(t, err)
			assert.Equal(t, board.RunStatusRunning, got.Run.Status)
			assert.Equal(t, "e2", got.Run.Entry, "Stop targets the surviving worker")
		})
	}
}

// TestPlay_ResumeEntry pins which entry a Play resumes on.
func TestPlay_ResumeEntry(t *testing.T) {
	card := func(id, state, worker string) *board.Card {
		c := todoCard("alpha", id)
		c.State, c.WorkerStatus = state, worker

		return c
	}
	stopped := func(entry string) *board.PlaybookRun {
		return &board.PlaybookRun{Status: board.RunStatusStopped, Entry: entry, Reason: "stopped by human:alice"}
	}

	tests := []struct {
		name      string
		cards     []*board.Card
		run       *board.PlaybookRun
		vanish    string // card deleted between Play's detail read and its scan
		wantEntry string
		launches  []string // cards the first pass launches
	}{
		{
			name:  "a hand run made while stopped is waited on",
			cards: []*board.Card{card("ALPHA-1", board.StateTodo, ""), card("ALPHA-2", board.StateTodo, "queued")},
			run:   stopped(""), wantEntry: "e2",
		},
		{
			name:  "the stopped run's own live entry wins over an earlier one",
			cards: []*board.Card{card("ALPHA-1", board.StateTodo, "running"), card("ALPHA-2", board.StateInProgress, "running")},
			run:   stopped("e2"), wantEntry: "e2",
		},
		{
			name:  "a failed card's resume relaunches it",
			cards: []*board.Card{card("ALPHA-1", board.StateTodo, "failed"), card("ALPHA-2", board.StateTodo, "")},
			run:   stopped("e1"), wantEntry: "", launches: []string{"ALPHA-1"},
		},
		{
			name:      "a first run starts from the top",
			cards:     []*board.Card{card("ALPHA-1", board.StateTodo, ""), card("ALPHA-2", board.StateTodo, "")},
			wantEntry: "", launches: []string{"ALPHA-1"},
		},
		{
			name:  "a terminal card's worker is not waited on",
			cards: []*board.Card{card("ALPHA-1", board.StateDone, "running"), card("ALPHA-2", board.StateTodo, "")},
			run:   stopped("e1"), wantEntry: "", launches: []string{"ALPHA-2"},
		},
		{
			name:  "a card read failure during the scan does not block Play",
			cards: []*board.Card{card("ALPHA-1", board.StateTodo, ""), card("ALPHA-2", board.StateTodo, "queued")},
			run:   stopped(""), vanish: "ALPHA-2",
			wantEntry: "", launches: []string{"ALPHA-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()

			for _, c := range tt.cards {
				e.cards.add(c)
			}

			p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
			p.Run = tt.run
			e.pbs.add(p)

			if tt.vanish != "" {
				e.pbs.mu.Lock()
				e.pbs.afterGet = func() {
					e.cards.mu.Lock()
					defer e.cards.mu.Unlock()

					delete(e.cards.cards, cardKey("alpha", tt.vanish))
				}
				e.pbs.mu.Unlock()
			}

			d, err := e.runner.Play(ctx, "rollout", "human:alice")
			require.NoError(t, err)
			assert.Equal(t, tt.wantEntry, d.Run.Entry)
			assert.Empty(t, d.Run.Reason)

			require.False(t, e.runner.pass(ctx, "rollout"))

			launched := make([]string, 0, e.launcher.count())
			for i := range e.launcher.count() {
				launched = append(launched, e.launcher.call(i).card)
			}

			assert.Equal(t, append([]string{}, tt.launches...), launched)
		})
	}
}

// TestPass_HoldsOnALiveWorkerOffTheRunEntry: a card entry the run does not
// point at has a live worker (a hand run that raced Play, one on another
// instance, a card the walk passed before a reorder). The run holds on it
// instead of starting the frontier beside it, and points at it for Stop.
func TestPass_HoldsOnALiveWorkerOffTheRunEntry(t *testing.T) {
	live := []struct {
		name string
		make func(e *env, c *board.Card)
	}{
		{"queued", func(_ *env, c *board.Card) { c.WorkerStatus = "queued" }},
		{"running", func(_ *env, c *board.Card) { c.WorkerStatus = "running" }},
		{"claimed elsewhere", func(e *env, c *board.Card) {
			c.AssignedAgent = "agent-1"
			e.cards.elsewhere[cardKey("alpha", "ALPHA-2")] = true
		}},
	}

	for _, l := range live {
		t.Run(l.name, func(t *testing.T) {
			e := newEnv(t)
			e.cards.add(todoCard("alpha", "ALPHA-1"))

			c := todoCard("alpha", "ALPHA-2")
			c.State = board.StateInProgress
			l.make(e, c)
			e.cards.add(c)

			p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
			e.activeRun(p, "")
			e.pbs.add(p)

			require.False(t, e.runner.pass(context.Background(), "rollout"))
			assert.Equal(t, 0, e.launcher.count(), "no second worker")

			run := e.pbs.lastRun()
			assert.Equal(t, board.RunStatusRunning, run.Status)
			assert.Equal(t, "e2", run.Entry, "the run points at the live worker")
		})
	}
}

// TestPass_RolledBackEntryBehindTheRunIsHeldAndStopKillsBoth: a completion
// whose commit failed was rolled back after the walk moved past it, leaving
// an entry behind the run's card with its own worker alive. Stop must kill
// both, and once the run's card ends the run moves to the rolled-back card.
func TestPass_RolledBackEntryBehindTheRunIsHeldAndStopKillsBoth(t *testing.T) {
	setup := func(t *testing.T) *env {
		t.Helper()

		e := newEnv(t)

		done := todoCard("alpha", "ALPHA-1")
		done.State = board.StateDone
		e.cards.add(done)
		e.cards.add(liveCard()) // ALPHA-2: rolled back to in_progress, worker running

		current := todoCard("alpha", "ALPHA-3")
		current.State, current.WorkerStatus = board.StateInProgress, "queued"
		e.cards.add(current)

		p := runnablePlaybook("rollout",
			cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"), cardEntry("e3", "alpha", "ALPHA-3"))
		e.activeRun(p, "e3")
		e.pbs.add(p)

		return e
	}

	t.Run("the run holds, then moves to the rolled-back card", func(t *testing.T) {
		e := setup(t)

		require.False(t, e.runner.pass(context.Background(), "rollout"))
		assert.Equal(t, 0, e.launcher.count())
		assert.Equal(t, 0, e.pbs.runWrites(), "already running on e3")

		e.cards.set("alpha", "ALPHA-3", func(c *board.Card) { c.State = board.StateDone })

		require.False(t, e.runner.pass(context.Background(), "rollout"))
		assert.Equal(t, 0, e.launcher.count())

		run := e.pbs.lastRun()
		assert.Equal(t, board.RunStatusRunning, run.Status)
		assert.Equal(t, "e2", run.Entry)
	})

	t.Run("stop kills both workers", func(t *testing.T) {
		e := setup(t)

		_, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"ALPHA-2", "ALPHA-3"}, e.stopper.called())
	})
}

// TestStop_KillsEveryLiveEntryWorker: Stop kills every queued or running
// worker this instance owns on a card that is not terminal, wherever the run
// points, and keeps going past a failed kill.
func TestStop_KillsEveryLiveEntryWorker(t *testing.T) {
	setup := func(t *testing.T) *env {
		t.Helper()

		e := newEnv(t)
		card := func(id, state, worker string) *board.Card {
			c := todoCard("alpha", id)
			c.State, c.WorkerStatus = state, worker

			return c
		}

		e.cards.add(card("ALPHA-1", board.StateInProgress, "running"))
		e.cards.add(card("ALPHA-2", board.StateInProgress, "queued")) // the run's entry
		e.cards.add(card("ALPHA-3", board.StateInProgress, "running"))
		e.cards.elsewhere[cardKey("alpha", "ALPHA-3")] = true    // another instance's worker
		e.cards.add(card("ALPHA-4", board.StateDone, "running")) // finishing up
		e.cards.add(card("ALPHA-5", board.StateTodo, ""))

		p := runnablePlaybook("rollout",
			cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"), cardEntry("e3", "alpha", "ALPHA-3"),
			cardEntry("e4", "alpha", "ALPHA-4"), cardEntry("e5", "alpha", "ALPHA-5"), cardEntry("e6", "alpha", "GONE-1"),
			manualEntry("e7", "deploy"))
		e.activeRun(p, "e2")
		e.pbs.add(p)

		return e
	}

	t.Run("every local live worker of a card that is not terminal", func(t *testing.T) {
		e := setup(t)

		d, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		require.NoError(t, err)
		assert.Equal(t, board.RunStatusStopped, d.Run.Status)
		assert.ElementsMatch(t, []string{"ALPHA-1", "ALPHA-2"}, e.stopper.called())
	})

	t.Run("a failed kill does not spare the others", func(t *testing.T) {
		e := setup(t)
		e.stopper.err = errors.New("kill webhook failed")

		d, err := e.runner.Stop(context.Background(), "rollout", "human:alice")
		require.ErrorIs(t, err, ErrStopWorker)
		require.NotNil(t, d)
		assert.Equal(t, board.RunStatusStopped, d.Run.Status)
		assert.ElementsMatch(t, []string{"ALPHA-1", "ALPHA-2"}, e.stopper.called())
	})
}

// TestPass_LiveWorkerElsewhereKeepsATriggeredRunEntry: a run that already
// triggered an entry keeps pointing at it while another entry's worker holds
// the run, so the walk still waits for a human on it afterwards instead of
// launching it again.
func TestPass_LiveWorkerElsewhereKeepsATriggeredRunEntry(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(e *env, p *board.Playbook, c1 *board.Card)
		wantReason string
	}{
		{
			name: "the triggered entry failed",
			setup: func(e *env, p *board.Playbook, c1 *board.Card) {
				c1.State, c1.WorkerStatus = board.StateInProgress, "failed"
			},
			wantReason: "ALPHA-1",
		},
		{
			name: "the launch of the triggered entry failed",
			setup: func(e *env, p *board.Playbook, c1 *board.Card) {
				p.Run.Status, p.Run.Reason = board.RunStatusWaiting, "launch ALPHA-1 failed"
			},
			wantReason: "launch ALPHA-1 failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)

			c1 := todoCard("alpha", "ALPHA-1")
			c2 := todoCard("alpha", "ALPHA-2")
			c2.State, c2.WorkerStatus = board.StateInProgress, "running"

			p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
			e.activeRun(p, "e1")
			tc.setup(e, p, c1)
			e.cards.add(c1)
			e.cards.add(c2)
			e.pbs.add(p)

			require.False(t, e.runner.pass(context.Background(), "rollout"))
			assert.Equal(t, 0, e.launcher.count())
			assert.Equal(t, 0, e.pbs.runWrites(), "the run is left as it was")

			e.cards.set("alpha", "ALPHA-2", func(c *board.Card) { c.State = board.StateDone })

			require.False(t, e.runner.pass(context.Background(), "rollout"))
			assert.Equal(t, 0, e.launcher.count(), "no retry without a human")

			d, err := e.pbs.Get(context.Background(), "rollout")
			require.NoError(t, err)
			assert.Equal(t, board.RunStatusWaiting, d.Run.Status)
			assert.Equal(t, "e1", d.Run.Entry)
			assert.Contains(t, d.Run.Reason, tc.wantReason)
		})
	}
}

// TestPlay_LiveEntryRemovedDuringPlay: Play read the playbook while e2's card
// had a live worker, then e2 was removed (allowed while stopped). A run
// decided from that read names a removed entry and fails validation; decided
// inside the write it sees the stored entries.
func TestPlay_LiveEntryRemovedDuringPlay(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	e.cards.add(todoCard("alpha", "ALPHA-1"))
	e.cards.add(liveCard())

	p := runnablePlaybook("rollout", cardEntry("e1", "alpha", "ALPHA-1"), cardEntry("e2", "alpha", "ALPHA-2"))
	p.Run = &board.PlaybookRun{Status: board.RunStatusStopped, Entry: "e2", Reason: "stopped by human:alice"}
	e.pbs.add(p)

	// The removal lands right after Play's read, the way RemoveEntry does it.
	e.pbs.mu.Lock()
	e.pbs.afterGet = func() {
		e.pbs.mu.Lock()
		defer e.pbs.mu.Unlock()

		stored := e.pbs.pbs["rollout"]
		stored.Entries = stored.Entries[:1]
		stored.Run.Entry, stored.Run.Reason = "", ""
	}
	e.pbs.mu.Unlock()

	d, err := e.runner.Play(ctx, "rollout", "human:alice")
	require.NoError(t, err)
	assert.Equal(t, board.RunStatusRunning, d.Run.Status)
	assert.Empty(t, d.Run.Entry, "no live entry is left to resume on")
}
