package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migration represents a single versioned schema change.
type migration struct {
	version int
	up      func(ctx context.Context, db *sql.DB) error
}

var migrations = []migration{
	{
		version: 1,
		up: func(ctx context.Context, db *sql.DB) error {
			return execAll(ctx, db, []string{
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
			})
		},
	},
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		return fmt.Errorf("chat schema_migrations table: %w", err)
	}

	applied, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}

		if err := m.up(ctx, db); err != nil {
			return fmt.Errorf("chat migration v%d: %w", m.version, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix(),
		); err != nil {
			return fmt.Errorf("chat record migration v%d: %w", m.version, err)
		}
	}

	return nil
}

func loadAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("chat schema_migrations query: %w", err)
	}

	defer rows.Close()

	applied := map[int]bool{}

	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("chat schema_migrations scan: %w", err)
		}

		applied[v] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chat schema_migrations rows: %w", err)
	}

	return applied, nil
}

func execAll(ctx context.Context, db *sql.DB, statements []string) error {
	for i, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}

	return nil
}
