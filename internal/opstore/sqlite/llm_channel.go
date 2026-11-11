package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
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
		project, cardID, mode, at.UnixNano()); err != nil {
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

// SubscriptionUsageRow is the newest stored reading of one subscription window.
type SubscriptionUsageRow struct {
	protocol.SubscriptionUsage
	SeenAt time.Time
}

// RecordSubscriptionUsage upserts each window that has a reset time with the
// time it was seen. An older reading, such as a card run's last one landing
// after a chat's, is ignored: a later reset wins, and within one period
// utilization only rises.
func (s *Store) RecordSubscriptionUsage(ctx context.Context, u []protocol.SubscriptionUsage, seen time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record subscription usage: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	for _, w := range u {
		if w.ResetsAt.IsZero() {
			continue
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subscription_usage (window, utilization, status, resets_at, seen_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(window) DO UPDATE SET utilization = excluded.utilization,
			   status = excluded.status, resets_at = excluded.resets_at, seen_at = excluded.seen_at
			 WHERE excluded.resets_at > subscription_usage.resets_at
			   OR (excluded.resets_at = subscription_usage.resets_at
			       AND excluded.utilization >= subscription_usage.utilization)`,
			w.Window, w.Utilization, w.Status, w.ResetsAt.Unix(), seen.Unix()); err != nil {
			return fmt.Errorf("record subscription usage: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record subscription usage: %w", err)
	}

	return nil
}

// SubscriptionUsage returns every window's newest reading, ordered by window.
func (s *Store) SubscriptionUsage(ctx context.Context) ([]SubscriptionUsageRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT window, utilization, status, resets_at, seen_at FROM subscription_usage ORDER BY window`)
	if err != nil {
		return nil, fmt.Errorf("list subscription usage: %w", err)
	}
	defer rows.Close()

	var out []SubscriptionUsageRow

	for rows.Next() {
		var (
			r            SubscriptionUsageRow
			resets, seen int64
		)

		if err := rows.Scan(&r.Window, &r.Utilization, &r.Status, &resets, &seen); err != nil {
			return nil, fmt.Errorf("scan subscription usage: %w", err)
		}

		r.ResetsAt = time.Unix(resets, 0).UTC()
		r.SeenAt = time.Unix(seen, 0).UTC()
		out = append(out, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list subscription usage: %w", err)
	}

	return out, nil
}
