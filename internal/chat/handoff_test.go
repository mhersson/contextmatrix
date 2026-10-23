package chat_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

func newHandoffManager(t *testing.T) (*chat.Manager, chat.Session, *chat.SSEHub) {
	t.Helper()

	hub := chat.NewSSEHub(128)
	mgr, _, _ := newManagerWithArchitectStub(t, func(c *chat.Config) { c.Hub = hub })

	sess, err := mgr.CreateSession(context.Background(), chat.CreateInput{CreatedBy: "human:web"})
	require.NoError(t, err)

	return mgr, sess, hub
}

func validHandoff() chat.Handoff {
	return chat.Handoff{Title: "Card time budgets", Need: "## Need\nbudgets", Projects: []string{"alpha"}}
}

func TestPostHandoff_PostsASystemRowAndPublishesIt(t *testing.T) {
	mgr, sess, hub := newHandoffManager(t)
	ctx := context.Background()

	events, _, err := hub.Subscribe(sess.ID, 0)
	require.NoError(t, err)

	h := validHandoff()
	h.Projects = []string{"alpha", "beta", "alpha"}

	msg, err := mgr.PostHandoff(ctx, sess.ID, h)
	require.NoError(t, err)
	assert.Equal(t, chat.RoleSystem, msg.Role)
	assert.Equal(t, chat.EventKindHandoff, msg.Kind)

	var got chat.Handoff
	require.NoError(t, json.Unmarshal([]byte(msg.Content), &got))
	assert.Equal(t, []string{"alpha", "beta"}, got.Projects)
	assert.Equal(t, h.Title, got.Title)
	assert.Equal(t, h.Need, got.Need)

	select {
	case ev := <-events:
		assert.Equal(t, chat.EventKindHandoff, ev.DataKind)
		assert.Equal(t, msg.Seq, ev.Seq)
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}

	after, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Empty(t, after.Title)
}

func TestPostHandoff_StoresAWorstCaseNeedWhole(t *testing.T) {
	mgr, sess, _ := newHandoffManager(t)

	need := strings.Repeat("<\"\\\x01", chat.MaxBlueprintMessageBytes/4)
	require.Len(t, need, chat.MaxBlueprintMessageBytes)

	h := validHandoff()
	h.Need = need

	msg, err := mgr.PostHandoff(context.Background(), sess.ID, h)
	require.NoError(t, err)
	assert.Greater(t, len(msg.Content), 32*1024)

	var got chat.Handoff
	require.NoError(t, json.Unmarshal([]byte(msg.Content), &got))
	assert.Equal(t, need, got.Need)
}

func TestPostHandoff_Refusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*chat.Handoff)
		want error
	}{
		{"empty title", func(h *chat.Handoff) { h.Title = "  " }, chat.ErrInvalidSessionInput},
		{"title over 256 bytes", func(h *chat.Handoff) { h.Title = strings.Repeat("t", 257) }, chat.ErrInvalidSessionInput},
		{"empty need", func(h *chat.Handoff) { h.Need = "\n" }, chat.ErrInvalidSessionInput},
		{"need over the limit", func(h *chat.Handoff) { h.Need = strings.Repeat("n", chat.MaxBlueprintMessageBytes+1) }, chat.ErrMessageTooLarge},
		{"no projects", func(h *chat.Handoff) { h.Projects = nil }, chat.ErrInvalidSessionInput},
		{"unknown project", func(h *chat.Handoff) { h.Projects = []string{"alpha", "nope"} }, chat.ErrInvalidSessionInput},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, sess, _ := newHandoffManager(t)
			ctx := context.Background()

			h := validHandoff()
			tc.edit(&h)

			_, err := mgr.PostHandoff(ctx, sess.ID, h)
			require.ErrorIs(t, err, tc.want)

			rows, err := mgr.ListMessages(ctx, sess.ID, 0, 10)
			require.NoError(t, err)
			assert.Empty(t, rows)
		})
	}
}

