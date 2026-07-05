package sqlite

import (
	"context"
	"fmt"
	"time"
)

// ModelOutcome is one candidate appearance in a Best-of-N game.
type ModelOutcome struct {
	Project, CardID, Model, Role, Result, JudgeModel string
	VerifyPass                                       bool
	CostUSD                                          float64
	NCandidates                                      int
}

// OutcomeStats aggregates a model's recorded appearances.
type OutcomeStats struct {
	Model        string
	Samples      int
	Wins         int
	ExpectedWins float64
	TotalCostUSD float64
}

// RecordModelOutcomes inserts one row per candidate appearance. The batch is
// validated up front and written in a single transaction.
func (s *Store) RecordModelOutcomes(ctx context.Context, rows []ModelOutcome) error {
	if len(rows) == 0 {
		return nil
	}

	for i, r := range rows {
		if r.Model == "" {
			return fmt.Errorf("record model outcomes: row %d: model required", i)
		}

		if r.Result != "win" && r.Result != "loss" && r.Result != "failed" {
			return fmt.Errorf("record model outcomes: row %d: invalid result %q", i, r.Result)
		}

		if r.NCandidates < 2 {
			return fmt.Errorf("record model outcomes: row %d: n_candidates must be >= 2, got %d", i, r.NCandidates)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record model outcomes: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()

	for _, r := range rows {
		role := r.Role
		if role == "" {
			role = "coder"
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_outcomes
				(project, card_id, model, role, result, verify_pass, cost_usd, n_candidates, judge_model, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.Project, r.CardID, r.Model, role, r.Result, boolInt(r.VerifyPass),
			r.CostUSD, r.NCandidates, nullable(r.JudgeModel), now); err != nil {
			return fmt.Errorf("record model outcome %q: %w", r.Model, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record model outcomes: commit: %w", err)
	}

	return nil
}

// ModelOutcomeStats aggregates per-model samples, wins, expected wins, and cost.
func (s *Store) ModelOutcomeStats(ctx context.Context) ([]OutcomeStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model,
		       COUNT(*),
		       SUM(CASE WHEN result = 'win' THEN 1 ELSE 0 END),
		       SUM(1.0 / n_candidates),
		       SUM(cost_usd)
		FROM model_outcomes
		GROUP BY model
		ORDER BY model`)
	if err != nil {
		return nil, fmt.Errorf("model outcome stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []OutcomeStats

	for rows.Next() {
		var st OutcomeStats
		if err := rows.Scan(&st.Model, &st.Samples, &st.Wins, &st.ExpectedWins, &st.TotalCostUSD); err != nil {
			return nil, fmt.Errorf("scan outcome stats: %w", err)
		}

		out = append(out, st)
	}

	return out, rows.Err()
}

// ResetModelOutcomes deletes all recorded outcomes and returns the row count.
func (s *Store) ResetModelOutcomes(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM model_outcomes`)
	if err != nil {
		return 0, fmt.Errorf("reset model outcomes: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reset model outcomes: rows affected: %w", err)
	}

	return n, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
