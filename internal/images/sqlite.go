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

// compile-time assertion that *SQLiteStore satisfies Store.
var _ Store = (*SQLiteStore)(nil)

// SQLiteStore is the SQLite-backed implementation of Store.
type SQLiteStore struct {
	db *sql.DB
}

// sqliteDSN builds a `file:` URI for the modernc.org/sqlite driver, passing
// PRAGMA settings via the query string. The `file:` prefix selects the URI
// VFS rather than the implicit filename VFS; we concatenate path directly
// (rather than via url.URL) because url.URL.String() places a relative path
// in the authority component (e.g. `file://images.db`), which modernc/sqlite
// rejects as an invalid URI authority. synchronous=NORMAL is the recommended
// pairing for WAL — durable across process crashes, only weakens behaviour
// under power loss, acceptable for cached image blobs.
func sqliteDSN(path string) string {
	return "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
}

// Open opens (or creates) the SQLite database at path and applies schema
// migrations. Parent directories are created as needed.
func Open(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("images: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("images: open sqlite: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Put processes raw bytes, derives a content-hash ID, and persists the result.
// Identical content is deduplicated by ID — a second call with the same bytes
// returns the existing ID without inserting a new row.
func (s *SQLiteStore) Put(ctx context.Context, raw []byte) (string, string, error) {
	processed, contentType, err := Process(raw)
	if err != nil {
		return "", "", err
	}

	sum := sha256.Sum256(processed)
	id := hex.EncodeToString(sum[:])[:IDLen]

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
func (s *SQLiteStore) Get(ctx context.Context, id string) ([]byte, string, error) {
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
