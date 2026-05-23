package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register sqlite driver

	"github.com/mhersson/contextmatrix/internal/chat"
)

// compile-time assertion that *Store satisfies chat.Store.
var _ chat.Store = (*Store)(nil)

// Store is the SQLite-backed implementation of chat.Store.
type Store struct {
	db *sql.DB
}

// sqliteDSN builds a `file:` URI for the modernc.org/sqlite driver, passing
// PRAGMA settings via the query string. The `file:` prefix selects the URI
// VFS rather than the implicit filename VFS; we concatenate path directly
// (rather than via url.URL) because url.URL.String() places a relative path
// in the authority component (e.g. `file://chats.db`), which modernc/sqlite
// rejects as an invalid URI authority. synchronous=NORMAL is the recommended
// pairing for WAL — durable across process crashes, only weakens behaviour
// under power loss, acceptable for cached chat data. Mirrors
// internal/images/sqlite.go:sqliteDSN; keep the two in sync.
func sqliteDSN(path string) string {
	return "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
}

// Open opens (or creates) the SQLite database at path and applies the
// schema migrations. Parent directories are created as needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("chat: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("chat: open sqlite: %w", err)
	}

	// SQLite is single-writer regardless of pool size; serialisation across
	// writers happens at the manager level (chat.Manager.mu held across
	// AppendMessage). MaxOpenConns > 1 lets concurrent readers (ListMessages,
	// MaxSeq, GetSession) avoid queueing behind a writer when WAL is on.
	db.SetMaxOpenConns(5)

	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// sessionColumns lists every column read by scanSession in the exact order
// the SELECT statement projects them. Kept as a single source of truth so
// new fields don't drift between GetSession, ListSessions, and scanSession.
const sessionColumns = `id, title, project, status, created_at, last_active, created_by,
    container_id, workspace, model, context_tokens, context_tokens_updated_at, rehydration_active,
    rehydration_started_at, prompt_tokens, completion_tokens, cache_read_tokens,
    cache_creation_tokens, estimated_cost_usd`

