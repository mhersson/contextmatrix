package sqlite

import (
	"context"
	"fmt"
	"time"
)

// PutParkReset records when the usage limit that parked the card resets. A card
// has one row: a later park replaces it.
func (s *Store) PutParkReset(ctx context.Context, project, cardID string, resetsAt time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO card_park_resets (project, card_id, resets_at, recorded_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(project, card_id) DO UPDATE SET resets_at = excluded.resets_at,
		   recorded_at = excluded.recorded_at`,
		project, cardID, resetsAt.Unix(), time.Now().Unix()); err != nil {
		return fmt.Errorf("put park reset: %w", err)
	}

	return nil
}

// ClearParkReset drops the card's row. It is a no-op when none exists.
func (s *Store) ClearParkReset(ctx context.Context, project, cardID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM card_park_resets WHERE project = ? AND card_id = ?`, project, cardID); err != nil {
		return fmt.Errorf("clear park reset: %w", err)
	}

	return nil
}

// ParkResets returns the reset time of every card in the project that has one.
func (s *Store) ParkResets(ctx context.Context, project string) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT card_id, resets_at FROM card_park_resets WHERE project = ?`, project)
	if err != nil {
		return nil, fmt.Errorf("list park resets: %w", err)
	}
	defer rows.Close()

	out := make(map[string]time.Time)

	for rows.Next() {
		var (
			id string
			at int64
		)

		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan park reset: %w", err)
		}

		out[id] = time.Unix(at, 0).UTC()
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list park resets: %w", err)
	}

	return out, nil
}
