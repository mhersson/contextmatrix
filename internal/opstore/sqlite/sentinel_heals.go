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
	investigation, check_result, sent_subtasks, sent_siblings, change, dropped,
	relaunched_at, cost_base_usd, source_card, updated_at, sent_findings, allowed`

// healMutableParams holds one parameter per healMutable column.
const healMutableParams = `?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`

const healSelect = `SELECT id, kind, project, card_id, run_started, created_at, ` + healMutable + `,
	(SELECT COALESCE(SUM(usd), 0) FROM sentinel_spend WHERE heal_id = h.id), version, result
	FROM sentinel_heals h`

// healKinds are the kinds a heal list, a board mark and the open-heal flow
// read; analysis and revision rows are never heals.
const healKinds = `'heal', 'forward'`

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

// OpenHeals returns every heal row whose stage is not terminal, oldest first.
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

// InFlightRows returns every row, of any kind, whose run kind was sent and is
// neither answered nor failed, whatever its stage, oldest first.
func (s *Store) InFlightRows(ctx context.Context) ([]state.Heal, error) {
	rows, err := s.queryHeals(ctx,
		`dispatch_at IS NOT NULL AND dispatch_answered_run_id = '' AND dispatch_failure = '' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list in-flight rows: %w", err)
	}

	return rows, nil
}

// CardHeals returns the card's heal and forward rows, newest first.
func (s *Store) CardHeals(ctx context.Context, project, cardID string) ([]state.Heal, error) {
	heals, err := s.queryHeals(ctx,
		`kind IN (`+healKinds+`) AND project = ? AND card_id = ? ORDER BY id DESC`, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("list card heals: %w", err)
	}

	return heals, nil
}

// CardAnalyses returns the card's analysis rows, newest first.
func (s *Store) CardAnalyses(ctx context.Context, project, cardID string) ([]state.Heal, error) {
	rows, err := s.queryHeals(ctx,
		`kind = ? AND project = ? AND card_id = ? ORDER BY id DESC`, state.HealKindAnalysis, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("list card analyses: %w", err)
	}

	return rows, nil
}

