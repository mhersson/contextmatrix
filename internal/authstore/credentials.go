package authstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CredentialKind distinguishes PAT and GitHub App pool entries.
type CredentialKind string

// The two credential kinds in the instance pool.
const (
	CredentialKindPAT CredentialKind = "pat"
	CredentialKindApp CredentialKind = "app"
)

// Credential is one instance-pool entry. Name is the immutable key that
// .board.yaml bindings reference. EncryptedSecret is opaque here - the store
// never sees plaintext secrets (encryption lives in internal/auth).
type Credential struct {
	Name            string
	Kind            CredentialKind
	Host            string
	APIBaseURL      string
	AppID           int64
	InstallationID  int64
	EncryptedSecret []byte
	CreatedBy       string
	Disabled        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      *time.Time
}

// CreateCredential inserts a pool entry. Name collisions return ErrDuplicate.
func (s *Store) CreateCredential(ctx context.Context, c *Credential, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO credentials (name, kind, host, api_base_url, app_id, installation_id, encrypted_secret, created_by, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, string(c.Kind), c.Host, c.APIBaseURL, c.AppID, c.InstallationID,
		c.EncryptedSecret, c.CreatedBy, boolToInt(c.Disabled), toUnix(now), toUnix(now),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}

		return fmt.Errorf("authstore: create credential: %w", err)
	}

	return nil
}

const credentialSelect = `SELECT name, kind, host, api_base_url, app_id, installation_id, encrypted_secret, created_by, disabled, created_at, updated_at, last_used_at FROM credentials` //nolint:gosec

// CredentialByName fetches one pool entry.
func (s *Store) CredentialByName(ctx context.Context, name string) (*Credential, error) {
	return scanCredential(s.db.QueryRowContext(ctx, credentialSelect+` WHERE name = ?`, name))
}

// ListCredentials returns all pool entries ordered by name. EncryptedSecret
// is included - the API layer is responsible for never serializing it.
func (s *Store) ListCredentials(ctx context.Context) ([]*Credential, error) {
	rows, err := s.db.QueryContext(ctx, credentialSelect+` ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("authstore: list credentials: %w", err)
	}
	defer rows.Close()

	var creds []*Credential

	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}

		creds = append(creds, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authstore: list credentials rows: %w", err)
	}

	return creds, nil
}

// UpdateCredentialSecret rotates the encrypted secret in place - bindings
// reference the name, so nothing else moves.
func (s *Store) UpdateCredentialSecret(ctx context.Context, name string, encryptedSecret []byte, now time.Time) error {
	return s.updateCredential(ctx, name,
		`encrypted_secret = ?, updated_at = ?`, encryptedSecret, toUnix(now))
}

// UpdateCredentialMetadata edits the non-secret fields. Name is immutable.
func (s *Store) UpdateCredentialMetadata(ctx context.Context, name, host, apiBaseURL string, appID, installationID int64, now time.Time) error {
	return s.updateCredential(ctx, name,
		`host = ?, api_base_url = ?, app_id = ?, installation_id = ?, updated_at = ?`,
		host, apiBaseURL, appID, installationID, toUnix(now))
}

// SetCredentialDisabled toggles the disabled flag - the softer alternative
// to deletion.
func (s *Store) SetCredentialDisabled(ctx context.Context, name string, disabled bool, now time.Time) error {
	return s.updateCredential(ctx, name, `disabled = ?, updated_at = ?`, boolToInt(disabled), toUnix(now))
}

// DeleteCredential removes a pool entry. The "still bound to projects" guard
// is API-layer policy - the store has no view of .board.yaml.
func (s *Store) DeleteCredential(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("authstore: delete credential: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("authstore: delete credential rows: %w", err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

// RotateCredentialSecrets re-encrypts every pool secret inside a single
// transaction. reencrypt receives each stored ciphertext blob and returns its
// replacement; the first error it returns - or any failed write - rolls back
// the whole batch, so the pool is never left half-rotated. It returns the
// number of entries rewritten. updated_at is deliberately not bumped: rotation
// re-wraps the same secret under a new key, it is not a metadata edit.
//
// The store stays crypto-agnostic - the decrypt-with-old/encrypt-with-new logic
// lives in the reencrypt closure (internal/auth). All rows are read into memory
// before any UPDATE runs so the single-connection transaction never interleaves
// an open query with a write.
func (s *Store) RotateCredentialSecrets(ctx context.Context, reencrypt func(oldSecret []byte) ([]byte, error)) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("authstore: begin rotate: %w", err)
	}

	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	rows, err := tx.QueryContext(ctx, `SELECT name, encrypted_secret FROM credentials ORDER BY name`)
	if err != nil {
		return 0, fmt.Errorf("authstore: rotate select: %w", err)
	}

	type entry struct {
		name string
		blob []byte
	}

	var entries []entry

	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.name, &e.blob); err != nil {
			_ = rows.Close()

			return 0, fmt.Errorf("authstore: rotate scan: %w", err)
		}

		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		_ = rows.Close()

		return 0, fmt.Errorf("authstore: rotate rows: %w", err)
	}

	_ = rows.Close()

	for _, e := range entries {
		newBlob, err := reencrypt(e.blob)
		if err != nil {
			return 0, fmt.Errorf("authstore: rotate re-encrypt %q: %w", e.name, err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE credentials SET encrypted_secret = ? WHERE name = ?`, newBlob, e.name); err != nil {
			return 0, fmt.Errorf("authstore: rotate update %q: %w", e.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("authstore: rotate commit: %w", err)
	}

	return len(entries), nil
}

func scanCredential(row rowScanner) (*Credential, error) {
	var (
		c                    Credential
		kind                 string
		disabled             int
		createdAt, updatedAt int64
		lastUsed             sql.NullInt64
	)

	err := row.Scan(&c.Name, &kind, &c.Host, &c.APIBaseURL, &c.AppID, &c.InstallationID,
		&c.EncryptedSecret, &c.CreatedBy, &disabled, &createdAt, &updatedAt, &lastUsed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("authstore: scan credential: %w", err)
	}

	c.Kind = CredentialKind(kind)
	c.Disabled = disabled == 1
	c.CreatedAt = fromUnix(createdAt)
	c.UpdatedAt = fromUnix(updatedAt)
	c.LastUsedAt = fromNullUnix(lastUsed)

	return &c, nil
}

// updateCredential applies SET clauses (which must include updated_at),
// mapping zero-rows-affected to ErrNotFound.
func (s *Store) updateCredential(ctx context.Context, name, setClauses string, values ...any) error {
	var args []any

	args = append(args, values...)
	args = append(args, name)

	res, err := s.db.ExecContext(ctx, `UPDATE credentials SET `+setClauses+` WHERE name = ?`, args...) //nolint:gosec
	if err != nil {
		return fmt.Errorf("authstore: update credential: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("authstore: update credential rows: %w", err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}
