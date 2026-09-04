package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// DefaultRepoName is the name the single-repo constructor gives its one
// boards repository. It matches the config default for the map form.
const DefaultRepoName = "boards"

// ErrUnknownBoardsRepo is returned when a caller names a boards repo that is
// not configured. It wraps storage.ErrInvalidInput so the API answers 400.
var ErrUnknownBoardsRepo = fmt.Errorf("%w: unknown boards_repo", storage.ErrInvalidInput)

// BoardsRepo is one boards repository the service writes to: its git
// manager, commit queue, directory, commit flags and, on a shared repo, the
// ownership identity and lease timings. Fields are set at wiring time and
// read-only afterwards except the sync stamp, which the syncer updates.
type BoardsRepo struct {
	Name string
	// Store is the repo's own view of the composite, whose ListProjects
	// returns only this repo's projects. On a single-repo service it is
	// the service store itself.
	Store             storage.Store
	Git               *gitops.Manager
	Dir               string
	GitAutoCommit     bool
	GitDeferredCommit bool
	// Shared switches LockWrites to a full queue drain and, once a runner
	// is set, makes every global decision push-verified.
	Shared bool
	Lock   *lock.Manager
	Queue  *gitops.CommitQueue
	// Instance, LeaseTimeout and PullInterval are set on a shared repo.
	// Instance stays empty on a private one, and every ownership and
	// epoch rule keys off that.
	Instance     string
	LeaseTimeout time.Duration
	PullInterval time.Duration

	runner   SyncRunner
	onCommit func()

	// syncMu guards lastSync: the service clock's reading at the end of
	// the last successful sync cycle of this repo. Foreign stalls need a
	// recent pull of the repo the card lives in.
	syncMu   sync.Mutex
	lastSync time.Time
}

// pushVerified reports whether mutations that depend on a global decision
// in this repo must be verified against the remote before they are
// acknowledged.
func (r *BoardsRepo) pushVerified() bool { return r.Shared && r.runner != nil }

func (r *BoardsRepo) sharedClaims() bool { return r.Instance != "" }

func (r *BoardsRepo) notifyCommit() {
	if r.onCommit != nil {
		r.onCommit()
	}
}

func (r *BoardsRepo) markSynced(now time.Time) {
	r.syncMu.Lock()
	r.lastSync = now
	r.syncMu.Unlock()
}

// recentlySynced reports whether the last successful cycle of this repo is
// within twice its pull interval.
func (r *BoardsRepo) recentlySynced(now time.Time) bool {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()

	return !r.lastSync.IsZero() && now.Sub(r.lastSync) <= 2*r.PullInterval
}

// repoRouter is what the composite store offers. A plain store routes every
// project to the first repo.
type repoRouter interface {
	RepoOf(project string) (string, bool)
}

// projectCreator is the composite's way to create a project in a named
// repo. A plain store creates it in its only directory.
type projectCreator interface {
	SaveProjectIn(ctx context.Context, repo string, cfg *board.ProjectConfig) error
}

// repoReloader reloads one repo of the composite.
type repoReloader interface {
	ReloadRepo(ctx context.Context, repo string) error
}

// The composite is the only store that routes by repo; pin that it still
// answers every question the resolver asks of it.
var (
	_ repoRouter     = (*storage.Composite)(nil)
	_ projectCreator = (*storage.Composite)(nil)
	_ repoReloader   = (*storage.Composite)(nil)
)

