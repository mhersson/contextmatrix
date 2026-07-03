// Package authstore persists multi-user state — accounts, sessions, one-time
// tokens, and the GitHub credential pool — in a dedicated SQLite database
// (auth.db) with a versioned migration ledger. Identity data must survive
// upgrades, so unlike ops.db this store is never delete-and-recreate.
//
// The store is crypto-agnostic: credential secrets arrive and leave as
// opaque encrypted []byte blobs (see internal/auth for the encryption).
package authstore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register sqlite driver
)

// Sentinel errors shared by all entity files in this package.
var (
	ErrNotFound          = errors.New("authstore: not found")
	ErrDuplicate         = errors.New("authstore: already exists")
	ErrTokenSpent        = errors.New("authstore: token already used")
	ErrTokenExpired      = errors.New("authstore: token expired")
	ErrInvalidUsername   = errors.New("authstore: invalid username")
	ErrNotBootstrappable = errors.New("authstore: users already exist")
	ErrLastAdminStore    = errors.New("authstore: last active admin")
)

// Store is the SQLite-backed multi-user state store.
type Store struct {
	db *sql.DB
}

// sqliteDSN builds a file: URI for the modernc.org/sqlite driver. Same
// rationale as internal/images: WAL + synchronous(NORMAL) is the recommended
// pairing; busy_timeout avoids spurious SQLITE_BUSY under concurrent access;
// foreign_keys(1) enforces the sessions/tokens → users references.
func sqliteDSN(path string) string {
	return "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

// Open opens (or creates) the auth database at path and applies schema
// migrations. Parent directories are created as needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("authstore: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("authstore: open sqlite: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := migrate(db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// toUnix converts a time to the stored UTC unix-seconds representation.
func toUnix(t time.Time) int64 { return t.UTC().Unix() }

// fromUnix converts stored unix seconds back to a UTC time.
func fromUnix(n int64) time.Time { return time.Unix(n, 0).UTC() }

// fromNullUnix converts a nullable stored timestamp to *time.Time.
func fromNullUnix(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}

	t := fromUnix(n.Int64)

	return &t
}
