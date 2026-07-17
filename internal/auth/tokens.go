package auth

import (
	"context"
	"errors"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
)

// OneTimeTokenTTL is the validity window for bootstrap, invite, and reset links.
const OneTimeTokenTTL = 48 * time.Hour

// Token-flow errors. The HTTP layer maps: invalid → 404, spent/expired → 410.
var (
	ErrTokenInvalid      = errors.New("auth: token invalid")
	ErrTokenSpent        = errors.New("auth: token already used")
	ErrTokenExpired      = errors.New("auth: token expired")
	ErrNotBootstrappable = errors.New("auth: users already exist")
)

// TokenInfo is what the redemption page needs to render the right form.
type TokenInfo struct {
	Purpose  authstore.TokenPurpose
	Username string
}

// IssueBootstrapToken mints a first-admin creation link token. Called by
// main.go on every zero-user startup; multiple outstanding bootstrap tokens
// are harmless because redemption re-checks that no users exist.
func (s *Service) IssueBootstrapToken(ctx context.Context) (string, error) {
	return s.issueToken(ctx, authstore.TokenPurposeBootstrap, nil)
}

// IssueInviteToken mints a set-first-password link for a freshly created
// user, invalidating any earlier unused invite for them.
func (s *Service) IssueInviteToken(ctx context.Context, userID int64) (string, error) {
	if _, err := s.store.InvalidateTokensForUser(ctx, userID, authstore.TokenPurposeInvite); err != nil {
		return "", err
	}

	return s.issueToken(ctx, authstore.TokenPurposeInvite, &userID)
}

// IssueResetToken mints a password-reset link, invalidating earlier unused
// resets for the user.
func (s *Service) IssueResetToken(ctx context.Context, userID int64) (string, error) {
	if _, err := s.store.InvalidateTokensForUser(ctx, userID, authstore.TokenPurposeReset); err != nil {
		return "", err
	}

	return s.issueToken(ctx, authstore.TokenPurposeReset, &userID)
}

func (s *Service) issueToken(ctx context.Context, purpose authstore.TokenPurpose, userID *int64) (string, error) {
	raw, hash, err := NewToken()
	if err != nil {
		return "", err
	}

	now := s.now()
	if err := s.store.CreateOneTimeToken(ctx, hash, purpose, userID, now, now.Add(OneTimeTokenTTL)); err != nil {
		return "", err
	}

	return raw, nil
}

