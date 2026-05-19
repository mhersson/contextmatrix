package images

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register sqlite driver
)

// imgMigration is a single versioned schema change for the images database.
type imgMigration struct {
	version int
	up      func(ctx context.Context, db *sql.DB) error
}

var imgMigrations = []imgMigration{
	{
		version: 1,
		up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS images (
				id           TEXT PRIMARY KEY,
				content_type TEXT NOT NULL,
				bytes        BLOB NOT NULL,
				width        INT,
				height       INT,
				created_at   TIMESTAMP
			)`)

			return err
		},
	},
	{
		version: 2,
		up: func(ctx context.Context, db *sql.DB) error {
			// width and height were written on every Put but never read by Get.
			// Drop the columns to reclaim storage. SQLite 3.35+ supports
			// ALTER TABLE … DROP COLUMN; modernc.org/sqlite embeds 3.47+.
			// The drops are guarded by column-existence checks so the migration
			// is safe to re-run against a DB that already had them removed.
			for _, col := range []string{"width", "height"} {
				rows, err := db.QueryContext(ctx, `PRAGMA table_info("images")`)
				if err != nil {
					return fmt.Errorf("images: introspect for drop %s: %w", col, err)
				}

				found := false

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
						_ = rows.Close()

						return fmt.Errorf("images: scan column info: %w", err)
					}

					if name == col {
						found = true

						break
					}
				}

				_ = rows.Close()

				if !found {
					continue
				}

				if _, err := db.ExecContext(ctx, `ALTER TABLE images DROP COLUMN `+col); err != nil {
					return fmt.Errorf("images: drop column %s: %w", col, err)
				}
			}

			return nil
		},
	},
}

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

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO images (id, content_type, bytes, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		id, contentType, processed, time.Now().UTC(),
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

// migrate applies schema migrations to db using a schema_migrations ledger.
// It is idempotent: re-running on a database that already has all migrations
// applied is a no-op. Re-running on a database that predates the ledger (i.e.
// has the images table but no schema_migrations table) is also safe because v1
// uses CREATE TABLE IF NOT EXISTS and will be recorded in the ledger without
// recreating the table.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("images: schema_migrations table: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("images: schema_migrations query: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}

	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("images: schema_migrations scan: %w", err)
		}

		applied[v] = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("images: schema_migrations rows: %w", err)
	}

	for _, m := range imgMigrations {
		if applied[m.version] {
			continue
		}

		if err := m.up(ctx, db); err != nil {
			return fmt.Errorf("images: migration v%d: %w", m.version, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix(),
		); err != nil {
			return fmt.Errorf("images: record migration v%d: %w", m.version, err)
		}
	}

	return nil
}
