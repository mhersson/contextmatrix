package main

import (
	"errors"
	"fmt"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// repoBundle is everything one boards repository owns on its own: git
// manager, store, commit queue and playbook store.
type repoBundle struct {
	cfg     config.BoardsConfig
	git     *gitops.Manager
	store   *storage.FilesystemStore
	queue   *gitops.CommitQueue
	pbStore *storage.FilesystemPlaybookStore // nil when playbooks are disabled
}

// boardsBundles is every boards repository plus the composites that join
// them and the service-facing bundles built on top.
type boardsBundles struct {
	repos     []*repoBundle
	composite *storage.Composite
	// playbooks is nil when any repo holds a project named playbooks;
	// playbooksDisabledBy names that repo.
	playbooks           *storage.PlaybookComposite
	playbooksDisabledBy string
	svcRepos            []*service.BoardsRepo
	pbRepos             []*service.PlaybookRepo
}

func (b *boardsBundles) queues() []*gitops.CommitQueue {
	out := make([]*gitops.CommitQueue, 0, len(b.repos))
	for _, r := range b.repos {
		out = append(out, r.queue)
	}

	return out
}

// buildBoards initializes every configured boards repository in config
// order and joins them. It refuses to start when a shared repo's clone has
// no origin (shared mode would silently stay off) and when two repos hold
// a project of the same name (only one could be served).
func buildBoards(cfg *config.Config, provider githubauth.TokenGenerator, heartbeat time.Duration, clk clock.Clock) (*boardsBundles, error) {
	out := &boardsBundles{}

	for _, e := range cfg.Boards {
		cloneURL := ""
		if e.GitCloneOnEmpty {
			cloneURL = e.GitRemoteURL
		}

		git, err := gitops.NewManager(e.Dir, cloneURL, e.Name, provider)
		if err != nil {
			return nil, fmt.Errorf("boards[%s]: git manager: %w", e.Name, err)
		}

		if e.Shared {
			// Peers reading the shared history need to see which instance
			// authored a commit. Set before any commit this process makes.
			git.SetAuthor("ContextMatrix", "contextmatrix@"+cfg.Instance.ID)

			if !git.HasRemote() {
				return nil, fmt.Errorf("boards[%s]: shared is set but the clone at %s has no origin remote; "+
					"clone it with git_clone_on_empty on an empty directory or add the remote by hand", e.Name, e.Dir)
			}
		}

		store, err := storage.NewFilesystemStore(e.Dir)
		if err != nil {
			return nil, fmt.Errorf("boards[%s]: storage: %w", e.Name, err)
		}

		// A 30-minute idle timeout tears down workers for quiet projects so
		// long-running servers with ephemeral projects do not accumulate
		// goroutines; the next Enqueue for that project spawns a fresh one.
		queue := gitops.NewCommitQueue(git, 0, gitops.WithIdleTimeout(30*time.Minute))

		bundle := &repoBundle{cfg: e, git: git, store: store, queue: queue}

		pbStore, err := storage.NewFilesystemPlaybookStore(e.Dir)

		switch {
		case errors.Is(err, storage.ErrPlaybooksDirIsProject):
			out.playbooksDisabledBy = e.Name
		case err != nil:
			return nil, fmt.Errorf("boards[%s]: playbook store: %w", e.Name, err)
		default:
			bundle.pbStore = pbStore
		}

		out.repos = append(out.repos, bundle)
	}

	named := make([]storage.NamedStore, 0, len(out.repos))
	for _, r := range out.repos {
		named = append(named, storage.NamedStore{Name: r.cfg.Name, Store: r.store})
	}

	composite, err := storage.NewComposite(named...)
	if err != nil {
		return nil, fmt.Errorf("composite store: %w", err)
	}

	if hidden := composite.Hidden(); len(hidden) > 0 {
		var errs []error
		for _, h := range hidden {
			errs = append(errs, fmt.Errorf("project %q exists in boards repos %s and %s; project names must be unique across repos", h.Name, h.VisibleIn, h.Repo))
		}

		return nil, errors.Join(errs...)
	}

	out.composite = composite

	for _, r := range out.repos {
		view, err := composite.View(r.cfg.Name)
		if err != nil {
			return nil, err
		}

		lockMgr := lock.NewManagerWithClock(view, heartbeat, clk)

		svcRepo := &service.BoardsRepo{
			Name:              r.cfg.Name,
			Store:             view,
			Git:               r.git,
			Dir:               r.cfg.Dir,
			GitAutoCommit:     r.cfg.GitAutoCommit,
			GitDeferredCommit: r.cfg.GitDeferredCommit,
			Shared:            r.cfg.Shared,
			Lock:              lockMgr,
			Queue:             r.queue,
		}

		if r.cfg.Shared {
			leaseInterval, err := r.cfg.LeaseIntervalDuration()
			if err != nil {
				return nil, fmt.Errorf("boards[%s]: lease_interval: %w", r.cfg.Name, err)
			}

			leaseTimeout, err := r.cfg.LeaseTimeoutDuration()
			if err != nil {
				return nil, fmt.Errorf("boards[%s]: lease_timeout: %w", r.cfg.Name, err)
			}

			pullInterval, err := r.cfg.PullIntervalDuration()
			if err != nil {
				return nil, fmt.Errorf("boards[%s]: git_pull_interval: %w", r.cfg.Name, err)
			}

			lockMgr.SetShared(cfg.Instance.ID, leaseInterval, leaseTimeout)
			svcRepo.Instance, svcRepo.LeaseTimeout, svcRepo.PullInterval = cfg.Instance.ID, leaseTimeout, pullInterval
		}

		out.svcRepos = append(out.svcRepos, svcRepo)
		out.pbRepos = append(out.pbRepos, &service.PlaybookRepo{Name: r.cfg.Name, Queue: r.queue, GitAutoCommit: r.cfg.GitAutoCommit})
	}

	if out.playbooksDisabledBy == "" {
		namedPB := make([]storage.NamedPlaybookStore, 0, len(out.repos))
		for _, r := range out.repos {
			namedPB = append(namedPB, storage.NamedPlaybookStore{Name: r.cfg.Name, Store: r.pbStore})
		}

		pbc, err := storage.NewPlaybookComposite(namedPB...)
		if err != nil {
			return nil, fmt.Errorf("playbook composite: %w", err)
		}

		out.playbooks = pbc
	}

	return out, nil
}

// boardsRepoInfos lists the configured repos for the app-config payload.
func boardsRepoInfos(b config.Boards) []api.BoardsRepoInfo {
	out := make([]api.BoardsRepoInfo, 0, len(b))
	for _, e := range b {
		out = append(out, api.BoardsRepoInfo{Name: e.Name, Shared: e.Shared})
	}

	return out
}
