package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
)

// NamedPlaybookStore pairs a boards repo name with its playbook store.
type NamedPlaybookStore struct {
	Name  string
	Store *FilesystemPlaybookStore
}

// PlaybookComposite presents the playbooks directories of several boards
// repositories as one store. Reads and mutations route by playbook ID;
// creation names its repo through CreateIn. IDs are unique across repos:
// an ID any repo owns is taken, and a file that arrives with an ID an
// earlier repo already owns is hidden with a warning.
type PlaybookComposite struct {
	repos []NamedPlaybookStore

	mu    sync.RWMutex
	owner map[string]int
}

// NewPlaybookComposite builds the composite over repos in config order.
func NewPlaybookComposite(repos ...NamedPlaybookStore) (*PlaybookComposite, error) {
	if len(repos) == 0 {
		return nil, errors.New("playbook composite: at least one repo is required")
	}

	seen := make(map[string]bool, len(repos))

	for _, r := range repos {
		if r.Name == "" || r.Store == nil {
			return nil, fmt.Errorf("playbook composite: repo %q needs a name and a store", r.Name)
		}

		if seen[r.Name] {
			return nil, fmt.Errorf("playbook composite: duplicate repo name %q", r.Name)
		}

		seen[r.Name] = true
	}

	c := &PlaybookComposite{repos: repos}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.rebuildLocked(context.Background()); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *PlaybookComposite) rebuildLocked(ctx context.Context) error {
	owner := make(map[string]int)

	for i, r := range c.repos {
		list, err := r.Store.List(ctx)
		if err != nil {
			return fmt.Errorf("list playbooks in %s: %w", r.Name, err)
		}

		for _, p := range list {
			if j, ok := owner[p.ID]; ok {
				slog.Warn("playbook hidden: an earlier boards repo owns the id",
					"id", p.ID, "repo", r.Name, "visible_in", c.repos[j].Name)

				continue
			}

			owner[p.ID] = i
		}
	}

	c.owner = owner

	return nil
}

func (c *PlaybookComposite) indexOf(repo string) (int, error) {
	for i, r := range c.repos {
		if r.Name == repo {
			return i, nil
		}
	}

	return 0, fmt.Errorf("%w: %q", ErrUnknownRepo, repo)
}

// RepoOf reports which repo owns the playbook.
func (c *PlaybookComposite) RepoOf(id string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	i, ok := c.owner[id]
	if !ok {
		return "", false
	}

	return c.repos[i].Name, true
}

func (c *PlaybookComposite) child(id string) (*FilesystemPlaybookStore, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	i, ok := c.owner[id]
	if !ok {
		return nil, ErrPlaybookNotFound
	}

	return c.repos[i].Store, nil
}

// CreateIn creates the playbook in the named repo. An ID any repo already
// owns is ErrPlaybookExists, so the service's suffix loop moves on.
func (c *PlaybookComposite) CreateIn(ctx context.Context, repo string, p *board.Playbook) error {
	i, err := c.indexOf(repo)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if j, ok := c.owner[p.ID]; ok {
		return fmt.Errorf("playbook %q is owned by boards repo %s: %w", p.ID, c.repos[j].Name, ErrPlaybookExists)
	}

	if err := c.repos[i].Store.Create(ctx, p); err != nil {
		return err
	}

	c.owner[p.ID] = i

	return nil
}

// ReloadRepo re-reads one repo's playbooks and re-derives ownership.
func (c *PlaybookComposite) ReloadRepo(ctx context.Context, repo string) error {
	i, err := c.indexOf(repo)
	if err != nil {
		return err
	}

	// Reload outside the lock: the walk must not block reads against
	// other repos. rebuildLocked still runs under the write lock.
	if err := c.repos[i].Store.ReloadIndex(ctx); err != nil {
		return fmt.Errorf("reload playbooks in %s: %w", repo, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.rebuildLocked(ctx)
}

func (c *PlaybookComposite) List(ctx context.Context) ([]*board.Playbook, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []*board.Playbook

	for i, r := range c.repos {
		list, err := r.Store.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list playbooks in %s: %w", r.Name, err)
		}

		for _, p := range list {
			if j, ok := c.owner[p.ID]; ok && j == i {
				out = append(out, p)
			}
		}
	}

	slices.SortFunc(out, func(a, b *board.Playbook) int {
		return strings.Compare(a.ID, b.ID)
	})

	return out, nil
}

func (c *PlaybookComposite) Get(ctx context.Context, id string) (*board.Playbook, error) {
	s, err := c.child(id)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, id)
}

// Create always fails: the composite cannot guess a repo. Use CreateIn.
func (c *PlaybookComposite) Create(_ context.Context, p *board.Playbook) error {
	return fmt.Errorf("playbook %q: %w: the composite needs a target repo, use CreateIn", p.ID, ErrUnknownRepo)
}

func (c *PlaybookComposite) Save(ctx context.Context, p *board.Playbook) error {
	s, err := c.child(p.ID)
	if err != nil {
		return err
	}

	return s.Save(ctx, p)
}

func (c *PlaybookComposite) Delete(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	i, ok := c.owner[id]
	if !ok {
		return ErrPlaybookNotFound
	}

	if err := c.repos[i].Store.Delete(ctx, id); err != nil {
		return err
	}

	return c.rebuildLocked(ctx)
}

func (c *PlaybookComposite) ReloadIndex(ctx context.Context) error {
	for _, r := range c.repos {
		if err := r.Store.ReloadIndex(ctx); err != nil {
			return fmt.Errorf("reload playbooks in %s: %w", r.Name, err)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.rebuildLocked(ctx)
}
