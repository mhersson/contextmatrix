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
	wg           sync.WaitGroup
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
	}
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

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("github syncer stopped")

			return
		case <-ticker.C:
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

		imported, err := s.syncProject(ctx, &cfg, owner, repo)
		if err != nil {
			if errors.Is(err, ErrRateLimited) {
				slog.Warn("github sync: rate limited, stopping this cycle")

				return
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

func (s *Syncer) syncProject(ctx context.Context, cfg *board.ProjectConfig, owner, repo string) (int, error) {
	issues, err := s.client.FetchOpenIssues(ctx, owner, repo, cfg.GitHub.Labels)
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

		// Vetted intentionally left false — imported cards must be vetted by a human
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
func resolveOwnerRepo(cfg *board.ProjectConfig, allowedHosts []string) (owner, repo string) {
	if cfg.GitHub == nil {
		return "", ""
	}

	if cfg.GitHub.Owner != "" && cfg.GitHub.Repo != "" {
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
