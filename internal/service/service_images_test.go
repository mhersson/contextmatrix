package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const (
	imgIDOne = "aabbccddeeff0011"
	imgIDTwo = "0123456789abcdef"
)

func TestImagesInRepo(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		project  string
		wantRepo string
		wantOK   bool
	}{
		{"alpha", "team", true},
		{"beta", "", false},
		{"nope", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run("project="+tt.project, func(t *testing.T) {
			repo, ok := sp.svc.ImagesInRepo(ctx, tt.project)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestImagesInRepo_SingleRepoServiceIsPrivate(t *testing.T) {
	svc, _, cleanup := setupTest(t)
	defer cleanup()

	_, ok := svc.ImagesInRepo(context.Background(), "test-project")
	assert.False(t, ok, "the single-repo constructor is never shared")
}

func TestWriteRepoImages_WritesAndCommitsThroughTheQueue(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	var notified atomic.Int32

	require.NoError(t, sp.svc.SetOnCommitFor("team", func() { notified.Add(1) }))

	err := sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{{Name: imgIDOne + ".png", Data: []byte("png bytes")}})
	require.NoError(t, err)

	path := filepath.Join(sp.team.Dir, "alpha", images.ImagesDir, imgIDOne+".png")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("png bytes"), data)

	msg, err := sp.team.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "[contextmatrix] alpha: add image "+imgIDOne, strings.TrimSpace(msg))

	clean, _, err := sp.team.Git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean, "the file is committed, nothing is left dirty")
	assert.Equal(t, int32(1), notified.Load(), "the on-commit hook fires so the syncer pushes")
}

func TestWriteRepoImages_SeveralFilesAreOneCommit(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	before, err := sp.team.Git.CommitCount()
	require.NoError(t, err)

	err = sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{
		{Name: imgIDOne + ".png", Data: []byte("one")},
		{Name: imgIDTwo + ".jpg", Data: []byte("two")},
	})
	require.NoError(t, err)

	after, err := sp.team.Git.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, before+1, after)

	msg, err := sp.team.Git.GetLastCommitMessage()
	require.NoError(t, err)
	assert.Equal(t, "[contextmatrix] alpha: add 2 images", strings.TrimSpace(msg))
	assert.FileExists(t, filepath.Join(sp.team.Dir, "alpha", images.ImagesDir, imgIDTwo+".jpg"))
}

func TestWriteRepoImages_RefusesPrivateRepoUnknownProjectAndBadNames(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()
	good := []images.RepoImage{{Name: imgIDOne + ".png", Data: []byte("x")}}

	err := sp.svc.WriteRepoImages(ctx, "beta", good)
	require.ErrorIs(t, err, ErrRepoNotShared)
	assert.NoFileExists(t, filepath.Join(sp.private.Dir, "beta", images.ImagesDir, imgIDOne+".png"))

	err = sp.svc.WriteRepoImages(ctx, "nope", good)
	require.ErrorIs(t, err, storage.ErrProjectNotFound)

	for _, name := range []string{"../" + imgIDOne + ".png", "notahash.png", imgIDOne + ".gif", imgIDOne, "sub/" + imgIDOne + ".png"} {
		t.Run(name, func(t *testing.T) {
			err := sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{{Name: name, Data: []byte("x")}})
			require.ErrorIs(t, err, storage.ErrInvalidInput)
		})
	}

	assert.NoError(t, sp.svc.WriteRepoImages(ctx, "alpha", nil), "nothing to write is not an error")
}

func TestWriteRepoImages_CommitFailureRemovesOnlyTheFilesItCreated(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()

	// One file already committed by an earlier successful write.
	require.NoError(t, sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{{Name: imgIDOne + ".png", Data: []byte("one")}}))

	sentinel := errors.New("disk on fire")
	_ = sp.team.Queue.Close(ctx)
	sp.team.Queue = gitops.NewCommitQueueWithCommitter(&failingCommitter{err: sentinel}, 0)

	err := sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{
		{Name: imgIDOne + ".png", Data: []byte("one")},
		{Name: imgIDTwo + ".jpg", Data: []byte("two")},
	})
	require.ErrorIs(t, err, sentinel)

	assert.FileExists(t, filepath.Join(sp.team.Dir, "alpha", images.ImagesDir, imgIDOne+".png"), "a file that existed before the call is kept")
	assert.NoFileExists(t, filepath.Join(sp.team.Dir, "alpha", images.ImagesDir, imgIDTwo+".jpg"), "the file this call created is removed")
}

func TestWriteRepoImages_NoAutoCommitLeavesTheFileUncommitted(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()
	sp.team.GitAutoCommit = false

	before, err := sp.team.Git.CommitCount()
	require.NoError(t, err)

	require.NoError(t, sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{{Name: imgIDOne + ".png", Data: []byte("one")}}))

	after, err := sp.team.Git.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.FileExists(t, filepath.Join(sp.team.Dir, "alpha", images.ImagesDir, imgIDOne+".png"))
}

func TestWriteRepoImages_DeferredCommitModeStillCommitsAtOnce(t *testing.T) {
	sp, cleanup := newSharedAndPrivateService(t)
	defer cleanup()

	ctx := context.Background()
	sp.team.GitDeferredCommit = true

	before, err := sp.team.Git.CommitCount()
	require.NoError(t, err)

	require.NoError(t, sp.svc.WriteRepoImages(ctx, "alpha", []images.RepoImage{{Name: imgIDOne + ".png", Data: []byte("one")}}))

	after, err := sp.team.Git.CommitCount()
	require.NoError(t, err)
	assert.Equal(t, before+1, after, "an image commits at once even when the repo defers card commits")

	clean, _, err := sp.team.Git.IsClean(ctx)
	require.NoError(t, err)
	assert.True(t, clean)
}
