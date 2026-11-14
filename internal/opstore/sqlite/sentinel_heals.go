package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// healMutable are the sentinel_heals columns SaveHeal rewrites, in healValues
// order.
const healMutable = `run_id, cause, response, stage, reason,
	dispatch_kind, dispatch_mode, dispatch_ceiling_usd, dispatch_at, dispatch_answered_run_id, dispatch_failure,
	investigation, check_result, sent_subtasks, sent_siblings, change,
	relaunched_at, cost_base_usd, source_card, updated_at`

// healMutableParams holds one parameter per healMutable column.
const healMutableParams = `?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`

const healSelect = `SELECT id, kind, project, card_id, run_started, created_at, ` + healMutable + `,
	(SELECT COALESCE(SUM(usd), 0) FROM sentinel_spend WHERE heal_id = h.id), version
	FROM sentinel_heals h`

const insertSpend = `INSERT INTO sentinel_spend (heal_id, mode, usd, at, run_id) VALUES (?, ?, ?, ?, ?)`

// OpenHeal inserts a heal or forward row and returns its ID. A heal also marks
// its run decided in the same transaction; a second heal for the same run
// inserts nothing and returns the first one's ID.
func (s *Store) OpenHeal(ctx context.Context, h state.Heal) (int64, error) {
	vals, err := healValues(h)
	if err != nil {
		return 0, fmt.Errorf("open heal: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("open heal: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	args := append([]any{h.Kind, h.Project, h.CardID, unixNano(h.RunStarted), unixNano(h.CreatedAt)}, vals...)

	var id int64

	err = tx.QueryRowContext(ctx,
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+healMutable+`)
		 VALUES (?, ?, ?, ?, ?, `+healMutableParams+`)
		 ON CONFLICT(project, card_id, run_started) WHERE kind = 'heal' DO NOTHING
		 RETURNING id`, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx,
			`SELECT id FROM sentinel_heals WHERE kind = 'heal' AND project = ? AND card_id = ? AND run_started = ?`,
			h.Project, h.CardID, unixNano(h.RunStarted)).Scan(&id)
	}

	if err != nil {
		return 0, fmt.Errorf("open heal: %w", err)
	}

	if h.Kind == state.HealKindHeal {
		if _, err := tx.ExecContext(ctx, markRunDecided, h.Project, h.CardID, unixNano(h.RunStarted)); err != nil {
			return 0, fmt.Errorf("open heal: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("open heal: %w", err)
	}

	return id, nil
}

// SaveHeal rewrites every column of the heal but its identity, run start and
// creation time, and advances h.Version with the row's, so h can be saved
// again. A row written since h was read is left alone, h untouched and
// ErrHealChanged returned, so a save from a stale read never wipes an answer
// RecordRunKindStatus stored meanwhile.
func (s *Store) SaveHeal(ctx context.Context, h *state.Heal) error {
	vals, err := healValues(*h)
	if err != nil {
		return fmt.Errorf("save heal: %w", err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE sentinel_heals SET (`+healMutable+`) = (`+healMutableParams+`), version = version + 1
		 WHERE id = ? AND version = ?`,
		append(vals, h.ID, h.Version)...)
	if err != nil {
		return fmt.Errorf("save heal: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("save heal: %w", err)
	}

	if n == 0 {
		return state.ErrHealChanged
	}

	h.Version++

	return nil
}

// OpenHeals returns every heal whose stage is not terminal, oldest first.
func (s *Store) OpenHeals(ctx context.Context) ([]state.Heal, error) {
	args := []any{state.HealKindHeal}
	for _, stage := range state.TerminalStages {
		args = append(args, stage)
	}

	heals, err := s.queryHeals(ctx,
		`kind = ? AND stage NOT IN (`+placeholders(len(state.TerminalStages))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list open heals: %w", err)
	}

	return heals, nil
}

// InFlightHeals returns every heal whose run kind was sent and is neither
// answered nor failed, whatever its stage, oldest first.
func (s *Store) InFlightHeals(ctx context.Context) ([]state.Heal, error) {
	heals, err := s.queryHeals(ctx,
		`kind = ? AND dispatch_at IS NOT NULL AND dispatch_answered_run_id = '' AND dispatch_failure = '' ORDER BY id`,
		state.HealKindHeal)
	if err != nil {
		return nil, fmt.Errorf("list in-flight heals: %w", err)
	}

	return heals, nil
}

// CardHeals returns the card's heal and forward rows, newest first.
func (s *Store) CardHeals(ctx context.Context, project, cardID string) ([]state.Heal, error) {
	heals, err := s.queryHeals(ctx, `project = ? AND card_id = ? ORDER BY id DESC`, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("list card heals: %w", err)
	}

	return heals, nil
}

// LatestHeals returns each card's newest heal or forward row in the project,
// by card ID.
func (s *Store) LatestHeals(ctx context.Context, project string) (map[string]state.Heal, error) {
	heals, err := s.queryHeals(ctx,
		`id IN (SELECT MAX(id) FROM sentinel_heals WHERE project = ? GROUP BY card_id)`, project)
	if err != nil {
		return nil, fmt.Errorf("list latest heals: %w", err)
	}

	latest := make(map[string]state.Heal, len(heals))
	for _, h := range heals {
		latest[h.CardID] = h
	}

	return latest, nil
}

// RecordRunKindStatus records a run kind's terminal status on the card's heal
// whose dispatch of that kind is neither answered nor failed, and charges its
// ledger once, whatever the heal's stage. Sentinel keeps at most one such heal
// per card and kind; should there be more, the newest answers. A run ID already
// on the ledger is a repeat and records and charges nothing; so does a report
// matching no heal, or one without a kind or run ID.
func (s *Store) RecordRunKindStatus(ctx context.Context, r state.RunKindReport) (healID int64, recorded bool, err error) {
	if r.Kind == "" || r.RunID == "" {
		return 0, false, nil
	}

	var investigation, check any // NULL keeps the stored result

	if r.Result != nil {
		switch r.Kind {
		case protocol.RunKindInvestigate:
			investigation = string(r.Result)
		case protocol.RunKindCheck:
			check = string(r.Result)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("record run kind status: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	var (
		mode    string
		ceiling float64
	)

	// The run_id != '' term lets the partial unique index on the ledger's run_id
	// serve the repeat lookup.
	err = tx.QueryRowContext(ctx,
		`UPDATE sentinel_heals SET dispatch_answered_run_id = ?, dispatch_failure = ?,
		   investigation = COALESCE(?, investigation), check_result = COALESCE(?, check_result),
		   version = version + 1
		 WHERE id = (SELECT id FROM sentinel_heals
		             WHERE project = ? AND card_id = ? AND dispatch_kind = ?
		               AND dispatch_answered_run_id = '' AND dispatch_failure = ''
		             ORDER BY id DESC LIMIT 1)
		   AND NOT EXISTS (SELECT 1 FROM sentinel_spend WHERE run_id = ? AND run_id != '')
		 RETURNING id, dispatch_mode, dispatch_ceiling_usd`,
		r.RunID, r.Failure, investigation, check, r.Project, r.CardID, r.Kind, r.RunID).
		Scan(&healID, &mode, &ceiling)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("record run kind status: %w", err)
	}

	usd := r.SpendUSD
	if r.ChargeCeiling {
		usd = ceiling
	}

	if _, err := tx.ExecContext(ctx, insertSpend, healID, mode, usd, unixNano(r.At), r.RunID); err != nil {
		return 0, false, fmt.Errorf("record run kind status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("record run kind status: %w", err)
	}

	return healID, true, nil
}

// AddFindings stores a heal's friction findings in their reported order.
func (s *Store) AddFindings(ctx context.Context, healID int64, project, cardID string, fs []protocol.FrictionFinding, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add findings: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	for _, f := range fs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sentinel_findings (heal_id, project, card_id, kind, title, text, evidence, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			healID, project, cardID, f.Kind, f.Title, f.Text, f.Evidence, unixNano(at)); err != nil {
			return fmt.Errorf("add findings: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add findings: %w", err)
	}

	return nil
}

// CardFindings returns the card's findings, newest heal first, each heal's in
// their reported order.
func (s *Store) CardFindings(ctx context.Context, project, cardID string) ([]state.Finding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT heal_id, kind, title, text, evidence, created_at FROM sentinel_findings
		 WHERE project = ? AND card_id = ? ORDER BY heal_id DESC, id`, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("list card findings: %w", err)
	}
	defer rows.Close()

	var findings []state.Finding

	for rows.Next() {
		var (
			f  state.Finding
			at int64
		)

		if err := rows.Scan(&f.HealID, &f.Kind, &f.Title, &f.Text, &f.Evidence, &at); err != nil {
			return nil, fmt.Errorf("list card findings: %w", err)
		}

		f.CreatedAt = fromUnixNano(at)
		findings = append(findings, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list card findings: %w", err)
	}

	return findings, nil
}

// Charge adds one row to a heal's spend ledger, tied to no run kind's status.
func (s *Store) Charge(ctx context.Context, healID int64, mode string, usd float64, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, insertSpend, healID, mode, usd, unixNano(at), ""); err != nil {
		return fmt.Errorf("charge heal: %w", err)
	}

	return nil
}

// SpentSince sums the ledger rows charged on mode at or after since.
func (s *Store) SpentSince(ctx context.Context, mode string, since time.Time) (float64, error) {
	var usd float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(usd), 0) FROM sentinel_spend WHERE mode = ? AND at >= ?`,
		mode, unixNano(since)).Scan(&usd); err != nil {
		return 0, fmt.Errorf("sum sentinel spend: %w", err)
	}

	return usd, nil
}

// HealedSince counts the distinct cards with a heal saved as healed at or
// after since. Only SaveHeal writes updated_at, and the one later save of a
// terminal heal (failing a run kind that never reported) keeps it, so a
// healed heal's updated_at is when it reached healed.
func (s *Store) HealedSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (SELECT DISTINCT project, card_id FROM sentinel_heals WHERE stage = ? AND updated_at >= ?)`,
		state.StageHealed, unixNano(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count healed cards: %w", err)
	}

	return n, nil
}

func (s *Store) queryHeals(ctx context.Context, where string, args ...any) ([]state.Heal, error) {
	rows, err := s.db.QueryContext(ctx, healSelect+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var heals []state.Heal

	for rows.Next() {
		h, err := scanHeal(rows)
		if err != nil {
			return nil, err
		}

		heals = append(heals, h)
	}

	return heals, rows.Err()
}

// healValues are the heal's healMutable values, its JSON fields encoded.
func healValues(h state.Heal) ([]any, error) {
	d := h.Dispatch
	vals := []any{
		h.RunID, h.Cause, h.Response, h.Stage, h.Reason,
		d.Kind, d.Mode, d.CeilingUSD, nullUnixNano(d.At), d.AnsweredRunID, d.Failure,
	}

	for _, v := range []any{h.Investigation, h.Check, h.SentSubtasks, h.SentSiblings, h.Change} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode heal: %w", err)
		}

		vals = append(vals, string(b))
	}

	return append(vals, nullUnixNano(h.RelaunchedAt), h.CostBaseUSD, h.SourceCard, unixNano(h.UpdatedAt)), nil
}

func scanHeal(sc scanner) (state.Heal, error) {
	var (
		h                                    state.Heal
		started, created, updated            int64
		dispatchAt, relaunched               sql.NullInt64
		inv, check, subtasks, siblings, chng string
	)

	d := &h.Dispatch
	if err := sc.Scan(&h.ID, &h.Kind, &h.Project, &h.CardID, &started, &created,
		&h.RunID, &h.Cause, &h.Response, &h.Stage, &h.Reason,
		&d.Kind, &d.Mode, &d.CeilingUSD, &dispatchAt, &d.AnsweredRunID, &d.Failure,
		&inv, &check, &subtasks, &siblings, &chng,
		&relaunched, &h.CostBaseUSD, &h.SourceCard, &updated, &h.SpendUSD, &h.Version); err != nil {
		return state.Heal{}, fmt.Errorf("scan heal: %w", err)
	}

	for _, doc := range []struct {
		text string
		into any
	}{
		{inv, &h.Investigation},
		{check, &h.Check},
		{subtasks, &h.SentSubtasks},
		{siblings, &h.SentSiblings},
		{chng, &h.Change},
	} {
		if err := json.Unmarshal([]byte(doc.text), doc.into); err != nil {
			return state.Heal{}, fmt.Errorf("decode heal %d: %w", h.ID, err)
		}
	}

	h.RunStarted, h.CreatedAt, h.UpdatedAt = fromUnixNano(started), fromUnixNano(created), fromUnixNano(updated)
	d.At, h.RelaunchedAt = nanoTime(dispatchAt), nanoTime(relaunched)

	return h, nil
}

// unixNano stores t in nanoseconds, the zero time as 0 so it reads back zero.
func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UnixNano()
}

func fromUnixNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}

	return time.Unix(0, n).UTC()
}

func nullUnixNano(t *time.Time) any {
	if t == nil {
		return nil
	}

	return t.UnixNano()
}

// placeholders is n comma-separated SQL parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
