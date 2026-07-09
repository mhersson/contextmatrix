package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
)

func TestOpen_CreatesSchema(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	sess := chat.Session{
		ID:         chat.NewID(),
		Title:      "test",
		Project:    "alpha",
		Status:     chat.StatusCold,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		LastActive: time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "human:web-abc",
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.Title, got.Title)
	assert.Equal(t, sess.Project, got.Project)
	assert.Equal(t, sess.Status, got.Status)
	assert.Equal(t, sess.CreatedBy, got.CreatedBy)
}

func TestOpen_IsIdempotent(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s1, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
}

func TestAppendAndList_Messages(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	sess := chat.Session{
		ID: chat.NewID(), Title: "t", Status: chat.StatusActive,
		CreatedAt: time.Now().UTC(), LastActive: time.Now().UTC(),
		CreatedBy: "human:web-x",
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	for i, body := range []string{"hello", "world", "claude"} {
		seq, err := s.AppendMessage(ctx, chat.Message{
			SessionID: sess.ID,
			Seq:       int64(i + 1),
			Role:      chat.RoleUser,
			Content:   `{"text":"` + body + `"}`,
			CreatedAt: time.Now().UTC(),
		})
		require.NoError(t, err)
		assert.Equal(t, int64(i+1), seq)
	}

	msgs, err := s.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, int64(1), msgs[0].Seq)
	assert.Equal(t, int64(3), msgs[2].Seq)

	msgs2, err := s.ListMessages(ctx, sess.ID, 1, 100)
	require.NoError(t, err)
	require.Len(t, msgs2, 2)
	assert.Equal(t, int64(2), msgs2[0].Seq)
}

func TestDeleteSession_CascadesMessages(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	sess := chat.Session{
		ID: chat.NewID(), Title: "t", Status: chat.StatusCold,
		CreatedAt: time.Now().UTC(), LastActive: time.Now().UTC(), CreatedBy: "x",
	}
	require.NoError(t, s.CreateSession(ctx, sess))
	_, err = s.AppendMessage(ctx, chat.Message{SessionID: sess.ID, Seq: 1, Role: chat.RoleUser, Content: "{}", CreatedAt: time.Now().UTC()})
	require.NoError(t, err)

	require.NoError(t, s.DeleteSession(ctx, sess.ID))
	msgs, err := s.ListMessages(ctx, sess.ID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

// TestSqliteDSN_PathFormats guards against a regression where url.URL.String()
// places a relative path in the authority component (e.g. `file://chats.db`),
// causing modernc.org/sqlite to error at first query with
// "invalid uri authority". Both absolute and relative paths must produce a
// DSN with no authority component, and must carry the synchronous=NORMAL
// pragma alongside journal_mode=WAL. Mirrors the images package guard.
func TestSqliteDSN_PathFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"absolute path", "/tmp/chats.db", "file:/tmp/chats.db?"},
		{"relative path", "chats.db", "file:chats.db?"},
		{"nested relative path", "data/chats.db", "file:data/chats.db?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dsn := sqliteDSN(tc.path)

			// Reject the broken `file://...` (authority) form.
			assert.False(t, strings.HasPrefix(dsn, "file://"),
				"DSN must not place path in authority component: %q", dsn)
			assert.True(t, strings.HasPrefix(dsn, tc.want),
				"DSN must start with %q, got %q", tc.want, dsn)
			assert.Contains(t, dsn, "_pragma=foreign_keys(1)")
			assert.Contains(t, dsn, "_pragma=journal_mode(WAL)")
			assert.Contains(t, dsn, "_pragma=synchronous(NORMAL)")
			assert.Contains(t, dsn, "_pragma=busy_timeout(5000)")
		})
	}
}

// TestStore_OpenRelativePath verifies the store opens cleanly when given a
// path relative to the current working directory. This is the scenario that
// a url.URL-based DSN would break (it would produce `file://chats.db?...`,
// which modernc/sqlite rejects at first query).
func TestStore_OpenRelativePath(t *testing.T) {
	// Not parallel: mutates the process-wide working directory.
	dir := t.TempDir()

	origWD, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))

	t.Cleanup(func() { _ = os.Chdir(origWD) })

	s, err := Open("chats.db")
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	// Smoke-test that the database is actually usable, not just open.
	ctx := context.Background()
	sess := chat.Session{
		ID:         chat.NewID(),
		Title:      "relpath",
		Status:     chat.StatusCold,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		LastActive: time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "human:test",
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
}

