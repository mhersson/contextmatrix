package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// schemaVersion is the current opstore schema revision. Bump it when adding
// tables/columns and add the corresponding migration below.
const schemaVersion = 2

// ensureSchema creates every operational table if absent. Clean-cut: there is
// no migration ledger and no backward-compat path - an obsolete DB is deleted
// and recreated by the operator. Holds the chat schema (sessions, messages,
// cost archive), the model blacklist, and Best-of-N model outcomes in a
// single ops.db.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("opstore: read schema version: %w", err)
	}

	if current > schemaVersion {
		return fmt.Errorf("opstore: db schema v%d is newer than this binary (expects v%d); "+
			"upgrade contextmatrix or delete the ops db to recreate", current, schemaVersion)
	}

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
		`CREATE TABLE IF NOT EXISTS model_outcomes (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			project      TEXT NOT NULL,
			card_id      TEXT NOT NULL,
			model        TEXT NOT NULL,
			role         TEXT NOT NULL,
			result       TEXT NOT NULL CHECK (result IN ('win','loss','failed')),
			verify_pass  INTEGER NOT NULL,
			cost_usd     REAL NOT NULL,
			n_candidates INTEGER NOT NULL,
			judge_model  TEXT,
			created_at   INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_outcomes_model ON model_outcomes(model)`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("opstore schema: %w", err)
		}
	}

	// Stamp the version. PRAGMA user_version takes no bound parameter; the value
	// is a compile-time constant, not user input.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("opstore: stamp schema version: %w", err)
	}

	return nil
}
