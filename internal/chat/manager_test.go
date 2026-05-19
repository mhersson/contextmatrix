package chat_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/chat/sqlite"
	"github.com/mhersson/contextmatrix/internal/clock"
)

// stubRunner is a fake chat.Runner used by manager tests. Counters are atomic
// because Manager.startConsumer spawns a goroutine that calls StreamLogs
// independently of the test goroutine — plain ints would race under -race.
type stubRunner struct {
	startCalls    atomic.Int64
	endCalls      atomic.Int64
	sendCalls     atomic.Int64
	streamCalls   atomic.Int64
	activeStreams atomic.Int32
	startErr      error
	sendErr       error
	streamLogsFn  func(ctx context.Context, sessionID string, onEntry func(chat.LogEntry)) error
	// sendErrSeq, when non-nil, returns the i-th sendErr (i = call index,
	// starting at 0). Pads with nil when len(seq) is less than the call
	// count. Takes precedence over sendErr when set.
	sendErrSeq []error
	mu         sync.Mutex
	lastOpts   chat.StartChatOpts
	// sendArgs captures every (content, messageID) pair passed to
	// SendChatMessage in call order. Tests assert ordering on clear+primer.
	sendArgs []sendArg
}

type sendArg struct{ Content, MessageID string }

func (s *stubRunner) StartChat(ctx context.Context, opts chat.StartChatOpts) (string, error) {
	s.startCalls.Add(1)
	s.mu.Lock()
	s.lastOpts = opts
	s.mu.Unlock()

	if s.startErr != nil {
		return "", s.startErr
	}

	return "container-abc", nil
}

func (s *stubRunner) EndChat(ctx context.Context, sessionID string) error {
	s.endCalls.Add(1)

	return nil
}

func (s *stubRunner) SendChatMessage(ctx context.Context, sessionID, content, messageID string) error {
	idx := s.sendCalls.Add(1) - 1

	s.mu.Lock()
	s.sendArgs = append(s.sendArgs, sendArg{Content: content, MessageID: messageID})
	seq := s.sendErrSeq
	s.mu.Unlock()

	if idx >= 0 && int(idx) < len(seq) {
		return seq[idx]
	}

	return s.sendErr
}

func (s *stubRunner) StreamLogs(ctx context.Context, sessionID string, onEntry func(chat.LogEntry)) error {
	s.streamCalls.Add(1)

	s.activeStreams.Add(1)
	defer s.activeStreams.Add(-1)

	if s.streamLogsFn != nil {
		return s.streamLogsFn(ctx, sessionID, onEntry)
	}

	<-ctx.Done()

	return ctx.Err()
}

func newManagerWithStubs(t *testing.T) (*chat.Manager, *stubRunner, chat.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	return mgr, runner, store
}

func TestManager_CreateSession_RowExists(t *testing.T) {
	mgr, _, _ := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{
		Title:     "runner-auth",
		Project:   "contextmatrix-runner",
		CreatedBy: "human:web-abcd1234",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, chat.StatusCold, sess.Status, "newly-created sessions are cold")
	assert.Equal(t, "runner-auth", sess.Title)
}

func TestManager_OpenSession_ColdStartsContainer(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		ResolveRepoURL: func(_ context.Context, _ string) (string, error) {
			return "https://example.com/alpha.git", nil
		},
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", Project: "alpha", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	got, err := mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)
	assert.Equal(t, "container-abc", got.ContainerID)
	assert.Equal(t, int64(1), runner.startCalls.Load(), "container started exactly once")
	assert.Equal(t, []string{"alpha"}, got.Workspace, "project recorded in workspace list")
}

func TestManager_OpenSession_WarmIdleReattaches(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	sess.Status = chat.StatusWarmIdle
	sess.ContainerID = "container-existing"
	require.NoError(t, store.UpdateSession(ctx, sess))

	got, err := mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)
	assert.Equal(t, "container-existing", got.ContainerID)
	assert.Equal(t, int64(0), runner.startCalls.Load(), "warm-idle reattach must not start a new container")
}

func TestManager_OpenSession_AlreadyActive_NoOp(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), runner.startCalls.Load())
}

// TestManager_OpenSession_AlreadyActive_StartsConsumer ensures the active
// branch reattaches the runner-log consumer. CM-restart strands the in-
// memory consumer goroutine while the session row stays active; without
// this, an /open call on an active session leaves the bridge missing.
func TestManager_OpenSession_AlreadyActive_StartsConsumer(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runner.streamCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond, "consumer must stream logs from runner")
	assert.Equal(t, int64(0), runner.startCalls.Load(), "no new container")
}

// TestManager_Reattach_Active starts a runner-log consumer for an already-
// active session whose in-memory consumer was lost (CM restart). The DB
// row is left as-is.
func TestManager_Reattach_Active(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runner.streamCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status, "Reattach must not change status")
	assert.Equal(t, int64(0), runner.startCalls.Load())
}

// TestManager_Reattach_WarmIdle starts a consumer for a warm-idle session
// and refreshes LastActive so the idle reaper doesn't end it. Status is
// intentionally left at warm-idle — Reattach is infrastructure-only;
// lifecycle promotion (warm-idle → active) is handled separately by
// MarkActive, called from the OnSubscribe callback.
func TestManager_Reattach_WarmIdle(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusWarmIdle
	sess.ContainerID = "container-warm"
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	sess.LastActive = old
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runner.streamCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusWarmIdle, got.Status)
	assert.True(t, got.LastActive.After(old), "LastActive must be refreshed")
	assert.Equal(t, int64(0), runner.startCalls.Load())
}

// TestManager_Reattach_Cold is a no-op — cold sessions have no container
// to reattach to.
func TestManager_Reattach_Cold(t *testing.T) {
	mgr, runner, _ := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)

	// Give any (incorrect) goroutine spawn time to call StreamLogs.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(0), runner.streamCalls.Load())
	assert.Equal(t, int64(0), runner.startCalls.Load())
}

// TestManager_Reattach_Idempotent guarantees concurrent or repeated calls
// don't spawn extra consumer goroutines.
func TestManager_Reattach_Idempotent(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)
	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)
	_, err = mgr.Reattach(ctx, sess.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runner.streamCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
	// Give any duplicate goroutine spawn a chance to (wrongly) increment.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), runner.streamCalls.Load(), "exactly one consumer")
}

func TestManager_EndSession_ActiveTransitionsToCold(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, store.UpdateSession(ctx, sess))

	require.NoError(t, mgr.EndSession(ctx, sess.ID))
	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusCold, got.Status)
	assert.Empty(t, got.ContainerID)
	assert.Equal(t, int64(1), runner.endCalls.Load())
}

func TestManager_EndSession_AlreadyCold_NoOp(t *testing.T) {
	mgr, runner, _ := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))
	assert.Equal(t, int64(0), runner.endCalls.Load(), "ending an already-cold session must not call runner")
}

// TestManager_EndSession_RecoversFromStuckEnding verifies that EndSession
// succeeds when the row is already in status=ending (a prior call failed
// between the two-write pattern and left the row wedged), and that the session
// can subsequently be reopened via OpenSession.
func TestManager_EndSession_RecoversFromStuckEnding(t *testing.T) {
	t.Parallel()
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Simulate a prior partial failure: set status=ending directly in the store.
	sess.Status = chat.StatusEnding
	require.NoError(t, store.UpdateSession(ctx, sess))

	// EndSession must succeed even though the row is already in ending.
	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, chat.StatusCold, got.Status, "session must be cold after EndSession recovers from stuck-ending")

	// The recovered session must be openable again (OpenSession previously
	// rejected status=ending rows, so a stuck row would prevent reopening).
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err, "session must be openable after EndSession clears the stuck-ending state")
}

// TestManager_EndSession_NeverPersistsEndingStatus verifies that a successful
// EndSession call never writes status=ending to the store (single-write
// contract). If the first write in the old two-step pattern had written
// status=ending, the injected fault on that write would cause EndSession to
// fail — but with the single-write pattern the fault is never triggered.
func TestManager_EndSession_NeverPersistsEndingStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	inner, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	ts := &trackingStore{Store: inner}
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   ts,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Move the session to active so EndSession has real work to do.
	sess.Status = chat.StatusActive
	sess.ContainerID = "container-x"
	require.NoError(t, inner.UpdateSession(ctx, sess))

	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// Verify no intermediate write ever persisted status=ending.
	for _, s := range ts.writtenStatuses() {
		require.NotEqual(t, chat.StatusEnding, s,
			"EndSession must never persist status=ending; got intermediate statuses: %v", ts.writtenStatuses())
	}

	// Session must end up cold.
	got, err := inner.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, chat.StatusCold, got.Status)
}

func TestManager_AppendMessage_AssignsMonotonicSeq(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	m1, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, `{"text":"hi"}`)
	require.NoError(t, err)
	assert.Equal(t, int64(1), m1.Seq)

	m2, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, `{"text":"hello"}`)
	require.NoError(t, err)
	assert.Equal(t, int64(2), m2.Seq)

	msgs, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
}

