package images

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRepoImage puts data at <dir>/<project>/images/<name> and returns its id.
func writeRepoImage(t *testing.T, dir, project, name string, data []byte) string {
	t.Helper()

	imagesDir := filepath.Join(dir, project, ImagesDir)
	require.NoError(t, os.MkdirAll(imagesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(imagesDir, name), data, 0o644))

	return IDOf(data)
}

func TestIDOf_MatchesSQLiteStoreIDs(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	raw := makeTinyPNG(t)

	id, _, err := s.Put(context.Background(), raw)
	require.NoError(t, err)

	processed, _, err := Process(raw)
	require.NoError(t, err)

	assert.Equal(t, id, IDOf(processed))
	assert.Len(t, id, IDLen)
}

func TestExtensionFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ct   string
		want string
		ok   bool
	}{
		{"image/png", ".png", true},
		{"image/jpeg", ".jpg", true},
		{"image/gif", "", false},
		{"image/webp", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			t.Parallel()

			ext, ok := ExtensionFor(tt.ct)
			assert.Equal(t, tt.want, ext)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestRepoIndex_ReloadIndexesOnlyListedProjectsAndWellFormedNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	png := makeTinyPNG(t)
	jpg := []byte("not really a jpeg but the index does not decode")

	pngID := writeRepoImage(t, dir, "alpha", IDOf(png)+".png", png)
	jpgID := writeRepoImage(t, dir, "beta", IDOf(jpg)+".jpg", jpg)
	hiddenID := writeRepoImage(t, dir, "hidden", IDOf([]byte("h"))+".png", []byte("h"))
	writeRepoImage(t, dir, "alpha", "notahash.png", []byte("x"))
	writeRepoImage(t, dir, "alpha", IDOf([]byte("g"))+".gif", []byte("g"))
	writeRepoImage(t, dir, "alpha", IDOf([]byte("u"))+".PNG", []byte("u"))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "alpha", ImagesDir, IDOf([]byte("d"))+".png"), 0o755), "a directory with a valid name is skipped")

	idx := NewRepoIndex("team", dir)
	require.NoError(t, idx.Reload(ctx, []string{"alpha", "beta", "noimages"}))

	assert.Equal(t, "team", idx.Name())
	assert.Equal(t, dir, idx.Dir())
	assert.Equal(t, 2, idx.Len())
	assert.True(t, idx.Has("alpha", pngID))
	assert.True(t, idx.Has("beta", jpgID))
	assert.False(t, idx.Has("beta", pngID), "dedupe is per project")
	assert.False(t, idx.Has("hidden", hiddenID), "a project not in the list is not indexed")

	data, ct, err := idx.Get(ctx, pngID)
	require.NoError(t, err)
	assert.Equal(t, png, data)
	assert.Equal(t, "image/png", ct)

	data, ct, err = idx.Get(ctx, jpgID)
	require.NoError(t, err)
	assert.Equal(t, jpg, data)
	assert.Equal(t, "image/jpeg", ct, "content type comes from the extension")

	_, _, err = idx.Get(ctx, hiddenID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRepoIndex_ReloadReplacesEarlierEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	png := makeTinyPNG(t)
	id := writeRepoImage(t, dir, "alpha", IDOf(png)+".png", png)

	idx := NewRepoIndex("team", dir)
	require.NoError(t, idx.Reload(ctx, []string{"alpha"}))
	require.True(t, idx.Has("alpha", id))

	require.NoError(t, os.Remove(filepath.Join(dir, "alpha", ImagesDir, id+".png")))
	require.NoError(t, idx.Reload(ctx, []string{"alpha"}))

	assert.False(t, idx.Has("alpha", id))
	assert.Equal(t, 0, idx.Len())
}

func TestRepoIndex_GetRejectsBytesThatDoNotHashToTheID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	id := IDOf([]byte("what the name claims"))
	writeRepoImage(t, dir, "alpha", id+".png", []byte("something else"))

	idx := NewRepoIndex("team", dir)
	require.NoError(t, idx.Reload(ctx, []string{"alpha"}))
	require.True(t, idx.Has("alpha", id), "the name is trusted at index time")

	_, _, err := idx.Get(ctx, id)
	assert.ErrorIs(t, err, ErrNotFound, "the bytes are verified on read")
}

func TestRepoIndex_AddServesWithoutReloadAndStaleEntriesReadAsNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	png := makeTinyPNG(t)
	id := writeRepoImage(t, dir, "alpha", IDOf(png)+".png", png)

	idx := NewRepoIndex("team", dir)
	idx.Add("alpha", id, "image/png")

	assert.True(t, idx.Has("alpha", id))
	assert.Equal(t, 1, idx.Len())

	data, ct, err := idx.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, png, data)
	assert.Equal(t, "image/png", ct)

	require.NoError(t, os.Remove(filepath.Join(dir, "alpha", ImagesDir, id+".png")))

	_, _, err = idx.Get(ctx, id)
	assert.ErrorIs(t, err, ErrNotFound, "a deleted file is not found, not an error")
}

func TestRepoIndex_GetFindsTheSameIDInASecondProject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	png := makeTinyPNG(t)
	id := writeRepoImage(t, dir, "alpha", IDOf(png)+".png", png)
	writeRepoImage(t, dir, "beta", IDOf(png)+".png", png)

	idx := NewRepoIndex("team", dir)
	require.NoError(t, idx.Reload(ctx, []string{"alpha", "beta"}))
	require.Equal(t, 2, idx.Len())

	require.NoError(t, os.Remove(filepath.Join(dir, "alpha", ImagesDir, id+".png")))

	data, _, err := idx.Get(ctx, id)
	require.NoError(t, err, "the copy in beta still serves")
	assert.Equal(t, png, data)
}

func TestRepoIndex_ReloadHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	idx := NewRepoIndex("team", t.TempDir())
	assert.ErrorIs(t, idx.Reload(ctx, []string{"alpha"}), context.Canceled)
}
