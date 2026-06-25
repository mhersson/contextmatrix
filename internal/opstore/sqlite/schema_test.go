package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureSchema_CreatesChatAndBlacklistTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ops.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// model_blacklist table and columns.
	assert.True(t, tableExists(t, s.db, "model_blacklist"))

	for _, col := range []string{
		"slug", "reason", "sample_card", "reported_by", "first_seen", "last_seen",
	} {
		assert.True(t, columnExists(t, s.db, "model_blacklist", col), "model_blacklist.%s missing", col)
	}

	// Unique index must exist; the old redundant non-unique one must not.
	assert.True(t, indexExists(t, s.db, "idx_chat_messages_session_seq_unique"))
	assert.False(t, indexExists(t, s.db, "idx_chat_messages_session_seq"))

	// chat_sessions columns.
	for _, col := range []string{
		"id", "title", "project", "status", "created_at", "last_active",
		"created_by", "container_id", "workspace", "model",
		"context_tokens", "context_tokens_updated_at", "rehydration_active",
		"rehydration_started_at", "prompt_tokens", "completion_tokens",
		"cache_read_tokens", "cache_creation_tokens", "estimated_cost_usd",
	} {
		assert.True(t, columnExists(t, s.db, "chat_sessions", col), "chat_sessions.%s missing", col)
	}

	// chat_messages columns.
	for _, col := range []string{
		"id", "session_id", "seq", "role", "content", "created_at",
		"rehydration_phase", "kind",
	} {
		assert.True(t, columnExists(t, s.db, "chat_messages", col), "chat_messages.%s missing", col)
	}

	// chat_cost_archive table and columns.
	assert.True(t, tableExists(t, s.db, "chat_cost_archive"))

	for _, col := range []string{
		"id", "project", "model", "last_active", "prompt_tokens",
		"completion_tokens", "cache_read_tokens", "cache_creation_tokens",
		"estimated_cost_usd", "deleted_at",
	} {
		assert.True(t, columnExists(t, s.db, "chat_cost_archive", col), "chat_cost_archive.%s missing", col)
	}

	// Indexes.
	for _, idx := range []string{
		"idx_chat_cost_archive_last_active",
		"idx_chat_messages_phase",
		"idx_chat_sessions_last_active",
		"idx_chat_sessions_status",
	} {
		assert.True(t, indexExists(t, s.db, idx), "index %s missing", idx)
	}
}

func TestEnsureSchema_ReopenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ops.db")

	s1, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	// Reopening an existing DB must not error: every CREATE uses IF NOT EXISTS.
	s2, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	assert.True(t, tableExists(t, s2.db, "chat_sessions"))
	assert.True(t, tableExists(t, s2.db, "model_blacklist"))
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	require.NoError(t, err)

	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)

		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk))

		if name == column {
			return true
		}
	}

	require.NoError(t, rows.Err())

	return false
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var n int

	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	require.NoError(t, err)

	return n > 0
}

func indexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var n int

	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n)
	require.NoError(t, err)

	return n > 0
}
