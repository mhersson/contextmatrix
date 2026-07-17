package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

// Store is the SQLite-backed operational store.
type Store struct{ db *sql.DB }

// Open opens (or creates) the SQLite database at path and applies the schema.
// Parent directories are created as needed. Serialisation across chat writers
// happens at the manager level (chat.Manager.mu held across AppendMessage).
func Open(path string) (*Store, error) {
	db, err := sqliteutil.Open(path, sqliteutil.WithForeignKeys())
	if err != nil {
		return nil, fmt.Errorf("opstore: %w", err)
	}

	if err := ensureSchema(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }
