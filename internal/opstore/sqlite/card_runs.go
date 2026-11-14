package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
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
// rerun replaces it and clears every per-run column. Heal attempts and
// usage-limit reruns reset only for a REST launch.
func (s *Store) OpenCardRun(ctx context.Context, project, cardID, mode string, at time.Time, facts state.LaunchFacts) error {
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
		   usage_reruns = CASE WHEN excluded.origin = 'rest' THEN 0 ELSE usage_reruns END,
		   run_id = '', stop_cause = '', stopped_at = NULL, human_stop = 0, decided = 0, analysis_queued = 0`,
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

// runColumns are the run row's columns in scanRun order.
const runColumns = `project, card_id, mode, started_at, ended_at, stopped_at, edit_snapshot,
	autonomous, origin, attempts, usage_reruns, run_id, stop_cause, human_stop, decided, analysis_queued`

// CardRunRow returns the card's run row, or nil when this instance never
// launched one.
func (s *Store) CardRunRow(ctx context.Context, project, cardID string) (*state.Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM card_run_modes WHERE project = ? AND card_id = ?`, project, cardID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // nil, nil means no row
	}

	if err != nil {
		return nil, fmt.Errorf("read card run: %w", err)
	}

	return &r, nil
}

// UndecidedRuns returns the autonomous runs Sentinel has not looked at.
func (s *Store) UndecidedRuns(ctx context.Context) ([]state.Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM card_run_modes WHERE autonomous = 1 AND decided = 0`)
	if err != nil {
		return nil, fmt.Errorf("list undecided runs: %w", err)
	}
	defer rows.Close()

	var runs []state.Run

	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("list undecided runs: %w", err)
		}

		runs = append(runs, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list undecided runs: %w", err)
	}

	return runs, nil
}

func scanRun(sc scanner) (state.Run, error) {
	var (
		r              state.Run
		started        int64
		ended, stopped sql.NullInt64
		snap           string
	)

	if err := sc.Scan(&r.Project, &r.CardID, &r.Mode, &started, &ended, &stopped, &snap,
		&r.Launch.Autonomous, &r.Launch.Origin, &r.Attempts, &r.UsageReruns, &r.RunID, &r.StopCause,
		&r.HumanStop, &r.Decided, &r.AnalysisQueued); err != nil {
		return state.Run{}, err
	}

	if snap != "" {
		if err := json.Unmarshal([]byte(snap), &r.Launch.Snapshot); err != nil {
			return state.Run{}, fmt.Errorf("decode snapshot: %w", err)
		}
	}

	r.StartedAt = time.Unix(0, started).UTC()
	r.EndedAt = nanoTime(ended)
	r.StoppedAt = nanoTime(stopped)

	return r, nil
}

// markRunDecided matches the run by its start, so a rerun opened since stays
// undecided.
const markRunDecided = `UPDATE card_run_modes SET decided = 1 WHERE project = ? AND card_id = ? AND started_at = ?`

// MarkRunDecided records that Sentinel has looked at the run that started at
// started.
func (s *Store) MarkRunDecided(ctx context.Context, project, cardID string, started time.Time) error {
	if _, err := s.db.ExecContext(ctx, markRunDecided, project, cardID, started.UnixNano()); err != nil {
		return fmt.Errorf("mark run decided: %w", err)
	}

	return nil
}

// markRunQueued matches the run by its start, like markRunDecided.
const markRunQueued = `UPDATE card_run_modes SET analysis_queued = 1 WHERE project = ? AND card_id = ? AND started_at = ?`

// MarkAnalysisQueued records that the run that started at started needs no
// analysis row.
func (s *Store) MarkAnalysisQueued(ctx context.Context, project, cardID string, started time.Time) error {
	if _, err := s.db.ExecContext(ctx, markRunQueued, project, cardID, started.UnixNano()); err != nil {
		return fmt.Errorf("mark analysis queued: %w", err)
	}

	return nil
}

// UnqueuedRuns returns the runs with an ID that are not yet past the analysis
// queue.
func (s *Store) UnqueuedRuns(ctx context.Context) ([]state.Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM card_run_modes WHERE run_id != '' AND analysis_queued = 0`)
	if err != nil {
		return nil, fmt.Errorf("list unqueued runs: %w", err)
	}
	defer rows.Close()

	var runs []state.Run

	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("list unqueued runs: %w", err)
		}

		runs = append(runs, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list unqueued runs: %w", err)
	}

	return runs, nil
}

// IncrementAttempts counts one heal rerun on the card's run row.
func (s *Store) IncrementAttempts(ctx context.Context, project, cardID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET attempts = attempts + 1 WHERE project = ? AND card_id = ?`,
		project, cardID); err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}

	return nil
}

// IncrementUsageReruns counts one usage-limit rerun on the card's run row.
func (s *Store) IncrementUsageReruns(ctx context.Context, project, cardID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE card_run_modes SET usage_reruns = usage_reruns + 1 WHERE project = ? AND card_id = ?`,
		project, cardID); err != nil {
		return fmt.Errorf("increment usage reruns: %w", err)
	}

	return nil
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
