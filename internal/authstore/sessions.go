package authstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is one logged-in browser. Only the SHA-256 of the cookie token is
// stored - a leaked database yields no usable sessions.
type Session struct {
	TokenHash  string
	UserID     int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// CreateSession inserts a session row.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, now, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, toUnix(now), toUnix(expiresAt), toUnix(now),
	)
	if err != nil {
		return fmt.Errorf("authstore: create session: %w", err)
	}

	return nil
}

// SessionByTokenHash fetches a session row. Expired rows are still returned -
// expiry policy (reject + renew-or-delete) is the middleware's decision.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	var (
		sess                           Session
		createdAt, expiresAt, lastSeen int64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at, last_seen_at FROM sessions WHERE token_hash = ?`,
		tokenHash,
	).Scan(&sess.TokenHash, &sess.UserID, &createdAt, &expiresAt, &lastSeen)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("authstore: get session: %w", err)
	}

	sess.CreatedAt = fromUnix(createdAt)
	sess.ExpiresAt = fromUnix(expiresAt)
	sess.LastSeenAt = fromUnix(lastSeen)

	return &sess, nil
}

// RenewSession implements sliding expiry: bumps expires_at and last_seen_at.
func (s *Store) RenewSession(ctx context.Context, tokenHash string, now, expiresAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET expires_at = ?, last_seen_at = ? WHERE token_hash = ?`,
		toUnix(expiresAt), toUnix(now), tokenHash,
	)
	if err != nil {
		return fmt.Errorf("authstore: renew session: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("authstore: renew session rows: %w", err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteSession removes one session. Idempotent - deleting a missing session
// is not an error (logout must never fail).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("authstore: delete session: %w", err)
	}

	return nil
}

// DeleteSessionsForUser removes all of a user's sessions (disable, logout-everywhere).
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("authstore: delete user sessions: %w", err)
	}

	return res.RowsAffected()
}

// DeleteSessionsForUserExcept removes all of a user's sessions except one -
// a password change keeps the session that performed it.
func (s *Store) DeleteSessionsForUserExcept(ctx context.Context, userID int64, keepTokenHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, keepTokenHash,
	)
	if err != nil {
		return 0, fmt.Errorf("authstore: delete other sessions: %w", err)
	}

	return res.RowsAffected()
}

// DeleteExpiredSessions sweeps sessions whose expiry has passed.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, toUnix(now))
	if err != nil {
		return 0, fmt.Errorf("authstore: delete expired sessions: %w", err)
	}

	return res.RowsAffected()
}