func TestStore_ListMessagesTail_ReturnsNewestNInChronologicalOrder(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	sessionID := chat.NewID()
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID:         sessionID,
		Title:      "tail-test",
		Status:     chat.StatusCold,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		LastActive: time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "human:test",
	}))

	// Insert 50 messages with seq 1..50.
	for i := 1; i <= 50; i++ {
		_, err := store.AppendMessage(ctx, chat.Message{
			SessionID: sessionID,
			Seq:       int64(i),
			Role:      chat.RoleUser,
			Content:   fmt.Sprintf(`{"text":"m%d"}`, i),
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		})
		require.NoError(t, err)
	}

	msgs, err := store.ListMessagesTail(ctx, sessionID, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 10)

	// Newest 10 are seq 41..50, returned in ASC order.
	for i, m := range msgs {
		require.Equal(t, int64(41+i), m.Seq, "row %d", i)
	}
}

// newTestSession is a helper that creates a minimal session and returns its ID.
func newTestSession(t *testing.T, store *Store) string {
	t.Helper()

	ctx := context.Background()
	sessionID := chat.NewID()
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID:         sessionID,
		Title:      "test",
		Status:     chat.StatusCold,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		LastActive: time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "human:test",
	}))

	return sessionID
}

func TestIncrementSessionCost_HappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	sessionID := newTestSession(t, store)

	// First frame: 100 prompt, 50 completion, 200 cache_read, 10 cache_creation, $0.01
	p, c, cr, cc, cost, err := store.IncrementSessionCost(ctx, sessionID, 100, 50, 200, 10, 0.01, "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, int64(100), p)
	assert.Equal(t, int64(50), c)
	assert.Equal(t, int64(200), cr)
	assert.Equal(t, int64(10), cc)
	assert.InDelta(t, 0.01, cost, 1e-9)

	// Second frame: another 100 prompt, 50 completion — totals double.
	p2, c2, cr2, cc2, cost2, err := store.IncrementSessionCost(ctx, sessionID, 100, 50, 0, 0, 0.01, "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, int64(200), p2)
	assert.Equal(t, int64(100), c2)
	assert.Equal(t, int64(200), cr2)
	assert.Equal(t, int64(10), cc2)
	assert.InDelta(t, 0.02, cost2, 1e-9)

	// Verify the row also reflects the totals via GetSession.
	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(200), sess.PromptTokens)
	assert.Equal(t, int64(100), sess.CompletionTokens)
	assert.InDelta(t, 0.02, sess.EstimatedCostUSD, 1e-9)
}

func TestIncrementSessionCost_NotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, "nonexistent-session", 100, 50, 0, 0, 0.01, "claude-sonnet-4-6")
	require.ErrorIs(t, err, chat.ErrSessionNotFound)
}

func TestIncrementSessionCost_ConcurrentIncrementsAreRaceFree(t *testing.T) {
	// N goroutines each increment by 1 token and $0.001. The final totals must
	// equal N×1 and N×0.001 exactly, proving the UPDATE is atomic.
	const N = 20

	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	sessionID := newTestSession(t, store)

	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			_, _, _, _, _, err := store.IncrementSessionCost(ctx, sessionID, 1, 0, 0, 0, 0.001, "claude-sonnet-4-6")
			assert.NoError(t, err)
		})
	}

	wg.Wait()

	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(N), sess.PromptTokens, "all increments must be visible")
	assert.InDelta(t, float64(N)*0.001, sess.EstimatedCostUSD, 1e-6)
}

func TestIncrementSessionCost_EmptyModelPreservesExisting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	// Seed session with a known model.
	sessionID := chat.NewID()
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID:         sessionID,
		Title:      "model-preserve-test",
		Status:     chat.StatusCold,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		LastActive: time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "human:test",
		Model:      "claude-sonnet-4-6",
	}))

	// Call with empty model — existing column value must be preserved.
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, sessionID, 10, 5, 0, 0, 0.001, "")
	require.NoError(t, err)

	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-6", sess.Model, "empty model must not overwrite existing value")

	// Call with a real model — column must be updated.
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, sessionID, 10, 5, 0, 0, 0.001, "claude-opus-4-7")
	require.NoError(t, err)

	sess, err = store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-7", sess.Model, "non-empty model must update the column")
}

