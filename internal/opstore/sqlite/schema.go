package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// schemaVersion is the current opstore schema revision. Bump it when an older
// binary cannot run correctly against the new schema: an image older than the
// stamp refuses to start. A column with a default that older binaries never
// name does not bump, so a rollback still opens the database. Every CREATE is
// IF NOT EXISTS, so an older database grows the new table on open and is
// stamped; a newer database is refused.
const schemaVersion = 6

// ensureSchema creates every operational table if absent. There is no
// migration ledger: columns added to an existing table are grown by
// addColumnIfMissing, and a newer database is refused. One ops.db holds the chat schema (sessions,
// messages, cost archive), the model blacklist, Best-of-N model outcomes, the
// selector tier ladders and price headroom, and the Jira writeback outbox.
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
			estimated_cost_usd        REAL NOT NULL DEFAULT 0,
			kind                      TEXT NOT NULL DEFAULT 'chat',
			mode                      TEXT NOT NULL DEFAULT '',
			cost_cap_usd              REAL NOT NULL DEFAULT 0,
			plain_language            INTEGER NOT NULL DEFAULT 0,
			origin_chat               TEXT NOT NULL DEFAULT '',
			origin_seq                INTEGER NOT NULL DEFAULT 0
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
		`CREATE TABLE IF NOT EXISTS selector_ladder (
			role       TEXT NOT NULL,
			tier       TEXT NOT NULL,
			bar        REAL NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (role, tier)
		)`,
		`CREATE TABLE IF NOT EXISTS selector_headroom (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			headroom   REAL NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS jira_writeback (
			project         TEXT NOT NULL,
			card_id         TEXT NOT NULL,
			kind            TEXT NOT NULL CHECK (kind IN ('started','done')),
			issue_key       TEXT NOT NULL,
			state           TEXT NOT NULL CHECK (state IN ('pending','done','skipped','failed')),
			attempts        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL,
			last_error      TEXT,
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			PRIMARY KEY (project, card_id, kind)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jira_writeback_due ON jira_writeback(state, next_attempt_at)`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("opstore schema: %w", err)
		}
	}

	for _, col := range []struct{ name, decl string }{
		{"kind", "TEXT NOT NULL DEFAULT 'chat'"},
		{"mode", "TEXT NOT NULL DEFAULT ''"},
		{"cost_cap_usd", "REAL NOT NULL DEFAULT 0"},
		{"plain_language", "INTEGER NOT NULL DEFAULT 0"},
		{"origin_chat", "TEXT NOT NULL DEFAULT ''"},
		{"origin_seq", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := addColumnIfMissing(ctx, db, "chat_sessions", col.name, col.decl); err != nil {
			return err
		}
	}

	// After the column loop: an upgraded database has the columns only now.
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_sessions_origin
		ON chat_sessions(origin_chat, origin_seq) WHERE origin_chat != ''`); err != nil {
		return fmt.Errorf("opstore schema: %w", err)
	}

	// Stamp the version. PRAGMA user_version takes no bound parameter; the value
	// is a compile-time constant, not user input.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("opstore: stamp schema version: %w", err)
	}

	return nil
}

// addColumnIfMissing grows an existing table by one column. CREATE TABLE IF
// NOT EXISTS leaves an existing table untouched, so a column added after a
// database was first created only exists on fresh databases without this.
func addColumnIfMissing(ctx context.Context, db *sql.DB, table, column, decl string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return fmt.Errorf("opstore: table_info %s: %w", table, err)
	}

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

		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("opstore: scan table_info %s: %w", table, err)
		}

		if name == column {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("opstore: table_info %s: %w", table, err)
	}

	if _, err := db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl); err != nil {
		return fmt.Errorf("opstore: add column %s.%s: %w", table, column, err)
	}

	return nil
}
