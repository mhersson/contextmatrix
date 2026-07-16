package auth

import (
	"context"
	"errors"

	"github.com/mhersson/contextmatrix/internal/authstore"
)

// ErrLastAdmin guards the invariant that at least one active admin exists —
// demoting or disabling the last one would lock everyone out of user
// management (the reset-admin CLI only resets a password; it cannot restore
// the admin role).
var ErrLastAdmin = errors.New("auth: cannot remove the last admin")

// ListUsers returns all accounts ordered by username.
func (s *Service) ListUsers(ctx context.Context) ([]*authstore.User, error) {
	return s.store.ListUsers(ctx)
}

// CreateUserWithInvite creates an account (no password — none exists anywhere)
// and returns the RAW invite token for the admin to hand over out-of-band.
func (s *Service) CreateUserWithInvite(ctx context.Context, username, displayName string, isAdmin bool) (*authstore.User, string, error) {
	user, err := s.store.CreateUser(ctx, username, displayName, isAdmin, s.now())
	if err != nil {
		return nil, "", err
	}

	raw, err := s.IssueInviteToken(ctx, user.ID)
	if err != nil {
		return nil, "", err
	}

	return user, raw, nil
}

func (s *Service) SetUserDisplayName(ctx context.Context, username, displayName string) error {
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return err
	}

	return s.store.SetDisplayName(ctx, user.ID, displayName, s.now())
}

// SetUserAdmin toggles the admin flag, refusing to demote the last active
// admin. The dangerous direction (demoting an active admin) routes through
// the guarded store update so the check and the write are atomic — closing
// the race where two concurrent demotes both pass a prior count check.
func (s *Service) SetUserAdmin(ctx context.Context, username string, isAdmin bool) error {
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return err
	}

	if !isAdmin && user.IsAdmin && !user.Disabled {
		if err := s.store.SetAdminGuarded(ctx, user.ID, s.now()); err != nil {
			if errors.Is(err, authstore.ErrLastAdminStore) {
				return ErrLastAdmin
			}

			return err
		}

		return nil
	}

	return s.store.SetAdmin(ctx, user.ID, isAdmin, s.now())
}

// SetUserDisabled toggles the disabled flag. Disabling deletes the user's
// sessions immediately (they are logged out mid-flight) and refuses to
// disable the last active admin. Re-enabling never resurrects sessions. The
// dangerous direction (disabling an active admin) routes through the guarded
// store update — see SetUserAdmin.
func (s *Service) SetUserDisabled(ctx context.Context, username string, disabled bool) error {
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return err
	}

	if disabled && user.IsAdmin && !user.Disabled {
		if err := s.store.SetDisabledGuarded(ctx, user.ID, s.now()); err != nil {
			if errors.Is(err, authstore.ErrLastAdminStore) {
				return ErrLastAdmin
			}

			return err
		}
	} else if err := s.store.SetDisabled(ctx, user.ID, disabled, s.now()); err != nil {
		return err
	}

	if disabled {
		if _, err := s.store.DeleteSessionsForUser(ctx, user.ID); err != nil {
			return err
		}
	}

	return nil
}

// RegenerateLink mints a fresh one-time link for a user: an invite when they
// have never set a password, a reset otherwise. Prior unused links of that
// purpose are invalidated by the issue call.
func (s *Service) RegenerateLink(ctx context.Context, username string) (string, authstore.TokenPurpose, error) {
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return "", "", err
	}

	if user.PasswordHash == nil {
		raw, err := s.IssueInviteToken(ctx, user.ID)

		return raw, authstore.TokenPurposeInvite, err
	}

	raw, err := s.IssueResetToken(ctx, user.ID)

	return raw, authstore.TokenPurposeReset, err
}

// UserByUsername exposes a read for the API layer's pre-flight checks.
func (s *Service) UserByUsername(ctx context.Context, username string) (*authstore.User, error) {
	return s.store.UserByUsername(ctx, username)
}

// CheckNotLastAdmin reports ErrLastAdmin when only one active admin exists.
// Advisory pre-flight — the guarded store updates remain the real barrier.
func (s *Service) CheckNotLastAdmin(ctx context.Context) error {
	n, err := s.store.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}

	if n <= 1 {
		return ErrLastAdmin
	}

	return nil
}
