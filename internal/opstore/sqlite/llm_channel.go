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