// NewCardServiceRepos creates a CardService over several boards
// repositories. store is the composite that routes by project name; each
// repo carries its own git manager, queue and lock manager. Every lock
// manager must share one clock and one heartbeat timeout: stall detection,
// the timeout-checker ticker and the lease rules compare timestamps against
// the same monotonic reading. See NewCardService for the single-repo form.
func NewCardServiceRepos(store storage.Store, bus *events.Bus, tokenCosts map[string]ModelRate, repos ...*BoardsRepo) (*CardService, error) {
	if len(repos) == 0 {
		return nil, errors.New("card service: at least one boards repo is required")
	}

	index := make(map[string]*BoardsRepo, len(repos))

	for _, r := range repos {
		if r == nil || r.Name == "" {
			return nil, errors.New("card service: every boards repo needs a name")
		}

		if r.Lock == nil {
			return nil, fmt.Errorf("card service: boards repo %s has no lock manager", r.Name)
		}

		if _, dup := index[r.Name]; dup {
			return nil, fmt.Errorf("card service: duplicate boards repo name %q", r.Name)
		}

		if r.Lock.Clock() != repos[0].Lock.Clock() {
			return nil, fmt.Errorf("card service: boards repo %s uses a different clock than %s", r.Name, repos[0].Name)
		}

		if r.Lock.Timeout() != repos[0].Lock.Timeout() {
			return nil, fmt.Errorf("card service: boards repo %s uses a different heartbeat timeout than %s", r.Name, repos[0].Name)
		}

		if r.Store == nil {
			r.Store = store
		}

		index[r.Name] = r
	}

	clk := repos[0].Lock.Clock()
	if clk == nil {
		clk = clock.Real()
	}

	slog.Debug("card service: adopting lock manager clock",
		"clock_type", fmt.Sprintf("%T", clk),
	)

	svc := &CardService{
		store:            store,
		bus:              bus,
		tokenCosts:       tokenCosts,
		repos:            repos,
		repoIndex:        index,
		heartbeatTimeout: repos[0].Lock.Timeout(),
		deferredPaths:    make(map[string][]string),
		phaseStarts:      make(map[string]phaseStart),
		runStarts:        make(map[string]time.Time),
		validator:        board.NewValidator(),
		clk:              clk,
		configs:          make(map[string]*board.ProjectConfig),
		templates:        make(map[string]map[string]string),
	}
	svc.stalledFn = svc.processStalled
	svc.validateStalledCardFn = svc.validator.ValidateCard

	return svc, nil
}

// Repos returns the boards repositories in config order.
func (s *CardService) Repos() []*BoardsRepo {
	return append([]*BoardsRepo(nil), s.repos...)
}

// repoOf resolves the boards repo that owns project. An unknown project
// resolves to the first repo: the store lookup that follows fails with
// ErrProjectNotFound, which is the error the caller should see.
func (s *CardService) repoOf(project string) *BoardsRepo {
	if len(s.repos) == 1 {
		return s.repos[0]
	}

	if rr, ok := s.store.(repoRouter); ok {
		if name, ok := rr.RepoOf(project); ok {
			if r, ok := s.repoIndex[name]; ok {
				return r
			}
		}
	}

	return s.repos[0]
}

// repoNamed resolves a boards repo by its config name. Empty means the
// first configured repo, the default target for creation.
func (s *CardService) repoNamed(name string) (*BoardsRepo, error) {
	if name == "" {
		return s.repos[0], nil
	}

	if r, ok := s.repoIndex[name]; ok {
		return r, nil
	}

	return nil, fmt.Errorf("%w: %q (configured: %v)", ErrUnknownBoardsRepo, name, s.repoNames())
}

func (s *CardService) repoNames() []string {
	names := make([]string, len(s.repos))
	for i, r := range s.repos {
		names[i] = r.Name
	}

	return names
}

// instanceFor returns the instance ID that owns claims in project's repo,
// empty on a private repo.
func (s *CardService) instanceFor(project string) string {
	return s.repoOf(project).Instance
}

// saveNewProject writes a project that no repo owns yet into r.
func (s *CardService) saveNewProject(ctx context.Context, r *BoardsRepo, cfg *board.ProjectConfig) error {
	if pc, ok := s.store.(projectCreator); ok {
		return pc.SaveProjectIn(ctx, r.Name, cfg)
	}

	return s.store.SaveProject(ctx, cfg)
}

// SetSyncRunnerFor routes push-verified mutations in repo through the
// syncer's cycle. Must be called before the server starts accepting
// requests.
func (s *CardService) SetSyncRunnerFor(repo string, run SyncRunner) error {
	r, err := s.repoNamed(repo)
	if err != nil {
		return err
	}

	r.runner = run

	return nil
}

// SetOnCommitFor registers the callback invoked after each successful git
// commit in repo.
func (s *CardService) SetOnCommitFor(repo string, fn func()) error {
	r, err := s.repoNamed(repo)
	if err != nil {
		return err
	}

	r.onCommit = fn

	return nil
}
