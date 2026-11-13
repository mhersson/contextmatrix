package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// maxStopCauseBytes bounds a stored stop cause; a longer one is stored as
// unknown rather than truncated.
const maxStopCauseBytes = 64

// CardRun is a card run still in flight and the LLM channel mode it started in.
type CardRun struct {
	Project   string
	CardID    string
	Mode      string
	StartedAt time.Time
}

// OpenCardRun records a run's mode and launch facts. A card has one row: a
// rerun replaces it and clears every per-run column. Heal attempts reset only
// for a REST launch.
func (s *Store) OpenCardRun(ctx context.Context, project, cardID, mode string, at time.Time, facts sentinel.LaunchFacts) error {
	snap, err := json.Marshal(facts.Snapshot)
	if err != nil {
		return fmt.Errorf("open card run: encode snapshot: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO card_run_modes (project, card_id, mode, started_at, ended_at, edit_snapshot, autonomous, origin)
		 VALUES (?, ?, ?, ?, NULL, ?, ?, ?)
		 ON CONFLICT(project, card_id) DO UPDATE SET mode = excluded.mode,
		   started_at = excluded.started_at, ended_at = NULL,
		   edit_snapshot = excluded.edit_snapshot, autonomous = excluded.autonomous, origin = excluded.origin,
		   attempts = CASE WHEN excluded.origin = 'rest' THEN 0 ELSE attempts END,
		   run_id = '', stop_cause = '', stopped_at = NULL, human_stop = 0`,
		project, cardID, mode, at.UnixNano(), string(snap), facts.Autonomous, facts.Origin); err != nil {
		return fmt.Errorf("open card run: %w", err)
	}

	return nil
}

// CloseCardRun marks the card's open run ended, but only when it started at or
// before startedNotAfter, so a rerun opened after the caller's view stays open.
// It is a no-op when no such run is open. started_at and ended_at are
// nanoseconds so a rerun within the same second stays distinguishable.
func (s *Store) CloseCardRun(ctx context.Context, project, cardID string, startedNotAfter, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET ended_at = ?
		 WHERE project = ? AND card_id = ? AND ended_at IS NULL AND started_at <= ?`,
		at.UnixNano(), project, cardID, startedNotAfter.UnixNano()); err != nil {
		return fmt.Errorf("close card run: %w", err)
	}

	return nil
}

// RecordRunStatus applies a worker status to the card's run row. A running
// status with a run ID adopts it while none is stored and the run has not
// stopped. A completed or failed status for the stored run ID records the stop
// cause and time, first terminal wins. Anything else is a no-op.
func (s *Store) RecordRunStatus(ctx context.Context, project, cardID, runID, status, stopCause string, at time.Time) error {
	if runID == "" {
		return nil
	}

	var (
		query string
		args  []any
	)

	switch status {
	case "running":
		query = `UPDATE card_run_modes SET run_id = ?
			 WHERE project = ? AND card_id = ? AND run_id = '' AND stopped_at IS NULL`
		args = []any{runID, project, cardID}
	case "completed", "failed":
		if len(stopCause) > maxStopCauseBytes {
			stopCause = ""
		}

		query = `UPDATE card_run_modes SET stop_cause = ?, stopped_at = ?
			 WHERE project = ? AND card_id = ? AND run_id = ? AND stopped_at IS NULL`
		args = []any{stopCause, at.UnixNano(), project, cardID, runID}
	default:
		return nil
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("record run status: %w", err)
	}

	return nil
}

// MarkHumanStop flags the card's run as stopped by a human.
func (s *Store) MarkHumanStop(ctx context.Context, project, cardID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET human_stop = 1 WHERE project = ? AND card_id = ?`,
		project, cardID); err != nil {
		return fmt.Errorf("mark human stop: %w", err)
	}

	return nil
}

// RefreshEditSnapshot replaces the run's stored edit snapshot.
func (s *Store) RefreshEditSnapshot(ctx context.Context, project, cardID string, snap board.EditSnapshot) error {
	b, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("refresh edit snapshot: encode: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET edit_snapshot = ? WHERE project = ? AND card_id = ?`,
		string(b), project, cardID); err != nil {
		return fmt.Errorf("refresh edit snapshot: %w", err)
	}

	return nil
}

// CardRunRow returns the card's run row, or nil when this instance never
// launched one.
func (s *Store) CardRunRow(ctx context.Context, project, cardID string) (*sentinel.Run, error) {
	var (
		r              sentinel.Run
		started        int64
		ended, stopped sql.NullInt64
		snap           string
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT project, card_id, mode, started_at, ended_at, stopped_at, edit_snapshot,
		        autonomous, origin, attempts, run_id, stop_cause, human_stop
		 FROM card_run_modes WHERE project = ? AND card_id = ?`, project, cardID).
		Scan(&r.Project, &r.CardID, &r.Mode, &started, &ended, &stopped, &snap,
			&r.Launch.Autonomous, &r.Launch.Origin, &r.Attempts, &r.RunID, &r.StopCause, &r.HumanStop)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // nil, nil means no row
	}

	if err != nil {
		return nil, fmt.Errorf("read card run: %w", err)
	}

	if snap != "" {
		if err := json.Unmarshal([]byte(snap), &r.Launch.Snapshot); err != nil {
			return nil, fmt.Errorf("read card run: decode snapshot: %w", err)
		}
	}

	r.StartedAt = time.Unix(0, started).UTC()
	r.EndedAt = nanoTime(ended)
	r.StoppedAt = nanoTime(stopped)

	return &r, nil
}

func nanoTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}

	t := time.Unix(0, n.Int64).UTC()

	return &t
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

		r.StartedAt = time.Unix(0, at).UTC()
		runs = append(runs, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open card runs: %w", err)
	}

	return runs, nil
}
