package images

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

var imgMigrations = []sqliteutil.Migration{
	{
		Version: 1,
		Up: func(ctx context.Context, db *sql.DB) error {
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
		Version: 2,
		Up: func(ctx context.Context, db *sql.DB) error {
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

// Open opens (or creates) the SQLite database at path and applies schema
// migrations. Parent directories are created as needed.
func Open(path string) (*SQLiteStore, error) {
	db, err := sqliteutil.Open(path, sqliteutil.WithConnMaxIdleTime(5*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("images: %w", err)
	}

	if err := sqliteutil.Migrate(context.Background(), db, "images", imgMigrations); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Put processes raw bytes, derives a content-hash ID, and persists the result.
// Identical content is deduplicated by ID - a second call with the same bytes
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
