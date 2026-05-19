package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
