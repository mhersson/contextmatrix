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

// SubscriptionUsageRow is the newest stored reading of one subscription window.
type SubscriptionUsageRow struct {
	protocol.SubscriptionUsage
	SeenAt time.Time
}

// RecordSubscriptionUsage upserts each window that has a reset time with the
// time it was seen. An older reading, such as a card run's last one landing
// after a chat's, is ignored: a later reset replaces the row. Within one
// period a rejected reading always wins its status, a higher utilization wins,
// an equal one never clears a rejected status, and the stored utilization only
// rises, so a rejected reading of unknown utilization never lowers it.
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
			 ON CONFLICT(window) DO UPDATE SET
			   utilization = CASE WHEN excluded.resets_at > subscription_usage.resets_at THEN excluded.utilization
			                      ELSE MAX(excluded.utilization, subscription_usage.utilization) END,
			   status = CASE WHEN excluded.resets_at > subscription_usage.resets_at OR excluded.status = 'rejected'
			                      OR excluded.utilization > subscription_usage.utilization THEN excluded.status
			                 WHEN excluded.utilization = subscription_usage.utilization
			                      AND subscription_usage.status <> 'rejected' THEN excluded.status
			                 ELSE subscription_usage.status END,
			   resets_at = excluded.resets_at, seen_at = excluded.seen_at
			 WHERE excluded.resets_at > subscription_usage.resets_at
			   OR (excluded.resets_at = subscription_usage.resets_at
			       AND (excluded.status = 'rejected'
			            OR excluded.utilization > subscription_usage.utilization
			            OR (excluded.utilization = subscription_usage.utilization
			                AND subscription_usage.status <> 'rejected')))`,
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
