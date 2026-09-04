package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// wireImages opens images.db, layers the image index of every shared
// boards repo over it with the card service as the repo writer, and
// exports into each shared repo every image its card bodies reference
// from the database. It runs after the startup pull, so the peers' cards
// are in, and before the listener opens, so no request reads a
// half-exported state. The export is idempotent: every start reconciles,
// which also covers a repo that turns shared later.
func wireImages(ctx context.Context, cfg *config.Config, boards *boardsBundles, svc *service.CardService) (*images.Layered, error) {
	db, err := images.Open(cfg.Images.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open image store %s: %w", cfg.Images.DBPath, err)
	}

	layered := images.NewLayered(db, svc, boards.imageIndexes()...)

	exportRepoImages(ctx, boards, svc, layered)

	return layered, nil
}

// exportRepoImages writes, per shared repo, every image a card body
// references that images.db holds and the repo does not. A failure is
// logged and startup continues: the images stay served from the database
// on this instance and the next start retries.
func exportRepoImages(ctx context.Context, boards *boardsBundles, svc *service.CardService, layered *images.Layered) {
	for _, b := range boards.repos {
		if b.images == nil {
			continue
		}

		view, err := boards.composite.View(b.cfg.Name)
		if err != nil {
			slog.Error("image export: repo view", "repo", b.cfg.Name, "error", err)

			continue
		}

		refs, err := referencedImages(ctx, svc, view)
		if err != nil {
			slog.Error("image export: collect references", "repo", b.cfg.Name, "error", err)

			continue
		}

		n, err := layered.Export(ctx, b.cfg.Name, refs)
		if err != nil {
			slog.Error("image export into shared boards repo failed", "repo", b.cfg.Name, "exported", n, "error", err)

			continue
		}

		if n > 0 {
			slog.Info("exported images into shared boards repo", "repo", b.cfg.Name, "count", n)
		}
	}
}

// referencedImages collects, per project the view serves, the image ids
// its card bodies reference, deduplicated in first-appearance order.
func referencedImages(ctx context.Context, svc *service.CardService, view *storage.RepoView) (map[string][]string, error) {
	projects, err := view.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	refs := make(map[string][]string, len(projects))

	for _, p := range projects {
		cards, err := svc.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			return nil, fmt.Errorf("list cards in %s: %w", p.Name, err)
		}

		seen := make(map[string]struct{})

		for _, c := range cards {
			for _, id := range images.ReferencedIDs(c.Body) {
				if _, dup := seen[id]; dup {
					continue
				}

				seen[id] = struct{}{}
				refs[p.Name] = append(refs[p.Name], id)
			}
		}
	}

	return refs, nil
}
