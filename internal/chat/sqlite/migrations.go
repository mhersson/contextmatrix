package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migration represents a single versioned schema change. Each Up function is
// idempotent (every statement uses IF EXISTS / IF NOT EXISTS) so the runner
// is safe to re-execute on pre-versioning databases without back-filling the
// schema_migrations rows separately.
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
				`CREATE INDEX IF NOT EXISTS idx_chat_sessions_last_active ON chat_sessions(last_active)`,
				`CREATE INDEX IF NOT EXISTS idx_chat_sessions_status ON chat_sessions(status)`,
				`CREATE TABLE IF NOT EXISTS chat_messages (
                    id          INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id  TEXT NOT NULL,
                    seq         INTEGER NOT NULL,
                    role        TEXT NOT NULL,
                    content     TEXT NOT NULL,
                    created_at  INTEGER NOT NULL,
                    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
                )`,
				`CREATE INDEX IF NOT EXISTS idx_chat_messages_session_seq ON chat_messages(session_id, seq)`,
			})
		},
	},
	{
		version: 2,
		up: func(ctx context.Context, db *sql.DB) error {
			return execAll(ctx, db, []string{
				`DROP INDEX IF EXISTS idx_chat_messages_session_seq`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_messages_session_seq_unique ON chat_messages(session_id, seq)`,
			})
		},
	},
	{
		version: 3,
		up: func(ctx context.Context, db *sql.DB) error {
			// SQLite cannot ALTER TABLE ADD COLUMN IF NOT EXISTS, so we
			// introspect pragma table_info first and skip already-present
			// columns. This keeps the migration safe to re-run against any
			// database that drifted from the versioned migration history
			// (e.g. one that had v3 partially applied before crashing).
			if err := addColumnIfMissing(ctx, db, "chat_sessions", "model",
				`ALTER TABLE chat_sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "context_tokens",
				`ALTER TABLE chat_sessions ADD COLUMN context_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "context_tokens_updated_at",
				`ALTER TABLE chat_sessions ADD COLUMN context_tokens_updated_at INTEGER`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "rehydration_active",
				`ALTER TABLE chat_sessions ADD COLUMN rehydration_active INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_messages", "rehydration_phase",
				`ALTER TABLE chat_messages ADD COLUMN rehydration_phase INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			return execAll(ctx, db, []string{
				`CREATE INDEX IF NOT EXISTS idx_chat_messages_phase ON chat_messages(session_id, rehydration_phase)`,
			})
		},
	},
	{
		version: 4,
		up: func(ctx context.Context, db *sql.DB) error {
			// kind discriminates structural markers (e.g. "divider" for the
			// "Context cleared" sentinel) on the persisted row. Empty string
			// means "regular message" — preserves wire compat for callers
			// that don't set it.
			return addColumnIfMissing(ctx, db, "chat_messages", "kind",
				`ALTER TABLE chat_messages ADD COLUMN kind TEXT NOT NULL DEFAULT ''`)
		},
	},
	{
		version: 5,
		up: func(ctx context.Context, db *sql.DB) error {
			// rehydration_started_at records when SetRehydrationActive(true)
			// ran. NULL when rehydration_active = 0. The reaper filters on
			// this column instead of last_active so an actively-typing user
			// whose agent crashed mid-rehydration cannot keep the stale-phase
			// sweep from firing.
			return addColumnIfMissing(ctx, db, "chat_sessions", "rehydration_started_at",
				`ALTER TABLE chat_sessions ADD COLUMN rehydration_started_at INTEGER`)
		},
	},
	{
		version: 6,
		up: func(ctx context.Context, db *sql.DB) error {
			// Token counters and running cost accumulate from usage stream-json
			// frames via IncrementSessionCost. All columns default to 0 so
			// pre-v6 rows are backward-compatible without a data migration.
			if err := addColumnIfMissing(ctx, db, "chat_sessions", "prompt_tokens",
				`ALTER TABLE chat_sessions ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "completion_tokens",
				`ALTER TABLE chat_sessions ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "cache_read_tokens",
				`ALTER TABLE chat_sessions ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			if err := addColumnIfMissing(ctx, db, "chat_sessions", "cache_creation_tokens",
				`ALTER TABLE chat_sessions ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			return addColumnIfMissing(ctx, db, "chat_sessions", "estimated_cost_usd",
				`ALTER TABLE chat_sessions ADD COLUMN estimated_cost_usd REAL NOT NULL DEFAULT 0`)
		},
	},
}

// addColumnIfMissing applies an ALTER TABLE ADD COLUMN statement only if the
// column is not already present. SQLite lacks IF NOT EXISTS on ADD COLUMN,
// and pre-versioning databases may have had columns added by an earlier code
// path that drifted from the migrations list.
func addColumnIfMissing(ctx context.Context, db *sql.DB, table, column, stmt string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return fmt.Errorf("introspect %s: %w", table, err)
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
			return fmt.Errorf("scan %s columns: %w", table, err)
		}

		if name == column {
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate %s columns: %w", table, err)
			}

			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}

	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}

	return nil
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