func TestManager_AutoTitle_FromFirstUserMessage(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
	require.NoError(t, err)
	assert.Empty(t, sess.Title)

	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "let's investigate the auth flow")
	require.NoError(t, err)

	got, _ := store.GetSession(ctx, sess.ID)
	assert.Equal(t, "let's investigate the auth flow", got.Title)
}

func TestManager_AutoTitle_TruncatesAt50Chars(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
	require.NoError(t, err)

	long := "this is a fairly long first message that exceeds fifty characters total"
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, long)
	require.NoError(t, err)

	got, _ := store.GetSession(ctx, sess.ID)
	assert.LessOrEqual(t, utf8.RuneCountInString(got.Title), 51) // 50 runes + ellipsis
	assert.True(t, strings.HasSuffix(got.Title, "…"))
}

// TestManager_AutoTitle_RuneSafe verifies that auto-title slices at a rune
// boundary, not a byte boundary. Multi-byte characters (UTF-8) like "é"
// (2 bytes) would otherwise be cut mid-rune and round-trip as U+FFFD garbage
// through JSON marshaling.
func TestManager_AutoTitle_RuneSafe(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
	require.NoError(t, err)

	// 49 ASCII chars + "é" places the first byte of "é" at byte index 49 and
	// the second byte at index 50. A naive byte-slice [:50] cuts mid-rune.
	long := strings.Repeat("a", 49) + "é trailing words"
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, long)
	require.NoError(t, err)

	got, _ := store.GetSession(ctx, sess.ID)
	assert.True(t, utf8.ValidString(got.Title),
		"auto-title must remain valid UTF-8; got %q", got.Title)
	assert.True(t, strings.HasSuffix(got.Title, "…"))
}

func TestManager_MarkWarmIdle_ActiveToWarmIdle(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	require.NoError(t, mgr.MarkWarmIdle(ctx, sess.ID))
	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusWarmIdle, got.Status)
	assert.Equal(t, "c-1", got.ContainerID, "container ID must survive warm-idle")
}

func TestManager_MarkWarmIdle_ColdNoOp(t *testing.T) {
	mgr, _, _ := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)
	// session is cold; MarkWarmIdle should not change anything
	require.NoError(t, mgr.MarkWarmIdle(ctx, sess.ID))
	got, _ := mgr.GetSession(ctx, sess.ID)
	assert.Equal(t, chat.StatusCold, got.Status, "cold sessions stay cold")
}

// newManagerWithHub creates a Manager wired to a real SSEHub and a FakeClock
// so tests can advance time and observe SSE events deterministically.
func newManagerWithHub(t *testing.T) (*chat.Manager, *stubRunner, chat.Store, *chat.SSEHub, *clock.FakeClock) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	hub := chat.NewSSEHub(128)
	clk := clock.Fake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clk,
		IdleTTL: time.Hour,
		Hub:     hub,
	})

	return mgr, runner, store, hub, clk
}

func TestManager_MarkWarmIdle_PublishesSessionUpdate(t *testing.T) {
	t.Parallel()
	mgr, _, store, hub, _ := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)
	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	require.NoError(t, mgr.MarkWarmIdle(ctx, sess.ID))

	select {
	case e := <-ch:
		require.Equal(t, chat.SSEKindSessionUpdate, e.Kind)
		require.NotNil(t, e.SessionUpdate)
		require.NotNil(t, e.SessionUpdate.Status)
		assert.Equal(t, chat.StatusWarmIdle, *e.SessionUpdate.Status)
	case <-time.After(2 * time.Second):
		t.Fatal("expected session_updated event for warm-idle transition")
	}
}

func TestManager_EndSession_PublishesSessionUpdate(t *testing.T) {
	t.Parallel()
	mgr, _, _, hub, _ := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)
	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// publishStatus runs in a goroutine, so we may receive status events
	// from earlier transitions (e.g. active from OpenSession) before cold.
	// Drain until we find the cold event or time out.
	deadline := time.After(2 * time.Second)

	for {
		select {
		case e := <-ch:
			require.Equal(t, chat.SSEKindSessionUpdate, e.Kind)
			require.NotNil(t, e.SessionUpdate)
			require.NotNil(t, e.SessionUpdate.Status)

			if *e.SessionUpdate.Status == chat.StatusCold {
				// Check that RehydrationActive is also set to false.
				require.NotNil(t, e.SessionUpdate.RehydrationActive,
					"EndSession session_updated must include RehydrationActive")
				assert.False(t, *e.SessionUpdate.RehydrationActive)

				return
			}
			// Any other status (e.g. active from OpenSession's goroutine) — keep draining.
		case <-deadline:
			t.Fatal("expected session_updated event for cold transition")
		}
	}
}

func TestManager_MarkActive_WarmIdleToActive_PublishesUpdate(t *testing.T) {
	t.Parallel()
	mgr, _, store, hub, clk := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	require.NoError(t, mgr.MarkWarmIdle(ctx, sess.ID))

	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)
	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	// publishStatus runs in a goroutine, so the warm-idle event from
	// MarkWarmIdle may race with our subscribe. We snapshot LastActive
	// before calling MarkActive and drain events until we find active.

	// Snapshot LastActive before MarkActive.
	before, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)

	// Advance clock so MarkActive stamps a distinct LastActive.
	clk.Advance(5 * time.Second)

	require.NoError(t, mgr.MarkActive(ctx, sess.ID))

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)
	assert.True(t, got.LastActive.After(before.LastActive), "LastActive must be refreshed")

	// Drain until we find the active event (may be preceded by the warm-idle
	// goroutine racing the subscribe).
	deadline := time.After(2 * time.Second)

	for {
		select {
		case e := <-ch:
			require.Equal(t, chat.SSEKindSessionUpdate, e.Kind)
			require.NotNil(t, e.SessionUpdate)
			require.NotNil(t, e.SessionUpdate.Status)

			if *e.SessionUpdate.Status == chat.StatusActive {
				return // found it
			}
			// warm-idle event from the earlier goroutine — keep draining.
		case <-deadline:
			t.Fatal("expected session_updated event for active promotion")
		}
	}
}

func TestManager_MarkActive_AlreadyActive_NoOp(t *testing.T) {
	t.Parallel()
	mgr, _, store, hub, _ := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	beforeLastActive := sess.LastActive

	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)
	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	// MarkActive on an already-active session is a no-op.
	require.NoError(t, mgr.MarkActive(ctx, sess.ID))

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)
	assert.Equal(t, beforeLastActive, got.LastActive, "LastActive must not change on no-op")

	// No SSE event should be published.
	select {
	case <-ch:
		t.Fatal("unexpected SSE event for no-op MarkActive on active session")
	default:
	}
}

func TestManager_MarkActive_Cold_NoOp(t *testing.T) {
	t.Parallel()
	mgr, _, store, hub, _ := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)
	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	// Session is cold by default; MarkActive must not change it.
	require.NoError(t, mgr.MarkActive(ctx, sess.ID))

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusCold, got.Status, "cold session must stay cold")

	// No SSE event.
	select {
	case <-ch:
		t.Fatal("unexpected SSE event for no-op MarkActive on cold session")
	default:
	}
}

func TestManager_SendUserMessage_WarmIdle_PromotesToActive(t *testing.T) {
	t.Parallel()
	mgr, _, store, _, _ := newManagerWithHub(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Force to warm-idle.
	sess.Status = chat.StatusWarmIdle
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	_, err = mgr.SendUserMessage(ctx, sess.ID, "hello")
	require.NoError(t, err)

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status, "SendUserMessage must promote warm-idle to active")
}

// TestManager_OpenSession_MaxConcurrent_ParallelTOCTOU exercises the
// concurrency cap under a tight race: ten goroutines call OpenSession at
// once with MaxConcurrent=2. Without the lock fix, the two ListSessions
// reads happen before any StartChat call mutates the store, so several
// goroutines pass the limit check simultaneously and the runner sees
// more than two StartChat calls. With the fix exactly two start.
func TestManager_OpenSession_MaxConcurrent_ParallelTOCTOU(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const total = 10

	// slowRunner stalls StartChat briefly to widen the race window.
	runner := &slowStartRunner{delay: 10 * time.Millisecond}

	mgr := chat.NewManager(chat.Config{
		Store: store, Runner: runner, Clock: clock.Real(),
		IdleTTL: time.Hour, MaxConcurrent: 2,
	})

	ctx := context.Background()

	ids := make([]string, total)

	for i := range total {
		sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
		require.NoError(t, err)

		ids[i] = sess.ID
	}

	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		rejects   atomic.Int64
	)

	for _, id := range ids {
		wg.Add(1)

		go func(sessID string) {
			defer wg.Done()

			_, err := mgr.OpenSession(ctx, sessID)

			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, chat.ErrTooManyConcurrent):
				rejects.Add(1)
			default:
				t.Errorf("OpenSession(%s) unexpected error: %v", sessID, err)
			}
		}(id)
	}

	wg.Wait()

	assert.Equal(t, int64(2), successes.Load(), "exactly MaxConcurrent (=2) opens must succeed")
	assert.Equal(t, int64(total-2), rejects.Load(), "all other opens must be rejected")
	assert.LessOrEqual(t, runner.startCalls.Load(), int64(2),
		"runner.StartChat must be called at most MaxConcurrent times (no leaked containers)")
}

