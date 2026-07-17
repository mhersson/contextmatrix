package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Syncer periodically fetches open GitHub issues and creates cards for them.
type Syncer struct {
	svc          *service.CardService
	store        *storage.FilesystemStore
	client       *Client
	boardsDir    string
	interval     time.Duration
	allowedHosts []string
	clk          clock.Clock
	wg           sync.WaitGroup
	// clientFor resolves a per-project GitHub client (credential binding
	// support). nil (the default) keeps every project on the
	// constructor-injected static client. Set via SetClientFor.
	clientFor func(ctx context.Context, cfg *board.ProjectConfig) (*Client, error)
}

// NewSyncer creates a new GitHub issue syncer.
func NewSyncer(
	svc *service.CardService,
	store *storage.FilesystemStore,
	client *Client,
	boardsDir string,
	interval time.Duration,
	allowedHosts []string,
) *Syncer {
	return &Syncer{
		svc:          svc,
		store:        store,
		client:       client,
		boardsDir:    boardsDir,
		interval:     interval,
		allowedHosts: allowedHosts,
		clk:          clock.Real(),
	}
}

// SetClientFor installs a per-project GitHub client resolver. When set,
// syncAll resolves the client for each project (by its credential binding)
// just before syncing it; a resolution error is logged and that project is
// skipped for the cycle - other projects keep syncing. Nil (the default)
// keeps every project on the constructor-injected static client, unchanged
// (none mode / no seam configured).
func (s *Syncer) SetClientFor(f func(ctx context.Context, cfg *board.ProjectConfig) (*Client, error)) {
	s.clientFor = f
}

// Start launches the periodic sync goroutine.
func (s *Syncer) Start(ctx context.Context) {
	s.wg.Go(func() {
		s.run(ctx)
	})
}

// Wait blocks until the sync goroutine has stopped.
func (s *Syncer) Wait() {
	s.wg.Wait()
}

func (s *Syncer) run(ctx context.Context) {
	// Run immediately on start, then on interval.
	s.safeSyncAll(ctx)

	ticker := s.clk.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("github syncer stopped")

			return
		case <-ticker.C():
			s.safeSyncAll(ctx)
		}
	}
}