// newTestSessionAtTime creates a session with a specific last_active timestamp.
func newTestSessionAtTime(t *testing.T, store *Store, lastActive time.Time, cost float64) string {
	t.Helper()

	ctx := context.Background()
	sessionID := chat.NewID()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID:         sessionID,
		Title:      "test-cost",
		Status:     chat.StatusCold,
		CreatedAt:  now,
		LastActive: lastActive,
		CreatedBy:  "human:test",
	}))

	if cost > 0 {
		_, _, _, _, _, err := store.IncrementSessionCost(ctx, sessionID, 10, 5, 0, 0, cost, "claude-sonnet-4-6")
		require.NoError(t, err)
	}

	return sessionID
}

func TestAggregateCost_HappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	// Use a fixed "now" for deterministic bucket indices.
	now := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := todayStart.Add(24 * time.Hour)
	since := todayStart.AddDate(0, 0, -29)

	// Session A: today (last_active = now-2h). Cost $1.00. -> series[29]
	newTestSessionAtTime(t, store, now.Add(-2*time.Hour), 1.00)

	// Session B: 10 days ago. Cost $2.00. -> series[19] (29-10)
	newTestSessionAtTime(t, store, todayStart.Add(-10*24*time.Hour).Add(6*time.Hour), 2.00)

	// Session C: 20 days ago. Cost $3.00. -> series[9] (29-20)
	newTestSessionAtTime(t, store, todayStart.Add(-20*24*time.Hour).Add(6*time.Hour), 3.00)

	last30d, prior30d, series30d, err := store.AggregateCost(ctx, since, until)
	require.NoError(t, err)

	// Total last30d = 1 + 2 + 3 = 6
	assert.InDelta(t, 6.00, last30d, 1e-9, "last30d should be sum of all three")
	assert.InDelta(t, 0.0, prior30d, 1e-9, "prior30d should be 0")

	require.Len(t, series30d, 30)
	assert.InDelta(t, 1.00, series30d[29], 1e-9, "today's session at series[29]")
	assert.InDelta(t, 2.00, series30d[19], 1e-9, "10-days-ago session at series[19]")
	assert.InDelta(t, 3.00, series30d[9], 1e-9, "20-days-ago session at series[9]")

	// All other buckets zero.
	for i, v := range series30d {
		if i != 9 && i != 19 && i != 29 {
			assert.InDelta(t, 0.0, v, 1e-9, "bucket %d should be zero", i)
		}
	}

	// sum(series30d) must equal last30d.
	var seriesSum float64
	for _, v := range series30d {
		seriesSum += v
	}

	assert.InDelta(t, last30d, seriesSum, 1e-9, "sum(series30d) must equal last30d")
}

func TestAggregateCost_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := todayStart.Add(24 * time.Hour)
	since := todayStart.AddDate(0, 0, -29)

	last30d, prior30d, series30d, err := store.AggregateCost(ctx, since, until)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, last30d, 1e-9)
	assert.InDelta(t, 0.0, prior30d, 1e-9)
	require.Len(t, series30d, 30)

	for i, v := range series30d {
		assert.InDelta(t, 0.0, v, 1e-9, "bucket %d should be zero", i)
	}
}

func TestAggregateCost_PriorPeriod(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := todayStart.Add(24 * time.Hour)
	since := todayStart.AddDate(0, 0, -29)

	// Session 35 days ago — in the prior window (since-30..since).
	newTestSessionAtTime(t, store, todayStart.Add(-35*24*time.Hour).Add(6*time.Hour), 5.00)

	last30d, prior30d, series30d, err := store.AggregateCost(ctx, since, until)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, last30d, 1e-9, "last30d should be 0 (session is outside window)")
	assert.InDelta(t, 5.0, prior30d, 1e-9, "prior30d should capture the 35-day-old session")

	require.Len(t, series30d, 30)

	for i, v := range series30d {
		assert.InDelta(t, 0.0, v, 1e-9, "series bucket %d should be zero", i)
	}
}

