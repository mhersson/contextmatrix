package authstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TokenPurpose is what a one-time token is redeemed for.
type TokenPurpose string

// The three one-time-token purposes: first-admin bootstrap, user invites,
// and password resets. One table and one mechanism power all three flows.
const (
	TokenPurposeBootstrap TokenPurpose = "bootstrap"
	TokenPurposeInvite    TokenPurpose = "invite"
	TokenPurposeReset     TokenPurpose = "reset"
)

// OneTimeToken is a single-use, expiring link token. UserID is nil for
// bootstrap tokens — the account does not exist until redemption.
type OneTimeToken struct {
	TokenHash string
	Purpose   TokenPurpose
	UserID    *int64
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// CreateOneTimeToken inserts a token row.
func (s *Store) CreateOneTimeToken(ctx context.Context, tokenHash string, purpose TokenPurpose, userID *int64, now, expiresAt time.Time) error {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO one_time_tokens (token_hash, purpose, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		tokenHash, string(purpose), uid, toUnix(now), toUnix(expiresAt),
	)
	if err != nil {
		return fmt.Errorf("authstore: create one-time token: %w", err)
	}

	return nil
}

// OneTimeTokenByHash fetches a token row without consuming it — the inspect
// endpoint uses this to render the right redemption form.
func (s *Store) OneTimeTokenByHash(ctx context.Context, tokenHash string) (*OneTimeToken, error) {
	var (
		tok                  OneTimeToken
		purpose              string
		uid                  sql.NullInt64
		createdAt, expiresAt int64
		usedAt               sql.NullInt64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, purpose, user_id, created_at, expires_at, used_at FROM one_time_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&tok.TokenHash, &purpose, &uid, &createdAt, &expiresAt, &usedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("authstore: get one-time token: %w", err)
	}

	tok.Purpose = TokenPurpose(purpose)
	if uid.Valid {
		tok.UserID = &uid.Int64
	}

	tok.CreatedAt = fromUnix(createdAt)
	tok.ExpiresAt = fromUnix(expiresAt)
	tok.UsedAt = fromNullUnix(usedAt)

	return &tok, nil
}

// ConsumeOneTimeToken atomically redeems a token: the single guarded UPDATE
// means exactly one concurrent redemption can win. Returns ErrNotFound for
// unknown tokens, ErrTokenSpent for already-used, ErrTokenExpired for
// expired-but-unused.
func (s *Store) ConsumeOneTimeToken(ctx context.Context, tokenHash string, now time.Time) (*OneTimeToken, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE one_time_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		toUnix(now), tokenHash, toUnix(now),
	)
	if err != nil {
		return nil, fmt.Errorf("authstore: consume one-time token: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("authstore: consume one-time token rows: %w", err)
	}

	if n == 0 {
		// Distinguish why the guarded update missed.
		tok, err := s.OneTimeTokenByHash(ctx, tokenHash)
		if err != nil {
			return nil, err // ErrNotFound or a real error
		}

		if tok.UsedAt != nil {
			return nil, ErrTokenSpent
		}

		return nil, ErrTokenExpired
	}

	return s.OneTimeTokenByHash(ctx, tokenHash)
}

// InvalidateTokensForUser deletes a user's unused tokens of one purpose —
// regenerating an invite or reset link kills the previous one.
func (s *Store) InvalidateTokensForUser(ctx context.Context, userID int64, purpose TokenPurpose) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM one_time_tokens WHERE user_id = ? AND purpose = ? AND used_at IS NULL`,
		userID, string(purpose),
	)
	if err != nil {
		return 0, fmt.Errorf("authstore: invalidate tokens: %w", err)
	}

	return res.RowsAffected()
}
