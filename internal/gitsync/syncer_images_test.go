package gitsync

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/images"
)

// syncPNG produces a 4x4 PNG whose red channel is fixed, so tests that
// need distinct images get distinct ids.
func syncPNG(t *testing.T, red uint8) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: red, G: 40, B: 200, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// imageNode gives a shared node the image stack production wires: a repo
// index on its clone hooked into its syncer, and a layered store over a
// private images.db with the node's service as the repo writer.
func imageNode(t *testing.T, n *sharedNode) (*images.Layered, *images.RepoIndex) {
	t.Helper()

	idx := images.NewRepoIndex("team", n.dir)
	require.NoError(t, idx.Reload(context.Background(), []string{"test-project"}))
	n.syncer.SetImages(idx)

	db, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	return images.NewLayered(db, n.svc, idx), idx
}

type recordingImageSync struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingImageSync) Reload(_ context.Context, projects []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, append([]string(nil), projects...))

	return nil
}

func (r *recordingImageSync) reloads() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([][]string(nil), r.calls...)
}

func TestShared_ImageUploadedOnOneNodeIsServedOnTheOtherAfterPull(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	la, _ := imageNode(t, a)
	lb, ib := imageNode(t, b)
	ctx := context.Background()

	id, ct, err := la.PutIn(ctx, "test-project", syncPNG(t, 10))
	require.NoError(t, err)
	assert.Equal(t, "image/png", ct)

	onA, err := os.ReadFile(filepath.Join(a.dir, "test-project", images.ImagesDir, id+".png"))
	require.NoError(t, err)
	assert.Equal(t, "[contextmatrix] test-project: add image "+id, a.lastCommit(t))

	_, _, err = lb.Get(ctx, id)
	require.ErrorIs(t, err, images.ErrNotFound, "b has not pulled yet")

	a.sync(t)
	b.sync(t)

	assert.True(t, ib.Has("test-project", id), "the pull reloaded b's index")

	onB, gotCT, err := lb.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "image/png", gotCT)
	assert.Equal(t, onA, onB)

	clean, _, err := b.git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean)
}

func TestShared_SameImageUploadedOnBothNodesMergesCleanly(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	la, _ := imageNode(t, a)
	lb, ib := imageNode(t, b)
	ctx := context.Background()

	raw := syncPNG(t, 20)

	idA, _, err := la.PutIn(ctx, "test-project", raw)
	require.NoError(t, err)

	idB, _, err := lb.PutIn(ctx, "test-project", raw)
	require.NoError(t, err)
	require.Equal(t, idA, idB)

	a.sync(t)
	report := b.sync(t)
	assert.Empty(t, report.Resolutions, "identical bytes at an identical path never conflict")

	require.True(t, ib.Has("test-project", idA))

	_, _, err = lb.Get(ctx, idA)
	require.NoError(t, err)

	clean, _, err := b.git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean)
}

func TestShared_PullReloadsTheImageIndexWithTheRepoProjects(t *testing.T) {
	a, b, _ := setupSharedPair(t)
	rec := &recordingImageSync{}
	b.syncer.SetImages(rec)

	a.create(t, "something to pull")
	a.sync(t)
	b.sync(t)

	reloads := rec.reloads()
	require.NotEmpty(t, reloads, "a pull that changed the tree reloads the index")
	assert.Equal(t, []string{"test-project"}, reloads[len(reloads)-1])
}

func TestMultiRepo_PrivateProjectImagesStayInTheDatabase(t *testing.T) {
	a, _ := setupMultiPair(t)
	ctx := context.Background()

	idx := images.NewRepoIndex("team", a.teamDir)
	require.NoError(t, idx.Reload(ctx, []string{"test-project"}))
	a.syncer.SetImages(idx)

	db, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	layered := images.NewLayered(db, a.svc, idx)

	privID, _, err := layered.PutIn(ctx, "private-project", syncPNG(t, 30))
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(a.privDir, "private-project", images.ImagesDir, privID+".png"))

	_, _, err = db.Get(ctx, privID)
	require.NoError(t, err, "a private project's image lives in images.db")

	teamID, _, err := layered.PutIn(ctx, "test-project", syncPNG(t, 40))
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(a.teamDir, "test-project", images.ImagesDir, teamID+".png"))

	_, _, err = db.Get(ctx, teamID)
	require.ErrorIs(t, err, images.ErrNotFound, "a shared project's image never lands in images.db")

	for _, id := range []string{privID, teamID} {
		_, _, err := layered.Get(ctx, id)
		assert.NoError(t, err, id)
	}
}
