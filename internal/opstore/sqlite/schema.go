package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureSchema creates every operational table if absent. Clean-cut: there is
// no migration ledger and no backward-compat path — an obsolete DB is deleted
// and recreated by the operator.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS model_blacklist (
			slug         TEXT PRIMARY KEY,
			reason       TEXT NOT NULL,
			sample_card  TEXT,
			reported_by  TEXT NOT NULL,
			first_seen   INTEGER NOT NULL,
			last_seen    INTEGER NOT NULL
		)`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("opstore schema: %w", err)
		}
	}

	return nil
}
