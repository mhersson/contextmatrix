package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LLMChannel returns the stored global LLM channel mode, when it was written
// and by whom. An empty mode and a zero time mean nothing is stored.
func (s *Store) LLMChannel(ctx context.Context) (string, time.Time, string, error) {
	var (
		mode, by string
		at       int64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT mode, updated_at, updated_by FROM llm_channel WHERE id = 1`).Scan(&mode, &at, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, "", nil
	}

	if err != nil {
		return "", time.Time{}, "", fmt.Errorf("read llm channel: %w", err)
	}

	return mode, time.Unix(at, 0).UTC(), by, nil
}

// PutLLMChannel replaces the single row. The caller validates the mode.
func (s *Store) PutLLMChannel(ctx context.Context, mode, by string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_channel (id, mode, updated_at, updated_by) VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET mode = excluded.mode,
		   updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		mode, time.Now().Unix(), by); err != nil {
		return fmt.Errorf("put llm channel: %w", err)
	}

	return nil
}

// CardRun is a card run still in flight and the LLM channel mode it started in.
type CardRun struct {
	Project   string
	CardID    string
	Mode      string
	StartedAt time.Time
}

// OpenCardRun records a run's mode. A card has one row: a rerun replaces it.
func (s *Store) OpenCardRun(ctx context.Context, project, cardID, mode string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO card_run_modes (project, card_id, mode, started_at, ended_at) VALUES (?, ?, ?, ?, NULL)
		 ON CONFLICT(project, card_id) DO UPDATE SET mode = excluded.mode,
		   started_at = excluded.started_at, ended_at = NULL`,
		project, cardID, mode, at.Unix()); err != nil {
		return fmt.Errorf("open card run: %w", err)
	}

	return nil
}

// CloseCardRun marks the card's run ended. It is a no-op when none is open.
func (s *Store) CloseCardRun(ctx context.Context, project, cardID string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET ended_at = ? WHERE project = ? AND card_id = ? AND ended_at IS NULL`,
		at.Unix(), project, cardID); err != nil {
		return fmt.Errorf("close card run: %w", err)
	}

	return nil
}

// OpenCardRuns returns every run not yet closed.
func (s *Store) OpenCardRuns(ctx context.Context) ([]CardRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, card_id, mode, started_at FROM card_run_modes WHERE ended_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("list open card runs: %w", err)
	}
	defer rows.Close()

	var runs []CardRun

	for rows.Next() {
		var (
			r  CardRun
			at int64
		)

		if err := rows.Scan(&r.Project, &r.CardID, &r.Mode, &at); err != nil {
			return nil, fmt.Errorf("scan card run: %w", err)
		}

		r.StartedAt = time.Unix(at, 0).UTC()
		runs = append(runs, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open card runs: %w", err)
	}

	return runs, nil
}