func TestPostHandoff_OnlyFromAPlainChatWithAnArchitect(t *testing.T) {
	ctx := context.Background()

	mgr, _, _ := newHandoffManager(t)

	arch, err := mgr.CreateSession(ctx, chat.CreateInput{
		Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: "human:web",
	})
	require.NoError(t, err)

	_, err = mgr.PostHandoff(ctx, arch.ID, validHandoff())
	require.ErrorIs(t, err, chat.ErrInvalidSessionInput)

	rows, err := mgr.ListMessages(ctx, arch.ID, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)

	_, err = mgr.PostHandoff(ctx, "missing", validHandoff())
	require.ErrorIs(t, err, chat.ErrSessionNotFound)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	plain := newManager(t, chat.Config{Store: store, Backend: &stubBackend{}, Clock: clock.Real(), IdleTTL: time.Hour})

	sess, err := plain.CreateSession(ctx, chat.CreateInput{CreatedBy: "human:web"})
	require.NoError(t, err)

	_, err = plain.PostHandoff(ctx, sess.ID, validHandoff())
	require.ErrorIs(t, err, chat.ErrKindUnavailable)

	rows, err = plain.ListMessages(ctx, sess.ID, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func architectFromHandoff(chatID string, seq int64) chat.CreateInput {
	return chat.CreateInput{
		Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: "human:web",
		OriginChat: chatID, OriginSeq: seq,
	}
}

func TestCreateSession_FromAHandoff(t *testing.T) {
	ctx := context.Background()
	mgr, origin, _ := newHandoffManager(t)

	row, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)

	sess, err := mgr.CreateSession(ctx, architectFromHandoff(origin.ID, row.Seq))
	require.NoError(t, err)
	assert.Equal(t, origin.ID, sess.OriginChat)
	assert.Equal(t, row.Seq, sess.OriginSeq)

	got, err := mgr.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, origin.ID, got.OriginChat)
	assert.Equal(t, row.Seq, got.OriginSeq)
}

func TestCreateSession_OriginTakenOnce(t *testing.T) {
	ctx := context.Background()
	mgr, origin, _ := newHandoffManager(t)

	row, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)

	first, err := mgr.CreateSession(ctx, architectFromHandoff(origin.ID, row.Seq))
	require.NoError(t, err)

	_, err = mgr.CreateSession(ctx, architectFromHandoff(origin.ID, row.Seq))
	require.ErrorIs(t, err, chat.ErrHandoffTaken)

	require.NoError(t, mgr.DeleteSession(ctx, first.ID))

	_, err = mgr.CreateSession(ctx, architectFromHandoff(origin.ID, row.Seq))
	require.NoError(t, err)
}

func TestCreateSession_ReplacedHandoffRefused(t *testing.T) {
	ctx := context.Background()
	mgr, origin, _ := newHandoffManager(t)

	s1, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)
	s2, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)
	require.Greater(t, s2.Seq, s1.Seq)

	_, err = mgr.CreateSession(ctx, architectFromHandoff(origin.ID, s1.Seq))
	require.ErrorIs(t, err, chat.ErrHandoffReplaced)

	_, err = mgr.CreateSession(ctx, architectFromHandoff(origin.ID, s2.Seq))
	require.NoError(t, err)
}

func TestCreateSession_InvalidOrigin(t *testing.T) {
	ctx := context.Background()
	mgr, origin, _ := newHandoffManager(t)

	row, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)

	arch, err := mgr.CreateSession(ctx, chat.CreateInput{
		Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: "human:web",
	})
	require.NoError(t, err)

	bare, err := mgr.CreateSession(ctx, chat.CreateInput{CreatedBy: "human:web"})
	require.NoError(t, err)

	tests := []struct {
		name string
		in   chat.CreateInput
	}{
		{"origin on a chat-kind create", chat.CreateInput{CreatedBy: "human:web", OriginChat: origin.ID, OriginSeq: row.Seq}},
		{"unknown origin", architectFromHandoff("missing", row.Seq)},
		{"origin is an architect session", architectFromHandoff(arch.ID, row.Seq)},
		{"origin has no handoff row", architectFromHandoff(bare.ID, 1)},
		{"zero seq", architectFromHandoff(origin.ID, 0)},
		{"seq without origin", architectFromHandoff("", row.Seq)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := mgr.ListSessions(ctx, chat.SessionFilter{})
			require.NoError(t, err)

			_, err = mgr.CreateSession(ctx, tt.in)
			require.ErrorIs(t, err, chat.ErrInvalidSessionInput)

			after, err := mgr.ListSessions(ctx, chat.SessionFilter{})
			require.NoError(t, err)
			assert.Len(t, after, len(before))
		})
	}
}

// handedOverBlueprint returns a manager and its architect stub with a
// blueprint started from a hand-off.
func handedOverBlueprint(t *testing.T) (*chat.Manager, *stubBackend, chat.Session) {
	t.Helper()

	ctx := context.Background()
	mgr, _, arch := newManagerWithArchitectStub(t)

	origin, err := mgr.CreateSession(ctx, chat.CreateInput{CreatedBy: "human:web"})
	require.NoError(t, err)

	row, err := mgr.PostHandoff(ctx, origin.ID, validHandoff())
	require.NoError(t, err)

	bp, err := mgr.CreateSession(ctx, architectFromHandoff(origin.ID, row.Seq))
	require.NoError(t, err)

	return mgr, arch, bp
}

