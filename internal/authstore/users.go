package authstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// User is a human account. PasswordHash is nil until the user redeems their
// invite link — and stays nullable as the seam for future OAuth-only users.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash *string
	IsAdmin      bool
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// usernameRe is the locked username rule: 1-32 chars of a-z 0-9 . _ -, no
// leading or trailing punctuation. Usernames feed human:<username> identity
// strings and boards-repo commit authors, so the charset stays tight.
var usernameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,30}[a-z0-9])?$`)

// NormalizeUsername lowercases and trims a username without validating it.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ValidUsername reports whether an already-normalized username matches the
// locked username rule.
func ValidUsername(username string) bool {
	return usernameRe.MatchString(username)
}

// CreateUser inserts a new account. The username is normalized (lowercased,
// trimmed) and validated; duplicates return ErrDuplicate.
func (s *Store) CreateUser(ctx context.Context, username, displayName string, isAdmin bool, now time.Time) (*User, error) {
	username = NormalizeUsername(username)
	if !usernameRe.MatchString(username) {
		return nil, ErrInvalidUsername
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, display_name, is_admin, disabled, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)`,
		username, displayName, boolToInt(isAdmin), toUnix(now), toUnix(now),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}

		return nil, fmt.Errorf("authstore: create user: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("authstore: create user id: %w", err)
	}

	return s.UserByID(ctx, id)
}

// UserByID fetches one user by primary key.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelect+` WHERE id = ?`, id))
}

// UserByUsername fetches one user by (normalized) username.
func (s *Store) UserByUsername(ctx context.Context, username string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelect+` WHERE username = ?`, NormalizeUsername(username)))
}

// ListUsers returns all users ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, userSelect+` ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("authstore: list users: %w", err)
	}
	defer rows.Close()

	var users []*User

	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}

		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authstore: list users rows: %w", err)
	}

	return users, nil
}

// SetPasswordHash stores a new password hash (argon2id PHC string).
func (s *Store) SetPasswordHash(ctx context.Context, id int64, hash string, now time.Time) error {
	return s.updateUser(ctx, id, `password_hash = ?`, hash, now)
}

// SetDisplayName updates the display name.
func (s *Store) SetDisplayName(ctx context.Context, id int64, displayName string, now time.Time) error {
	return s.updateUser(ctx, id, `display_name = ?`, displayName, now)
}

// SetAdmin toggles the admin flag. The last-admin guard lives in the API
// layer via CountActiveAdmins — the store stays policy-free.
func (s *Store) SetAdmin(ctx context.Context, id int64, isAdmin bool, now time.Time) error {
	return s.updateUser(ctx, id, `is_admin = ?`, boolToInt(isAdmin), now)
}

// SetDisabled toggles the disabled flag. Callers must also delete the user's
// sessions (see DeleteSessionsForUser) — the store does not couple the two.
func (s *Store) SetDisabled(ctx context.Context, id int64, disabled bool, now time.Time) error {
	return s.updateUser(ctx, id, `disabled = ?`, boolToInt(disabled), now)
}

// TouchLastLogin stamps a successful login.
func (s *Store) TouchLastLogin(ctx context.Context, id int64, now time.Time) error {
	return s.updateUser(ctx, id, `last_login_at = ?`, toUnix(now), now)
}

// CountActiveAdmins counts admins that are not disabled — the input to the
// last-admin guard.
func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled = 0`,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("authstore: count admins: %w", err)
	}

	return n, nil
}

const userSelect = `SELECT id, username, display_name, password_hash, is_admin, disabled, created_at, updated_at, last_login_at FROM users`

// rowScanner covers both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanUser(row rowScanner) (*User, error) {
	var (
		u         User
		pwHash    sql.NullString
		isAdmin   int
		disabled  int
		createdAt int64
		updatedAt int64
		lastLogin sql.NullInt64
	)

	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &pwHash, &isAdmin, &disabled, &createdAt, &updatedAt, &lastLogin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("authstore: scan user: %w", err)
	}

	if pwHash.Valid {
		u.PasswordHash = &pwHash.String
	}

	u.IsAdmin = isAdmin == 1
	u.Disabled = disabled == 1
	u.CreatedAt = fromUnix(createdAt)
	u.UpdatedAt = fromUnix(updatedAt)
	u.LastLoginAt = fromNullUnix(lastLogin)

	return &u, nil
}

// updateUser applies one SET clause plus the updated_at bump, mapping
// zero-rows-affected to ErrNotFound.
func (s *Store) updateUser(ctx context.Context, id int64, setClause string, value any, now time.Time) error {
	// nolint:gosec // setClause is an internal constant built by callers above, not user input
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET `+setClause+`, updated_at = ? WHERE id = ?`,
		value, toUnix(now), id,
	)
	if err != nil {
		return fmt.Errorf("authstore: update user: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("authstore: update user rows: %w", err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// isUniqueViolation detects SQLite UNIQUE constraint failures. The modernc
// driver does not export a stable typed error for this, so the string match
// is the accepted idiom.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
