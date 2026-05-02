package service

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/board"
)

// GetProjectKB returns the tiered KB for a project. Empty layers are
// represented as zero-valued fields; the caller can use ProjectKB.IsEmpty
// to detect "no KB at all". Optional repoSlugFilter narrows the repo tier.
func (s *CardService) GetProjectKB(ctx context.Context, project string, repoSlugFilter ...string) (board.ProjectKB, error) {
	cfg, err := s.store.GetProject(ctx, project)
	if err != nil {
		return board.ProjectKB{}, fmt.Errorf("get project %s: %w", project, err)
	}

	return s.store.ReadProjectKB(ctx, cfg, repoSlugFilter...)
}
