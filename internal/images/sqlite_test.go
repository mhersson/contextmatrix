package images

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestStore opens a sqliteStore backed by a temp-dir database.
func openTestStore(t *testing.T) *sqliteStore {
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

func TestSQLiteStore_HasUnknown(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := context.Background()

	ok, err := s.Has(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSQLiteStore_HasKnown(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := context.Background()

	raw := makeTinyPNG(t)

	id, _, err := s.Put(ctx, raw)
	require.NoError(t, err)

	ok, err := s.Has(ctx, id)
	require.NoError(t, err)
	assert.True(t, ok)
}
