package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

const playbooksDirName = "playbooks"

var (
	// ErrPlaybookNotFound is returned when a playbook id is not indexed.
	ErrPlaybookNotFound = errors.New("playbook not found")

	// ErrPlaybookExists is returned when attempting to create a playbook
	// whose id is already indexed or already present on disk.
	ErrPlaybookExists = errors.New("playbook already exists")

	// ErrPlaybooksDirIsProject is returned when the playbooks directory is
	// occupied by a pre-existing project legally named "playbooks".
	ErrPlaybooksDirIsProject = errors.New("playbooks directory belongs to a project")
)

// FilesystemPlaybookStore implements an in-memory index over
// <boardsDir>/playbooks/*.yaml. Cards are not the unit of storage here -
// each file is a whole Playbook. Reads and writes copy at every boundary so
// callers can never mutate the cached value through a returned pointer.
type FilesystemPlaybookStore struct {
	boardsDir string
	mu        sync.RWMutex
	playbooks map[string]*board.Playbook
}

// NewFilesystemPlaybookStore creates a new FilesystemPlaybookStore and loads
// the index. A missing playbooks/ directory is not an error - it yields an
// empty index and is created on first write.
func NewFilesystemPlaybookStore(boardsDir string) (*FilesystemPlaybookStore, error) {
	if _, err := os.Stat(filepath.Join(boardsDir, playbooksDirName, ".board.yaml")); err == nil {
		return nil, fmt.Errorf("%w: rename the %q project to enable playbooks", ErrPlaybooksDirIsProject, playbooksDirName)
	}

	s := &FilesystemPlaybookStore{boardsDir: boardsDir, playbooks: make(map[string]*board.Playbook)}
	if err := s.loadIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("load playbook index: %w", err)
	}

	return s, nil
}

func (s *FilesystemPlaybookStore) dir() string {
	return filepath.Join(s.boardsDir, playbooksDirName)
}

func (s *FilesystemPlaybookStore) path(id string) string {
	return filepath.Join(s.dir(), id+".yaml")
}

// loadIndex reads every non-dotfile *.yaml in playbooks/. Callers must hold
// s.mu.Lock unless the store is still being constructed. Malformed files are
// warn+skipped - one mangled file must never abort startup or a sync.
func (s *FilesystemPlaybookStore) loadIndex(ctx context.Context) error {
	entries, err := os.ReadDir(s.dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read playbooks dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".yaml") {
			continue
		}

		path := filepath.Join(s.dir(), name)

		data, err := readFileCapped(path, maxCardFileSize)
		if err != nil {
			ctxlog.Logger(ctx).Warn("skipping unreadable playbook file", "path", path, "error", err)

			continue
		}

		p, err := board.ParsePlaybook(data)
		if err == nil {
			err = p.Validate()
		}

		if err != nil {
			ctxlog.Logger(ctx).Warn("skipping unparseable playbook file", "path", path, "error", err)

			continue
		}

		s.playbooks[p.ID] = p
	}

	return nil
}

// copyPlaybook returns a deep copy of the playbook so that callers cannot
// mutate the cached value. Entries and each entry's DoneAt pointer are
// cloned.
func copyPlaybook(p *board.Playbook) *board.Playbook {
	if p == nil {
		return nil
	}

	cp := *p
	cp.Entries = slices.Clone(p.Entries)

	for i := range cp.Entries {
		if cp.Entries[i].DoneAt != nil {
			at := *cp.Entries[i].DoneAt
			cp.Entries[i].DoneAt = &at
		}
	}

	return &cp
}

// List returns all playbooks ordered by ID ascending. Results are deep
// copies of the cached playbooks.
func (s *FilesystemPlaybookStore) List(ctx context.Context) ([]*board.Playbook, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*board.Playbook, 0, len(s.playbooks))
	for _, p := range s.playbooks {
		list = append(list, copyPlaybook(p))
	}

	slices.SortFunc(list, func(a, b *board.Playbook) int {
		return strings.Compare(a.ID, b.ID)
	})

	return list, nil
}

// Get returns a deep copy of the requested playbook.
func (s *FilesystemPlaybookStore) Get(ctx context.Context, id string) (*board.Playbook, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.playbooks[id]
	if !ok {
		return nil, ErrPlaybookNotFound
	}

	return copyPlaybook(p), nil
}

// Create writes a new playbook file. It rejects the id if it is already
// indexed or already present on disk - a file skipped as malformed during
// load must never be silently overwritten.
func (s *FilesystemPlaybookStore) Create(ctx context.Context, p *board.Playbook) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.playbooks[p.ID]; exists {
		return ErrPlaybookExists
	}

	filePath := s.path(p.ID)

	if _, err := os.Stat(filePath); err == nil {
		return ErrPlaybookExists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat playbook file: %w", err)
	}

	data, err := board.SerializePlaybook(p)
	if err != nil {
		return fmt.Errorf("serialize playbook: %w", err)
	}

	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return fmt.Errorf("create playbooks directory: %w", err)
	}

	if err := atomicWriteFile(filePath, data); err != nil {
		return fmt.Errorf("write playbook file: %w", err)
	}

	s.playbooks[p.ID] = copyPlaybook(p)

	return nil
}

// Save overwrites an existing playbook file. It returns ErrPlaybookNotFound
// if the id is not indexed.
func (s *FilesystemPlaybookStore) Save(ctx context.Context, p *board.Playbook) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.playbooks[p.ID]; !exists {
		return ErrPlaybookNotFound
	}

	data, err := board.SerializePlaybook(p)
	if err != nil {
		return fmt.Errorf("serialize playbook: %w", err)
	}

	if err := atomicWriteFile(s.path(p.ID), data); err != nil {
		return fmt.Errorf("write playbook file: %w", err)
	}

	s.playbooks[p.ID] = copyPlaybook(p)

	return nil
}

// Delete removes a playbook's file and index entry.
func (s *FilesystemPlaybookStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.playbooks[id]; !exists {
		return ErrPlaybookNotFound
	}

	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove playbook file: %w", err)
	}

	delete(s.playbooks, id)

	return nil
}

// ReloadIndex rebuilds the in-memory index from disk. This is used after a
// git pull brings new/changed playbook files.
func (s *FilesystemPlaybookStore) ReloadIndex(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.playbooks = make(map[string]*board.Playbook)

	return s.loadIndex(ctx)
}
