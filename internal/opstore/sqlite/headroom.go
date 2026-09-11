package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidHeadroom wraps every validation failure of the selector price
// headroom so a handler can map it to 422 without matching message text.
var ErrInvalidHeadroom = errors.New("invalid selector headroom")

// ValidateSelectorHeadroom is the one rule for a headroom CM accepts, on
// write and on preview: a finite multiplier of at least 1. Below 1 the band
// would sit under the cheapest candidate; the shared selector reads such a
// value as the built-in one, so storing it would show the operator a number
// that does not run.
func ValidateSelectorHeadroom(h float64) error {
	if math.IsNaN(h) || math.IsInf(h, 0) || h < 1 {
		return fmt.Errorf("%w: must be a number of at least 1, got %g", ErrInvalidHeadroom, h)
	}

	return nil
}

// SelectorHeadroom returns the stored price headroom and the time it was
// written. 0 and a zero time mean nothing is stored: the caller reads that
// as the built-in headroom.
func (s *Store) SelectorHeadroom(ctx context.Context) (float64, time.Time, error) {
	var (
		h  float64
		at int64
	)

	err := s.db.QueryRowContext(ctx, `SELECT headroom, updated_at FROM selector_headroom WHERE id = 1`).Scan(&h, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, nil
	}

	if err != nil {
		return 0, time.Time{}, fmt.Errorf("read selector headroom: %w", err)
	}

	return h, time.Unix(at, 0).UTC(), nil
}

// PutSelectorHeadroom validates the value and replaces the single row, so
// the store never holds a headroom the selector would ignore.
func (s *Store) PutSelectorHeadroom(ctx context.Context, h float64) error {
	if err := ValidateSelectorHeadroom(h); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO selector_headroom (id, headroom, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET headroom = excluded.headroom, updated_at = excluded.updated_at`,
		h, time.Now().Unix()); err != nil {
		return fmt.Errorf("put selector headroom: %w", err)
	}

	return nil
}
