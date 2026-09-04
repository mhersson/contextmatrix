package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ErrRepoNotShared is returned when a caller asks to store an image as a
// file in a boards repo that is private. Private repos keep images.db.
var ErrRepoNotShared = errors.New("boards repo is not shared")

// The card service is the repo writer the layered image store routes
// uploads through; pin that it keeps satisfying that seam.
var _ images.RepoWriter = (*CardService)(nil)

// ImagesInRepo names the boards repo that stores project's images as files:
// the project's own repo, and only when that repo is shared. A private
// repo, an unknown project and an empty name report false, and the image
// stays in images.db.
func (s *CardService) ImagesInRepo(ctx context.Context, project string) (string, bool) {
	if project == "" {
		return "", false
	}

	if _, err := s.store.GetProject(ctx, project); err != nil {
		return "", false
	}

	r := s.repoOf(project)
	if !r.Shared {
		return "", false
	}

	return r.Name, true
}

// WriteRepoImages writes files under <project>/images/ in the shared repo
// that owns project and commits them as one commit through that repo's
// queue. The files are written and the commit enqueued under the write
// lock, like a card write, so nothing lands while a sync cycle is merging;
// the commit is awaited outside it. A failed commit removes the files this
// call created. Deferred-commit mode does not apply: an image is not tied
// to a card's lifecycle, so it is committed at once.
func (s *CardService) WriteRepoImages(ctx context.Context, project string, files []images.RepoImage) error {
	if len(files) == 0 {
		return nil
	}

	for _, f := range files {
		if !images.IsRepoFileName(f.Name) {
			return fmt.Errorf("%w: image file name %q", storage.ErrInvalidInput, f.Name)
		}
	}

	if _, err := s.store.GetProject(ctx, project); err != nil {
		return fmt.Errorf("write repo images: %w", err)
	}

	r := s.repoOf(project)
	if !r.Shared {
		return fmt.Errorf("write repo images in %s: %w", r.Name, ErrRepoNotShared)
	}

	rel := make([]string, len(files))
	for i, f := range files {
		rel[i] = filepath.Join(project, images.ImagesDir, f.Name)
	}

	s.writeMu.Lock()

	created, err := writeRepoFiles(r.Dir, rel, files)
	if err != nil {
		removeRepoFiles(ctx, r.Dir, created)
		s.writeMu.Unlock()

		return fmt.Errorf("write repo images: %w", err)
	}

	if !r.GitAutoCommit {
		s.writeMu.Unlock()

		return nil
	}

	msg := repoImagesCommitMessage(project, files)

	var done <-chan error

	if r.Queue != nil {
		done = r.Queue.Enqueue(gitops.CommitJob{
			Project: project,
			Kind:    gitops.CommitKindFiles,
			Paths:   rel,
			Message: msg,
			Ctx:     ctx,
		})
	} else {
		ch := make(chan error, 1)
		ch <- r.Git.CommitFiles(ctx, rel, msg)

		close(ch)

		done = ch
	}

	s.writeMu.Unlock()

	if err := <-done; err != nil {
		s.writeMu.Lock()
		removeRepoFiles(ctx, r.Dir, created)
		s.writeMu.Unlock()

		return fmt.Errorf("git commit images: %w", err)
	}

	r.notifyCommit()

	return nil
}

// writeRepoFiles writes each file at dir/rel[i] and returns the relative
// paths of the files that did not exist before, so a failure can undo
// exactly what this call added and nothing a peer already pushed.
func writeRepoFiles(dir string, rel []string, files []images.RepoImage) ([]string, error) {
	var created []string

	for i, f := range files {
		full := filepath.Join(dir, rel[i])

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return created, fmt.Errorf("mkdir for %s: %w", rel[i], err)
		}

		_, statErr := os.Stat(full)
		existed := statErr == nil

		if err := os.WriteFile(full, f.Data, 0o644); err != nil {
			return created, fmt.Errorf("write %s: %w", rel[i], err)
		}

		if !existed {
			created = append(created, rel[i])
		}
	}

	return created, nil
}

func removeRepoFiles(ctx context.Context, dir string, rel []string) {
	for _, p := range rel {
		if err := os.Remove(filepath.Join(dir, p)); err != nil && !errors.Is(err, os.ErrNotExist) {
			ctxlog.Logger(ctx).Warn("remove repo image after failed commit", "path", p, "error", err)
		}
	}
}

func repoImagesCommitMessage(project string, files []images.RepoImage) string {
	if len(files) == 1 {
		id := strings.TrimSuffix(files[0].Name, filepath.Ext(files[0].Name))

		return fmt.Sprintf("[contextmatrix] %s: add image %s", project, id)
	}

	return fmt.Sprintf("[contextmatrix] %s: add %d images", project, len(files))
}
