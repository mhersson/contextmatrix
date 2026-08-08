package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const awaitTestProject = "test-project"

// awaitProjectConfig is testProjectConfig plus a todo→not_planned edge so the
// tests can park a subtask in the second terminal state.
func awaitProjectConfig() *board.ProjectConfig {
	cfg := testProjectConfig()
	cfg.Transitions["todo"] = []string{"in_progress", "not_planned"}

	return cfg
}

// awaitEnv is a full MCP surface (in-memory client session) wired to a service
// running on a fake clock, so blocking waits can be driven deterministically.
type awaitEnv struct {
	session *mcp.ClientSession
	svc     *service.CardService
	bus     *events.Bus
	clk     *clock.FakeClock
	start   time.Time
}

func setupAwaitMCP(t *testing.T, awaitMax time.Duration) *awaitEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, awaitTestProject)
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, awaitProjectConfig()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fake := clock.Fake(start)
	// The service adopts the lock manager's clock, so the fake must go in here.
	lockMgr := lock.NewManagerWithClock(store, 30*time.Minute, fake)
	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	server := NewServer(ServerConfig{
		Service:           svc,
		WorkflowSkillsDir: filepath.Join(tmpDir, "workflow-skills"),
		Bus:               bus,
		AwaitMax:          awaitMax,
	})

	ctx, cancel := context.WithCancel(context.Background())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "await-test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	env := &awaitEnv{session: session, svc: svc, bus: bus, clk: fake, start: start}

	t.Cleanup(func() {
		// A cancelled CallTool returns to the client while the handler is still
		// unwinding server-side, so wait for the handler itself before letting
		// t.TempDir delete the boards directory out from under an in-flight
		// commit. The bus subscription is the observable: the wait drops it on
		// the way out.
		assert.Eventually(t, func() bool {
			return bus.SubscriberCount() == 0
		}, 5*time.Second, time.Millisecond, "await_subtasks handler still running at teardown")

		// Cancel first: Close waits for in-flight calls, and a regression that
		// leaves a wait blocked would hang the suite instead of failing it.
		cancel()

		_ = session.Close()
	})

	return env
}

// newParent creates a parent card and n subtasks under it, returning the parent
// ID and the subtask IDs in creation order.
func newParent(t *testing.T, env *awaitEnv, n int) (string, []string) {
	t.Helper()

	ctx := context.Background()

	parent, err := env.svc.CreateCard(ctx, awaitTestProject, service.CreateCardInput{
		Title: "Parent", Type: "feature", Priority: "high",
	})
	require.NoError(t, err)

	subs := make([]string, 0, n)

	for i := range n {
		sub, err := env.svc.CreateCard(ctx, awaitTestProject, service.CreateCardInput{
			Title:    "Subtask " + string(rune('A'+i)),
			Priority: "medium",
			Parent:   parent.ID,
		})
		require.NoError(t, err)

		subs = append(subs, sub.ID)
	}

	return parent.ID, subs
}

// driveTo walks a card through the given states via the real patch path, so
// every hop publishes the events production publishes.
func driveTo(t *testing.T, env *awaitEnv, cardID string, states ...string) {
	t.Helper()

	for _, state := range states {
		_, err := env.svc.PatchCard(context.Background(), awaitTestProject, cardID, service.PatchCardInput{
			State: &state,
		})
		require.NoError(t, err, "drive %s to %s", cardID, state)
	}
}

type awaitCall struct {
	res *mcp.CallToolResult
	err error
}

// startAwait calls await_subtasks on a goroutine so the test can keep driving
// the board while the handler blocks. The returned cancel is also registered as
// a cleanup: ClientSession.Close waits for in-flight calls, so a regression that
// leaves a wait blocked must be unwound or it hangs the whole suite instead of
// failing this one test.
func startAwait(t *testing.T, env *awaitEnv, args map[string]any) (<-chan awaitCall, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ch := make(chan awaitCall, 1)

	go func() {
		res, err := env.session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "await_subtasks",
			Arguments: args,
		})
		ch <- awaitCall{res: res, err: err}
	}()

	return ch, cancel
}

// waitForBlocked blocks until the handler has subscribed to the bus and armed
// its clock waiters (two tickers plus the deadline timer). Advancing the fake
// clock before that point would race the registration and produce a deadline
// measured from the wrong instant.
func waitForBlocked(t *testing.T, env *awaitEnv) {
	t.Helper()

	require.Eventually(t, func() bool {
		return env.bus.SubscriberCount() >= 1 &&
			env.clk.ActiveTickers() >= 2 &&
			env.clk.PendingTimers() >= 1
	}, 5*time.Second, time.Millisecond, "await_subtasks never reached its blocking select")
}

