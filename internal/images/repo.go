package images

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

// ImagesDir is the directory under a project in a boards repo that holds
// the project's images, one file per content-hash id.
const ImagesDir = "images"

// repoFileName is the shape of a repo-stored image file: the id and one of
// the extensions the processor's output formats map to.
var repoFileName = regexp.MustCompile(`^(` + IDPatternFragment + `)\.(png|jpg)$`)

// ExtensionFor maps a processed content type to the file extension a
// repo-stored image carries. The processor only ever emits image/png and
// image/jpeg; anything else has no repo form.
func ExtensionFor(contentType string) (string, bool) {
	switch contentType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	default:
		return "", false
	}
}

func contentTypeOf(ext string) string {
	if ext == "jpg" {
		return "image/jpeg"
	}

	return "image/png"
}

// RepoIndex is the in-memory index of the images stored as files in one
// boards repo, under <project>/images/<id>.<ext>. Names are trusted when
// indexed and the bytes are verified against the id when read. Safe for
// concurrent use.
type RepoIndex struct {
	name string
	dir  string

	mu       sync.RWMutex
	entries  map[string]map[string]string // project -> id -> content type
	projects []string                     // keys of entries, sorted
}

// NewRepoIndex returns an empty index for the repo named name rooted at dir.
func NewRepoIndex(name, dir string) *RepoIndex {
	return &RepoIndex{name: name, dir: dir, entries: make(map[string]map[string]string)}
}

func (x *RepoIndex) Name() string { return x.name }

func (x *RepoIndex) Dir() string { return x.dir }

// Reload rebuilds the index from the images directories of the given
// projects, replacing every earlier entry. A project without an images
// directory contributes nothing; a file that does not match the repo name
// shape is ignored.
func (x *RepoIndex) Reload(ctx context.Context, projects []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	entries := make(map[string]map[string]string, len(projects))

	for _, project := range projects {
		found, err := x.scan(project)
		if err != nil {
			return err
		}

		if len(found) > 0 {
			entries[project] = found
		}
	}

	x.mu.Lock()
	x.entries = entries
	x.projects = sortedKeys(entries)
	x.mu.Unlock()

	return nil
}

func (x *RepoIndex) scan(project string) (map[string]string, error) {
	dirEntries, err := os.ReadDir(filepath.Join(x.dir, project, ImagesDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("images: read %s/%s: %w", project, ImagesDir, err)
	}

	found := make(map[string]string)

	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}

		m := repoFileName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}

		found[m[1]] = contentTypeOf(m[2])
	}

	return found, nil
}

// Has reports whether project holds an image with id.
func (x *RepoIndex) Has(project, id string) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()

	_, ok := x.entries[project][id]

	return ok
}

// Add records that project now holds an image with id and contentType,
// after the caller wrote the file.
func (x *RepoIndex) Add(project, id, contentType string) {
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.entries[project] == nil {
		x.entries[project] = make(map[string]string)
		x.projects = sortedKeys(x.entries)
	}

	x.entries[project][id] = contentType
}

// Len returns the number of indexed images across projects.
func (x *RepoIndex) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()

	n := 0
	for _, ids := range x.entries {
		n += len(ids)
	}

	return n
}

// Get reads the image with id from the first project, in name order, that
// holds it. A file that has gone missing or whose bytes do not hash to the
// id is skipped; ErrNotFound when nothing serves the id.
func (x *RepoIndex) Get(ctx context.Context, id string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	x.mu.RLock()

	candidates := make([]struct{ project, ct string }, 0, 1)

	for _, project := range x.projects {
		if ct, ok := x.entries[project][id]; ok {
			candidates = append(candidates, struct{ project, ct string }{project, ct})
		}
	}

	x.mu.RUnlock()

	for _, c := range candidates {
		ext, _ := ExtensionFor(c.ct)
		path := filepath.Join(x.dir, c.project, ImagesDir, id+ext)

		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, "", fmt.Errorf("images: read %s: %w", path, err)
		}

		if IDOf(data) != id {
			slog.Warn("images: repo file does not hash to its name, skipped",
				"repo", x.name, "project", c.project, "id", id)

			continue
		}

		return data, c.ct, nil
	}

	return nil, "", ErrNotFound
}

func sortedKeys(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