// TestManager_AppendMessage_SeqMonotonicUnderConcurrency exercises the
// serialisation fix: concurrent AppendMessage calls on the same session must
// land in the store both (a) with strictly monotonic seq values and (b) in
// insertion order — so the rowid order matches the seq order. Without
// holding m.mu across the store insert, two appends can race past one
// another and land out of seq order on disk.
func TestManager_AppendMessage_SeqMonotonicUnderConcurrency(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")

	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	mgr := chat.NewManager(chat.Config{
		Store: store, Runner: &stubRunner{}, Clock: clock.Real(), IdleTTL: time.Hour,
	})

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "seq", CreatedBy: "x"})
	require.NoError(t, err)

	const N = 50

	var wg sync.WaitGroup

	wg.Add(N)

	for i := range N {
		go func(i int) {
			defer wg.Done()

			_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, strconv.Itoa(i))
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// (a) ListMessages orders by seq; verify seqs are 1..N with no holes.
	msgs, err := store.ListMessages(ctx, sess.ID, 0, 1000)
	require.NoError(t, err)
	require.Len(t, msgs, N)

	for i, m := range msgs {
		assert.Equal(t, int64(i+1), m.Seq, "seq %d should be %d", i, i+1)
	}

	// (b) Open the DB directly and query in rowid order. The seq column
	// must increase monotonically with rowid — i.e. the insertion order
	// matches the seq order. This is the assertion that fails when the
	// store write happens outside the seq-assignment lock.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.QueryContext(ctx,
		`SELECT seq FROM chat_messages WHERE session_id = ? ORDER BY id ASC`, sess.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	var prev int64

	for rows.Next() {
		var seq int64
		require.NoError(t, rows.Scan(&seq))
		assert.Greater(t, seq, prev,
			"insertion order: seq must increase with rowid, got prev=%d cur=%d", prev, seq)
		prev = seq
	}

	require.NoError(t, rows.Err())
}

type slowStartRunner struct {
	delay      time.Duration
	startCalls atomic.Int64
}

func (s *slowStartRunner) StartChat(_ context.Context, _ chat.StartChatOpts) (string, error) {
	s.startCalls.Add(1)
	time.Sleep(s.delay)

	return "container-" + strconv.FormatInt(s.startCalls.Load(), 10), nil
}

func (s *slowStartRunner) EndChat(_ context.Context, _ string) error { return nil }

func (s *slowStartRunner) SendChatMessage(_ context.Context, _, _, _ string) error { return nil }

func (s *slowStartRunner) StreamLogs(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
	<-ctx.Done()

	return ctx.Err()
}

func TestManager_OpenSession_RespectsMaxConcurrent(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store: store, Runner: runner, Clock: clock.Real(),
		IdleTTL: time.Hour, MaxConcurrent: 2,
		ResolveRepoURL: func(ctx context.Context, project string) (string, error) {
			return "", nil
		},
	})

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
		require.NoError(t, err)
		_, err = mgr.OpenSession(ctx, sess.ID)
		require.NoError(t, err)
	}

	// Third open should fail with ErrTooManyConcurrent.
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "", CreatedBy: "x"})
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.ErrorIs(t, err, chat.ErrTooManyConcurrent)
}

func TestManager_ListSessions_FilterByStatus(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess1, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "a", CreatedBy: "x"})
	require.NoError(t, err)
	sess2, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "b", CreatedBy: "x"})
	require.NoError(t, err)

	// Flip sess2 to warm-idle in the store.
	sess2.Status = chat.StatusWarmIdle
	require.NoError(t, store.UpdateSession(ctx, sess2))

	all, err := mgr.ListSessions(ctx, chat.SessionFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 2)

	cold, err := mgr.ListSessions(ctx, chat.SessionFilter{Status: chat.StatusCold})
	require.NoError(t, err)
	assert.Len(t, cold, 1)
	assert.Equal(t, sess1.ID, cold[0].ID)
}

func TestManager_DeleteSession_ColdDeletesRow(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	require.NoError(t, mgr.DeleteSession(ctx, sess.ID))

	_, err = store.GetSession(ctx, sess.ID)
	require.ErrorIs(t, err, chat.ErrSessionNotFound)
}

func TestManager_DeleteSession_ActiveEndsFirst(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	require.NoError(t, mgr.DeleteSession(ctx, sess.ID))

	assert.Equal(t, int64(1), runner.endCalls.Load(), "EndSession must have stopped the container")

	_, err = store.GetSession(ctx, sess.ID)
	require.ErrorIs(t, err, chat.ErrSessionNotFound)
}

func TestManager_SendUserMessage_HappyPath(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)
	// Pre-open to active so OpenSession is not needed inside SendUserMessage.
	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	msgID, err := mgr.SendUserMessage(ctx, sess.ID, "hello world")
	require.NoError(t, err)
	assert.NotEmpty(t, msgID)
	assert.Equal(t, int64(1), runner.sendCalls.Load(), "SendChatMessage must be called once")

	msgs, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, chat.RoleUser, msgs[0].Role)
	assert.Equal(t, "hello world", msgs[0].Content)
}

// TestManager_SendUserMessage_RunnerErrorDoesNotPersist exercises the
// runner-first ordering: if the runner.SendChatMessage call fails, the
// user message is NOT persisted and not published to the hub. The UI sees
// the error and can retry without ending up with an orphaned echo.
func TestManager_SendUserMessage_RunnerErrorDoesNotPersist(t *testing.T) {
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Status = chat.StatusActive
	sess.ContainerID = "c-1"
	require.NoError(t, store.UpdateSession(ctx, sess))

	runner.sendErr = errors.New("runner unreachable")

	_, err = mgr.SendUserMessage(ctx, sess.ID, "hello")
	require.Error(t, err, "runner failure must propagate to the caller")
	assert.Contains(t, err.Error(), "runner unreachable")

	// No persisted user message — the runner-first ordering means we never
	// got past the runner call.
	msgs, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, msgs, "no user message must be persisted when runner.SendChatMessage fails")
}

func TestManager_SendUserMessage_OpensColdSession(t *testing.T) {
	mgr, runner, _ := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)
	// Session remains cold — SendUserMessage must open it first.

	_, err = mgr.SendUserMessage(ctx, sess.ID, "hi")
	require.NoError(t, err)
	assert.Equal(t, int64(1), runner.startCalls.Load(), "cold session must trigger StartChat")
	assert.Equal(t, int64(1), runner.sendCalls.Load())
}

func TestManager_UpdateSessionMetadata_ChangesTitle(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "old", CreatedBy: "x"})
	require.NoError(t, err)

	sess.Title = "new title"
	require.NoError(t, mgr.UpdateSessionMetadata(ctx, sess))

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "new title", got.Title)
}

// TestManager_OpenSession_BridgesRunnerLogs verifies that an assistant text
// event emitted by the runner's /logs stream is persisted as an
// assistant_text message and published to the SSE hub. Without this
// bridge, the browser would see only the user echo and no reply.
func TestManager_OpenSession_BridgesRunnerLogs(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	delivered := make(chan struct{})
	runner := &stubRunner{
		streamLogsFn: func(ctx context.Context, _ string, onEntry func(chat.LogEntry)) error {
			onEntry(chat.LogEntry{Type: "text", Content: "Hello back."})
			close(delivered)

			<-ctx.Done()

			return ctx.Err()
		},
	}

	hub := chat.NewSSEHub(128)
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		Hub:     hub,
	})

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "bridge", CreatedBy: "human:test"})
	require.NoError(t, err)

	ch, _, _ := hub.Subscribe(sess.ID, 0)

	t.Cleanup(func() { hub.Unsubscribe(sess.ID, ch) })

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamLogs onEntry never invoked")
	}

	// Drain until we find the assistant_text message event. The goroutine-based
	// publishStatus from OpenSession (cold→active) may deliver a session_updated
	// event first — skip those.
	deadline := time.After(2 * time.Second)

	for {
		select {
		case e := <-ch:
			if e.Kind == chat.SSEKindSessionUpdate {
				continue // skip lifecycle events
			}

			assert.Equal(t, chat.RoleAssistantText, e.Role)
			assert.Equal(t, "Hello back.", e.Content)
			assert.Equal(t, int64(1), e.Seq)

			goto foundMessage
		case <-deadline:
			t.Fatal("hub did not receive assistant_text event")
		}
	}

foundMessage:

	// EndSession should stop the consumer; verify via streamCalls staying at 1.
	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// Re-opening should kick off a new consumer (idempotency check would
	// require another OpenSession; verifying stop is enough for this test).
	assert.Equal(t, int64(1), runner.streamCalls.Load())
}

