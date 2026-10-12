package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func TestJiraWriteback_Schema(t *testing.T) {
	s := openTestStore(t)

	assert.True(t, tableExists(t, s.db, "jira_writeback"))
	assert.True(t, indexExists(t, s.db, "idx_jira_writeback_due"))
}

func TestJiraWriteback_EnqueueIsFirstEntryOnly(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		project      string
		card         string
		kind         string
		wantInserted bool
	}{
		{name: "first started", project: "api", card: "API-001", kind: "started", wantInserted: true},
		{name: "re-entry into in_progress", project: "api", card: "API-001", kind: "started", wantInserted: false},
		{name: "done for same card", project: "api", card: "API-001", kind: "done", wantInserted: true},
		{name: "same card id other project", project: "web", card: "API-001", kind: "started", wantInserted: true},
	}

	for _, tt := range tests {
		inserted, err := s.EnqueueJiraWriteback(ctx, tt.project, tt.card, tt.kind, "PROJ-1", 100)
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.wantInserted, inserted, tt.name)
	}

	require.NoError(t, s.FinishJiraWriteback(ctx, "api", "API-001", "done", JiraWritebackDone, "", 110))

	inserted, err := s.EnqueueJiraWriteback(ctx, "api", "API-001", "done", "PROJ-1", 120)
	require.NoError(t, err)
	assert.False(t, inserted, "a reopened card reaching done again writes nothing")

	_, err = s.EnqueueJiraWriteback(ctx, "api", "API-002", "review", "PROJ-2", 100)
	require.Error(t, err, "kind is constrained")
}

func TestJiraWriteback_EnqueueKeysOnIssue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	inserted, err := s.EnqueueJiraWriteback(ctx, "api", "API-001", "done", "PROJ-1", 100)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, s.RetryJiraWriteback(ctx, "api", "API-001", "done", 2, 150, "boom", 140))

	inserted, err = s.EnqueueJiraWriteback(ctx, "api", "API-001", "done", "PROJ-1", 200)
	require.NoError(t, err)
	assert.False(t, inserted, "same issue: row untouched")

	row, _, err := s.GetJiraWriteback(ctx, "api", "API-001", "done")
	require.NoError(t, err)
	assert.Equal(t, 2, row.Attempts)
	assert.Equal(t, int64(100), row.CreatedAt)

	require.NoError(t, s.FinishJiraWriteback(ctx, "api", "API-001", "done", JiraWritebackDone, "", 210))

	inserted, err = s.EnqueueJiraWriteback(ctx, "api", "API-001", "done", "OTHER-9", 300)
	require.NoError(t, err)
	assert.True(t, inserted, "a recreated project reusing the card id points at a new issue")

	row, _, err = s.GetJiraWriteback(ctx, "api", "API-001", "done")
	require.NoError(t, err)
	assert.Equal(t, JiraWriteback{
		Project: "api", CardID: "API-001", Kind: "done", IssueKey: "OTHER-9", State: JiraWritebackPending,
		NextAttemptAt: 300, CreatedAt: 300, UpdatedAt: 300,
	}, row)
}

func TestJiraWriteback_DueRetryFinish(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for i, card := range []string{"A-001", "A-002", "A-003"} {
		_, err := s.EnqueueJiraWriteback(ctx, "a", card, "started", "PROJ-1", int64(100+i))
		require.NoError(t, err)
	}

	due, err := s.DueJiraWritebacks(ctx, 101, 10)
	require.NoError(t, err)
	require.Len(t, due, 2, "only rows whose next attempt has come")
	assert.Equal(t, "A-001", due[0].CardID, "oldest first")

	due, err = s.DueJiraWritebacks(ctx, 1000, 1)
	require.NoError(t, err)
	assert.Len(t, due, 1, "limit applies")

	require.NoError(t, s.RetryJiraWriteback(ctx, "a", "A-001", "started", 1, 500, "status 503", 200))

	row, ok, err := s.GetJiraWriteback(ctx, "a", "A-001", "started")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, JiraWriteback{
		Project: "a", CardID: "A-001", Kind: "started", IssueKey: "PROJ-1", State: JiraWritebackPending,
		Attempts: 1, NextAttemptAt: 500, LastError: "status 503", CreatedAt: 100, UpdatedAt: 200,
	}, row)

	pending, err := s.HasPendingJiraWriteback(ctx, "a", "A-001", "started")
	require.NoError(t, err)
	assert.True(t, pending)

	require.NoError(t, s.FinishJiraWriteback(ctx, "a", "A-001", "started", JiraWritebackFailed, "gave up", 300))

	pending, err = s.HasPendingJiraWriteback(ctx, "a", "A-001", "started")
	require.NoError(t, err)
	assert.False(t, pending)

	due, err = s.DueJiraWritebacks(ctx, 10_000, 10)
	require.NoError(t, err)
	assert.Len(t, due, 2, "finished rows are never due")

	require.NoError(t, s.RetryJiraWriteback(ctx, "a", "A-001", "started", 9, 1, "late", 400))

	row, _, err = s.GetJiraWriteback(ctx, "a", "A-001", "started")
	require.NoError(t, err)
	assert.Equal(t, JiraWritebackFailed, row.State, "a finished row is not reopened by a stray retry")

	_, ok, err = s.GetJiraWriteback(ctx, "a", "NOPE-1", "started")
	require.NoError(t, err)
	assert.False(t, ok)
}