func handoffNeedRows(t *testing.T, mgr *chat.Manager, sessionID string) []chat.Message {
	t.Helper()

	rows, err := mgr.ListMessages(context.Background(), sessionID, 0, 100)
	require.NoError(t, err)

	var needs []chat.Message

	for _, r := range rows {
		if r.Kind == chat.EventKindHandoffNeed {
			needs = append(needs, r)
		}
	}

	return needs
}

func TestSendHandoffNeed_SecondCallIsANoOp(t *testing.T) {
	ctx := context.Background()
	mgr, arch, bp := handedOverBlueprint(t)
	need := validHandoff().Need

	id, err := mgr.SendHandoffNeed(ctx, bp.ID, need)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	again, err := mgr.SendHandoffNeed(ctx, bp.ID, need)
	require.NoError(t, err)
	assert.Empty(t, again)

	assert.Equal(t, int64(1), arch.sendCalls.Load())

	arch.mu.Lock()
	sent := append([]sendArg(nil), arch.sendArgs...)
	arch.mu.Unlock()
	require.Len(t, sent, 1)
	assert.Equal(t, need, sent[0].Content)
	assert.Equal(t, id, sent[0].MessageID)

	rows, err := mgr.ListMessages(ctx, bp.ID, 0, 100)
	require.NoError(t, err)

	var users []chat.Message

	for _, r := range rows {
		if r.Role == chat.RoleUser {
			users = append(users, r)
		}
	}

	require.Len(t, users, 1)
	assert.Equal(t, chat.EventKindHandoffNeed, users[0].Kind)
	assert.Equal(t, need, users[0].Content)
}

func TestSendHandoffNeed_ConcurrentSendsPostOnce(t *testing.T) {
	ctx := context.Background()
	mgr, arch, bp := handedOverBlueprint(t)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	arch.sendFn = func() {
		if arch.sendCalls.Load() == 1 {
			entered <- struct{}{}

			<-release
		}
	}

	type result struct {
		id  string
		err error
	}

	results := make(chan result, 2)
	send := func() {
		id, err := mgr.SendHandoffNeed(ctx, bp.ID, "need")
		results <- result{id, err}
	}

	go send()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the first send never reached the backend")
	}

	go send()

	// Let the second caller reach the need lock while the first is inside the send.
	time.Sleep(50 * time.Millisecond)
	close(release)

	var ids []string

	for range 2 {
		select {
		case r := <-results:
			require.NoError(t, r.err)

			ids = append(ids, r.id)
		case <-time.After(5 * time.Second):
			t.Fatal("a send never returned")
		}
	}

	assert.Equal(t, int64(1), arch.sendCalls.Load())
	assert.Len(t, handoffNeedRows(t, mgr, bp.ID), 1)
	assert.ElementsMatch(t, []bool{true, false}, []bool{ids[0] != "", ids[1] != ""})
}

func TestSendHandoffNeed_NeedsAHandedOverBlueprint(t *testing.T) {
	ctx := context.Background()
	mgr, chatStub, arch := newManagerWithArchitectStub(t)

	bare, err := mgr.CreateSession(ctx, chat.CreateInput{
		Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: "human:web",
	})
	require.NoError(t, err)

	plain, err := mgr.CreateSession(ctx, chat.CreateInput{CreatedBy: "human:web"})
	require.NoError(t, err)

	for name, id := range map[string]string{"blueprint without origin": bare.ID, "plain chat": plain.ID} {
		t.Run(name, func(t *testing.T) {
			_, err := mgr.SendHandoffNeed(ctx, id, "need")
			require.ErrorIs(t, err, chat.ErrInvalidSessionInput)
			assert.Empty(t, handoffNeedRows(t, mgr, id))
		})
	}

	assert.Equal(t, int64(0), arch.sendCalls.Load())
	assert.Equal(t, int64(0), chatStub.sendCalls.Load())
}

func TestSendHandoffNeed_OverTheBlueprintLimitIsRefused(t *testing.T) {
	ctx := context.Background()
	mgr, arch, bp := handedOverBlueprint(t)
	tooLarge := strings.Repeat("a", chat.MaxBlueprintMessageBytes+1)

	_, err := mgr.SendHandoffNeed(ctx, bp.ID, tooLarge)
	require.ErrorIs(t, err, chat.ErrMessageTooLarge)
	assert.Equal(t, int64(0), arch.sendCalls.Load())
	assert.Empty(t, handoffNeedRows(t, mgr, bp.ID))

	_, err = mgr.SendHandoffNeed(ctx, bp.ID, "need")
	require.NoError(t, err)

	_, err = mgr.SendHandoffNeed(ctx, bp.ID, tooLarge)
	require.ErrorIs(t, err, chat.ErrMessageTooLarge, "a repeat is refused over the limit too")
	assert.Equal(t, int64(1), arch.sendCalls.Load())
}