// TestDeleteSession_ArchivesBehavior covers the archive-on-delete contract.
func TestDeleteSession_ArchivesBehavior(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	now := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC).Truncate(time.Second)
	sessionID := chat.NewID()
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID:         sessionID,
		Title:      "archive-test",
		Project:    "myproject",
		Status:     chat.StatusCold,
		CreatedAt:  now,
		LastActive: now,
		CreatedBy:  "human:test",
		Model:      "claude-sonnet-4-6",
	}))
	// Add cost so we can assert it's preserved in the archive.
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, sessionID, 100, 50, 20, 10, 0.042, "claude-sonnet-4-6")
	require.NoError(t, err)

	// Append a message so we can verify CASCADE.
	_, err = store.AppendMessage(ctx, chat.Message{
		SessionID: sessionID, Seq: 1, Role: chat.RoleUser,
		Content: "{}", CreatedAt: now,
	})
	require.NoError(t, err)

	beforeDelete := time.Now().UTC().Unix()

	require.NoError(t, store.DeleteSession(ctx, sessionID))

	afterDelete := time.Now().UTC().Unix()

	// chat_sessions row gone.
	var sessionCount int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_sessions WHERE id = ?`, sessionID,
	).Scan(&sessionCount))
	assert.Equal(t, 0, sessionCount, "chat_sessions row must be gone after delete")

	// chat_messages rows gone (CASCADE).
	var msgCount int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_messages WHERE session_id = ?`, sessionID,
	).Scan(&msgCount))
	assert.Equal(t, 0, msgCount, "chat_messages must be cascade-deleted")

	// Archive row present with correct cost fields.
	var (
		archPrompt, archCompletion, archCacheRead, archCacheCreation int64
		archCost                                                     float64
		archDeletedAt                                                int64
	)
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT prompt_tokens, completion_tokens, cache_read_tokens,
		        cache_creation_tokens, estimated_cost_usd, deleted_at
		 FROM chat_cost_archive WHERE id = ?`, sessionID,
	).Scan(&archPrompt, &archCompletion, &archCacheRead, &archCacheCreation, &archCost, &archDeletedAt))

	assert.Equal(t, int64(100), archPrompt)
	assert.Equal(t, int64(50), archCompletion)
	assert.Equal(t, int64(20), archCacheRead)
	assert.Equal(t, int64(10), archCacheCreation)
	assert.InDelta(t, 0.042, archCost, 1e-9)
	assert.GreaterOrEqual(t, archDeletedAt, beforeDelete)
	assert.LessOrEqual(t, archDeletedAt, afterDelete)

	// GetSession, ListSessions, CountSessionsByStatus no longer see the id.
	_, err = store.GetSession(ctx, sessionID)
	require.ErrorIs(t, err, chat.ErrSessionNotFound)

	sessions, err := store.ListSessions(ctx, chat.SessionFilter{})
	require.NoError(t, err)

	for _, s := range sessions {
		assert.NotEqual(t, sessionID, s.ID)
	}

	count, err := store.CountSessionsByStatus(ctx, chat.StatusCold, chat.StatusActive)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestDeleteSession_NonExistentIsNoOp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.DeleteSession(ctx, "does-not-exist"))

	// No archive row created.
	var n int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_cost_archive WHERE id = ?`, "does-not-exist",
	).Scan(&n))
	assert.Equal(t, 0, n)
}