func (s *Store) CreateSession(ctx context.Context, sess chat.Session) error {
	workspaceJSON, err := json.Marshal(sess.Workspace)
	if err != nil {
		return fmt.Errorf("chat: marshal workspace: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO chat_sessions
        (id, title, project, status, created_at, last_active, created_by, container_id, workspace, model)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, nullIf(sess.Project), string(sess.Status),
		sess.CreatedAt.Unix(), sess.LastActive.Unix(), sess.CreatedBy,
		nullIf(sess.ContainerID), string(workspaceJSON), sess.Model,
	)
	if err != nil {
		return fmt.Errorf("chat: insert session: %w", err)
	}

	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (chat.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM chat_sessions WHERE id = ?`, id)

	return scanSession(row)
}

func (s *Store) ListSessions(ctx context.Context, f chat.SessionFilter) ([]chat.Session, error) {
	q := `SELECT ` + sessionColumns + ` FROM chat_sessions WHERE 1=1`

	var args []any

	if f.Project != "" {
		q += " AND project = ?"

		args = append(args, f.Project)
	}

	if f.Status != "" {
		q += " AND status = ?"

		args = append(args, string(f.Status))
	}

	if f.CreatedBy != "" {
		q += " AND created_by = ?"

		args = append(args, f.CreatedBy)
	}

	if f.RehydrationActive != nil {
		q += " AND rehydration_active = ?"

		if *f.RehydrationActive {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	if !f.LastActiveBefore.IsZero() {
		q += " AND last_active < ?"

		args = append(args, f.LastActiveBefore.Unix())
	}

	if !f.RehydrationStartedBefore.IsZero() {
		q += " AND rehydration_started_at IS NOT NULL AND rehydration_started_at < ?"

		args = append(args, f.RehydrationStartedBefore.Unix())
	}

	q += " ORDER BY last_active DESC"

	if f.Limit > 0 {
		q += " LIMIT ?"

		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("chat: list sessions: %w", err)
	}

	defer rows.Close()

	var out []chat.Session

	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sess)
	}

	return out, rows.Err()
}

// CountSessionsByStatus returns the number of sessions whose status is one of
// the supplied values. A single SELECT COUNT(*) query avoids fetching full rows
// just to call len() on the result — used by openCold to enforce MaxConcurrent.
func (s *Store) CountSessionsByStatus(ctx context.Context, statuses ...chat.Status) (int, error) {
	if len(statuses) == 0 {
		return 0, nil
	}

	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = string(st)
	}

	placeholders := "?"
	for range statuses[1:] {
		placeholders += ",?"
	}

	var n int

	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_sessions WHERE status IN (`+placeholders+`)`,
		args...,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("chat: count sessions by status: %w", err)
	}

	return n, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess chat.Session) error {
	workspaceJSON, err := json.Marshal(sess.Workspace)
	if err != nil {
		return fmt.Errorf("chat: marshal workspace: %w", err)
	}

	var startedAt any // nil → SQL NULL

	if sess.RehydrationStartedAt != nil {
		startedAt = sess.RehydrationStartedAt.Unix()
	}

	res, err := s.db.ExecContext(ctx, `UPDATE chat_sessions SET
        title=?, project=?, status=?, last_active=?, container_id=?, workspace=?, model=?,
        rehydration_started_at=?
        WHERE id=?`,
		sess.Title, nullIf(sess.Project), string(sess.Status),
		sess.LastActive.Unix(), nullIf(sess.ContainerID), string(workspaceJSON),
		sess.Model, startedAt, sess.ID)
	if err != nil {
		return fmt.Errorf("chat: update session: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return chat.ErrSessionNotFound
	}

	return nil
}

// SetRehydrationActive flips the rehydration_active flag on a session row.
// When active is true, rehydration_started_at is set to startedAt (caller
// supplies the clock-sourced time); when false, it is set to NULL. Targeted
// update avoids scribbling the entire session, which the consumer path would
// otherwise have to do via UpdateSession.
func (s *Store) SetRehydrationActive(ctx context.Context, sessionID string, active bool, startedAt time.Time) error {
	flag := 0

	var startedAtVal any // nil → SQL NULL

	if active {
		flag = 1
		startedAtVal = startedAt.Unix()
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE chat_sessions SET rehydration_active = ?, rehydration_started_at = ? WHERE id = ?`,
		flag, startedAtVal, sessionID)
	if err != nil {
		return fmt.Errorf("chat: set rehydration_active: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return chat.ErrSessionNotFound
	}

	return nil
}

// UpdateSessionTitle writes only the title column. Used by the auto-title
// path in chat.Manager.AppendMessage so a concurrent OpenSession/MarkActive
// between the title-read and the title-write cannot have its
// ContainerID/Status/Workspace overwritten by a stale snapshot.
func (s *Store) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE chat_sessions SET title = ? WHERE id = ?`,
		title, sessionID)
	if err != nil {
		return fmt.Errorf("chat: update session title: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return chat.ErrSessionNotFound
	}

	return nil
}

// UpdateContextTokens stamps the latest Claude usage block onto the session
// row. Called from the runner-log consumer when a "usage" event arrives.
func (s *Store) UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE chat_sessions SET context_tokens = ?, context_tokens_updated_at = ? WHERE id = ?`,
		tokens, updatedAt.Unix(), sessionID)
	if err != nil {
		return fmt.Errorf("chat: update context_tokens: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return chat.ErrSessionNotFound
	}

	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("chat: delete session: begin tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO chat_cost_archive
			(id, project, model, last_active,
			 prompt_tokens, completion_tokens, cache_read_tokens,
			 cache_creation_tokens, estimated_cost_usd, deleted_at)
		SELECT id, project, model, last_active,
			prompt_tokens, completion_tokens, cache_read_tokens,
			cache_creation_tokens, estimated_cost_usd, ?
		FROM chat_sessions WHERE id = ?
		ON CONFLICT(id) DO NOTHING`,
		time.Now().UTC().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("chat: delete session: archive: %w", err)
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM chat_sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("chat: delete session: delete: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("chat: delete session: commit: %w", err)
	}

	return nil
}

func (s *Store) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	phase := 0
	if m.RehydrationPhase {
		phase = 1
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO chat_messages
        (session_id, seq, role, content, created_at, rehydration_phase, kind)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.SessionID, m.Seq, string(m.Role), m.Content, m.CreatedAt.Unix(), phase, m.Kind)
	if err != nil {
		return 0, fmt.Errorf("chat: append message: %w", err)
	}

	return m.Seq, nil
}

func (s *Store) MaxSeq(ctx context.Context, sessionID string) (int64, error) {
	var maxSeq sql.NullInt64

	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM chat_messages WHERE session_id = ?`,
		sessionID,
	).Scan(&maxSeq)
	if err != nil {
		return 0, fmt.Errorf("chat: max seq: %w", err)
	}

	return maxSeq.Int64, nil
}

func (s *Store) ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]chat.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, seq, role, content, created_at, rehydration_phase, kind
        FROM chat_messages
        WHERE session_id = ? AND seq > ?
        ORDER BY seq ASC LIMIT ?`, sessionID, sinceSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("chat: list messages: %w", err)
	}

	defer rows.Close()

	var out []chat.Message

	for rows.Next() {
		var (
			m         chat.Message
			createdAt int64
			role      string
			phase     int
		)

		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &role, &m.Content, &createdAt, &phase, &m.Kind); err != nil {
			return nil, err
		}

		m.Role = chat.Role(role)
		m.CreatedAt = time.Unix(createdAt, 0).UTC()
		m.RehydrationPhase = phase != 0
		out = append(out, m)
	}

	return out, rows.Err()
}