// TestManager_EndThenReopen_SpawnsFreshConsumer is the regression for the
// startConsumer ↔ stopConsumer cleanup race. With the unfixed code,
// stopConsumer cancels the consumer context and returns immediately; the
// goroutine's deferred map-delete runs asynchronously. A fast Reopen that
// runs while the deferred delete is still pending finds a stale entry in
// m.consumers and returns early — the new session has no log bridge.
//
// We simulate slow goroutine exit with a streamLogsFn that sleeps after
// ctx.Done. With the fix, stopConsumer waits on a per-consumer done channel
// and the entry is gone before Reopen runs.
func TestManager_EndThenReopen_SpawnsFreshConsumer(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{
		streamLogsFn: func(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
			<-ctx.Done()
			// Simulate slow goroutine exit — the goroutine has received cancel
			// but has not yet run its cleanup defers.
			time.Sleep(50 * time.Millisecond)

			return ctx.Err()
		},
	}

	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:x"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return runner.streamCalls.Load() == 1 },
		time.Second, 5*time.Millisecond, "first open must start consumer")

	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Without the fix, streamCalls stays at 1 because the second startConsumer
	// returned early on a stale map entry.
	require.Eventually(t, func() bool { return runner.streamCalls.Load() == 2 },
		time.Second, 5*time.Millisecond,
		"Reopen after End must spawn a fresh runner-log consumer")

	require.NoError(t, mgr.EndSession(ctx, sess.ID))
}

// TestManager_AppendMessage_TruncatesOversizedContent verifies that
// runner-emitted entries beyond the per-message size cap are truncated with
// a marker before persistence. Without this cap, a chatty tool (cat of a
// large file, verbose tool_result) fills chats.db linearly and never
// reclaims the space.
func TestManager_AppendMessage_TruncatesOversizedContent(t *testing.T) {
	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	huge := strings.Repeat("a", 100*1024)

	msg, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, huge)
	require.NoError(t, err)
	assert.Less(t, len(msg.Content), len(huge), "oversized content must be truncated")
	assert.LessOrEqual(t, len(msg.Content), 32*1024+64, "truncated content must fit the cap (with marker)")
	assert.Contains(t, msg.Content, "[truncated]", "truncation must leave a marker")

	msgs, err := store.ListMessages(ctx, sess.ID, 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg.Content, msgs[0].Content, "persisted content must match returned content")
}

// TestManager_AppendMessage_DoesNotTruncateSmallContent ensures the cap only
// fires on oversized content.
func TestManager_AppendMessage_DoesNotTruncateSmallContent(t *testing.T) {
	mgr, _, _ := newManagerWithStubs(t)
	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	msg, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "hello world")
	require.NoError(t, err)
	assert.Equal(t, "hello world", msg.Content, "small content must not be touched")
}

func TestManager_OpenSession_ColdWithPriorTranscript_SendsResume(t *testing.T) {
	mgr, runner, _ := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Seed a transcript so transcript.Build returns a non-nil resume.
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "first goal")
	require.NoError(t, err)
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "okay")
	require.NoError(t, err)

	// End so the next OpenSession follows the cold-branch.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// Reopen.
	reopened, err := mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, reopened.Status)

	runner.mu.Lock()
	opts := runner.lastOpts
	runner.mu.Unlock()

	assert.Equal(t, sess.ID, opts.SessionID)
	require.NotNil(t, opts.Resume, "Resume must be sent on cold-reopen with prior transcript")
	require.GreaterOrEqual(t, len(opts.Resume.Turns), 2,
		"resume payload should carry the prior user + assistant turns")
}

func TestManager_OpenSession_ColdEmptyTranscript_OmitsResume(t *testing.T) {
	mgr, runner, _ := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	opts := runner.lastOpts
	runner.mu.Unlock()

	assert.Nil(t, opts.Resume, "fresh session must not carry a Resume")
}

func TestManager_OpenSession_PassesModel(t *testing.T) {
	mgr, runner, _ := newManagerWithStubsAndConfig(t, chat.Config{
		IdleTTL:      time.Hour,
		DefaultModel: "claude-sonnet-4-6",
	})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{
		Title:     "t",
		CreatedBy: "x",
		Model:     "claude-opus-4-7",
	})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	opts := runner.lastOpts
	runner.mu.Unlock()

	assert.Equal(t, "claude-opus-4-7", opts.Model,
		"session-stored model must be passed to runner")
}

func TestManager_OpenSession_FallsBackToDefaultModel(t *testing.T) {
	mgr, runner, _ := newManagerWithStubsAndConfig(t, chat.Config{
		IdleTTL:      time.Hour,
		DefaultModel: "claude-sonnet-4-6",
	})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	opts := runner.lastOpts
	runner.mu.Unlock()

	assert.Equal(t, "claude-sonnet-4-6", opts.Model,
		"empty session.Model falls back to config DefaultModel")
}

func TestManager_CompleteRehydration_PersistsSummaryAndFlipsFlag(t *testing.T) {
	mgr, _, store := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Seed a transcript and reopen so rehydration_active flips on.
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "task")
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	reopened, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, reopened.RehydrationActive, "reopen with prior transcript should set rehydration_active=true")

	err = mgr.CompleteRehydration(ctx, sess.ID, "Picking up where we left off — re-cloned foo.")
	require.NoError(t, err)

	flipped, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, flipped.RehydrationActive, "CompleteRehydration must flip flag off")

	msgs, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)

	var summary *chat.Message

	for i, msg := range msgs {
		if msg.Role == chat.RoleAssistantText && msg.Content[:7] == "Picking" {
			summary = &msgs[i]

			break
		}
	}

	require.NotNil(t, summary, "summary message must be persisted")
	assert.False(t, summary.RehydrationPhase, "summary message must NOT carry the phase flag")
}

func TestManager_CompleteRehydration_Idempotent(t *testing.T) {
	t.Parallel()
	mgr, _, store := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Set rehydration active, then complete it.
	require.NoError(t, mgr.SetRehydrationActiveForTest(ctx, sess.ID, true))
	require.NoError(t, mgr.CompleteRehydration(ctx, sess.ID, "first call"))

	// Second call — must succeed and NOT append a second summary.
	before, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)

	require.NoError(t, mgr.CompleteRehydration(ctx, sess.ID, "second call"))

	after, err := store.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)

	assert.Len(t, after, len(before),
		"second call must not append another summary")
}

func TestManager_SendUserMessage_EndsRehydrationPhase(t *testing.T) {
	mgr, _, store := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "task")
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	active, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, active.RehydrationActive)

	_, err = mgr.SendUserMessage(ctx, sess.ID, "follow up")
	require.NoError(t, err)

	after, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, after.RehydrationActive,
		"first user message during rehydration must flip the flag off")
}

func TestManager_EndSession_ResetsRehydrationActive(t *testing.T) {
	mgr, _, store := newManagerWithStubsAndConfig(t, chat.Config{IdleTTL: time.Hour})
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "task")
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, got.RehydrationActive,
		"EndSession must clear the rehydration flag in the cold transition")
}

// TestManager_OpenSession_RollbackOnRehydrationPersistFailure verifies that if
// the store.SetRehydrationActive write fails after the container is already up,
// OpenSession rolls back the container (EndChat), clears the in-memory cache,
// resets the session row to cold, and returns an error — leaving no orphaned
// active container with an unset rehydration flag.
func TestManager_OpenSession_RollbackOnRehydrationPersistFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	inner, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	fstore := &failingStore{Store: inner}
	runner := &stubRunner{}

	mgr := chat.NewManager(chat.Config{
		Store:   fstore,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	// Seed a message so cold-reopen triggers the rehydration path.
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, `{"text":"hi"}`)
	require.NoError(t, err)

	// End the session so next OpenSession is cold with a non-empty transcript.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// Arm the one-shot fault: next SetRehydrationActive call will fail.
	fstore.FailNextSetRehydration()

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.Error(t, err, "OpenSession must fail when the rehydration flag cannot be persisted")

	// The container that was started must have been rolled back.
	require.Equal(t, int64(2), runner.endCalls.Load(),
		"EndChat must be called once for the explicit EndSession, once for the rollback")

	// Session must be back to cold so the next open is a clean retry.
	got, err := inner.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, chat.StatusCold, got.Status, "failed open must leave session cold")
	assert.Empty(t, got.ContainerID, "container ID must be cleared on rollback")
	assert.False(t, got.RehydrationActive, "rehydration_active must not be set after failed open")
}

