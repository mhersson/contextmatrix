package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
)

// ErrUnknownRepo is returned when a boards repo name is not configured.
var ErrUnknownRepo = errors.New("unknown boards repo")

// NamedStore pairs a boards repo name with its store, in config order.
type NamedStore struct {
	Name  string
	Store *FilesystemStore
}

// HiddenProject is a project whose name an earlier repo already owns. It
// stays on disk and keeps syncing but is absent from the composite index.
type HiddenProject struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	VisibleIn string `json:"visible_in"`
}

// Composite presents several boards repositories as one Store. Every call
// routes by project name to the repo that owns it; ownership follows config
// order, so the earliest repo holding a name wins and later copies are
// hidden. The routing table is rebuilt whenever a repo reloads.
type Composite struct {
	repos []NamedStore

	mu     sync.RWMutex
	owner  map[string]int // project name -> index into repos
	hidden []HiddenProject
}

// NewComposite builds the composite over repos in the given order and
// indexes their projects.
func NewComposite(repos ...NamedStore) (*Composite, error) {
	if len(repos) == 0 {
		return nil, errors.New("composite store: at least one repo is required")
	}

	seen := make(map[string]bool, len(repos))

	for _, r := range repos {
		if r.Name == "" || r.Store == nil {
			return nil, fmt.Errorf("composite store: repo %q needs a name and a store", r.Name)
		}

		if seen[r.Name] {
			return nil, fmt.Errorf("composite store: duplicate repo name %q", r.Name)
		}

		seen[r.Name] = true
	}

	c := &Composite{repos: repos}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.rebuildLocked(context.Background()); err != nil {
		return nil, err
	}

	return c, nil
}

// rebuildLocked recomputes ownership from the children. Caller holds c.mu.
func (c *Composite) rebuildLocked(ctx context.Context) error {
	owner := make(map[string]int)

	var hidden []HiddenProject

	for i, r := range c.repos {
		projects, err := r.Store.ListProjects(ctx)
		if err != nil {
			return fmt.Errorf("list projects in %s: %w", r.Name, err)
		}

		for _, p := range projects {
			if j, ok := owner[p.Name]; ok {
				hidden = append(hidden, HiddenProject{Name: p.Name, Repo: r.Name, VisibleIn: c.repos[j].Name})

				continue
			}

			owner[p.Name] = i
		}
	}

	sort.Slice(hidden, func(a, b int) bool {
		if hidden[a].Name != hidden[b].Name {
			return hidden[a].Name < hidden[b].Name
		}

		return hidden[a].Repo < hidden[b].Repo
	})

	c.owner, c.hidden = owner, hidden

	return nil
}

// RepoNames returns the repo names in config order.
func (c *Composite) RepoNames() []string {
	names := make([]string, len(c.repos))
	for i, r := range c.repos {
		names[i] = r.Name
	}

	return names
}

// RepoOf reports which repo owns project.
func (c *Composite) RepoOf(project string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	i, ok := c.owner[project]
	if !ok {
		return "", false
	}

	return c.repos[i].Name, true
}

// Hidden returns every project a later repo holds under a name an earlier
// repo owns, sorted by name then repo.
func (c *Composite) Hidden() []HiddenProject {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]HiddenProject(nil), c.hidden...)
}

// childLocked resolves the store owning project. Caller holds c.mu.
func (c *Composite) childLocked(project string) (*FilesystemStore, string, error) {
	i, ok := c.owner[project]
	if !ok {
		return nil, "", ErrProjectNotFound
	}

	return c.repos[i].Store, c.repos[i].Name, nil
}

func (c *Composite) child(project string) (*FilesystemStore, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.childLocked(project)
}

func (c *Composite) indexOf(repo string) (int, error) {
	for i, r := range c.repos {
		if r.Name == repo {
			return i, nil
		}
	}

	return 0, fmt.Errorf("%w: %q", ErrUnknownRepo, repo)
}

// View returns the repo-scoped store for repo.
func (c *Composite) View(repo string) (*RepoView, error) {
	if _, err := c.indexOf(repo); err != nil {
		return nil, err
	}

	return &RepoView{c: c, repo: repo}, nil
}

// ReloadRepo rebuilds one repo's index from disk and re-derives ownership,
// so a project a pull brought in becomes routable.
func (c *Composite) ReloadRepo(ctx context.Context, repo string) error {
	i, err := c.indexOf(repo)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.repos[i].Store.ReloadIndex(ctx); err != nil {
		return fmt.Errorf("reload %s: %w", repo, err)
	}

	return c.rebuildLocked(ctx)
}

// ReloadIndex rebuilds every repo's index and the ownership table.
func (c *Composite) ReloadIndex(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, r := range c.repos {
		if err := r.Store.ReloadIndex(ctx); err != nil {
			return fmt.Errorf("reload %s: %w", r.Name, err)
		}
	}

	return c.rebuildLocked(ctx)
}

// SaveProjectIn creates a project in the named repo and registers the
// route. A name any repo already owns is ErrProjectExists.
func (c *Composite) SaveProjectIn(ctx context.Context, repo string, cfg *board.ProjectConfig) error {
	i, err := c.indexOf(repo)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if j, ok := c.owner[cfg.Name]; ok {
		return fmt.Errorf("project %q is owned by boards repo %s: %w", cfg.Name, c.repos[j].Name, ErrProjectExists)
	}

	if err := c.repos[i].Store.SaveProject(ctx, cfg); err != nil {
		return err
	}

	c.owner[cfg.Name] = i

	return nil
}