// InspectToken reports a token's purpose (and target username, when it has
// one) WITHOUT consuming it - the GET endpoint behind the redemption page.
func (s *Service) InspectToken(ctx context.Context, rawToken string) (*TokenInfo, error) {
	tok, err := s.lookupLiveToken(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	info := &TokenInfo{Purpose: tok.Purpose}

	if tok.UserID != nil {
		user, err := s.store.UserByID(ctx, *tok.UserID)
		if err != nil {
			return nil, ErrTokenInvalid
		}

		info.Username = user.Username
	}

	return info, nil
}

// RedeemBootstrap consumes a bootstrap token and creates the FIRST account,
// as admin, logging it in. Validation (password length, username shape,
// zero-users) runs before the token is consumed, so a rejected form does not
// burn the link.
func (s *Service) RedeemBootstrap(ctx context.Context, rawToken, username, displayName, password string) (*authstore.User, string, error) {
	if len(password) < MinPasswordLength {
		return nil, "", ErrPasswordTooShort
	}

	tok, err := s.lookupLiveToken(ctx, rawToken)
	if err != nil {
		return nil, "", err
	}

	if tok.Purpose != authstore.TokenPurposeBootstrap {
		return nil, "", ErrTokenInvalid
	}

	normalized := authstore.NormalizeUsername(username)
	if !authstore.ValidUsername(normalized) {
		return nil, "", authstore.ErrInvalidUsername
	}

	if _, err := s.store.ConsumeOneTimeToken(ctx, HashToken(rawToken), s.now()); err != nil {
		return nil, "", mapStoreTokenErr(err)
	}

	now := s.now()

	user, err := s.store.CreateFirstAdmin(ctx, normalized, displayName, now)
	if err != nil {
		if errors.Is(err, authstore.ErrNotBootstrappable) {
			return nil, "", ErrNotBootstrappable
		}

		return nil, "", err
	}

	return s.setPasswordAndLogin(ctx, user, password)
}

// RedeemInviteOrReset consumes an invite or reset token, sets the user's
// password, and logs them in. Reset additionally kills every pre-existing
// session for the account.
func (s *Service) RedeemInviteOrReset(ctx context.Context, rawToken, password string) (*authstore.User, string, error) {
	if len(password) < MinPasswordLength {
		return nil, "", ErrPasswordTooShort
	}

	tok, err := s.lookupLiveToken(ctx, rawToken)
	if err != nil {
		return nil, "", err
	}

	if tok.Purpose != authstore.TokenPurposeInvite && tok.Purpose != authstore.TokenPurposeReset {
		return nil, "", ErrTokenInvalid
	}

	if tok.UserID == nil {
		return nil, "", ErrTokenInvalid
	}

	user, err := s.store.UserByID(ctx, *tok.UserID)
	if err != nil || user.Disabled {
		return nil, "", ErrTokenInvalid
	}

	if _, err := s.store.ConsumeOneTimeToken(ctx, HashToken(rawToken), s.now()); err != nil {
		return nil, "", mapStoreTokenErr(err)
	}

	if tok.Purpose == authstore.TokenPurposeReset {
		if _, err := s.store.DeleteSessionsForUser(ctx, user.ID); err != nil {
			return nil, "", err
		}
	}

	return s.setPasswordAndLogin(ctx, user, password)
}

// ChangePassword is the logged-in self-service path: requires the current
// password, enforces the length rule, and kills the user's other sessions
// (keeping the one identified by keepRawToken).
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, newPassword, keepRawToken string) error {
	if len(newPassword) < MinPasswordLength {
		return ErrPasswordTooShort
	}

	user, err := s.store.UserByID(ctx, userID)
	if err != nil || user.PasswordHash == nil {
		return ErrInvalidCredentials
	}

	ok, err := VerifyPassword(current, *user.PasswordHash)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	now := s.now()
	if err := s.store.SetPasswordHash(ctx, userID, hash, now); err != nil {
		return err
	}

	if _, err := s.store.DeleteSessionsForUserExcept(ctx, userID, HashToken(keepRawToken)); err != nil {
		return err
	}

	return nil
}

// lookupLiveToken fetches a token and maps its state to the token errors.
func (s *Service) lookupLiveToken(ctx context.Context, rawToken string) (*authstore.OneTimeToken, error) {
	tok, err := s.store.OneTimeTokenByHash(ctx, HashToken(rawToken))
	if err != nil {
		return nil, ErrTokenInvalid
	}

	if tok.UsedAt != nil {
		return nil, ErrTokenSpent
	}

	if !tok.ExpiresAt.After(s.now()) {
		return nil, ErrTokenExpired
	}

	return tok, nil
}

// mapStoreTokenErr converts authstore consume errors (a concurrent redeem
// may have won between lookup and consume) to the auth-level sentinels.
func mapStoreTokenErr(err error) error {
	switch {
	case errors.Is(err, authstore.ErrTokenSpent):
		return ErrTokenSpent
	case errors.Is(err, authstore.ErrTokenExpired):
		return ErrTokenExpired
	default:
		return ErrTokenInvalid
	}
}

// setPasswordAndLogin finishes every redemption: hash + store the password,
// then mint a session (auto-login).
func (s *Service) setPasswordAndLogin(ctx context.Context, user *authstore.User, password string) (*authstore.User, string, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, "", err
	}

	now := s.now()
	if err := s.store.SetPasswordHash(ctx, user.ID, hash, now); err != nil {
		return nil, "", err
	}

	raw, tokenHash, err := NewToken()
	if err != nil {
		return nil, "", err
	}

	if err := s.store.CreateSession(ctx, tokenHash, user.ID, now, now.Add(s.idleTTL)); err != nil {
		return nil, "", err
	}

	_ = s.store.TouchLastLogin(ctx, user.ID, now)

	return user, raw, nil
}