// TestSetRehydrationActive_StoreAndCacheStayInSync drives many concurrent
// flips through setRehydrationActive and asserts the on-disk value equals
// the in-memory cache value once the dust settles. When the store write
// happens outside m.mu, two callers writing opposite booleans can land in
// opposite orders on disk vs cache, leaving the cache permanently desynced.
// Holding m.mu across both writes forces a single serialization point so
// whichever value commits to disk last is also the cache value on return.
//
// The store is wrapped in yieldingStore which sleeps a jittered amount
// after every SetRehydrationActive call. SQLite's UPDATE is heavyweight
// relative to the trivial cache write that follows, so without an
// explicit, variable post-store delay the cache writes drain in lockstep
// with the store commits and the race window collapses. The jitter
// scatters cache writes out of store-commit order — the schedule that
// exposes the regression. Multiple flips per goroutine compound the
// variance; iterating the outer batch a few times makes a single CI run
// likely to catch the bug.
func TestSetRehydrationActive_StoreAndCacheStayInSync(t *testing.T) {
	inner, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	store := &yieldingStore{Store: inner}
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	sess, err := mgr.CreateSession(context.Background(), chat.CreateInput{
		Title:     "ordering",
		Project:   "alpha",
		CreatedBy: "human:test",
	})
	require.NoError(t, err)

	// Several batches of 100 concurrent flips. After each batch the
	// cache value must equal the persisted store value — they are
	// written under the same lock, so no schedule should split them.
	// Each batch is an independent observation; running enough of them
	// makes a single -count=10 CI run very likely to surface a
	// regression.
	//
	// flipErr captures any error from inside the goroutines. testifylint
	// (go-require) bans require.* in goroutines because it Goexits the
	// caller, not the test — flip errors are funneled out here instead.
	var flipErr atomic.Pointer[error]

	for batch := range 5 {
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)

			active := i%2 == 0

			go func() {
				defer wg.Done()

				if err := mgr.SetRehydrationActiveForTest(context.Background(), sess.ID, active); err != nil {
					flipErr.Store(&err)
				}
			}()
		}

		wg.Wait()

		if err := flipErr.Load(); err != nil {
			require.NoError(t, *err, "setRehydrationActive flip failed inside goroutine")
		}

		stored, err := store.GetSession(context.Background(), sess.ID)
		require.NoError(t, err)

		cached, ok := mgr.RehydrationActiveCacheForTest(sess.ID)
		require.True(t, ok, "cache must be populated after setRehydrationActive calls")
		require.Equalf(t, stored.RehydrationActive, cached,
			"batch %d: cache value %v diverged from stored value %v",
			batch, cached, stored.RehydrationActive)
	}
}

func TestManager_HandleUsageEntry_UpdatesContextTokens(t *testing.T) {
	hub := chat.NewSSEHub(64)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &usageStreamingRunner{
		entries: []chat.LogEntry{
			{
				Type: "usage",
				Usage: &chat.TokenUsage{
					InputTokens:       1000,
					OutputTokens:      500,
					CacheReadTokens:   4000,
					CacheCreateTokens: 200,
				},
				Model: "claude-sonnet-4-6",
			},
		},
	}

	mgr := chat.NewManager(chat.Config{
		Store:        store,
		Runner:       runner,
		Clock:        clock.Real(),
		IdleTTL:      time.Hour,
		Hub:          hub,
		DefaultModel: "claude-sonnet-4-6",
	})

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "x"})
	require.NoError(t, err)

	// Subscribe BEFORE opening so we observe the session_updated event.
	events, _, _ := hub.Subscribe(sess.ID, 0)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Wait for the usage event to propagate through the consumer. Drain any
	// lifecycle events (e.g. active from cold→active goroutine publish) until
	// we find a session_updated with ContextTokens set.
	var got chat.SSEEvent

	deadline := time.After(2 * time.Second)

	for {
		select {
		case got = <-events:
		case <-deadline:
			t.Fatal("timed out waiting for session_updated event with context_tokens")
		}

		require.Equal(t, chat.SSEKindSessionUpdate, got.Kind, "event must be a session_updated push")
		require.NotNil(t, got.SessionUpdate)

		if got.SessionUpdate.ContextTokens > 0 {
			break // found the usage event
		}
		// Skip lifecycle-status events (e.g. active from cold→active publish).
	}

	// 1000 + 4000 + 200 = 5200 (output tokens NOT included in context).
	assert.Equal(t, int64(5200), got.SessionUpdate.ContextTokens,
		"context_tokens = input + cache_read + cache_create")

	// Wait briefly for the DB write (handleUsageEntry persists then publishes).
	dbDeadline := time.Now().Add(time.Second)
	for time.Now().Before(dbDeadline) {
		s, err := store.GetSession(ctx, sess.ID)
		require.NoError(t, err)

		if s.ContextTokens == 5200 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("session.context_tokens never reached 5200")
}

// usageStreamingRunner is a stub RunnerClient that delivers a canned list
// of LogEntry values through StreamLogs (in order, with a small delay so
// the consumer reliably observes them).
type usageStreamingRunner struct {
	entries []chat.LogEntry
}

func (r *usageStreamingRunner) StartChat(_ context.Context, _ chat.StartChatOpts) (string, error) {
	return "container-usage", nil
}

func (r *usageStreamingRunner) EndChat(_ context.Context, _ string) error { return nil }

func (r *usageStreamingRunner) SendChatMessage(_ context.Context, _, _, _ string) error {
	return nil
}

func (r *usageStreamingRunner) StreamLogs(ctx context.Context, _ string, onEntry func(chat.LogEntry)) error {
	for _, e := range r.entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		onEntry(e)
	}

	<-ctx.Done()

	return ctx.Err()
}

// newManagerWithStubsAndConfig is like newManagerWithStubs but lets the
// caller override the manager Config fields (DefaultModel, IdleTTL, etc.)
// without duplicating the store + stubRunner wiring boilerplate.
func newManagerWithStubsAndConfig(t *testing.T, base chat.Config) (*chat.Manager, *stubRunner, chat.Store) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}

	base.Store = store
	base.Runner = runner

	if base.Clock == nil {
		base.Clock = clock.Real()
	}

	mgr := chat.NewManager(base)

	return mgr, runner, store
}

// TestManager_BuildResume_UsesTailOnLongSession is a regression for
// buildResume loading the oldest 600 messages instead of the newest.
// Sessions past ~600 messages would lose recent context — the "pin last 20
// turns" guarantee in transcript.Build operated on a stale prefix.
func TestManager_BuildResume_UsesTailOnLongSession(t *testing.T) {
	t.Parallel()

	mgr, _, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "long", Project: "p", CreatedBy: "human:test"})
	require.NoError(t, err)

	// Seed 650 messages directly via the store (bypasses Manager seq tracking,
	// which is intentional — we are testing the read path, not the write path).
	// maxMessagesForBuild is 600, so messages 1..50 must be excluded when
	// using the old ListMessages(0, 600) call but present when using the tail.
	const total = 650

	for i := 1; i <= total; i++ {
		_, err := store.AppendMessage(ctx, chat.Message{
			SessionID: sess.ID,
			Seq:       int64(i),
			Role:      chat.RoleAssistantText,
			Content:   fmt.Sprintf(`{"text":"msg-%d"}`, i),
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		})
		require.NoError(t, err)
	}

	rc := mgr.BuildResumeForTest(ctx, sess.ID)
	require.NotNil(t, rc)
	require.NotEmpty(t, rc.Turns)

	// The most recent message must be in the resume payload.
	last := rc.Turns[len(rc.Turns)-1]
	require.Contains(t, last.Content, `msg-650`, "tail must include the newest message")
}

func TestManager_CompleteRehydration_UnknownSession(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManagerWithStubs(t)
	err := mgr.CompleteRehydration(context.Background(), "01DOES_NOT_EXIST", "summary text")
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionNotFound)
}

func TestManager_OpenSession_WorkspaceDedupesOnReopen(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "proj", CreatedBy: "human:t"})
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err := mgr.OpenSession(ctx, sess.ID)
		require.NoError(t, err)
		err = mgr.EndSession(ctx, sess.ID)
		require.NoError(t, err)
	}

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"proj"}, got.Workspace, "project must appear once regardless of reopen count")
}

func TestManager_Close_StopsAllConsumers(t *testing.T) {
	t.Parallel()
	mgr, runner, _ := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "p", CreatedBy: "human:t"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Wait for StreamLogs goroutine to start and increment activeStreams.
	require.Eventually(t, func() bool {
		return runner.activeStreams.Load() == 1
	}, time.Second, time.Millisecond, "StreamLogs goroutine must start before Close")

	require.NoError(t, mgr.Close(context.Background()))
	require.Equal(t, int32(0), runner.activeStreams.Load(), "Close must stop all log streams")
}

// countingRunner is a fake RunnerClient whose StartChat behaviour is fully
// controlled by the test via the startChat func field. Used to gate cold-open
// progress on a per-test signal so we can assert that two distinct sessions
// reach the runner concurrently.
type countingRunner struct {
	startChat func(ctx context.Context, opts chat.StartChatOpts) (string, error)
}

func (r *countingRunner) StartChat(ctx context.Context, opts chat.StartChatOpts) (string, error) {
	if r.startChat != nil {
		return r.startChat(ctx, opts)
	}

	return "container-" + opts.SessionID, nil
}

func (r *countingRunner) EndChat(_ context.Context, _ string) error { return nil }

func (r *countingRunner) SendChatMessage(_ context.Context, _, _, _ string) error { return nil }

func (r *countingRunner) StreamLogs(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
	<-ctx.Done()

	return ctx.Err()
}

// newTestManagerWithRunner constructs a chat.Manager wired to the supplied
// RunnerClient and a fresh sqlite store, with MaxConcurrent explicitly set
// to 0 (unlimited) so the limit-bounded serialisation path does not gate
// the cold-open singleflight test.
func newTestManagerWithRunner(t *testing.T, runner chat.RunnerClient) (*chat.Manager, chat.Store, func()) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)

	mgr := chat.NewManager(chat.Config{
		Store:         store,
		Runner:        runner,
		Clock:         clock.Real(),
		IdleTTL:       time.Hour,
		MaxConcurrent: 0,
	})

	cleanup := func() {
		_ = mgr.Close(context.Background())
		_ = store.Close()
	}

	return mgr, store, cleanup
}

