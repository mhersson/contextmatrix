package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

func newHandoffTestManager(t *testing.T, withArchitect bool) *chat.Manager {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cfg := chat.Config{
		Store:   store,
		Backend: &chatStubBackend{},
		Clock:   clock.Real(),
		IdleTTL: time.Hour,
		ResolveProjectExec: func(_ context.Context, project string) (chat.ProjectExecInfo, error) {
			if project != "alpha" {
				return chat.ProjectExecInfo{}, chat.ErrSessionNotFound
			}

			return chat.ProjectExecInfo{}, nil
		},
	}
	if withArchitect {
		cfg.ArchitectBackend = &chatStubBackend{}
	}

	mgr := chat.NewManager(cfg)

	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	return mgr
}

func TestHandOffToArchitect_PostsIntoTheCallingChat(t *testing.T) {
	t.Parallel()

	mgr := newHandoffTestManager(t, true)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "alpha", CreatedBy: "human:t"})
	require.NoError(t, err)

	tool := buildHandOffToArchitectTool(mgr)
	_, out, err := tool(mcpcontext.WithChatSession(ctx, sess.ID), nil, handOffInput{
		Title: "Plan", Need: "do the thing", Projects: []string{"alpha"},
	})
	require.NoError(t, err)
	require.True(t, out.OK)
	require.Positive(t, out.Seq)

	msgs, err := mgr.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)

	var handoffs []chat.Message

	for _, m := range msgs {
		if m.Kind == chat.EventKindHandoff {
			handoffs = append(handoffs, m)
		}
	}

	require.Len(t, handoffs, 1)
	assert.Equal(t, out.Seq, handoffs[0].Seq)
}

func TestHandOffToArchitect_RefusesWithoutAChatHeader(t *testing.T) {
	t.Parallel()

	mgr := newHandoffTestManager(t, true)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "alpha", CreatedBy: "human:t"})
	require.NoError(t, err)

	tool := buildHandOffToArchitectTool(mgr)
	_, _, err = tool(ctx, nil, handOffInput{Title: "Plan", Need: "n", Projects: []string{"alpha"}})
	require.Error(t, err)
	// Refused before reaching the manager, which would report an unknown session.
	require.NotErrorIs(t, err, chat.ErrSessionNotFound)

	msgs, err := mgr.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)

	for _, m := range msgs {
		assert.NotEqual(t, chat.EventKindHandoff, m.Kind)
	}
}

func TestHandOffToArchitect_PassesManagerRefusalsThrough(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	valid := handOffInput{Title: "Plan", Need: "n", Projects: []string{"alpha"}}

	t.Run("blueprint session", func(t *testing.T) {
		t.Parallel()

		mgr := newHandoffTestManager(t, true)
		arch, err := mgr.CreateSession(ctx, chat.CreateInput{
			Kind: chat.KindArchitect, Projects: []string{"alpha"}, CreatedBy: "human:t",
		})
		require.NoError(t, err)

		_, _, err = buildHandOffToArchitectTool(mgr)(mcpcontext.WithChatSession(ctx, arch.ID), nil, valid)
		require.ErrorIs(t, err, chat.ErrInvalidSessionInput)
	})

	t.Run("no architect backend", func(t *testing.T) {
		t.Parallel()

		mgr := newHandoffTestManager(t, false)
		sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "alpha", CreatedBy: "human:t"})
		require.NoError(t, err)

		_, _, err = buildHandOffToArchitectTool(mgr)(mcpcontext.WithChatSession(ctx, sess.ID), nil, valid)
		require.ErrorIs(t, err, chat.ErrKindUnavailable)
	})

	t.Run("need too large", func(t *testing.T) {
		t.Parallel()

		mgr := newHandoffTestManager(t, true)
		sess, err := mgr.CreateSession(ctx, chat.CreateInput{Project: "alpha", CreatedBy: "human:t"})
		require.NoError(t, err)

		big := valid
		big.Need = strings.Repeat("x", chat.MaxBlueprintMessageBytes+1)

		_, _, err = buildHandOffToArchitectTool(mgr)(mcpcontext.WithChatSession(ctx, sess.ID), nil, big)
		require.ErrorIs(t, err, chat.ErrMessageTooLarge)
	})
}

func TestNewServer_RegistersHandOffWithAChatManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr := newHandoffTestManager(t, true)

	has := func(env *testEnv) bool {
		res, err := env.session.ListTools(ctx, nil)
		require.NoError(t, err)

		for _, tl := range res.Tools {
			if tl.Name == "hand_off_to_architect" {
				return true
			}
		}

		return false
	}

	assert.True(t, has(setupMCPWithPlaybooks(t, func(c *ServerConfig) { c.ChatManager = mgr })))
	assert.False(t, has(setupMCP(t)))
}
