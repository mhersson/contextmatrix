package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Jira writeback outbox row states.
const (
	JiraWritebackPending = "pending"
	JiraWritebackDone    = "done"
	JiraWritebackSkipped = "skipped"
	JiraWritebackFailed  = "failed"
)

// JiraWriteback is one outbox row: a status write owed to Jira for a card.
// Times are unix seconds.
type JiraWriteback struct {
	Project       string
	CardID        string
	Kind          string
	IssueKey      string
	State         string
	Attempts      int
	NextAttemptAt int64
	LastError     string
	CreatedAt     int64
	UpdatedAt     int64
}

const jiraWritebackColumns = `project, card_id, kind, issue_key, state, attempts, next_attempt_at,
	COALESCE(last_error, ''), created_at, updated_at`

// EnqueueJiraWriteback records a pending write unless a row for
// (project, card, kind) already targets the same issue, in any state. That
// makes a second entry into in_progress, or a reopened card's second done, a
// no-op. A row for a different issue (a recreated project reusing card IDs)
// is reset to pending for the new one. It reports whether a row was written.
func (s *Store) EnqueueJiraWriteback(ctx context.Context, project, cardID, kind, issueKey string, now int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO jira_writeback (project, card_id, kind, issue_key, state, attempts, next_attempt_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'pending', 0, ?, ?, ?)
		ON CONFLICT(project, card_id, kind) DO UPDATE SET
			issue_key = excluded.issue_key, state = 'pending', attempts = 0,
			next_attempt_at = excluded.next_attempt_at, last_error = NULL,
			created_at = excluded.created_at, updated_at = excluded.updated_at
		WHERE jira_writeback.issue_key <> excluded.issue_key`,
		project, cardID, kind, issueKey, now, now, now)
	if err != nil {
		return false, fmt.Errorf("enqueue jira writeback %s/%s %s: %w", project, cardID, kind, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("enqueue jira writeback: rows affected: %w", err)
	}

	return n == 1, nil
}

// DueJiraWritebacks returns pending rows whose next attempt is due, oldest first.
func (s *Store) DueJiraWritebacks(ctx context.Context, now int64, limit int) ([]JiraWriteback, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+jiraWritebackColumns+`
		FROM jira_writeback
		WHERE state = 'pending' AND next_attempt_at <= ?
		ORDER BY next_attempt_at, created_at
		LIMIT ?`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due jira writebacks: %w", err)
	}

	defer rows.Close()

	var out []JiraWriteback

	for rows.Next() {
		r, err := scanJiraWriteback(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// HasPendingJiraWriteback reports whether a pending row exists for (project, card, kind).
func (s *Store) HasPendingJiraWriteback(ctx context.Context, project, cardID, kind string) (bool, error) {
	var n int

	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jira_writeback
		WHERE project = ? AND card_id = ? AND kind = ? AND state = 'pending'`,
		project, cardID, kind).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check pending jira writeback: %w", err)
	}

	return n > 0, nil
}

// FinishJiraWriteback moves a pending row to a terminal state.
func (s *Store) FinishJiraWriteback(ctx context.Context, project, cardID, kind, state, lastErr string, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jira_writeback SET state = ?, last_error = ?, updated_at = ?
		WHERE project = ? AND card_id = ? AND kind = ? AND state = 'pending'`,
		state, nullable(lastErr), now, project, cardID, kind)
	if err != nil {
		return fmt.Errorf("finish jira writeback %s/%s %s: %w", project, cardID, kind, err)
	}

	return nil
}

// RetryJiraWriteback records a failed attempt on a pending row and when to
// try again.
func (s *Store) RetryJiraWriteback(ctx context.Context, project, cardID, kind string, attempts int, nextAttemptAt int64, lastErr string, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jira_writeback SET attempts = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE project = ? AND card_id = ? AND kind = ? AND state = 'pending'`,
		attempts, nextAttemptAt, nullable(lastErr), now, project, cardID, kind)
	if err != nil {
		return fmt.Errorf("retry jira writeback %s/%s %s: %w", project, cardID, kind, err)
	}

	return nil
}

// GetJiraWriteback returns one row; ok is false when none exists.
func (s *Store) GetJiraWriteback(ctx context.Context, project, cardID, kind string) (JiraWriteback, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+jiraWritebackColumns+`
		FROM jira_writeback WHERE project = ? AND card_id = ? AND kind = ?`,
		project, cardID, kind)

	r, err := scanJiraWriteback(row)
	if errors.Is(err, sql.ErrNoRows) {
		return JiraWriteback{}, false, nil
	}

	if err != nil {
		return JiraWriteback{}, false, err
	}

	return r, true, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJiraWriteback(sc rowScanner) (JiraWriteback, error) {
	var r JiraWriteback

	err := sc.Scan(&r.Project, &r.CardID, &r.Kind, &r.IssueKey, &r.State, &r.Attempts,
		&r.NextAttemptAt, &r.LastError, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, err
	}

	if err != nil {
		return r, fmt.Errorf("scan jira writeback: %w", err)
	}

	return r, nil
}