// safeSyncAll wraps syncAll with panic recovery so a single cycle failure
// does not kill the ticker loop.
func (s *Syncer) safeSyncAll(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("github syncer panicked", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	s.syncAll(ctx)
}

func (s *Syncer) syncAll(ctx context.Context) {
	projects, err := board.DiscoverProjects(s.boardsDir)
	if err != nil {
		slog.Error("github sync: discover projects", "error", err)

		return
	}

	for _, cfg := range projects {
		if ctx.Err() != nil {
			return
		}

		if cfg.GitHub == nil || !cfg.GitHub.ImportIssues {
			continue
		}

		owner, repo := resolveOwnerRepo(&cfg, s.allowedHosts)
		if owner == "" || repo == "" {
			slog.Debug("github sync: skipping project, no GitHub repo resolved",
				"project", cfg.Name)

			continue
		}

		client := s.client

		if s.clientFor != nil {
			resolved, err := s.clientFor(ctx, &cfg)
			if err != nil {
				// Fail closed: a broken per-project credential binding must
				// never fall back to the syncer's static client. Log the
				// project and error only - TokenProviderFor's error text
				// carries the credential name, never secret material.
				slog.Warn("github sync: resolve client for project; skipping for this cycle",
					"project", cfg.Name, "error", err)

				continue
			}

			client = resolved
		}

		imported, err := s.syncProject(ctx, &cfg, client, owner, repo)
		if err != nil {
			if errors.Is(err, ErrRateLimited) {
				// Rate-limit is per-host (or per-installation token) so a
				// single rate-limited project must not starve well-quota'd
				// hosts on subsequent iterations. Skip this one and continue.
				slog.Warn("github sync: project rate limited; skipping for this cycle",
					"project", cfg.Name, "owner", owner, "repo", repo)

				continue
			}

			if errors.Is(err, ErrPermissionDenied) {
				slog.Error("github sync: project sync denied by permission/auth check",
					"project", cfg.Name, "owner", owner, "repo", repo, "error", err)

				continue
			}

			slog.Error("github sync: project sync failed",
				"project", cfg.Name, "error", err)

			continue
		}

		if imported > 0 {
			slog.Info("github sync: imported issues",
				"project", cfg.Name, "count", imported)
		}
	}
}

func (s *Syncer) syncProject(ctx context.Context, cfg *board.ProjectConfig, client *Client, owner, repo string) (int, error) {
	issues, err := client.FetchOpenIssues(ctx, owner, repo, cfg.GitHub.Labels)
	if err != nil && !errors.Is(err, ErrRateLimited) {
		return 0, fmt.Errorf("fetch issues: %w", err)
	}

	rateLimited := errors.Is(err, ErrRateLimited)

	imported := 0

	for _, issue := range issues {
		if ctx.Err() != nil {
			return imported, ctx.Err()
		}

		externalID := fmt.Sprintf("%s/%s#%d", owner, repo, issue.Number)

		// Check if card already exists for this issue.
		existing, err := s.store.ListCards(ctx, cfg.Name, storage.CardFilter{
			ExternalID: externalID,
		})
		if err != nil {
			slog.Error("github sync: check existing card",
				"project", cfg.Name, "external_id", externalID, "error", err)

			continue
		}

		if len(existing) > 0 {
			continue
		}

		cardType := cfg.GitHub.CardType
		if cardType == "" {
			cardType = "task"
		}

		priority := cfg.GitHub.DefaultPriority
		if priority == "" {
			priority = "medium"
		}

		var labels []string
		for _, l := range issue.Labels {
			labels = append(labels, l.Name)
		}

		// Vetted intentionally left false - imported cards must be vetted by a human
		// before agents can claim them.
		_, err = s.svc.CreateCard(ctx, cfg.Name, service.CreateCardInput{
			Title:    issue.Title,
			Type:     cardType,
			Priority: priority,
			Body:     issue.Body,
			Labels:   labels,
			Source: &board.Source{
				System:      "github",
				ExternalID:  externalID,
				ExternalURL: issue.HTMLURL,
			},
		})
		if err != nil {
			slog.Error("github sync: create card",
				"project", cfg.Name, "issue", externalID, "error", err)

			continue
		}

		imported++
	}

	if rateLimited {
		return imported, ErrRateLimited
	}

	return imported, nil
}

// resolveOwnerRepo determines the GitHub owner/repo for a project.
// Uses explicit GitHub config if set, otherwise parses the project's repo URL.
// Returns empty strings if GitHub config is nil (import not enabled).
//
// When the explicit Owner/Repo override is used, the project's Repo URL is
// still validated against allowedHosts so that a misconfigured .board.yaml
// cannot redirect the PAT to an unexpected host.
func resolveOwnerRepo(cfg *board.ProjectConfig, allowedHosts []string) (owner, repo string) {
	if cfg.GitHub == nil {
		return "", ""
	}

	if cfg.GitHub.Owner != "" && cfg.GitHub.Repo != "" {
		// Require that the project's Repo URL parses and belongs to an allowed
		// host before honouring the explicit override.  This prevents an
		// operator typo in .board.yaml from sending the PAT to an unintended
		// host.
		if cfg.Repo != "" {
			_, _, _, ok := ParseGitHubRepo(cfg.Repo, allowedHosts)
			if !ok {
				slog.Warn("github sync: explicit owner/repo ignored because project repo URL is not an allowed GitHub host",
					"project", cfg.Name, "repo", cfg.Repo)

				return "", ""
			}
		}

		return cfg.GitHub.Owner, cfg.GitHub.Repo
	}

	if cfg.Repo != "" {
		owner, repo, _, ok := ParseGitHubRepo(cfg.Repo, allowedHosts)
		if ok {
			return owner, repo
		}
	}

	return "", ""
}