func (c *Composite) ListProjects(ctx context.Context) ([]board.ProjectConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]board.ProjectConfig, 0)

	for i, r := range c.repos {
		projects, err := r.Store.ListProjects(ctx)
		if err != nil {
			return nil, fmt.Errorf("list projects in %s: %w", r.Name, err)
		}

		for _, p := range projects {
			j, ok := c.owner[p.Name]
			if !ok || j != i {
				continue // a hidden copy, or an entry the table does not know
			}

			p.BoardsRepo = r.Name
			out = append(out, p)
		}
	}

	return out, nil
}

func (c *Composite) GetProject(ctx context.Context, name string) (*board.ProjectConfig, error) {
	s, repo, err := c.child(name)
	if err != nil {
		return nil, err
	}

	cfg, err := s.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}

	cfg.BoardsRepo = repo

	return cfg, nil
}

// SaveProject updates an existing project in place. A project no repo owns
// yet must go through SaveProjectIn, which names the target.
func (c *Composite) SaveProject(ctx context.Context, cfg *board.ProjectConfig) error {
	s, _, err := c.child(cfg.Name)
	if err != nil {
		return fmt.Errorf("project %q has no boards repo yet, use SaveProjectIn: %w", cfg.Name, err)
	}

	return s.SaveProject(ctx, cfg)
}

func (c *Composite) DeleteProject(ctx context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, _, err := c.childLocked(name)
	if err != nil {
		return err
	}

	if err := s.DeleteProject(ctx, name); err != nil {
		return err
	}

	return c.rebuildLocked(ctx)
}

func (c *Composite) ProjectCardCount(ctx context.Context, name string) (int, error) {
	s, _, err := c.child(name)
	if err != nil {
		return 0, err
	}

	return s.ProjectCardCount(ctx, name)
}

func (c *Composite) ListCards(ctx context.Context, project string, filter CardFilter) ([]*board.Card, error) {
	s, _, err := c.child(project)
	if err != nil {
		return nil, err
	}

	return s.ListCards(ctx, project, filter)
}

func (c *Composite) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	s, _, err := c.child(project)
	if err != nil {
		return nil, err
	}

	return s.GetCard(ctx, project, id)
}

func (c *Composite) CreateCard(ctx context.Context, project string, card *board.Card) error {
	s, _, err := c.child(project)
	if err != nil {
		return err
	}

	return s.CreateCard(ctx, project, card)
}

func (c *Composite) UpdateCard(ctx context.Context, project string, card *board.Card) error {
	s, _, err := c.child(project)
	if err != nil {
		return err
	}

	return s.UpdateCard(ctx, project, card)
}

func (c *Composite) DeleteCard(ctx context.Context, project, id string) error {
	s, _, err := c.child(project)
	if err != nil {
		return err
	}

	return s.DeleteCard(ctx, project, id)
}

// RepoView is one repo's window on the composite: listing shows only the
// projects that repo owns, reloading reloads that repo, and every other
// call routes through the composite as usual. A syncer and a lock manager
// each hold the view of their own repo.
type RepoView struct {
	c    *Composite
	repo string
}

// Name returns the repo name.
func (v *RepoView) Name() string { return v.repo }

// Hidden returns the projects hidden in this repo.
func (v *RepoView) Hidden() []HiddenProject {
	var out []HiddenProject

	for _, h := range v.c.Hidden() {
		if h.Repo == v.repo {
			out = append(out, h)
		}
	}

	return out
}

// ReloadIndex reloads this repo and re-derives ownership.
func (v *RepoView) ReloadIndex(ctx context.Context) error {
	return v.c.ReloadRepo(ctx, v.repo)
}

func (v *RepoView) ListProjects(ctx context.Context) ([]board.ProjectConfig, error) {
	all, err := v.c.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]board.ProjectConfig, 0)

	for _, p := range all {
		if p.BoardsRepo == v.repo {
			out = append(out, p)
		}
	}

	return out, nil
}

func (v *RepoView) GetProject(ctx context.Context, name string) (*board.ProjectConfig, error) {
	return v.c.GetProject(ctx, name)
}

func (v *RepoView) SaveProject(ctx context.Context, cfg *board.ProjectConfig) error {
	return v.c.SaveProject(ctx, cfg)
}

func (v *RepoView) DeleteProject(ctx context.Context, name string) error {
	return v.c.DeleteProject(ctx, name)
}

func (v *RepoView) ProjectCardCount(ctx context.Context, name string) (int, error) {
	return v.c.ProjectCardCount(ctx, name)
}

func (v *RepoView) ListCards(ctx context.Context, project string, filter CardFilter) ([]*board.Card, error) {
	return v.c.ListCards(ctx, project, filter)
}

func (v *RepoView) GetCard(ctx context.Context, project, id string) (*board.Card, error) {
	return v.c.GetCard(ctx, project, id)
}

func (v *RepoView) CreateCard(ctx context.Context, project string, card *board.Card) error {
	return v.c.CreateCard(ctx, project, card)
}

func (v *RepoView) UpdateCard(ctx context.Context, project string, card *board.Card) error {
	return v.c.UpdateCard(ctx, project, card)
}

func (v *RepoView) DeleteCard(ctx context.Context, project, id string) error {
	return v.c.DeleteCard(ctx, project, id)
}

var (
	_ Store = (*Composite)(nil)
	_ Store = (*RepoView)(nil)
)