// newTestManagerWithStore constructs a chat.Manager wired to the supplied
// Store (typically a wrapper around the real sqlite store that injects faults
// or instruments calls) and a stubRunner. The wrapped store is responsible
// for embedding the real chat.Store; this helper just wires it in. Used by
// tests that need to gate AppendMessage independently from the runner path.
func newTestManagerWithStore(t *testing.T, store chat.Store) (*chat.Manager, *stubRunner, func()) {
	t.Helper()

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:         store,
		Runner:        runner,
		Clock:         clock.Real(),
		IdleTTL:       time.Hour,
		MaxConcurrent: 0,
	})

	cleanup := func() {
		_ = mgr.Close(context.Background())
		_ = store.Close()
	}

	return mgr, runner, cleanup
}

// TestAppendMessage_UnrelatedSessionsDoNotSerialize asserts that two appends
// to two different sessions execute in parallel. The gatingStore parks the
// underlying store write on a per-session channel; the test verifies that
// both calls reach the parked point before either returns. Regression for
// the global m.mu lock in AppendMessage, which used to couple unrelated
// sessions through the seq-assign window.
func TestAppendMessage_UnrelatedSessionsDoNotSerialize(t *testing.T) {
	t.Parallel()

	innerStore, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)

	gate := newSessionGate()
	gating := &gatingStore{Store: innerStore, gate: gate}

	mgr, _, cleanup := newTestManagerWithStore(t, gating)
	defer cleanup()

	sess1, err := mgr.CreateSession(context.Background(), chat.CreateInput{Title: "a", CreatedBy: "human:t"})
	require.NoError(t, err)
	sess2, err := mgr.CreateSession(context.Background(), chat.CreateInput{Title: "b", CreatedBy: "human:t"})
	require.NoError(t, err)

	gate.block(sess1.ID)
	gate.block(sess2.ID)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		_, _ = mgr.AppendMessage(context.Background(), sess1.ID, chat.RoleUser, "x")
	}()

	go func() {
		defer wg.Done()

		_, _ = mgr.AppendMessage(context.Background(), sess2.ID, chat.RoleUser, "y")
	}()

	require.Eventually(t, func() bool { return gate.waiting(sess1.ID) && gate.waiting(sess2.ID) },
		time.Second, 5*time.Millisecond,
		"both AppendMessage calls must reach the store write concurrently")

	gate.release(sess1.ID)
	gate.release(sess2.ID)
	wg.Wait()
}

// TestOpenSession_ConcurrentColdOpensRunInParallel asserts that two cold
// opens for distinct session IDs route through their own singleflight slot
// and reach the runner concurrently. Before the singleflight refactor, a
// global openMu serialised the cold-start path so the second call observed
// the first's full StartChat latency; one slow docker pull stalled every
// other cold open. With singleflight keyed on sessionID, two distinct
// sessions complete within ~one StartChat duration.
func TestOpenSession_ConcurrentColdOpensRunInParallel(t *testing.T) {
	release := make(chan struct{})

	var calls atomic.Int64

	runner := &countingRunner{
		startChat: func(_ context.Context, opts chat.StartChatOpts) (string, error) {
			calls.Add(1)
			<-release

			return "container-" + opts.SessionID, nil
		},
	}

	mgr, _, cleanup := newTestManagerWithRunner(t, runner)
	defer cleanup()

	sess1, err := mgr.CreateSession(context.Background(), chat.CreateInput{Title: "a", Project: "alpha", CreatedBy: "human:t"})
	require.NoError(t, err)
	sess2, err := mgr.CreateSession(context.Background(), chat.CreateInput{Title: "b", Project: "alpha", CreatedBy: "human:t"})
	require.NoError(t, err)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() { defer wg.Done(); _, _ = mgr.OpenSession(context.Background(), sess1.ID) }()
	go func() { defer wg.Done(); _, _ = mgr.OpenSession(context.Background(), sess2.ID) }()

	require.Eventually(t, func() bool { return calls.Load() == 2 }, time.Second, 5*time.Millisecond,
		"both runner.StartChat calls must be in flight concurrently")

	close(release)
	wg.Wait()
}

// --- chat-mode primer tests ---

func writeTempPrimer(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chat-mode.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	return path
}

func newManagerWithPrimerPath(t *testing.T, primerPath string) (*chat.Manager, *stubRunner, chat.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:      store,
		Runner:     runner,
		Clock:      clock.Real(),
		IdleTTL:    time.Hour,
		PrimerPath: primerPath,
	})

	return mgr, runner, store
}

func TestManager_OpenCold_PassesPrimerToRunner_Fresh(t *testing.T) {
	primerPath := writeTempPrimer(t, "ORIENT")
	mgr, runner, _ := newManagerWithPrimerPath(t, primerPath)

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	defer runner.mu.Unlock()

	assert.Equal(t, "ORIENT", runner.lastOpts.Primer,
		"fresh cold-open must pass primer content to StartChat")
	assert.Nil(t, runner.lastOpts.Resume, "fresh session has no resume")
}

func TestManager_OpenCold_PassesPrimerToRunner_Resume(t *testing.T) {
	primerPath := writeTempPrimer(t, "ORIENT")
	mgr, runner, _ := newManagerWithPrimerPath(t, primerPath)

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Seed a prior transcript so buildResume returns non-nil. AppendMessage
	// does not change session status; the session stays cold and OpenSession
	// takes the cold path.
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleUser, "earlier turn")
	require.NoError(t, err)
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "earlier reply")
	require.NoError(t, err)

	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	defer runner.mu.Unlock()

	assert.Equal(t, "ORIENT", runner.lastOpts.Primer,
		"resume cold-open must also pass primer content")
	assert.NotNil(t, runner.lastOpts.Resume, "resume payload must be present")
}

func TestManager_OpenCold_PrimerFileMissing(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "does-not-exist.md")
	mgr, runner, _ := newManagerWithPrimerPath(t, missingPath)

	ctx := context.Background()
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Must NOT return an error — fail-open posture.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err, "missing primer file must not block session open")

	runner.mu.Lock()
	defer runner.mu.Unlock()

	assert.Empty(t, runner.lastOpts.Primer,
		"missing primer file must result in empty Primer, not garbage")
}

func TestManager_OpenCold_PrimerReadOnEachOpen(t *testing.T) {
	primerPath := writeTempPrimer(t, "VERSION-1")
	mgr, runner, _ := newManagerWithPrimerPath(t, primerPath)

	ctx := context.Background()

	// First cold open with VERSION-1.
	sess1, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "a", CreatedBy: "human:web-x"})
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess1.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	first := runner.lastOpts.Primer
	runner.mu.Unlock()
	assert.Equal(t, "VERSION-1", first)

	// Edit the primer file between opens.
	require.NoError(t, os.WriteFile(primerPath, []byte("VERSION-2"), 0o644))

	// Second cold open (different session) must see the new content —
	// confirms read-on-each-cold-open rather than boot-cache.
	sess2, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "b", CreatedBy: "human:web-x"})
	require.NoError(t, err)
	_, err = mgr.OpenSession(ctx, sess2.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	defer runner.mu.Unlock()

	assert.Equal(t, "VERSION-2", runner.lastOpts.Primer,
		"second cold-open must read the updated primer (hot-reload)")
}

// --- ClearContext tests ---

func newManagerForClear(t *testing.T, primer string) (*chat.Manager, *stubRunner, chat.Store) {
	t.Helper()
	primerPath := writeTempPrimer(t, primer)

	return newManagerWithPrimerPath(t, primerPath)
}

func TestClearContext_HappyPath(t *testing.T) {
	mgr, runner, store := newManagerForClear(t, "PRIMER")
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open the session so the runner container is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Pre-clear: 3 transcript messages.
	for i := range 3 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "msg-"+strconv.Itoa(i))
		require.NoError(t, err)
	}

	require.NoError(t, mgr.ClearContext(ctx, sess.ID))

	// Runner saw /clear then primer in order.
	runner.mu.Lock()
	args := append([]sendArg(nil), runner.sendArgs...)
	runner.mu.Unlock()
	require.Len(t, args, 2)
	assert.Equal(t, "/clear", args[0].Content)
	assert.Equal(t, "PRIMER", args[1].Content)
	assert.NotEqual(t, args[0].MessageID, args[1].MessageID, "each runner write must use a fresh message id")

	// All three pre-clear rows now have rehydration_phase=true.
	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 4, "3 pre-clear + 1 divider")

	for _, m := range msgs[:3] {
		assert.True(t, m.RehydrationPhase,
			"pre-clear row seq=%d must be flipped to rehydration_phase=true", m.Seq)
	}

	// Divider row is the last entry: system role, marker content, kind=divider.
	divider := msgs[3]
	assert.Equal(t, chat.RoleSystem, divider.Role)
	assert.Equal(t, chat.ContextClearedMarker, divider.Content)
	assert.Equal(t, chat.EventKindDivider, divider.Kind,
		"divider row must persist kind so REST-bootstrap reload renders the rule")
}

