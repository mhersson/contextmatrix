package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// SentinelSettings returns the stored settings, when they were written and by
// whom. Defaults and a zero time mean nothing is stored.
func (s *Store) SentinelSettings(ctx context.Context) (state.Settings, time.Time, string, error) {
	var (
		set                           state.Settings
		enabled, healing, friction, t int64
		by                            string
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT enabled, healing, friction, daily_cap_usd, investigate_ceiling_usd, headroom, updated_at, updated_by
		 FROM sentinel_settings WHERE id = 1`).
		Scan(&enabled, &healing, &friction, &set.DailyCapUSD, &set.InvestigateCeilingUSD, &set.Headroom, &t, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return state.DefaultSettings(), time.Time{}, "", nil
	}

	if err != nil {
		return state.Settings{}, time.Time{}, "", fmt.Errorf("read sentinel settings: %w", err)
	}

	set.Enabled, set.Healing, set.Friction = enabled != 0, healing != 0, friction != 0

	return set, time.Unix(t, 0).UTC(), by, nil
}

// PutSentinelSettings validates and replaces the single row; nothing is stored
// when the settings are invalid. The ecosystem column is left untouched.
func (s *Store) PutSentinelSettings(ctx context.Context, set state.Settings, by string) error {
	if err := set.Validate(); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sentinel_settings
		   (id, enabled, healing, friction, daily_cap_usd, investigate_ceiling_usd, headroom, updated_at, updated_by)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled, healing = excluded.healing,
		   friction = excluded.friction, daily_cap_usd = excluded.daily_cap_usd,
		   investigate_ceiling_usd = excluded.investigate_ceiling_usd, headroom = excluded.headroom,
		   updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		b2i(set.Enabled), b2i(set.Healing), b2i(set.Friction), set.DailyCapUSD, set.InvestigateCeilingUSD,
		set.Headroom, time.Now().Unix(), by); err != nil {
		return fmt.Errorf("put sentinel settings: %w", err)
	}

	return nil
}

// SubscriptionWindows returns every window's newest reading without its seen time.
func (s *Store) SubscriptionWindows(ctx context.Context) ([]protocol.SubscriptionUsage, error) {
	rows, err := s.SubscriptionUsage(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]protocol.SubscriptionUsage, len(rows))
	for i, r := range rows {
		out[i] = r.SubscriptionUsage
	}

	return out, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}
