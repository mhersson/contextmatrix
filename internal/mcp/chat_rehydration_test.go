package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/chat/sqlite"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
)

// chatTestDeps holds the store so tests can manipulate rehydration_active
// independently of the manager.
type chatTestDeps struct {
	store chat.Store
}

// setRehydrationActive flips the rehydration flag directly via the store,
// bypassing the manager's in-memory cache to simulate a just-opened session.
func (d *chatTestDeps) setRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	return d.store.SetRehydrationActive(ctx, sessionID, active, time.Now().UTC())
}

// newTestChatManager creates a chat.Manager backed by a real SQLite store and
// a no-op stub runner, suitable for unit-testing tool handlers.
func newTestChatManager(t *testing.T) (*chat.Manager, *chatTestDeps) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Backend: &chatStubRunner{},
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	return mgr, &chatTestDeps{store: store}
}

// chatStubRunner is the minimal Backend stub needed for chat manager tests
// in the mcp package. It satisfies the chat.Backend interface without any
// real behaviour — we never actually start containers in these tests.
type chatStubRunner struct{}

func (r *chatStubRunner) StartChat(_ context.Context, _ chat.StartChatOpts) (string, error) {
	return "stub-container", nil
}

func (r *chatStubRunner) EndChat(_ context.Context, _ string) error { return nil }

func (r *chatStubRunner) SendChatMessage(_ context.Context, _, _, _ string) error { return nil }

func (r *chatStubRunner) StreamLogs(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
	<-ctx.Done()

	return ctx.Err()
}

// --- Tests ---

func TestChatRehydrationCompleteTool_UnknownSession(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestChatManager(t)
	tool := buildChatRehydrationCompleteTool(mgr)
	_, _, err := tool(context.Background(), nil, chatRehydrationCompleteInput{
		SessionID: "01UNKNOWN",
		Summary:   "x",
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "session not found")
}

func TestChatRehydrationCompleteTool_HappyPath(t *testing.T) {
	t.Parallel()
	mgr, deps := newTestChatManager(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Flip the flag on via the store directly, simulating a cold reopen.
	require.NoError(t, deps.setRehydrationActive(ctx, sess.ID, true))

	tool := buildChatRehydrationCompleteTool(mgr)
	_, out, err := tool(ctx, nil, chatRehydrationCompleteInput{
		SessionID: sess.ID,
		Summary:   "restored.",
	})
	require.NoError(t, err)
	require.True(t, out.OK)

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.False(t, got.RehydrationActive, "flag must flip off")
}

func TestChatRehydrationCompleteTool_AlreadyInactive(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestChatManager(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// rehydration_active is false by default — call should be a no-op success.
	tool := buildChatRehydrationCompleteTool(mgr)
	_, out, err := tool(ctx, nil, chatRehydrationCompleteInput{
		SessionID: sess.ID,
		Summary:   "no-op",
	})
	require.NoError(t, err)
	require.True(t, out.OK)
}

func TestChatRehydrationCompleteTool_CrossSessionRejected(t *testing.T) {
	t.Parallel()
	mgr, deps := newTestChatManager(t)
	ctx := context.Background()
	// Two sessions; both rehydration-active.
	a, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	b, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	require.NoError(t, deps.setRehydrationActive(ctx, a.ID, true))
	require.NoError(t, deps.setRehydrationActive(ctx, b.ID, true))
	// Agent in container A calls the tool with B's session_id.
	ctxA := mcpcontext.WithChatSession(ctx, a.ID)
	tool := buildChatRehydrationCompleteTool(mgr)
	_, _, err = tool(ctxA, nil, chatRehydrationCompleteInput{
		SessionID: b.ID,
		Summary:   "I am evil",
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "session mismatch")
	// B's rehydration_active must still be true.
	got, _ := mgr.GetSession(ctx, b.ID)
	require.True(t, got.RehydrationActive)
}

func TestChatRehydrationCompleteTool_SummaryTooLarge(t *testing.T) {
	t.Parallel()
	mgr, deps := newTestChatManager(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)
	require.NoError(t, deps.setRehydrationActive(ctx, sess.ID, true))

	oversize := strings.Repeat("x", maxSummaryBytes+1)
	tool := buildChatRehydrationCompleteTool(mgr)
	_, _, err = tool(ctx, nil, chatRehydrationCompleteInput{
		SessionID: sess.ID,
		Summary:   oversize,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestSanitizeLogField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"normal", "normal"},
		{"with\nnewline", "withnewline"},
		{"ansi\x1b[31mred\x1b[0m", "ansi[31mred[0m"},
		{"tabs\there", "tabshere"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			require.Equal(t, tc.want, sanitizeLogField(tc.in))
		})
	}
}
