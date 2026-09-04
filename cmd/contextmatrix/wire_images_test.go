package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/service"
)

func wirePNG(t *testing.T, red uint8) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: red, G: 90, B: 10, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// wireSharedBoards builds one shared repo "team" (project alpha, with an
// origin) next to one private repo "private" (project beta), plus the
// service over both.
func wireSharedBoards(t *testing.T) (*boardsBundles, *config.Config, *service.CardService) {
	t.Helper()

	shared := wireGitSyncUpstream(t)
	private := t.TempDir()
	wireProject(t, private, "beta", "BETA")

	sharedEntry := wireEntry("team", shared)
	sharedEntry.Shared = true
	sharedEntry.GitAutoPull, sharedEntry.GitAutoPush = true, true

	cfg := &config.Config{
		Boards:   config.Boards{wireEntry("private", private), sharedEntry},
		Instance: config.InstanceConfig{ID: "lap-a"},
		Images:   config.ImagesConfig{DBPath: filepath.Join(t.TempDir(), "images.db")},
	}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	t.Cleanup(func() {
		for _, q := range boards.queues() {
			_ = q.Close(context.Background())
		}
	})

	svc, err := service.NewCardServiceRepos(boards.composite, events.NewBus(), nil, boards.svcRepos...)
	require.NoError(t, err)

	return boards, cfg, svc
}

func TestWireImages_ExportsReferencedImagesIntoTheSharedRepo(t *testing.T) {
	boards, cfg, svc := wireSharedBoards(t)
	ctx := context.Background()

	// Seed images.db the way an instance that ran before the repo turned
	// shared would have: two blobs, one referenced by a shared card, one by
	// a private card, one referenced nowhere.
	db, err := images.Open(cfg.Images.DBPath)
	require.NoError(t, err)

	sharedID, _, err := db.Put(ctx, wirePNG(t, 10))
	require.NoError(t, err)

	privateID, _, err := db.Put(ctx, wirePNG(t, 20))
	require.NoError(t, err)

	orphanID, _, err := db.Put(ctx, wirePNG(t, 30))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = svc.CreateCard(ctx, "alpha", service.CreateCardInput{
		Title: "with a screenshot", Type: "task", Priority: "low",
		Body: "see ![shot](/api/images/" + sharedID + ") and a dangling ![](/api/images/ffffffffffffffff)",
	})
	require.NoError(t, err)

	_, err = svc.CreateCard(ctx, "beta", service.CreateCardInput{
		Title: "private", Type: "task", Priority: "low",
		Body: "![](/api/images/" + privateID + ")",
	})
	require.NoError(t, err)

	layered, err := wireImages(ctx, cfg, boards, svc)
	require.NoError(t, err)

	t.Cleanup(func() { _ = layered.Close() })

	teamDir := boards.repos[1].cfg.Dir
	assert.FileExists(t, filepath.Join(teamDir, "alpha", images.ImagesDir, sharedID+".png"))
	assert.NoFileExists(t, filepath.Join(teamDir, "alpha", images.ImagesDir, orphanID+".png"), "an unreferenced image is not exported")
	assert.NoFileExists(t, filepath.Join(boards.repos[0].cfg.Dir, "beta", images.ImagesDir, privateID+".png"), "a private repo gets no files")
	assert.True(t, boards.repos[1].images.Has("alpha", sharedID))

	clean, _, err := boards.repos[1].git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean, "the export is committed")

	msg, err := boards.repos[1].git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Contains(t, msg, "alpha: add image "+sharedID)

	for _, id := range []string{sharedID, privateID, orphanID} {
		_, _, err := layered.Get(ctx, id)
		require.NoError(t, err, id)
	}

	// A second start exports nothing more.
	before, err := boards.repos[1].git.CommitCount()
	require.NoError(t, err)

	exportRepoImages(ctx, boards, svc, layered)

	after, err := boards.repos[1].git.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestReferencedImages_CollectsPerVisibleProject(t *testing.T) {
	boards, _, svc := wireSharedBoards(t)
	ctx := context.Background()

	_, err := svc.CreateCard(ctx, "alpha", service.CreateCardInput{
		Title: "a", Type: "task", Priority: "low",
		Body: "![](/api/images/aabbccddeeff0011) ![](/api/images/aabbccddeeff0011) ![](/api/images/0123456789abcdef)",
	})
	require.NoError(t, err)

	view, err := boards.composite.View("team")
	require.NoError(t, err)

	refs, err := referencedImages(ctx, svc, view)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"alpha": {"aabbccddeeff0011", "0123456789abcdef"}}, refs)
}

func TestWireImages_WithoutSharedReposIsAPlainStore(t *testing.T) {
	dir := t.TempDir()
	wireProject(t, dir, "alpha", "ALPHA")

	cfg := &config.Config{
		Boards: config.Boards{wireEntry("boards", dir)},
		Images: config.ImagesConfig{DBPath: filepath.Join(t.TempDir(), "images.db")},
	}

	boards, err := buildBoards(cfg, wireProvider(t), 30*time.Minute, clock.Real())
	require.NoError(t, err)

	t.Cleanup(func() {
		for _, q := range boards.queues() {
			_ = q.Close(context.Background())
		}
	})

	svc, err := service.NewCardServiceRepos(boards.composite, events.NewBus(), nil, boards.svcRepos...)
	require.NoError(t, err)

	layered, err := wireImages(context.Background(), cfg, boards, svc)
	require.NoError(t, err)

	t.Cleanup(func() { _ = layered.Close() })

	assert.Empty(t, boards.imageIndexes())

	id, _, err := layered.PutIn(context.Background(), "alpha", wirePNG(t, 5))
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "alpha", images.ImagesDir, id+".png"), "a private single repo never gets image files")

	_, statErr := os.Stat(cfg.Images.DBPath)
	assert.NoError(t, statErr)
}
