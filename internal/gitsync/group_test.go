package gitsync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/storage"
)

func TestGroup_StatusesListEveryRepoInOrder(t *testing.T) {
	a, _, _ := setupSharedPair(t)

	hidden := func() []storage.HiddenProject {
		return []storage.HiddenProject{{Name: "alpha", Repo: "private", VisibleIn: "team"}}
	}

	g := NewGroup(hidden, GroupEntry{Name: "team", Syncer: a.syncer}, GroupEntry{Name: "private"})
	assert.True(t, g.Enabled())

	statuses := g.Statuses()
	require.Len(t, statuses, 2)

	assert.Equal(t, "team", statuses[0].Repo)
	assert.True(t, statuses[0].Enabled)
	assert.True(t, statuses[0].Shared)
	assert.Empty(t, statuses[0].HiddenProjects)

	assert.Equal(t, "private", statuses[1].Repo)
	assert.False(t, statuses[1].Enabled)
	assert.Equal(t, []string{"alpha"}, statuses[1].HiddenProjects)
}

func TestGroup_TriggerSyncRoutesByName(t *testing.T) {
	a, _, _ := setupSharedPair(t)
	ctx := context.Background()

	g := NewGroup(nil, GroupEntry{Name: "team", Syncer: a.syncer}, GroupEntry{Name: "private"})

	require.ErrorIs(t, g.TriggerSync(ctx, "private"), ErrSyncDisabled)
	require.ErrorIs(t, g.TriggerSync(ctx, "nope"), storage.ErrUnknownRepo)

	require.NoError(t, g.TriggerSync(ctx, "team"))
	assert.NotNil(t, a.syncer.Status().LastSyncTime)

	require.NoError(t, g.TriggerSync(ctx, ""), "empty runs every enabled repo")

	none := NewGroup(nil, GroupEntry{Name: "private"})
	assert.False(t, none.Enabled())
	require.ErrorIs(t, none.TriggerSync(ctx, ""), ErrSyncDisabled)
	assert.Equal(t, "private", none.Statuses()[0].Repo)
}