func TestClearContext_PrimerMissing(t *testing.T) {
	// PrimerPath is "" → loadPrimer returns "" → no primer-send call.
	mgr, runner, store := newManagerWithStubs(t)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active (runner container is running).
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	require.NoError(t, mgr.ClearContext(ctx, sess.ID))

	runner.mu.Lock()
	args := append([]sendArg(nil), runner.sendArgs...)
	runner.mu.Unlock()
	require.Len(t, args, 1, "only /clear runs when there's no primer file")
	assert.Equal(t, "/clear", args[0].Content)

	// Divider still persists.
	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, chat.EventKindDivider, msgs[0].Kind)
}

func TestClearContext_RunnerFailure_ClearStep(t *testing.T) {
	mgr, runner, store := newManagerForClear(t, "PRIMER")

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "pre-clear")
	require.NoError(t, err)

	// Arm the failure: the next SendChatMessage (i.e. the /clear call) returns
	// an error. SendCalls are indexed from 0; StartChat does not count.
	runner.mu.Lock()
	runner.sendErrSeq = []error{errors.New("runner unreachable")}
	runner.mu.Unlock()

	err = mgr.ClearContext(ctx, sess.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrRunnerSend,
		"runner /clear failure must wrap ErrRunnerSend so API maps to 502, got: %v", err)

	// Transcript untouched: only the pre-clear row, no divider, phase=false.
	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].RehydrationPhase, "phase must not flip when /clear fails")
}

func TestClearContext_PrimerFailure(t *testing.T) {
	mgr, runner, store := newManagerForClear(t, "PRIMER")

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "pre-clear")
	require.NoError(t, err)

	// Arm failure: /clear succeeds (index 0 → nil), primer fails (index 1 → error).
	runner.mu.Lock()
	runner.sendErrSeq = []error{nil, errors.New("primer send failed")}
	runner.mu.Unlock()

	err = mgr.ClearContext(ctx, sess.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrRunnerSend,
		"primer-send failure must wrap ErrRunnerSend, got: %v", err)

	// Both runner calls attempted, but transcript untouched.
	runner.mu.Lock()
	args := append([]sendArg(nil), runner.sendArgs...)
	runner.mu.Unlock()
	require.Len(t, args, 2)
	assert.Equal(t, "/clear", args[0].Content)
	assert.Equal(t, "PRIMER", args[1].Content)

	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1, "no divider appended when primer fails")
	assert.False(t, msgs[0].RehydrationPhase)
}

func TestClearContext_RepeatedClears(t *testing.T) {
	mgr, _, store := newManagerForClear(t, "PRIMER")
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Batch A — 2 messages, then clear.
	for i := range 2 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "a-"+strconv.Itoa(i))
		require.NoError(t, err)
	}

	require.NoError(t, mgr.ClearContext(ctx, sess.ID))

	// Batch B — 1 message, then clear again.
	_, err = mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "b-0")
	require.NoError(t, err)
	require.NoError(t, mgr.ClearContext(ctx, sess.ID))

	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	// 2 batch-A + 1 divider + 1 batch-B + 1 divider = 5
	require.Len(t, msgs, 5)

	// Cumulative marking: every row prior to the latest divider is in
	// phase=true. The most recent divider (appended AFTER the mark step)
	// stays phase=false — it is the "current marker" until the next clear.
	for _, m := range msgs[:4] {
		assert.True(t, m.RehydrationPhase,
			"seq=%d role=%s must be flipped after the second clear", m.Seq, m.Role)
	}

	assert.False(t, msgs[4].RehydrationPhase,
		"the most-recent divider stays phase=false until a subsequent clear")
	assert.Equal(t, chat.EventKindDivider, msgs[4].Kind)
}

func TestClearContext_SessionNotFound(t *testing.T) {
	mgr, _, _ := newManagerForClear(t, "PRIMER")
	err := mgr.ClearContext(context.Background(), "nope")
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionNotFound,
		"unknown session must surface ErrSessionNotFound for 404 mapping, got: %v", err)
}

// TestClearContext_ConcurrentCallsSerialised verifies that singleflight
// serialises concurrent ClearContext calls for the same session. Because
// singleflight.Do deduplicates in-flight calls keyed on sessionID, only
// one /clear + primer pair actually runs (the others share the result).
// The transcript must contain exactly one divider row, and all
// /clear + primer pairs (if any) must appear back-to-back in sendArgs.
func TestClearContext_ConcurrentCallsSerialised(t *testing.T) {
	t.Parallel()

	primerPath := writeTempPrimer(t, "PRIMER")
	mgr, runner, store := newManagerWithPrimerPath(t, primerPath)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	const n = 5

	var wg sync.WaitGroup

	errs := make([]error, n)

	for i := range n {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			errs[idx] = mgr.ClearContext(ctx, sess.ID)
		}(i)
	}

	wg.Wait()

	// All callers must succeed (singleflight shares the result).
	for i, e := range errs {
		require.NoError(t, e, "goroutine %d got unexpected error", i)
	}

	// Exactly one divider row should be in the transcript — singleflight
	// deduplicates concurrent calls so only one tx commits.
	msgs, err := store.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)

	var dividerCount int

	for _, m := range msgs {
		if m.Kind == chat.EventKindDivider {
			dividerCount++
		}
	}

	require.Equal(t, 1, dividerCount,
		"singleflight must deduplicate concurrent clears to exactly one divider row")

	// The /clear + primer pairs that did run must appear consecutively in
	// sendArgs (no interleaving). With singleflight the only pair is at
	// indices 0 and 1 (from the single execution).
	runner.mu.Lock()
	args := append([]sendArg(nil), runner.sendArgs...)
	runner.mu.Unlock()

	// At least one /clear + primer pair must have executed.
	require.GreaterOrEqual(t, len(args), 2, "at least one /clear + primer pair must have run")

	// Each consecutive pair must be /clear then primer.
	for i := 0; i+1 < len(args); i += 2 {
		assert.Equal(t, "/clear", args[i].Content,
			"expected /clear at index %d, got %q", i, args[i].Content)
		assert.Equal(t, "PRIMER", args[i+1].Content,
			"expected PRIMER at index %d, got %q", i+1, args[i+1].Content)
	}
}

// TestClearContext_DividerFailureLeavesTranscriptClean verifies that when
// ClearTranscriptAtomic fails (simulating a divider INSERT failure inside the
// transaction), no rows are marked as rehydration_phase=true. The transaction
// rollback must leave the transcript completely unchanged.
func TestClearContext_DividerFailureLeavesTranscriptClean(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	inner, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	fstore := &clearAtomicFailingStore{Store: inner}
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   fstore,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Seed 3 messages.
	for i := range 3 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText, "msg-"+strconv.Itoa(i))
		require.NoError(t, err)
	}

	// Arm the one-shot failure so ClearTranscriptAtomic returns an error.
	fstore.FailNext()

	err = mgr.ClearContext(ctx, sess.ID)
	require.Error(t, err, "ClearContext must propagate the atomic tx failure")

	// Read directly from inner store so we bypass any manager cache.
	msgs, err := inner.ListMessagesTail(ctx, sess.ID, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 3, "no divider row must have been inserted")

	for _, m := range msgs {
		assert.False(t, m.RehydrationPhase,
			"seq=%d must not be marked: transaction must have rolled back", m.Seq)
	}
}

// TestClearContext_ColdReopen_RehydrationPayloadEmpty verifies the full
// clear → cold → reopen seam: after ClearContext all prior messages are
// stamped rehydration_phase=true, so when the session is ended and reopened
// transcript.Build finds nothing to include and StartChat receives a nil
// Resume (fresh start, no rehydration payload).
func TestClearContext_ColdReopen_RehydrationPayloadEmpty(t *testing.T) {
	t.Parallel()

	primerPath := writeTempPrimer(t, "PRIMER")
	mgr, runner, _ := newManagerWithPrimerPath(t, primerPath)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Open so the session is active.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	// Append 3 assistant messages before clearing.
	for i := range 3 {
		_, err := mgr.AppendMessage(ctx, sess.ID, chat.RoleAssistantText,
			`{"text":"pre-clear-`+strconv.Itoa(i)+`"}`)
		require.NoError(t, err)
	}

	// Clear — marks all 3 messages as rehydration_phase=true.
	require.NoError(t, mgr.ClearContext(ctx, sess.ID))

	// End the session so the next open is a cold start.
	require.NoError(t, mgr.EndSession(ctx, sess.ID))

	// Reopen — cold path, buildResume reads the transcript.
	_, err = mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)

	runner.mu.Lock()
	opts := runner.lastOpts
	runner.mu.Unlock()

	// All pre-clear rows are in rehydration_phase=true, so transcript.Build
	// must return nil (nothing to include after the Clear divider).
	assert.Nil(t, opts.Resume,
		"Resume must be nil after clear: all prior messages are phase=true and should be excluded by transcript.Build")
}

// TestClearContext_ColdSession asserts that ClearContext returns
// ErrSessionNotRunning when the session is cold (no live runner container)
// and that the runner is never called.
func TestClearContext_ColdSession(t *testing.T) {
	t.Parallel()
	mgr, runner, _ := newManagerForClear(t, "PRIMER")
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Session is cold (newly created). ClearContext must reject it.
	err = mgr.ClearContext(ctx, sess.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionNotRunning,
		"cold session must return ErrSessionNotRunning, got: %v", err)

	// Runner must never have been contacted.
	assert.Equal(t, int64(0), runner.sendCalls.Load(), "runner must not be called for a cold session")
}

