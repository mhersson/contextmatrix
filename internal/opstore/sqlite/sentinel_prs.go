package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

const standingPRColumns = `project, pr_url, head_sha, file, pushed_at, pr_state, read_at`

// StandingPRs returns every project's standing PR record, by project.
func (s *Store) StandingPRs(ctx context.Context) ([]state.StandingPR, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+standingPRColumns+` FROM sentinel_standing_prs ORDER BY project`)
	if err != nil {
		return nil, fmt.Errorf("list standing prs: %w", err)
	}
	defer rows.Close()

	var out []state.StandingPR

	for rows.Next() {
		p, err := scanStandingPR(rows)
		if err != nil {
			return nil, fmt.Errorf("list standing prs: %w", err)
		}

		out = append(out, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list standing prs: %w", err)
	}

	return out, nil
}

// StandingPR answers nil when the project has no record.
func (s *Store) StandingPR(ctx context.Context, project string) (*state.StandingPR, error) {
	p, err := scanStandingPR(s.db.QueryRowContext(ctx,
		`SELECT `+standingPRColumns+` FROM sentinel_standing_prs WHERE project = ?`, project))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no record is not an error
	}

	if err != nil {
		return nil, fmt.Errorf("read standing pr: %w", err)
	}

	return &p, nil
}

// PutStandingPR stores the project's record, replacing any earlier one.
func (s *Store) PutStandingPR(ctx context.Context, p state.StandingPR) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sentinel_standing_prs (`+standingPRColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project) DO UPDATE SET pr_url = excluded.pr_url, head_sha = excluded.head_sha,
		 file = excluded.file, pushed_at = excluded.pushed_at, pr_state = excluded.pr_state, read_at = excluded.read_at`,
		p.Project, p.URL, p.HeadSHA, p.File, unixNano(p.PushedAt), p.State, unixNano(p.ReadAt))
	if err != nil {
		return fmt.Errorf("store standing pr: %w", err)
	}

	return nil
}

func scanStandingPR(r scanner) (state.StandingPR, error) {
	var (
		p              state.StandingPR
		pushed, readAt int64
	)

	if err := r.Scan(&p.Project, &p.URL, &p.HeadSHA, &p.File, &pushed, &p.State, &readAt); err != nil {
		return p, err
	}

	p.PushedAt, p.ReadAt = fromUnixNano(pushed), fromUnixNano(readAt)

	return p, nil
}
