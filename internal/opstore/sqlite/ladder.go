package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix-protocol/selection"
)

// ErrInvalidLadder wraps every validation failure of a selector ladder so a
// handler can map it to 422 without matching message text.
var ErrInvalidLadder = errors.New("invalid selector ladder")

// selectorRoles is the closed set of roles a stored ladder must cover. The
// store refuses a partial write: an absent role would silently read as the
// built-in ladder while the operator believes they set it.
var selectorRoles = []selection.Role{selection.RoleCoder, selection.RoleReviewer}

// ValidateSelectorLadders is the one rule for a ladder CM accepts, on write
// and on preview: both roles present and non-empty, each validating under
// the shared selection rule (merged over the defaults, non-decreasing,
// every bar in [0, 1]). The result carries all four tiers per role.
func ValidateSelectorLadders(in map[string]map[string]float64) (selection.Ladders, error) {
	for _, role := range selectorRoles {
		if len(in[string(role)]) == 0 {
			return nil, fmt.Errorf("%w: missing role %q", ErrInvalidLadder, role)
		}
	}

	ladders, err := selection.LaddersFromWire(in)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidLadder, err)
	}

	return ladders, nil
}

// SelectorLadders returns the stored per-role ladders as wire maps (role to
// tier to bar) and the time they were written. An empty map and a zero
// time mean nothing is stored: the caller reads that as the built-in ladder
// for both roles.
func (s *Store) SelectorLadders(ctx context.Context) (map[string]map[string]float64, time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, tier, bar, updated_at FROM selector_ladder ORDER BY role, tier`)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read selector ladders: %w", err)
	}

	defer rows.Close() //nolint:errcheck

	out := map[string]map[string]float64{}

	var updated int64

	for rows.Next() {
		var (
			role, tier string
			bar        float64
			at         int64
		)

		if err := rows.Scan(&role, &tier, &bar, &at); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan selector ladder: %w", err)
		}

		if out[role] == nil {
			out[role] = map[string]float64{}
		}

		out[role][tier] = bar
		updated = max(updated, at)
	}

	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("read selector ladders: %w", err)
	}

	if updated == 0 {
		return out, time.Time{}, nil
	}

	return out, time.Unix(updated, 0).UTC(), nil
}

// PutSelectorLadders validates ladders and replaces every stored row in one
// transaction, so a reader never sees half a ladder and the store never
// holds a ladder the agent would reject.
func (s *Store) PutSelectorLadders(ctx context.Context, ladders map[string]map[string]float64) error {
	valid, err := ValidateSelectorLadders(ladders)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put selector ladders: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM selector_ladder`); err != nil {
		return fmt.Errorf("put selector ladders: clear: %w", err)
	}

	now := time.Now().Unix()

	for role, bars := range valid {
		for tier, bar := range bars {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO selector_ladder (role, tier, bar, updated_at) VALUES (?, ?, ?, ?)`,
				string(role), string(tier), bar, now); err != nil {
				return fmt.Errorf("put selector ladder %s/%s: %w", role, tier, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("put selector ladders: commit: %w", err)
	}

	return nil
}