func TestDeleteSession_IdempotentDoubleDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := chat.NewID()
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID: sessionID, Title: "dbl", Status: chat.StatusCold,
		CreatedAt: now, LastActive: now, CreatedBy: "human:test",
	}))
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, sessionID, 10, 5, 0, 0, 0.01, "claude-sonnet-4-6")
	require.NoError(t, err)

	require.NoError(t, store.DeleteSession(ctx, sessionID))

	// Second delete: the source SELECT finds no chat_sessions row (hard-deleted
	// above), so the INSERT is a silent no-op; the existing archive row is
	// untouched. ON CONFLICT does NOT fire in this path — see
	// TestDeleteSession_ReinsertedIDPreservesArchive for that case.
	require.NoError(t, store.DeleteSession(ctx, sessionID))

	// Exactly one archive row.
	var n int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_cost_archive WHERE id = ?`, sessionID,
	).Scan(&n))
	assert.Equal(t, 1, n)
}

// TestDeleteSession_ReinsertedIDPreservesArchive exercises the ON CONFLICT(id)
// DO NOTHING clause in DeleteSession. It pre-inserts a row into chat_cost_archive
// with a sentinel cost and deleted_at, then creates a chat_sessions row with the
// same id and a different cost. When DeleteSession runs, the INSERT … SELECT
// finds the source row but the id already exists in the archive, so ON CONFLICT
// fires and the original archive values are preserved.
func TestDeleteSession_ReinsertedIDPreservesArchive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	id := chat.NewID()
	now := time.Now().UTC().Truncate(time.Second)

	const sentinelCost = 7.77

	sentinelDeletedAt := int64(1_700_000_000) // fixed Unix timestamp as sentinel

	// Step 1: Pre-insert a row directly into chat_cost_archive with sentinel values.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO chat_cost_archive
			(id, project, model, last_active,
			 prompt_tokens, completion_tokens, cache_read_tokens,
			 cache_creation_tokens, estimated_cost_usd, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "test-project", "claude-sonnet-4-6", now.Unix(),
		100, 50, 10, 5, sentinelCost, sentinelDeletedAt,
	)
	require.NoError(t, err)

	// Step 2: Create a chat_sessions row with the SAME id and a clearly different cost.
	require.NoError(t, store.CreateSession(ctx, chat.Session{
		ID: id, Title: "reinsertion-test", Status: chat.StatusCold,
		CreatedAt: now, LastActive: now, CreatedBy: "human:test",
	}))
	_, _, _, _, _, err = store.IncrementSessionCost(ctx, id, 200, 100, 0, 0, 99.99, "claude-sonnet-4-6")
	require.NoError(t, err)

	// Step 3: DeleteSession — INSERT … SELECT finds the source row AND the id
	// already exists in chat_cost_archive, so ON CONFLICT(id) DO NOTHING fires.
	require.NoError(t, store.DeleteSession(ctx, id))

	// Step 4: Assert the archive row retains the original sentinel values.
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_cost_archive WHERE id = ?`, id,
	).Scan(&count))
	assert.Equal(t, 1, count, "exactly one archive row")

	var (
		gotCost      float64
		gotDeletedAt int64
	)
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT estimated_cost_usd, deleted_at FROM chat_cost_archive WHERE id = ?`, id,
	).Scan(&gotCost, &gotDeletedAt))
	assert.InDelta(t, sentinelCost, gotCost, 0.001, "archive cost must be the original sentinel, not the session cost")
	assert.Equal(t, sentinelDeletedAt, gotDeletedAt, "archive deleted_at must be the original sentinel, not updated")

	// The chat_sessions row must be gone.
	var sessionCount int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_sessions WHERE id = ?`, id,
	).Scan(&sessionCount))
	assert.Equal(t, 0, sessionCount, "chat_sessions row must be hard-deleted")
}

// TestAggregateCost_IncludesArchivedSessions checks that deleted sessions
// contribute to all three aggregate outputs.
func TestAggregateCost_IncludesArchivedSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	now := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := todayStart.Add(24 * time.Hour)
	since := todayStart.AddDate(0, 0, -29)

	// Live session: today, $1.00.
	newTestSessionAtTime(t, store, now.Add(-2*time.Hour), 1.00)

	// Session to delete: 10 days ago, $2.00. -> series[19]
	tenDaysAgo := todayStart.Add(-10 * 24 * time.Hour).Add(6 * time.Hour)
	idToDelete := newTestSessionAtTime(t, store, tenDaysAgo, 2.00)

	// Session to delete in prior window: 35 days ago, $5.00.
	thirtyFiveDaysAgo := todayStart.Add(-35 * 24 * time.Hour).Add(6 * time.Hour)
	idToDeletePrior := newTestSessionAtTime(t, store, thirtyFiveDaysAgo, 5.00)

	// Delete both sessions so they move to the archive.
	require.NoError(t, store.DeleteSession(ctx, idToDelete))
	require.NoError(t, store.DeleteSession(ctx, idToDeletePrior))

	last30d, prior30d, series30d, err := store.AggregateCost(ctx, since, until)
	require.NoError(t, err)

	// last30d must include live ($1) + archived ($2) = $3.
	assert.InDelta(t, 3.00, last30d, 1e-9, "last30d must include archived session")

	// prior30d must include archived $5.
	assert.InDelta(t, 5.00, prior30d, 1e-9, "prior30d must include archived session")

	require.Len(t, series30d, 30)

	// series[29] = today's live session $1.
	assert.InDelta(t, 1.00, series30d[29], 1e-9, "today bucket")
	// series[19] = 10-days-ago archived session $2.
	assert.InDelta(t, 2.00, series30d[19], 1e-9, "10-days-ago archived bucket")

	// sum(series30d) must equal last30d.
	var seriesSum float64
	for _, v := range series30d {
		seriesSum += v
	}

	assert.InDelta(t, last30d, seriesSum, 1e-9, "sum(series30d) must equal last30d")
}
