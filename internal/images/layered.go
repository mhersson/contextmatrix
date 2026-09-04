package images

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
)

// RepoImage is one file to write into a boards repo, named <id>.<ext>.
type RepoImage struct {
	Name string
	Data []byte
}

// RepoWriter is what Layered needs from the card service: which projects
// store their images in a boards repo, and a way to write files there
// through that repo's write lock and commit queue.
type RepoWriter interface {
	// ImagesInRepo names the repo that stores project's images as files.
	// ok is false for a project-less upload, an unknown project, and a
	// project whose repo is private; the image then stays in images.db.
	ImagesInRepo(ctx context.Context, project string) (repo string, ok bool)
	// WriteRepoImages writes files under <project>/images/ in the repo
	// that owns project and commits them as one commit.
	WriteRepoImages(ctx context.Context, project string, files []RepoImage) error
}

// compile-time assertion that *Layered satisfies Store.
var _ Store = (*Layered)(nil)

// Layered serves images from the boards repos first and images.db second.
// Uploads that name a project whose repo is shared are written into that
// repo; every other upload goes to images.db. With no repo index it is a
// transparent wrapper over db.
type Layered struct {
	db     Store
	writer RepoWriter
	repos  []*RepoIndex // config order
	byName map[string]*RepoIndex
}

// NewLayered layers repos, in config order, over db. writer may be nil
// when there are no repos.
func NewLayered(db Store, writer RepoWriter, repos ...*RepoIndex) *Layered {
	byName := make(map[string]*RepoIndex, len(repos))
	for _, r := range repos {
		byName[r.Name()] = r
	}

	return &Layered{db: db, writer: writer, repos: repos, byName: byName}
}

// Put stores a project-less upload in images.db.
func (l *Layered) Put(ctx context.Context, raw []byte) (string, string, error) {
	return l.db.Put(ctx, raw)
}

// PutIn stores an upload for project: in the project's boards repo when
// that repo is shared, otherwise in images.db. The same image uploaded
// twice into one project is written once.
func (l *Layered) PutIn(ctx context.Context, project string, raw []byte) (string, string, error) {
	idx := l.indexFor(ctx, project)
	if idx == nil {
		return l.db.Put(ctx, raw)
	}

	processed, contentType, err := Process(raw)
	if err != nil {
		return "", "", err
	}

	id := IDOf(processed)

	ext, ok := ExtensionFor(contentType)
	if !ok {
		return "", "", fmt.Errorf("images: no repo file extension for %s", contentType)
	}

	if idx.Has(project, id) {
		return id, contentType, nil
	}

	if err := l.writer.WriteRepoImages(ctx, project, []RepoImage{{Name: id + ext, Data: processed}}); err != nil {
		return "", "", fmt.Errorf("images: write to repo %s: %w", idx.Name(), err)
	}

	idx.Add(project, id, contentType)

	return id, contentType, nil
}

// indexFor returns the index of the shared repo that stores project's
// images, nil when the image belongs in images.db.
func (l *Layered) indexFor(ctx context.Context, project string) *RepoIndex {
	if project == "" || l.writer == nil || len(l.repos) == 0 {
		return nil
	}

	repo, ok := l.writer.ImagesInRepo(ctx, project)
	if !ok {
		return nil
	}

	return l.byName[repo]
}

// Get reads id from the repos in config order, then from images.db.
func (l *Layered) Get(ctx context.Context, id string) ([]byte, string, error) {
	for _, idx := range l.repos {
		data, contentType, err := idx.Get(ctx, id)
		if err == nil {
			return data, contentType, nil
		}

		if !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
	}

	return l.db.Get(ctx, id)
}

// Close closes images.db. Repo indexes hold no resources.
func (l *Layered) Close() error {
	return l.db.Close()
}

// Export writes into repo every image that images.db holds and refs names
// but the repo does not yet hold, one commit per project, projects in
// name order. refs maps a project to the ids its card bodies reference.
// Returns the number of files written; on a failed write the count is
// what landed before it. Idempotent: a second call writes nothing.
func (l *Layered) Export(ctx context.Context, repo string, refs map[string][]string) (int, error) {
	idx := l.byName[repo]
	if idx == nil {
		return 0, fmt.Errorf("images: no repo index for %q", repo)
	}

	projects := make([]string, 0, len(refs))
	for p := range refs {
		projects = append(projects, p)
	}

	sort.Strings(projects)

	written := 0

	for _, project := range projects {
		files, err := l.missingFromRepo(ctx, idx, project, refs[project])
		if err != nil {
			return written, err
		}

		if len(files) == 0 {
			continue
		}

		if err := l.writer.WriteRepoImages(ctx, project, files); err != nil {
			return written, fmt.Errorf("images: export %d images into %s/%s: %w", len(files), repo, project, err)
		}

		for _, f := range files {
			ext := f.Name[len(f.Name)-4:]
			idx.Add(project, f.Name[:len(f.Name)-4], contentTypeOf(ext[1:]))
		}

		written += len(files)
	}

	return written, nil
}

// missingFromRepo collects, from images.db, the referenced ids project
// does not hold yet, deduplicated, in reference order.
func (l *Layered) missingFromRepo(ctx context.Context, idx *RepoIndex, project string, ids []string) ([]RepoImage, error) {
	seen := make(map[string]struct{}, len(ids))
	files := make([]RepoImage, 0)

	for _, id := range ids {
		if _, dup := seen[id]; dup || idx.Has(project, id) {
			continue
		}

		seen[id] = struct{}{}

		data, contentType, err := l.db.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}

			return nil, fmt.Errorf("images: read %s for export: %w", id, err)
		}

		ext, ok := ExtensionFor(contentType)
		if !ok {
			slog.Warn("images: stored image has no repo form, left in the database", "id", id, "content_type", contentType)

			continue
		}

		files = append(files, RepoImage{Name: id + ext, Data: data})
	}

	return files, nil
}
