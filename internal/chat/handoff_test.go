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