// LatestHeals returns each card's newest heal or forward row in the project,
// by card ID.
func (s *Store) LatestHeals(ctx context.Context, project string) (map[string]state.Heal, error) {
	heals, err := s.queryHeals(ctx,
		`id IN (SELECT MAX(id) FROM sentinel_heals WHERE kind IN (`+healKinds+`) AND project = ? GROUP BY card_id)`, project)
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

	var investigation, check, result any // NULL keeps the stored result

	if r.Result != nil {
		switch r.Kind {
		case protocol.RunKindInvestigate:
			investigation = string(r.Result)
		case protocol.RunKindCheck:
			check = string(r.Result)
		case protocol.RunKindAnalyse, protocol.RunKindGuidancePR:
			result = string(r.Result)
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
		   result = COALESCE(?, result),
		   version = version + 1
		 WHERE id = (SELECT id FROM sentinel_heals
		             WHERE project = ? AND card_id = ? AND dispatch_kind = ?
		               AND dispatch_answered_run_id = '' AND dispatch_failure = ''
		             ORDER BY id DESC LIMIT 1)
		   AND NOT EXISTS (SELECT 1 FROM sentinel_spend WHERE run_id = ? AND run_id != '')
		 RETURNING id, dispatch_mode, dispatch_ceiling_usd`,
		r.RunID, r.Failure, investigation, check, result, r.Project, r.CardID, r.Kind, r.RunID).
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
			`INSERT INTO sentinel_findings (heal_id, project, card_id, kind, title, text, evidence, state, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			healID, project, cardID, f.Kind, f.Title, f.Text, f.Evidence, state.InitialFindingState(f.Kind),
			unixNano(at), unixNano(at)); err != nil {
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
	return s.queryFindings(ctx, `project = ? AND card_id = ? ORDER BY heal_id DESC, id`, project, cardID)
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

// HealedSince counts the distinct cards with a heal row saved as healed at or
// after since. Only SaveHeal writes updated_at, and the one later save of a
// terminal heal (failing a run kind that never reported) keeps it, so a
// healed heal's updated_at is when it reached healed.
func (s *Store) HealedSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (SELECT DISTINCT project, card_id FROM sentinel_heals WHERE kind = 'heal' AND stage = ? AND updated_at >= ?)`,
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

	for _, v := range []any{h.Investigation, h.Check, h.SentSubtasks, h.SentSiblings, h.Change, h.Dropped} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode heal: %w", err)
		}

		vals = append(vals, string(b))
	}

	vals = append(vals, nullUnixNano(h.RelaunchedAt), h.CostBaseUSD, h.SourceCard, unixNano(h.UpdatedAt))

	for _, v := range []any{h.SentFindings, h.Allowed} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode heal: %w", err)
		}

		vals = append(vals, string(b))
	}

	return vals, nil
}

func scanHeal(sc scanner) (state.Heal, error) {
	var (
		h                                             state.Heal
		started, created, updated                     int64
		dispatchAt, relaunched                        sql.NullInt64
		inv, check, subtasks, siblings, chng, dropped string
		sentFindings, allowed, result                 string
	)

	d := &h.Dispatch

	err := sc.Scan(&h.ID, &h.Kind, &h.Project, &h.CardID, &started, &created,
		&h.RunID, &h.Cause, &h.Response, &h.Stage, &h.Reason,
		&d.Kind, &d.Mode, &d.CeilingUSD, &dispatchAt, &d.AnsweredRunID, &d.Failure,
		&inv, &check, &subtasks, &siblings, &chng, &dropped,
		&relaunched, &h.CostBaseUSD, &h.SourceCard, &updated, &sentFindings, &allowed, &h.SpendUSD, &h.Version, &result)
	if err != nil {
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
		{dropped, &h.Dropped},
		{sentFindings, &h.SentFindings},
		{allowed, &h.Allowed},
	} {
		if err := json.Unmarshal([]byte(doc.text), doc.into); err != nil {
			return state.Heal{}, fmt.Errorf("decode heal %d: %w", h.ID, err)
		}
	}

	switch h.Dispatch.Kind {
	case protocol.RunKindAnalyse:
		err = json.Unmarshal([]byte(result), &h.Analysis)
	case protocol.RunKindGuidancePR:
		err = json.Unmarshal([]byte(result), &h.Revision)
	}

	if err != nil {
		return state.Heal{}, fmt.Errorf("decode heal %d result: %w", h.ID, err)
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

// QueueAnalysis stores an analysis row for the run h names and marks the run
// row that started at h.RunStarted queued, in one transaction. A run ID that
// already has an analysis row inserts nothing.
func (s *Store) QueueAnalysis(ctx context.Context, h state.Heal) error {
	vals, err := healValues(h)
	if err != nil {
		return fmt.Errorf("queue analysis: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("queue analysis: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	args := append([]any{state.HealKindAnalysis, h.Project, h.CardID, unixNano(h.RunStarted), unixNano(h.CreatedAt)}, vals...)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+healMutable+`)
		 VALUES (?, ?, ?, ?, ?, `+healMutableParams+`)
		 ON CONFLICT(project, card_id, run_id) WHERE kind = 'analysis' DO NOTHING`, args...); err != nil {
		return fmt.Errorf("queue analysis: %w", err)
	}

	if _, err := tx.ExecContext(ctx, markRunQueued, h.Project, h.CardID, unixNano(h.RunStarted)); err != nil {
		return fmt.Errorf("queue analysis: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("queue analysis: %w", err)
	}

	return nil
}

// OpenAnalyses returns every analysis row still at the analyse stage, newest
// first.
func (s *Store) OpenAnalyses(ctx context.Context) ([]state.Heal, error) {
	rows, err := s.queryHeals(ctx, `kind = ? AND stage = ? ORDER BY id DESC`, state.HealKindAnalysis, state.StageAnalyse)
	if err != nil {
		return nil, fmt.Errorf("list open analyses: %w", err)
	}

	return rows, nil
}

// RunHeal returns the heal of the run that started at started, or nil when
// there is none.
func (s *Store) RunHeal(ctx context.Context, project, cardID string, started time.Time) (*state.Heal, error) {
	heals, err := s.queryHeals(ctx, `kind = ? AND project = ? AND card_id = ? AND run_started = ?`,
		state.HealKindHeal, project, cardID, unixNano(started))
	if err != nil {
		return nil, fmt.Errorf("read run heal: %w", err)
	}

	if len(heals) == 0 {
		return nil, nil //nolint:nilnil // nil, nil means no heal
	}

	return &heals[0], nil
}

const findingColumns = `id, heal_id, project, card_id, kind, title, text, evidence, state, reason, output, revision_id,
	created_at, updated_at`

// QueuedFindings returns the findings waiting to be routed, oldest first.
func (s *Store) QueuedFindings(ctx context.Context) ([]state.Finding, error) {
	return s.queryFindings(ctx, `state = 'queued' ORDER BY id`)
}

// ProjectGuidance returns the project's guidance findings that are pending or
// already in, or turned down from, a guidance PR.
func (s *Store) ProjectGuidance(ctx context.Context, project string) ([]state.Finding, error) {
	return s.queryFindings(ctx,
		`project = ? AND kind = 'guidance' AND state IN ('pending', 'proposed', 'merged', 'rejected') ORDER BY id`, project)
}

// ProposedFindings returns the project's guidance findings proposed in the
// standing PR.
func (s *Store) ProposedFindings(ctx context.Context, project string) ([]state.Finding, error) {
	return s.queryFindings(ctx, `project = ? AND kind = 'guidance' AND state = 'proposed' ORDER BY id`, project)
}

// CardedSince counts the project's findings that became friction cards at or
// after since.
func (s *Store) CardedSince(ctx context.Context, project string, since time.Time) (int, error) {
	var n int

	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sentinel_findings WHERE project = ? AND state = 'carded' AND updated_at >= ?`,
		project, unixNano(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count carded findings: %w", err)
	}

	return n, nil
}

// CardedFindings returns the IDs of the project's cards Sentinel created from
// a finding.
func (s *Store) CardedFindings(ctx context.Context, project string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT output FROM sentinel_findings WHERE project = ? AND state = 'carded' AND output != ''`, project)
	if err != nil {
		return nil, fmt.Errorf("list carded findings: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list carded findings: %w", err)
		}

		out[id] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list carded findings: %w", err)
	}

	return out, nil
}

// SetFindingOutcome moves a finding to st with the reason, output and revision
// that explain it.
func (s *Store) SetFindingOutcome(ctx context.Context, id int64, st, reason, output string, revisionID int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sentinel_findings SET state = ?, reason = ?, output = ?, revision_id = ?, updated_at = ? WHERE id = ?`,
		st, reason, output, revisionID, unixNano(at), id)
	if err != nil {
		return fmt.Errorf("set finding outcome: %w", err)
	}

	return nil
}

func (s *Store) queryFindings(ctx context.Context, where string, args ...any) ([]state.Finding, error) {
	// where is a constant of this file's callers, never user input.
	rows, err := s.db.QueryContext(ctx, `SELECT `+findingColumns+` FROM sentinel_findings WHERE `+where, args...) //nolint:gosec // see above
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()

	var findings []state.Finding

	for rows.Next() {
		var (
			f                state.Finding
			created, updated int64
		)

		if err := rows.Scan(&f.ID, &f.HealID, &f.Project, &f.CardID, &f.Kind, &f.Title, &f.Text, &f.Evidence,
			&f.State, &f.Reason, &f.Output, &f.RevisionID, &created, &updated); err != nil {
			return nil, fmt.Errorf("list findings: %w", err)
		}

		f.CreatedAt, f.UpdatedAt = fromUnixNano(created), fromUnixNano(updated)
		findings = append(findings, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}

	return findings, nil
}

// OpenRevision returns the project's open revision row, at stage revise,
// inserting one created at at when there is none.
func (s *Store) OpenRevision(ctx context.Context, project string, at time.Time) (state.Heal, error) {
	vals, err := healValues(state.Heal{Stage: state.StageRevise, UpdatedAt: at})
	if err != nil {
		return state.Heal{}, fmt.Errorf("open revision: %w", err)
	}

	args := append([]any{state.HealKindRevision, project, "", int64(0), unixNano(at)}, vals...)

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sentinel_heals (kind, project, card_id, run_started, created_at, `+healMutable+`)
		 VALUES (?, ?, ?, ?, ?, `+healMutableParams+`)
		 ON CONFLICT(project) WHERE kind = 'revision' AND stage = 'revise' DO NOTHING`, args...); err != nil {
		return state.Heal{}, fmt.Errorf("open revision: %w", err)
	}

	rows, err := s.queryHeals(ctx, `kind = ? AND stage = ? AND project = ?`, state.HealKindRevision, state.StageRevise, project)
	if err != nil {
		return state.Heal{}, fmt.Errorf("open revision: %w", err)
	}

	if len(rows) == 0 {
		return state.Heal{}, fmt.Errorf("open revision: no open row for %s", project)
	}

	return rows[0], nil
}

// OpenRevisions returns every revision row still at the revise stage, oldest
// first.
func (s *Store) OpenRevisions(ctx context.Context) ([]state.Heal, error) {
	rows, err := s.queryHeals(ctx, `kind = ? AND stage = ? ORDER BY id`, state.HealKindRevision, state.StageRevise)
	if err != nil {
		return nil, fmt.Errorf("list open revisions: %w", err)
	}

	return rows, nil
}

// LastRefusedRevision returns the project's newest refused revision row
// created after since, or nil when there is none.
func (s *Store) LastRefusedRevision(ctx context.Context, project string, since time.Time) (*state.Heal, error) {
	h, err := s.lastRevision(ctx, `stage = ? AND created_at > ?`, project, state.StageRevisionRefused, unixNano(since))
	if err != nil {
		return nil, fmt.Errorf("read last refused revision: %w", err)
	}

	return h, nil
}

// LastFailedRevision returns the project's newest revision row that ended
// failed or stale, or nil when there is none.
func (s *Store) LastFailedRevision(ctx context.Context, project string) (*state.Heal, error) {
	h, err := s.lastRevision(ctx, `stage IN (?, ?)`, project, state.StageRevisionFailed, state.StageRevisionStale)
	if err != nil {
		return nil, fmt.Errorf("read last failed revision: %w", err)
	}

	return h, nil
}

// LastRevisedRevision returns the project's newest revision row that ended
// revised, or nil when there is none.
func (s *Store) LastRevisedRevision(ctx context.Context, project string) (*state.Heal, error) {
	h, err := s.lastRevision(ctx, `stage = ?`, project, state.StageRevised)
	if err != nil {
		return nil, fmt.Errorf("read last revised revision: %w", err)
	}

	return h, nil
}

// lastRevision returns the project's newest revision row that also matches
// where, a constant of its callers, or nil.
func (s *Store) lastRevision(ctx context.Context, where, project string, args ...any) (*state.Heal, error) {
	rows, err := s.queryHeals(ctx, `kind = ? AND project = ? AND `+where+` ORDER BY id DESC LIMIT 1`,
		append([]any{state.HealKindRevision, project}, args...)...)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil //nolint:nilnil // nil, nil means no such revision
	}

	return &rows[0], nil
}

// PendingFindings returns the project's guidance findings waiting for the
// standing PR, oldest first.
func (s *Store) PendingFindings(ctx context.Context, project string) ([]state.Finding, error) {
	return s.queryFindings(ctx, `project = ? AND kind = 'guidance' AND state = 'pending' ORDER BY id`, project)
}

// Findings returns the findings with the given IDs, whatever their state, in
// ID order.
func (s *Store) Findings(ctx context.Context, ids []int64) ([]state.Finding, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	return s.queryFindings(ctx, `id IN (`+placeholders(len(ids))+`) ORDER BY id`, args...)
}
