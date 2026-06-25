package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureSchema creates every operational table if absent. Clean-cut: there is
// no migration ledger and no backward-compat path — an obsolete DB is deleted
// and recreated by the operator. Holds both the chat schema (sessions,
// messages, cost archive) and the model blacklist in a single ops.db.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS model_blacklist (
			slug         TEXT PRIMARY KEY,
			reason       TEXT NOT NULL,
			sample_card  TEXT,
			reported_by  TEXT NOT NULL,
			first_seen   INTEGER NOT NULL,
			last_seen    INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id                        TEXT PRIMARY KEY,
			title                     TEXT NOT NULL,
			project                   TEXT,
			status                    TEXT NOT NULL,
			created_at                INTEGER NOT NULL,
			last_active               INTEGER NOT NULL,
			created_by                TEXT NOT NULL,
			container_id              TEXT,
			workspace                 TEXT,
			model                     TEXT NOT NULL DEFAULT '',
			context_tokens            INTEGER NOT NULL DEFAULT 0,
			context_tokens_updated_at INTEGER,
			rehydration_active        INTEGER NOT NULL DEFAULT 0,
			rehydration_started_at    INTEGER,
			prompt_tokens             INTEGER NOT NULL DEFAULT 0,
			completion_tokens         INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens         INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens     INTEGER NOT NULL DEFAULT 0,
			estimated_cost_usd        REAL NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_sessions_last_active ON chat_sessions(last_active)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_sessions_status ON chat_sessions(status)`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id        TEXT NOT NULL,
			seq               INTEGER NOT NULL,
			role              TEXT NOT NULL,
			content           TEXT NOT NULL,
			created_at        INTEGER NOT NULL,
			rehydration_phase INTEGER NOT NULL DEFAULT 0,
			kind              TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_messages_session_seq_unique ON chat_messages(session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_phase ON chat_messages(session_id, rehydration_phase)`,
		`CREATE TABLE IF NOT EXISTS chat_cost_archive (
			id                    TEXT PRIMARY KEY,
			project               TEXT,
			model                 TEXT NOT NULL DEFAULT '',
			last_active           INTEGER NOT NULL,
			prompt_tokens         INTEGER NOT NULL DEFAULT 0,
			completion_tokens     INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			estimated_cost_usd    REAL NOT NULL DEFAULT 0,
			deleted_at            INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_cost_archive_last_active ON chat_cost_archive(last_active)`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("opstore schema: %w", err)
		}
	}

	return nil
}