// receiveAwait waits for the call to come back and decodes the tool output.
func receiveAwait(t *testing.T, ch <-chan awaitCall) awaitSubtasksOutput {
	t.Helper()

	select {
	case got := <-ch:
		require.NoError(t, got.err)
		require.NotNil(t, got.res)
		require.False(t, got.res.IsError, "tool returned an error result")

		var out awaitSubtasksOutput

		require.NotEmpty(t, got.res.Content)
		text, ok := got.res.Content[0].(*mcp.TextContent)
		require.True(t, ok, "expected TextContent, got %T", got.res.Content[0])
		require.NoError(t, json.Unmarshal([]byte(text.Text), &out))

		return out
	case <-time.After(10 * time.Second):
		t.Fatal("await_subtasks did not return")

		return awaitSubtasksOutput{}
	}
}

// TestAwaitSubtasks_AllTerminalReturnsImmediately pins the fast path: when
// every subtask is already terminal the call must not block at all. done and
// not_planned both count as terminal; stalled deliberately does not.
func TestAwaitSubtasks_AllTerminalReturnsImmediately(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	parent, subs := newParent(t, env, 2)
	driveTo(t, env, subs[0], "in_progress", "review", "done")
	driveTo(t, env, subs[1], "not_planned")

	ch, _ := startAwait(t, env, map[string]any{
		"project":         awaitTestProject,
		"parent_id":       parent,
		"timeout_seconds": 300,
	})
	out := receiveAwait(t, ch)

	assert.Equal(t, parent, out.ParentID)
	assert.True(t, out.Completed)
	assert.False(t, out.TimedOut)
	assert.Empty(t, out.Stalled)
	assert.Equal(t, 0, out.WaitedSeconds)
	assert.Equal(t, map[string]int{"done": 1, "not_planned": 1}, out.Counts)
}

// TestAwaitSubtasks_NoSubtasksReturnsImmediately covers the degenerate case: a
// parent with no subtasks is vacuously complete and must never block.
func TestAwaitSubtasks_NoSubtasksReturnsImmediately(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	parent, _ := newParent(t, env, 0)

	ch, _ := startAwait(t, env, map[string]any{
		"project":   awaitTestProject,
		"parent_id": parent,
	})
	out := receiveAwait(t, ch)

	assert.True(t, out.Completed)
	assert.Empty(t, out.Counts)
	assert.Equal(t, 0, out.WaitedSeconds)
}

// TestAwaitSubtasks_UnknownParentErrors guards against the silent-lie failure
// mode: a typo'd parent ID lists zero subtasks, which would otherwise look
// exactly like "all subtasks finished".
func TestAwaitSubtasks_UnknownParentErrors(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	res, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "await_subtasks",
		Arguments: map[string]any{
			"project":   awaitTestProject,
			"parent_id": "TEST-404",
		},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "unknown parent must surface as a tool error")
}

// TestAwaitSubtasks_ReturnsWhenSubtaskFinishesMidWait is the core promise: a
// transition arriving on the bus wakes the wait immediately, with no clock
// advance and therefore no reliance on the recheck ticker.
func TestAwaitSubtasks_ReturnsWhenSubtaskFinishesMidWait(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	parent, subs := newParent(t, env, 1)
	driveTo(t, env, subs[0], "in_progress")

	ch, _ := startAwait(t, env, map[string]any{
		"project":         awaitTestProject,
		"parent_id":       parent,
		"timeout_seconds": 300,
	})
	waitForBlocked(t, env)

	driveTo(t, env, subs[0], "review", "done")

	out := receiveAwait(t, ch)
	assert.True(t, out.Completed)
	assert.False(t, out.TimedOut)
	assert.Equal(t, map[string]int{"done": 1}, out.Counts)
	assert.Equal(t, 0, out.WaitedSeconds, "no clock advance, so no measurable wait")
}

// TestAwaitSubtasks_StalledSubtaskReturnsEarly pins the respawn hook: one
// stalled subtask ends the wait even though the others are still running, so
// the orchestrator can act instead of sitting out the full window.
func TestAwaitSubtasks_StalledSubtaskReturnsEarly(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	parent, subs := newParent(t, env, 2)
	driveTo(t, env, subs[0], "in_progress")
	driveTo(t, env, subs[1], "in_progress")

	ch, _ := startAwait(t, env, map[string]any{
		"project":         awaitTestProject,
		"parent_id":       parent,
		"timeout_seconds": 300,
	})
	waitForBlocked(t, env)

	driveTo(t, env, subs[1], "stalled")

	out := receiveAwait(t, ch)
	assert.False(t, out.Completed)
	assert.False(t, out.TimedOut)
	assert.Equal(t, []string{subs[1]}, out.Stalled)
	assert.Equal(t, map[string]int{"in_progress": 1, "stalled": 1}, out.Counts)
}

