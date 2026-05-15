package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate_FreshDB_AppliesAllVersionsAndDropsRedundantIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, []int{1, 2, 3}, appliedVersions(t, s.db))
	assert.True(t, indexExists(t, s.db, "idx_chat_messages_session_seq_unique"))
	assert.False(t, indexExists(t, s.db, "idx_chat_messages_session_seq"))
}

func TestMigrate_PreWave38DB_AppliesV2(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	seedV1OnlySchema(t, dbPath)

	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, []int{1, 2, 3}, appliedVersions(t, s.db))
	assert.True(t, indexExists(t, s.db, "idx_chat_messages_session_seq_unique"))
	assert.False(t, indexExists(t, s.db, "idx_chat_messages_session_seq"))
}

func TestMigrate_Wave38DB_DropsRedundantNonUniqueIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	seedV1OnlySchema(t, dbPath)
	addUniqueIndex(t, dbPath)

	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, []int{1, 2, 3}, appliedVersions(t, s.db))
	assert.True(t, indexExists(t, s.db, "idx_chat_messages_session_seq_unique"))
	assert.False(t, indexExists(t, s.db, "idx_chat_messages_session_seq"))
}

func TestMigrate_ReopenDoesNotDuplicateVersionRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s1, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	assert.Equal(t, []int{1, 2, 3}, appliedVersions(t, s2.db))
}

func TestMigrate_V3_AddsRehydrationAndModelColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.True(t, columnExists(t, s.db, "chat_sessions", "model"))
	assert.True(t, columnExists(t, s.db, "chat_sessions", "context_tokens"))
	assert.True(t, columnExists(t, s.db, "chat_sessions", "context_tokens_updated_at"))
	assert.True(t, columnExists(t, s.db, "chat_sessions", "rehydration_active"))
	assert.True(t, columnExists(t, s.db, "chat_messages", "rehydration_phase"))
	assert.True(t, indexExists(t, s.db, "idx_chat_messages_phase"))
}

func TestMigrate_V3_IdempotentOnPreV3DBWithPartialColumns(t *testing.T) {
	// Simulate a database that drifted from the version history: v1 + v2
	// schema in place but one v3 column already exists (e.g. added by a
	// buggy intermediate build). addColumnIfMissing must not error.
	dbPath := filepath.Join(t.TempDir(), "chats.db")
	seedV1OnlySchema(t, dbPath)
	addUniqueIndex(t, dbPath)
	addPartialV3(t, dbPath)

	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, []int{1, 2, 3}, appliedVersions(t, s.db))
	assert.True(t, columnExists(t, s.db, "chat_sessions", "model"))
	assert.True(t, columnExists(t, s.db, "chat_sessions", "rehydration_active"))
	assert.True(t, columnExists(t, s.db, "chat_messages", "rehydration_phase"))
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

func addPartialV3(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	_, err = db.Exec(`ALTER TABLE chat_sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`)
	require.NoError(t, err)

	require.NoError(t, db.Close())
}

func appliedVersions(t *testing.T, db *sql.DB) []int {
	t.Helper()

	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version ASC`)
	require.NoError(t, err)

	defer rows.Close()

	out := []int{}

	for rows.Next() {
		var v int

		require.NoError(t, rows.Scan(&v))

		out = append(out, v)
	}

	require.NoError(t, rows.Err())

	return out
}

func indexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var n int

	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n)
	require.NoError(t, err)

	return n > 0
}

func seedV1OnlySchema(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	for _, stmt := range []string{
		`CREATE TABLE chat_sessions (
            id            TEXT PRIMARY KEY,
            title         TEXT NOT NULL,
            project       TEXT,
            status        TEXT NOT NULL,
            created_at    INTEGER NOT NULL,
            last_active   INTEGER NOT NULL,
            created_by    TEXT NOT NULL,
            container_id  TEXT,
            workspace     TEXT
        )`,
		`CREATE INDEX idx_chat_sessions_last_active ON chat_sessions(last_active)`,
		`CREATE INDEX idx_chat_sessions_status ON chat_sessions(status)`,
		`CREATE TABLE chat_messages (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id  TEXT NOT NULL,
            seq         INTEGER NOT NULL,
            role        TEXT NOT NULL,
            content     TEXT NOT NULL,
            created_at  INTEGER NOT NULL,
            FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
        )`,
		`CREATE INDEX idx_chat_messages_session_seq ON chat_messages(session_id, seq)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())
}

func addUniqueIndex(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	_, err = db.Exec(`CREATE UNIQUE INDEX idx_chat_messages_session_seq_unique ON chat_messages(session_id, seq)`)
	require.NoError(t, err)

	require.NoError(t, db.Close())
}