func (s *Store) ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]chat.Message, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, seq, role, content, created_at, rehydration_phase, kind
		FROM (
			SELECT id, session_id, seq, role, content, created_at, rehydration_phase, kind
			FROM chat_messages
			WHERE session_id = ?
			ORDER BY seq DESC
			LIMIT ?
		)
		ORDER BY seq ASC
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tail: %w", err)
	}

	defer rows.Close()

	var out []chat.Message

	for rows.Next() {
		var (
			m         chat.Message
			createdAt int64
			role      string
			phase     int
		)

		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &role, &m.Content, &createdAt, &phase, &m.Kind); err != nil {
			return nil, err
		}

		m.Role = chat.Role(role)
		m.CreatedAt = time.Unix(createdAt, 0).UTC()
		m.RehydrationPhase = phase != 0
		out = append(out, m)
	}

	return out, rows.Err()
}

// ClearTranscriptAtomic runs the two-step Clear Context transcript mutation
// inside a single BEGIN/COMMIT transaction:
//  1. UPDATE … SET rehydration_phase = 1 WHERE … AND rehydration_phase = 0
//  2. INSERT the divider message row
//
// A rollback is deferred so any partial failure leaves the transcript
// unchanged — the caller can retry the full ClearContext without worrying
// about half-marked rows.
func (s *Store) ClearTranscriptAtomic(ctx context.Context, sessionID string, divider chat.Message) (int64, chat.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, chat.Message{}, fmt.Errorf("chat: ClearTranscriptAtomic: begin tx: %w", err)
	}

	// Rollback is a no-op after a successful Commit.
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE chat_messages SET rehydration_phase = 1 WHERE session_id = ? AND rehydration_phase = 0`,
		sessionID)
	if err != nil {
		return 0, chat.Message{}, fmt.Errorf("chat: ClearTranscriptAtomic: mark phase: %w", err)
	}

	markedCount, _ := res.RowsAffected()

	phase := 0
	if divider.RehydrationPhase {
		phase = 1
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO chat_messages
        (session_id, seq, role, content, created_at, rehydration_phase, kind)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		divider.SessionID, divider.Seq, string(divider.Role), divider.Content,
		divider.CreatedAt.Unix(), phase, divider.Kind)
	if err != nil {
		return 0, chat.Message{}, fmt.Errorf("chat: ClearTranscriptAtomic: insert divider: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, chat.Message{}, fmt.Errorf("chat: ClearTranscriptAtomic: commit: %w", err)
	}

	return markedCount, divider, nil
}

// nullIf returns a sql.NullString that is NULL when s is empty.
func nullIf(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: s, Valid: true}
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(sc scanner) (chat.Session, error) {
	var (
		s                           chat.Session
		project, containerID, model sql.NullString
		workspaceJSON               sql.NullString
		createdAt, lastActive       int64
		status                      string
		contextTokens               int64
		contextTokensUpdatedAt      sql.NullInt64
		rehydrationActive           int
		rehydrationStartedAt        sql.NullInt64
		promptTokens                int64
		completionTokens            int64
		cacheReadTokens             int64
		cacheCreationTokens         int64
		estimatedCostUSD            float64
	)

	if err := sc.Scan(
		&s.ID, &s.Title, &project, &status, &createdAt, &lastActive, &s.CreatedBy,
		&containerID, &workspaceJSON,
		&model, &contextTokens, &contextTokensUpdatedAt, &rehydrationActive,
		&rehydrationStartedAt,
		&promptTokens, &completionTokens, &cacheReadTokens, &cacheCreationTokens,
		&estimatedCostUSD,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return chat.Session{}, chat.ErrSessionNotFound
		}

		return chat.Session{}, fmt.Errorf("chat: scan session: %w", err)
	}

	s.Project = project.String
	s.ContainerID = containerID.String
	s.Status = chat.Status(status)
	s.CreatedAt = time.Unix(createdAt, 0).UTC()
	s.LastActive = time.Unix(lastActive, 0).UTC()
	s.Model = model.String
	s.ContextTokens = contextTokens

	if contextTokensUpdatedAt.Valid {
		s.ContextTokensUpdatedAt = time.Unix(contextTokensUpdatedAt.Int64, 0).UTC()
	}

	s.RehydrationActive = rehydrationActive != 0

	if rehydrationStartedAt.Valid {
		t := time.Unix(rehydrationStartedAt.Int64, 0).UTC()
		s.RehydrationStartedAt = &t
	}

	s.PromptTokens = promptTokens
	s.CompletionTokens = completionTokens
	s.CacheReadTokens = cacheReadTokens
	s.CacheCreationTokens = cacheCreationTokens
	s.EstimatedCostUSD = estimatedCostUSD

	if workspaceJSON.Valid && workspaceJSON.String != "" && workspaceJSON.String != "null" {
		if err := json.Unmarshal([]byte(workspaceJSON.String), &s.Workspace); err != nil {
			return chat.Session{}, fmt.Errorf("chat: unmarshal workspace: %w", err)
		}
	}

	return s, nil
}

// AggregateCost returns cost aggregates over the 30-day window [since, until)
// and the equal-width prior window [since-30d, since). series30d is a
// length-30 daily slice; index 0 = since, index 29 = the day before until.
// Days with no sessions are filled with 0.0. Both live sessions and archived
// (deleted) sessions contribute via UNION ALL over chat_sessions and
// chat_cost_archive.
func (s *Store) AggregateCost(ctx context.Context, since, until time.Time) (last30d, prior30d float64, series30d []float64, err error) {
	const numDays = 30

	series30d = make([]float64, numDays)

	// last30d: sum of cost in [since, until) across live + archived sessions.
	row := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0.0) FROM (
		     SELECT estimated_cost_usd AS cost FROM chat_sessions     WHERE last_active >= ? AND last_active < ?
		     UNION ALL
		     SELECT estimated_cost_usd AS cost FROM chat_cost_archive WHERE last_active >= ? AND last_active < ?
		 )`,
		since.Unix(), until.Unix(), since.Unix(), until.Unix(),
	)
	if err = row.Scan(&last30d); err != nil {
		return 0, 0, nil, fmt.Errorf("chat: aggregate cost last30d: %w", err)
	}

	// prior30d: sum of cost in the equal-width window before since.
	priorStart := since.Add(-30 * 24 * time.Hour)

	row = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0.0) FROM (
		     SELECT estimated_cost_usd AS cost FROM chat_sessions     WHERE last_active >= ? AND last_active < ?
		     UNION ALL
		     SELECT estimated_cost_usd AS cost FROM chat_cost_archive WHERE last_active >= ? AND last_active < ?
		 )`,
		priorStart.Unix(), since.Unix(), priorStart.Unix(), since.Unix(),
	)
	if err = row.Scan(&prior30d); err != nil {
		return 0, 0, nil, fmt.Errorf("chat: aggregate cost prior30d: %w", err)
	}

	// series30d: per-day sums bucketed by date(last_active, 'unixepoch').
	// Returns only rows with data; the Go fill-loop zeroes the rest.
	rows, err := s.db.QueryContext(ctx,
		`SELECT date(last_active, 'unixepoch') AS day, SUM(estimated_cost_usd) AS day_cost
		 FROM (
		     SELECT last_active, estimated_cost_usd FROM chat_sessions     WHERE last_active >= ? AND last_active < ?
		     UNION ALL
		     SELECT last_active, estimated_cost_usd FROM chat_cost_archive WHERE last_active >= ? AND last_active < ?
		 )
		 GROUP BY day
		 ORDER BY day ASC`,
		since.Unix(), until.Unix(), since.Unix(), until.Unix(),
	)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("chat: aggregate cost series: %w", err)
	}
	defer rows.Close()

	// Compute once: midnight UTC of the start of the window.
	sinceDay := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)

	for rows.Next() {
		var (
			dayStr  string
			dayCost float64
		)

		if err = rows.Scan(&dayStr, &dayCost); err != nil {
			return 0, 0, nil, fmt.Errorf("chat: aggregate cost series scan: %w", err)
		}

		// Parse the day string (SQLite date() returns "YYYY-MM-DD").
		t, parseErr := time.Parse("2006-01-02", dayStr)
		if parseErr != nil {
			continue // skip unparseable rows rather than abort
		}

		// Compute the bucket index: days since since (truncated to midnight UTC).
		tDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		idx := int(tDay.Sub(sinceDay).Hours() / 24)

		if idx >= 0 && idx < numDays {
			series30d[idx] += dayCost
		}
	}

	if err = rows.Err(); err != nil {
		return 0, 0, nil, fmt.Errorf("chat: aggregate cost series rows: %w", err)
	}

	return last30d, prior30d, series30d, nil
}

// IncrementSessionCost atomically adds token deltas and cost to the session row
// using a single UPDATE … RETURNING statement. Race-free: the DB performs the
// arithmetic so concurrent increments cannot interleave. Returns ErrSessionNotFound
// when no matching row exists.
func (s *Store) IncrementSessionCost(
	ctx context.Context,
	sessionID string,
	dPrompt, dCompletion, dCacheRead, dCacheCreation int64,
	dCost float64,
	model string,
) (newPrompt, newCompletion, newCacheRead, newCacheCreation int64, newCost float64, err error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE chat_sessions SET
			prompt_tokens       = prompt_tokens       + ?,
			completion_tokens   = completion_tokens   + ?,
			cache_read_tokens   = cache_read_tokens   + ?,
			cache_creation_tokens = cache_creation_tokens + ?,
			estimated_cost_usd  = estimated_cost_usd  + ?,
			model               = COALESCE(NULLIF(?, ''), model)
		WHERE id = ?
		RETURNING prompt_tokens, completion_tokens, cache_read_tokens,
		          cache_creation_tokens, estimated_cost_usd`,
		dPrompt, dCompletion, dCacheRead, dCacheCreation, dCost, model, sessionID,
	)

	if err := row.Scan(&newPrompt, &newCompletion, &newCacheRead, &newCacheCreation, &newCost); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, 0, 0, chat.ErrSessionNotFound
		}

		return 0, 0, 0, 0, 0, fmt.Errorf("chat: increment session cost: %w", err)
	}

	return newPrompt, newCompletion, newCacheRead, newCacheCreation, newCost, nil
}