// TestAwaitSubtasks_TimesOut pins the bounded window: the effective wait is
// min(timeout_seconds, await_max), and a timeout still carries the counts so
// the caller can decide whether to re-call.
func TestAwaitSubtasks_TimesOut(t *testing.T) {
	tests := []struct {
		name           string
		awaitMax       time.Duration
		timeoutSeconds int
		agentID        string
		wantWaited     int
	}{
		{
			name:           "caller timeout below the cap",
			awaitMax:       8 * time.Minute,
			timeoutSeconds: 120,
			wantWaited:     120,
		},
		{
			// Also exercises the heartbeat tick against an unclaimed parent:
			// the 4m refresh fires twice inside this window and must be
			// swallowed rather than failing the wait.
			name:           "caller timeout capped by await_max",
			awaitMax:       8 * time.Minute,
			timeoutSeconds: 3600,
			agentID:        "agent-without-claim",
			wantWaited:     480,
		},
		{
			name:       "omitted timeout defaults to the cap",
			awaitMax:   90 * time.Second,
			wantWaited: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupAwaitMCP(t, tt.awaitMax)

			parent, subs := newParent(t, env, 1)
			driveTo(t, env, subs[0], "in_progress")

			args := map[string]any{"project": awaitTestProject, "parent_id": parent}
			if tt.timeoutSeconds > 0 {
				args["timeout_seconds"] = tt.timeoutSeconds
			}

			if tt.agentID != "" {
				args["agent_id"] = tt.agentID
			}

			ch, _ := startAwait(t, env, args)
			waitForBlocked(t, env)

			env.clk.Advance(time.Duration(tt.wantWaited) * time.Second)

			out := receiveAwait(t, ch)
			assert.True(t, out.TimedOut)
			assert.False(t, out.Completed)
			assert.Equal(t, tt.wantWaited, out.WaitedSeconds)
			assert.Equal(t, map[string]int{"in_progress": 1}, out.Counts)
		})
	}
}

// TestAwaitSubtasks_RefreshesCallerClaim pins the liveness guarantee that makes
// long waits safe: an orchestrator blocked here must not stall its own parent
// card while it waits.
func TestAwaitSubtasks_RefreshesCallerClaim(t *testing.T) {
	env := setupAwaitMCP(t, 30*time.Minute)

	ctx := context.Background()
	parent, subs := newParent(t, env, 1)
	driveTo(t, env, subs[0], "in_progress")

	claimed, err := env.svc.ClaimCard(ctx, awaitTestProject, parent, "agent-orchestrator")
	require.NoError(t, err)
	require.NotNil(t, claimed.LastHeartbeat)

	claimedAt := *claimed.LastHeartbeat

	ch, cancel := startAwait(t, env, map[string]any{
		"project":         awaitTestProject,
		"parent_id":       parent,
		"agent_id":        "agent-orchestrator",
		"timeout_seconds": 1800,
	})
	waitForBlocked(t, env)

	env.clk.Advance(4 * time.Minute)

	require.Eventually(t, func() bool {
		card, err := env.svc.GetCard(ctx, awaitTestProject, parent)
		if err != nil || card.LastHeartbeat == nil {
			return false
		}

		return card.LastHeartbeat.After(claimedAt)
	}, 5*time.Second, time.Millisecond, "claim heartbeat was not refreshed during the wait")

	card, err := env.svc.GetCard(ctx, awaitTestProject, parent)
	require.NoError(t, err)
	assert.Equal(t, env.start.Add(4*time.Minute), card.LastHeartbeat.UTC())

	cancel()
	<-ch
}

// TestAwaitSubtasks_ContextCancelledReturnsPromptly pins teardown: when the
// caller goes away the handler unwinds instead of holding a bus subscription
// (and a goroutine) until the deadline.
func TestAwaitSubtasks_ContextCancelledReturnsPromptly(t *testing.T) {
	env := setupAwaitMCP(t, 8*time.Minute)

	parent, subs := newParent(t, env, 1)
	driveTo(t, env, subs[0], "in_progress")

	ch, cancel := startAwait(t, env, map[string]any{
		"project":         awaitTestProject,
		"parent_id":       parent,
		"timeout_seconds": 300,
	})
	waitForBlocked(t, env)

	cancel()

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("await_subtasks did not unwind after the caller cancelled")
	}

	require.Eventually(t, func() bool {
		return env.bus.SubscriberCount() == 0
	}, 5*time.Second, time.Millisecond, "handler leaked its bus subscription")
}
