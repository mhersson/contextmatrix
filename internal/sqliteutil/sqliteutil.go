// Package sqliteutil provides the SQLite plumbing shared by the
// modernc.org/sqlite backed stores: file-URI DSN construction, pooled
// database opening, and a schema_migrations ledger runner.
package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register sqlite driver
)

// Option adjusts how DSN and Open configure the database.
type Option func(*options)

type options struct {
	foreignKeys     bool
	connMaxIdleTime time.Duration
}

// WithForeignKeys enables foreign-key enforcement via the foreign_keys pragma.
func WithForeignKeys() Option {
	return func(o *options) { o.foreignKeys = true }
}

// WithConnMaxIdleTime bounds how long idle pooled connections are retained.
func WithConnMaxIdleTime(d time.Duration) Option {
	return func(o *options) { o.connMaxIdleTime = d }
}

func buildOptions(opts []Option) options {
	var o options

	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// DSN builds a `file:` URI for the modernc.org/sqlite driver, passing PRAGMA
// settings via the query string. The `file:` prefix selects the URI VFS
// rather than the implicit filename VFS; the path is concatenated directly
// (rather than via url.URL) because url.URL.String() places a relative path
// in the authority component (e.g. `file://images.db`), which modernc/sqlite
// rejects as an invalid URI authority. synchronous=NORMAL is the recommended
// pairing for WAL - durable across process crashes; busy_timeout avoids
// spurious SQLITE_BUSY under concurrent access.
func DSN(path string, opts ...Option) string {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"

	if buildOptions(opts).foreignKeys {
		dsn += "&_pragma=foreign_keys(1)"
	}

	return dsn
}

// Open opens (or creates) the SQLite database at path, creating parent
// directories as needed, and tunes the connection pool. SQLite is
// single-writer regardless of pool size; MaxOpenConns > 1 lets concurrent
// readers avoid queueing behind a writer when WAL is on.
func Open(path string, opts ...Option) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", DSN(path, opts...))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	if d := buildOptions(opts).connMaxIdleTime; d > 0 {
		db.SetConnMaxIdleTime(d)
	}

	return db, nil
}

// Migration is a single versioned schema change.
type Migration struct {
	Version int
	Up      func(ctx context.Context, db *sql.DB) error
}

// Migrate applies migrations in order using a schema_migrations ledger,
// wrapping errors with prefix. It is idempotent: re-running against a fully
// migrated database is a no-op. Databases that predate the ledger are also
// safe as long as the first migration only uses CREATE ... IF NOT EXISTS; it
// is then recorded in the ledger without recreating anything.
func Migrate(ctx context.Context, db *sql.DB, prefix string, migrations []Migration) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("%s: schema_migrations table: %w", prefix, err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("%s: schema_migrations query: %w", prefix, err)
	}
	defer rows.Close()

	applied := map[int]bool{}

	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("%s: schema_migrations scan: %w", prefix, err)
		}

		applied[v] = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s: schema_migrations rows: %w", prefix, err)
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}

		if err := m.Up(ctx, db); err != nil {
			return fmt.Errorf("%s: migration v%d: %w", prefix, m.Version, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.Version, time.Now().Unix(),
		); err != nil {
			return fmt.Errorf("%s: record migration v%d: %w", prefix, m.Version, err)
		}
	}

	return nil
}
