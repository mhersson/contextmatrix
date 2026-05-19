package images

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestStore opens a SQLiteStore backed by a temp-dir database.
func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	path := filepath.Join(t.TempDir(), "images.db")

	s, err := Open(path)
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	return s
}

// makeTinyPNG produces a 4x4 solid-blue PNG for use in store tests.
func makeTinyPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{B: 255, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func TestSQLiteStore_PutGet(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := context.Background()

	raw := makeTinyPNG(t)

	id, ct, err := s.Put(ctx, raw)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Equal(t, "image/png", ct)

	data, gotCT, err := s.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, ct, gotCT)

	// The retrieved bytes must decode as a valid PNG.
	_, err = png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
}

func TestSQLiteStore_Dedup(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := context.Background()

	raw := makeTinyPNG(t)

	id1, _, err := s.Put(ctx, raw)
	require.NoError(t, err)

	id2, _, err := s.Put(ctx, raw)
	require.NoError(t, err)

	assert.Equal(t, id1, id2, "identical payloads must produce the same ID")

	// Confirm only one row was inserted.
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE id = ?`, id1).Scan(&count))
	assert.Equal(t, 1, count, "dedup must not produce duplicate rows")
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := context.Background()

	_, _, err := s.Get(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestSqliteDSN_PathFormats guards against a regression where url.URL.String()
// placed a relative path in the authority component (e.g. `file://images.db`),
// causing modernc.org/sqlite to error at first query with
// "invalid uri authority". Both absolute and relative paths must produce a
// DSN with no authority component, and must carry the synchronous=NORMAL
// pragma alongside journal_mode=WAL.
func TestSqliteDSN_PathFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"absolute path", "/tmp/images.db", "file:/tmp/images.db?"},
		{"relative path", "images.db", "file:images.db?"},
		{"nested relative path", "data/images.db", "file:data/images.db?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dsn := sqliteDSN(tc.path)

			// Reject the broken `file://...` (authority) form.
			assert.False(t, strings.HasPrefix(dsn, "file://"),
				"DSN must not place path in authority component: %q", dsn)
			assert.True(t, strings.HasPrefix(dsn, tc.want),
				"DSN must start with %q, got %q", tc.want, dsn)
			assert.Contains(t, dsn, "_pragma=journal_mode(WAL)")
			assert.Contains(t, dsn, "_pragma=synchronous(NORMAL)")
			assert.Contains(t, dsn, "_pragma=busy_timeout(5000)")
		})
	}
}

// TestSQLiteStore_OpenRelativePath verifies the store opens cleanly when
// given a path relative to the current working directory. This is the
// scenario that the previous url.URL-based DSN broke (it produced
// `file://images.db?...`, which modernc/sqlite rejects at first query).
func TestSQLiteStore_OpenRelativePath(t *testing.T) {
	// Not parallel: mutates the process-wide working directory.
	dir := t.TempDir()

	origWD, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))

	t.Cleanup(func() { _ = os.Chdir(origWD) })

	s, err := Open("images.db")
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	// Smoke-test that the database is actually usable, not just open.
	id, _, err := s.Put(context.Background(), makeTinyPNG(t))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}
