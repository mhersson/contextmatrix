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

// Open opens (or creates) the SQLite database at path and applies the
// schema migrations. Parent directories are created as needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("chat: ensure db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
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
    container_id, workspace, model, context_tokens, context_tokens_updated_at, rehydration_active`

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

func (s *Store) UpdateSession(ctx context.Context, sess chat.Session) error {
	workspaceJSON, err := json.Marshal(sess.Workspace)
	if err != nil {
		return fmt.Errorf("chat: marshal workspace: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `UPDATE chat_sessions SET
        title=?, project=?, status=?, last_active=?, container_id=?, workspace=?, model=?
        WHERE id=?`,
		sess.Title, nullIf(sess.Project), string(sess.Status),
		sess.LastActive.Unix(), nullIf(sess.ContainerID), string(workspaceJSON),
		sess.Model, sess.ID)
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
// Targeted update avoids scribbling the entire session, which the consumer
// path would otherwise have to do via UpdateSession.
func (s *Store) SetRehydrationActive(ctx context.Context, sessionID string, active bool) error {
	flag := 0
	if active {
		flag = 1
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE chat_sessions SET rehydration_active = ? WHERE id = ?`,
		flag, sessionID)
	if err != nil {
		return fmt.Errorf("chat: set rehydration_active: %w", err)
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
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("chat: delete session: %w", err)
	}

	return nil
}

func (s *Store) AppendMessage(ctx context.Context, m chat.Message) (int64, error) {
	phase := 0
	if m.RehydrationPhase {
		phase = 1
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO chat_messages
        (session_id, seq, role, content, created_at, rehydration_phase)
        VALUES (?, ?, ?, ?, ?, ?)`,
		m.SessionID, m.Seq, string(m.Role), m.Content, m.CreatedAt.Unix(), phase)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, seq, role, content, created_at, rehydration_phase
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

		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &role, &m.Content, &createdAt, &phase); err != nil {
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
		SELECT id, session_id, seq, role, content, created_at, rehydration_phase
		FROM (
			SELECT id, session_id, seq, role, content, created_at, rehydration_phase
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

		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &role, &m.Content, &createdAt, &phase); err != nil {
			return nil, err
		}

		m.Role = chat.Role(role)
		m.CreatedAt = time.Unix(createdAt, 0).UTC()
		m.RehydrationPhase = phase != 0
		out = append(out, m)
	}

	return out, rows.Err()
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
	)

	if err := sc.Scan(
		&s.ID, &s.Title, &project, &status, &createdAt, &lastActive, &s.CreatedBy,
		&containerID, &workspaceJSON,
		&model, &contextTokens, &contextTokensUpdatedAt, &rehydrationActive,
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

	if workspaceJSON.Valid && workspaceJSON.String != "" && workspaceJSON.String != "null" {
		if err := json.Unmarshal([]byte(workspaceJSON.String), &s.Workspace); err != nil {
			return chat.Session{}, fmt.Errorf("chat: unmarshal workspace: %w", err)
		}
	}

	return s, nil
}
