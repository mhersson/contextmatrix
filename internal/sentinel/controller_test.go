package sentinel

import (
	"context"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

var epoch = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

type env struct {
	store  *fakeStore
	clk    *clock.FakeClock
	bus    *events.Bus
	ctl    *Controller
	cancel context.CancelFunc
}

func newEnv(t *testing.T, store *fakeStore, mode llmchannel.Mode) *env {
	t.Helper()

	e := &env{store: store, clk: clock.Fake(epoch), bus: events.NewBus()}
	e.ctl = New(Config{
		Store: store, Channel: fakeChannel{config.BackendNameAgent: mode}, Bus: e.bus, Clock: e.clk, Tick: 30 * time.Second,
	})

	return e
}

func (e *env) start(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.ctl.Start(ctx)

	t.Cleanup(func() { e.stop(t) })
}

func (e *env) stop(t *testing.T) {
	t.Helper()

	e.cancel()
	require.NoError(t, e.ctl.Shutdown(context.Background()))
}

// waitPasses waits until n passes have started; passes run one at a time, so
// every earlier pass has finished.
func (e *env) waitPasses(t *testing.T, n int) {
	t.Helper()

	require.Eventually(t, func() bool { return e.store.passes() == n }, time.Second, time.Millisecond)
}

func (e *env) waitState(t *testing.T, state string) {
	t.Helper()

	require.Eventually(t, func() bool { return e.ctl.Status().State == state }, time.Second, time.Millisecond)
}

func TestControllerPassesOnStartTickEventAndNudge(t *testing.T) {
	e := newEnv(t, &fakeStore{}, llmchannel.ModeEndpoint)
	e.start(t)
	e.waitPasses(t, 1)

	e.clk.Advance(30 * time.Second)
	e.waitPasses(t, 2)

	e.bus.Publish(events.Event{Type: events.CardStalled, Project: "alpha", CardID: "ALPHA-1"})
	e.waitPasses(t, 3)

	e.ctl.Nudge()
	e.waitPasses(t, 4)
}

func TestWakes(t *testing.T) {
	for _, typ := range []events.EventType{
		events.WorkerTriggered, events.WorkerStarted, events.WorkerFailed, events.WorkerKilled, events.WorkerParked,
		events.CardStalled, events.CardUpdated, events.CardStateChanged, events.LLMChannelChanged, events.PlaybookUpdated,
	} {
		assert.True(t, wakes(typ), typ)
	}

	for _, typ := range []events.EventType{
		events.CardCreated, events.CardLogAdded, events.CardClaimed, events.PlaybookCreated, events.SyncCompleted, events.SentinelChanged,
	} {
		assert.False(t, wakes(typ), typ)
	}
}

func TestControllerSurvivesPanickingPass(t *testing.T) {
	store := &fakeStore{settings: enabledSettings(), panicNext: true}
	e := newEnv(t, store, llmchannel.ModeEndpoint)
	e.start(t)
	e.waitPasses(t, 1)

	e.clk.Advance(30 * time.Second)
	e.waitPasses(t, 2)
	e.waitState(t, StateWatching)
}

func TestControllerPublishesOnlyOnStatusChange(t *testing.T) {
	store := &fakeStore{settings: DefaultSettings()}
	e := newEnv(t, store, llmchannel.ModeEndpoint)

	ch, unsubscribe := e.bus.Subscribe()
	defer unsubscribe()

	changes := 0
	published := func() int {
		for {
			select {
			case ev := <-ch:
				if ev.Type == events.SentinelChanged {
					assert.Empty(t, ev.Project)

					changes++
				}
			default:
				return changes
			}
		}
	}

	e.start(t)
	e.waitPasses(t, 1)
	e.ctl.Nudge()
	e.waitPasses(t, 2)
	e.ctl.Nudge()
	e.waitPasses(t, 3)
	assert.Equal(t, 1, published(), "the first pass publishes, unchanged passes do not")

	store.set(enabledSettings())
	e.ctl.Nudge()
	e.waitPasses(t, 4)
	e.ctl.Nudge()
	e.waitPasses(t, 5)
	assert.Equal(t, 2, published(), "a changed status publishes once")
}

func TestControllerPassReadsAgentChannelAndWindows(t *testing.T) {
	windows := []protocol.SubscriptionUsage{{Window: "five_hour", Utilization: 0.9, Status: "allowed", ResetsAt: epoch.Add(time.Hour)}}
	e := newEnv(t, &fakeStore{settings: enabledSettings(), windows: windows}, llmchannel.ModeClaudeCode)
	e.start(t)
	e.waitState(t, StatePaused)

	st := e.ctl.Status()
	assert.Equal(t, "waiting for subscription headroom", st.Reason)
	assert.Equal(t, "claude_code", st.Mode)
	assert.Equal(t, windows, st.Windows)
}

func TestControllerReportsStoppedWhenPassesStop(t *testing.T) {
	e := newEnv(t, &fakeStore{settings: enabledSettings()}, llmchannel.ModeEndpoint)
	e.start(t)
	e.waitState(t, StateWatching)
	e.stop(t)

	e.clk.Advance(91 * time.Second)

	st := e.ctl.Status()
	assert.Equal(t, StateNotRunning, st.State)
	assert.Equal(t, "controller stopped", st.Reason)
}