// TestClearContext_EndingSession asserts that ClearContext returns
// ErrSessionNotRunning when the session is in the "ending" state.
func TestClearContext_EndingSession(t *testing.T) {
	t.Parallel()
	mgr, runner, store := newManagerForClear(t, "PRIMER")
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:web-x"})
	require.NoError(t, err)

	// Force the session into the "ending" state directly in the store.
	sess.Status = chat.StatusEnding
	require.NoError(t, store.UpdateSession(ctx, sess))

	err = mgr.ClearContext(ctx, sess.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionNotRunning,
		"ending session must return ErrSessionNotRunning, got: %v", err)

	assert.Equal(t, int64(0), runner.sendCalls.Load(), "runner must not be called for an ending session")
}

// TestMarkActive_OnSubscribe_NoDeadlock is a regression test for the
// OnSubscribe → MarkActive → PublishSessionUpdate deadlock. SSEHub.Subscribe
// holds the per-session lock (sh.mu) while invoking OnSubscribe. When the
// session is warm-idle, MarkActive calls publishStatus which now runs the
// hub.PublishSessionUpdate in a separate goroutine to avoid re-entering
// sh.mu on the same thread. Without the goroutine-publish fix, this test
// would hang forever (deadlock) with:
//
//	hub.Subscribe → sh.mu.Lock → OnSubscribe → MarkActive →
//	hub.PublishSessionUpdate → sh.mu.Lock  (deadlock: non-reentrant)
func TestMarkActive_OnSubscribe_NoDeadlock(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hub := chat.NewSSEHub(128)
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		Hub:     hub,
	})

	ctx := context.Background()

	// Create a session and manually set it to warm-idle (container already running).
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "deadlock-test", CreatedBy: "human:test"})
	require.NoError(t, err)

	sess.Status = chat.StatusWarmIdle
	sess.ContainerID = "container-warm"
	require.NoError(t, store.UpdateSession(ctx, sess))

	// Wire OnSubscribe to call MarkActive — exactly as main.go does.
	hub.OnSubscribe = func(sessionID string) {
		reattachSess, reattachErr := mgr.Reattach(ctx, sessionID)
		if reattachErr != nil {
			return
		}

		if reattachSess.Status == chat.StatusWarmIdle {
			_ = mgr.MarkActive(ctx, sessionID)
		}
	}

	// Subscribe in a goroutine; assert it returns within 2 seconds (no deadlock).
	done := make(chan struct{})

	var ch <-chan chat.SSEEvent

	go func() {
		defer close(done)

		var subErr error

		ch, _, subErr = hub.Subscribe(sess.ID, 0)
		if subErr != nil {
			t.Errorf("Subscribe returned error: %v", subErr)
		}
	}()

	select {
	case <-done:
		// Subscribe returned — no deadlock.
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: hub.Subscribe did not return within 2 seconds")
	}

	// Give the goroutine-publish a moment to land.
	var gotUpdate *chat.SSEEvent

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case ev := <-ch:
		gotUpdate = &ev
	case <-timer.C:
		t.Fatal("timeout waiting for session_updated SSE event")
	}

	require.NotNil(t, gotUpdate, "expected a session_updated SSE event")
	require.NotNil(t, gotUpdate.SessionUpdate, "expected SessionUpdate payload")
	require.NotNil(t, gotUpdate.SessionUpdate.Status, "expected Status in SessionUpdate")
	assert.Equal(t, chat.StatusActive, *gotUpdate.SessionUpdate.Status)

	// Confirm the DB row was promoted.
	got, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status, "session must be active after OnSubscribe-triggered MarkActive")
}

// TestOpenSession_WarmIdle_PublishesActive asserts that OpenSession on a
// warm-idle session publishes a session_updated SSE event with status=active.
func TestOpenSession_WarmIdle_PublishesActive(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hub := chat.NewSSEHub(128)
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		Hub:     hub,
	})

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:test"})
	require.NoError(t, err)

	sess.Status = chat.StatusWarmIdle
	sess.ContainerID = "container-warm"
	require.NoError(t, store.UpdateSession(ctx, sess))

	// Subscribe before OpenSession to capture the event.
	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)

	got, err := mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case ev := <-ch:
		require.NotNil(t, ev.SessionUpdate)
		require.NotNil(t, ev.SessionUpdate.Status)
		assert.Equal(t, chat.StatusActive, *ev.SessionUpdate.Status, "warm-idle→active must publish SSE event")
	case <-timer.C:
		t.Fatal("timeout waiting for session_updated SSE event from warm-idle OpenSession")
	}
}

// TestOpenSession_Cold_PublishesActive asserts that OpenSession on a cold
// session publishes a session_updated SSE event with status=active after the
// container is started (cold→active branch in openCold).
func TestOpenSession_Cold_PublishesActive(t *testing.T) {
	t.Parallel()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hub := chat.NewSSEHub(128)
	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		Hub:     hub,
	})

	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "t", CreatedBy: "human:test"})
	require.NoError(t, err)
	// Leave session cold (default).

	// Subscribe before OpenSession to capture the event.
	ch, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)

	got, err := mgr.OpenSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatusActive, got.Status)

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case ev := <-ch:
		require.NotNil(t, ev.SessionUpdate)
		require.NotNil(t, ev.SessionUpdate.Status)
		assert.Equal(t, chat.StatusActive, *ev.SessionUpdate.Status, "cold→active must publish SSE event")
	case <-timer.C:
		t.Fatal("timeout waiting for session_updated SSE event from cold OpenSession")
	}
}

// TestOpenSession_WarmIdle_RaceWith_MarkWarmIdle exercises the per-session
// statusLock across the OpenSession warm-idle→active branch and a concurrent
// MarkWarmIdle call. Without the lock, the two goroutines can interleave their
// read→write windows and leave the row in an inconsistent state.
//
// Each iteration starts with status=active so MarkWarmIdle performs a real
// write (active→warm-idle) rather than a no-op. OpenSession is called
// concurrently; the interleaving exercises both the warm-idle fast path and
// the re-check-after-lock guard. After each pair resolves:
//   - No goroutine panics (the -race detector catches data races).
//   - The final DB row is active or warm-idle (no impossible state).
//   - OpenSession's return value is internally consistent: whenever it
//     returned status=active the ContainerID must be non-empty.
func TestOpenSession_WarmIdle_RaceWith_MarkWarmIdle(t *testing.T) {
	const iterations = 50

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner := &stubRunner{}
	mgr := chat.NewManager(chat.Config{
		Store:   store,
		Runner:  runner,
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
	})

	ctx := context.Background()

	// Create the session once; reset on each iteration.
	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Title: "race-test", CreatedBy: "human:test"})
	require.NoError(t, err)

	for i := range iterations {
		// Start each iteration in active state so MarkWarmIdle performs a
		// real active→warm-idle write rather than a no-op. OpenSession will
		// then race against that write on the warm-idle→active transition.
		sess.Status = chat.StatusActive
		sess.ContainerID = "container-warm"
		require.NoError(t, store.UpdateSession(ctx, sess), "reset iteration %d", i)

		// Immediately flip to warm-idle so OpenSession sees the warm-idle
		// path on entry, while MarkWarmIdle also competes for the same
		// status lock.
		sess.Status = chat.StatusWarmIdle
		require.NoError(t, store.UpdateSession(ctx, sess), "warm-idle reset iteration %d", i)

		var (
			wg      sync.WaitGroup
			openRet chat.Session
		)

		wg.Add(2)

		// Goroutine 1: OpenSession (warm-idle → active path, or drift path).
		go func() {
			defer wg.Done()

			openRet, _ = mgr.OpenSession(ctx, sess.ID)
		}()

		// Goroutine 2: MarkWarmIdle (active→warm-idle when racing, no-op
		// when it sees warm-idle/cold). Pre-arm the DB with active so the
		// call has real work to do on at least some iterations.
		go func() {
			defer wg.Done()

			// Flip to active directly in the store so MarkWarmIdle races
			// with OpenSession for real.
			active := sess
			active.Status = chat.StatusActive
			_ = store.UpdateSession(ctx, active)

			_ = mgr.MarkWarmIdle(ctx, sess.ID)
		}()

		wg.Wait()

		// Post-condition 1: DB row must be in a valid state.
		got, err := store.GetSession(ctx, sess.ID)
		require.NoError(t, err, "iteration %d: GetSession", i)
		assert.True(t,
			got.Status == chat.StatusActive || got.Status == chat.StatusWarmIdle,
			"iteration %d: DB status must be active or warm-idle, got %q", i, got.Status)

		// Post-condition 2: if OpenSession returned active, ContainerID
		// must be non-empty — an active session without a container is
		// the inconsistency the statusLock guards against.
		if openRet.Status == chat.StatusActive {
			assert.NotEmpty(t, openRet.ContainerID,
				"iteration %d: OpenSession returned active but ContainerID is empty", i)
		}
	}
}
