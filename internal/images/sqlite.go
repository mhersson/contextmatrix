package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register sqlite driver
)

// compile-time assertion that *sqliteStore satisfies Store.
var _ Store = (*sqliteStore)(nil)

// sqliteStore is the SQLite-backed implementation of Store.
type sqliteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies schema
// migrations. Parent directories are created as needed.
func Open(path string) (*sqliteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("images: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("images: open sqlite: %w", err)
	}

	db.SetMaxOpenConns(5)

	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &sqliteStore{db: db}, nil
}

// Close releases the underlying database connection.
func (s *sqliteStore) Close() error { return s.db.Close() }

// Put processes raw bytes, derives a content-hash ID, and persists the result.
// Identical content is deduplicated by ID — a second call with the same bytes
// returns the existing ID without inserting a new row.
func (s *sqliteStore) Put(ctx context.Context, raw []byte) (string, string, error) {
	processed, contentType, err := Process(raw)
	if err != nil {
		return "", "", err
	}

	sum := sha256.Sum256(processed)
	id := hex.EncodeToString(sum[:])[:16]

	// Decode dimensions from the processed bytes for the width/height columns.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(processed))
	if err != nil {
		// Non-fatal: store zeros if decode fails (shouldn't happen post-Process).
		cfg = image.Config{}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO images (id, content_type, bytes, width, height, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		id, contentType, processed, cfg.Width, cfg.Height, time.Now().UTC(),
	)
	if err != nil {
		return "", "", fmt.Errorf("images: insert: %w", err)
	}

	return id, contentType, nil
}

// Get retrieves image bytes and content type by ID. Returns ErrNotFound when
// no row matches.
func (s *sqliteStore) Get(ctx context.Context, id string) ([]byte, string, error) {
	var (
		data        []byte
		contentType string
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT bytes, content_type FROM images WHERE id = ?`, id,
	).Scan(&data, &contentType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}

		return nil, "", fmt.Errorf("images: get: %w", err)
	}

	return data, contentType, nil
}

// Has reports whether an image with the given ID exists in the store.
func (s *sqliteStore) Has(ctx context.Context, id string) (bool, error) {
	var exists int

	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM images WHERE id = ? LIMIT 1`, id,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("images: has: %w", err)
	}

	return true, nil
}

// migrate applies idempotent schema migrations to db.
func migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS images (
		id           TEXT PRIMARY KEY,
		content_type TEXT NOT NULL,
		bytes        BLOB NOT NULL,
		width        INT,
		height       INT,
		created_at   TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("images: create table: %w", err)
	}

	return nil
}

