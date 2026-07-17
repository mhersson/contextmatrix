// Package authstore persists multi-user state - accounts, sessions, one-time
// tokens, and the GitHub credential pool - in a dedicated SQLite database
// (auth.db) with a versioned migration ledger. Identity data must survive
// upgrades, so unlike ops.db this store is never delete-and-recreate.
//
// The store is crypto-agnostic: credential secrets arrive and leave as
// opaque encrypted []byte blobs (see internal/auth for the encryption).
package authstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/sqliteutil"
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

// Open opens (or creates) the auth database at path and applies schema
// migrations. Parent directories are created as needed.
func Open(path string) (*Store, error) {
	db, err := sqliteutil.Open(path,
		// foreign_keys enforces the sessions/tokens -> users references.
		sqliteutil.WithForeignKeys(),
		sqliteutil.WithConnMaxIdleTime(5*time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("authstore: %w", err)
	}

	if err := sqliteutil.Migrate(context.Background(), db, "authstore", migrations); err != nil {
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
