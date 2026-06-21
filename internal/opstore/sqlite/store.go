package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register sqlite driver
)

// Store is the SQLite-backed operational store.
type Store struct{ db *sql.DB }

// sqliteDSN builds a `file:` URI for the modernc.org/sqlite driver, passing
// PRAGMA settings via the query string. The `file:` prefix selects the URI
// VFS rather than the implicit filename VFS; we concatenate path directly
// (rather than via url.URL) because url.URL.String() places a relative path
// in the authority component, which modernc/sqlite rejects as an invalid URI
// authority. synchronous=NORMAL is the recommended pairing for WAL — durable
// across process crashes, acceptable for operational metadata.
func sqliteDSN(path string) string {
	return "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
}

// Open opens (or creates) the SQLite database at path and applies the schema.
// Parent directories are created as needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("opstore: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("opstore: open sqlite: %w", err)
	}

	// SQLite is single-writer; WAL lets concurrent readers avoid queueing.
	db.SetMaxOpenConns(5)

	if err := ensureSchema(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }
