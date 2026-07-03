package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
)

// MinPasswordLength is enforced wherever a password is set (redemption and
// self-service change) — never on login, so raising it cannot lock users out.
const MinPasswordLength = 10

// renewAfter is how stale a session's last_seen may get before a validation
// performs a sliding renewal. Keeps the hot path from writing on every request.
const renewAfter = 5 * time.Minute

// Uniform auth errors. ErrInvalidCredentials deliberately covers unknown
// user, disabled user, unset password, and wrong password — no oracle.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrSessionInvalid     = errors.New("auth: session invalid")
	ErrPasswordTooShort   = fmt.Errorf("auth: password must be at least %d characters", MinPasswordLength)
)

// RateLimitedError reports a login attempt rejected by the rate limiter.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("auth: rate limited, retry in %s", e.RetryAfter)
}

// SessionResult is a validated session. Renewed tells the HTTP layer to
// refresh the cookie's max-age.
type SessionResult struct {
	User    *authstore.User
	Renewed bool
}

// Service orchestrates the authstore for login, sessions, and one-time-token
// flows. It is HTTP-free — the api package maps it onto endpoints.
type Service struct {
	store   *authstore.Store
	idleTTL time.Duration
	limiter *Limiter
	now     func() time.Time

	dummyOnce sync.Once
	dummyHash string
}

// NewService wires a Service on the real clock.
func NewService(store *authstore.Store, idleTTL time.Duration) *Service {
	return &Service{
		store:   store,
		idleTTL: idleTTL,
		limiter: NewLimiter(),
		now:     time.Now,
	}
}

// IdleTTL exposes the sliding session lifetime for cookie max-age.
func (s *Service) IdleTTL() time.Duration { return s.idleTTL }

// Login verifies credentials and creates a session. It returns the user and
// the RAW session token (the caller sets it as a cookie; only its hash is
// stored). Failures are uniform ErrInvalidCredentials; repeated failures per
// account+IP earn a RateLimitedError.
func (s *Service) Login(ctx context.Context, username, password, clientIP string) (*authstore.User, string, error) {
	key := authstore.NormalizeUsername(username) + "|" + clientIP

	if retry, ok := s.limiter.Allow(key); !ok {
		return nil, "", &RateLimitedError{RetryAfter: retry}
	}

	fail := func() (*authstore.User, string, error) {
		s.limiter.Failure(key)

		return nil, "", ErrInvalidCredentials
	}

	user, err := s.store.UserByUsername(ctx, username)
	if err != nil || user.Disabled || user.PasswordHash == nil {
		// Burn comparable time so the response does not reveal whether the
		// username exists (argon2 dominates the timing).
		_, _ = VerifyPassword(password, s.timingDummyHash())

		return fail()
	}

	ok, err := VerifyPassword(password, *user.PasswordHash)
	if err != nil || !ok {
		return fail()
	}

	now := s.now()

	if NeedsRehash(*user.PasswordHash) {
		if newHash, hashErr := HashPassword(password); hashErr == nil {
			_ = s.store.SetPasswordHash(ctx, user.ID, newHash, now)
		}
	}

	raw, hash, err := NewToken()
	if err != nil {
		return nil, "", err
	}

	if err := s.store.CreateSession(ctx, hash, user.ID, now, now.Add(s.idleTTL)); err != nil {
		return nil, "", err
	}

	_ = s.store.TouchLastLogin(ctx, user.ID, now)
	s.limiter.Reset(key)

	return user, raw, nil
}

// Logout deletes the session behind a raw token. Idempotent — logging out an
// already-dead session succeeds.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.store.DeleteSession(ctx, HashToken(rawToken))
}

// ValidateSession resolves a raw cookie token to a live user. Expired
// (expires_at <= now) and orphaned sessions are deleted on sight; disabled
// users are invalid regardless of session state. Sessions idle past
// renewAfter get a sliding renewal to now+idleTTL.
func (s *Service) ValidateSession(ctx context.Context, rawToken string) (*SessionResult, error) {
	hash := HashToken(rawToken)

	sess, err := s.store.SessionByTokenHash(ctx, hash)
	if err != nil {
		return nil, ErrSessionInvalid
	}

	now := s.now()
	if !sess.ExpiresAt.After(now) {
		_ = s.store.DeleteSession(ctx, hash)

		return nil, ErrSessionInvalid
	}

	user, err := s.store.UserByID(ctx, sess.UserID)
	if err != nil || user.Disabled {
		_ = s.store.DeleteSession(ctx, hash)

		return nil, ErrSessionInvalid
	}

	renewed := false

	if now.Sub(sess.LastSeenAt) > renewAfter {
		if err := s.store.RenewSession(ctx, hash, now, now.Add(s.idleTTL)); err == nil {
			renewed = true
		}
	}

	return &SessionResult{User: user, Renewed: renewed}, nil
}

// timingDummyHash lazily produces a real argon2id hash used only to equalize
// login timing on the unknown-user path. sync.Once instead of init() per the
// no-init convention.
func (s *Service) timingDummyHash() string {
	s.dummyOnce.Do(func() {
		h, err := HashPassword("timing-equalizer-not-a-real-password")
		if err != nil {
			h = ""
		}

		s.dummyHash = h
	})

	return s.dummyHash
}

// ClientIP extracts the host part of an addr like "1.2.3.4:5678" for limiter
// keys. Falls back to the raw string when it does not split.
func ClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}
