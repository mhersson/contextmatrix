package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

var svcNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

// newTestService returns a Service on a fresh store with a settable clock.
func newTestService(t *testing.T) (*Service, *authstore.Store, *time.Time) {
	t.Helper()

	store, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	clock := svcNow
	svc := NewService(store, 720*time.Hour)
	svc.now = func() time.Time { return clock }
	svc.limiter.now = svc.now

	return svc, store, &clock
}

// seedUser creates a user with a set password and returns it.
func seedUser(t *testing.T, svc *Service, store *authstore.Store, username, password string, admin bool) *authstore.User {
	t.Helper()

	u, err := store.CreateUser(context.Background(), username, "", admin, svcNow)
	require.NoError(t, err)

	hash, err := HashPassword(password)
	require.NoError(t, err)
	require.NoError(t, store.SetPasswordHash(context.Background(), u.ID, hash, svcNow))

	got, err := store.UserByID(context.Background(), u.ID)
	require.NoError(t, err)

	return got
}

func TestLogin_SuccessCreatesSession(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "correct horse battery", true)

	user, raw, err := svc.Login(ctx, "Alice", "correct horse battery", "1.2.3.4")
	require.NoError(t, err)
	assert.Equal(t, "alice", user.Username)
	assert.NotEmpty(t, raw)

	res, err := svc.ValidateSession(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, "alice", res.User.Username)

	got, err := store.UserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.LastLoginAt, "login stamps last_login_at")
}

func TestLogin_UniformFailures(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	alice := seedUser(t, svc, store, "alice", "correct horse battery", false)

	nopw, err := store.CreateUser(ctx, "invited", "", false, svcNow)
	require.NoError(t, err)

	_ = nopw

	require.NoError(t, store.SetDisabled(ctx, alice.ID, true, svcNow))

	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "unknown user", username: "ghost", password: "whatever12"},
		{name: "wrong password", username: "alice", password: "wrong password"},
		{name: "disabled user right password", username: "alice", password: "correct horse battery"},
		{name: "invite not yet redeemed", username: "invited", password: "whatever12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.Login(ctx, tt.username, tt.password, "1.2.3.4")
			require.ErrorIs(t, err, ErrInvalidCredentials)
		})
	}
}

func TestLogin_RateLimited(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "correct horse battery", false)

	for range 3 {
		_, _, err := svc.Login(ctx, "alice", "wrong", "1.2.3.4")
		require.ErrorIs(t, err, ErrInvalidCredentials)
	}

	_, _, err := svc.Login(ctx, "alice", "correct horse battery", "1.2.3.4")

	var rle *RateLimitedError

	require.ErrorAs(t, err, &rle)
	assert.Positive(t, rle.RetryAfter)

	_, _, err = svc.Login(ctx, "alice", "correct horse battery", "9.9.9.9")
	assert.NoError(t, err, "another IP is not blocked")
}

func TestValidateSession_ExpiryAndRenewal(t *testing.T) {
	svc, store, clock := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "correct horse battery", false)

	_, raw, err := svc.Login(ctx, "alice", "correct horse battery", "1.2.3.4")
	require.NoError(t, err)

	// Within the renewal threshold: no write.
	*clock = clock.Add(time.Minute)

	res, err := svc.ValidateSession(ctx, raw)
	require.NoError(t, err)
	assert.False(t, res.Renewed)

	// Past the threshold: sliding renewal.
	*clock = clock.Add(10 * time.Minute)

	res, err = svc.ValidateSession(ctx, raw)
	require.NoError(t, err)
	assert.True(t, res.Renewed)

	sess, err := store.SessionByTokenHash(ctx, HashToken(raw))
	require.NoError(t, err)
	assert.Equal(t, clock.Add(720*time.Hour), sess.ExpiresAt, "expiry slides to now+ttl")

	// At exactly expires_at the session is invalid (<= semantics), and the
	// row is deleted on sight.
	*clock = sess.ExpiresAt

	_, err = svc.ValidateSession(ctx, raw)
	require.ErrorIs(t, err, ErrSessionInvalid)

	_, err = store.SessionByTokenHash(ctx, HashToken(raw))
	assert.ErrorIs(t, err, authstore.ErrNotFound, "expired session deleted on sight")
}

func TestValidateSession_DisabledUserAndGarbage(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	alice := seedUser(t, svc, store, "alice", "correct horse battery", false)

	_, raw, err := svc.Login(ctx, "alice", "correct horse battery", "1.2.3.4")
	require.NoError(t, err)

	require.NoError(t, store.SetDisabled(ctx, alice.ID, true, svcNow))

	_, err = svc.ValidateSession(ctx, raw)
	require.ErrorIs(t, err, ErrSessionInvalid, "disabled user's session is invalid even if rows linger")

	_, err = svc.ValidateSession(ctx, "not-a-real-token")
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestLogout_Idempotent(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "correct horse battery", false)

	_, raw, err := svc.Login(ctx, "alice", "correct horse battery", "1.2.3.4")
	require.NoError(t, err)

	require.NoError(t, svc.Logout(ctx, raw))

	_, err = svc.ValidateSession(ctx, raw)
	require.ErrorIs(t, err, ErrSessionInvalid)

	assert.NoError(t, svc.Logout(ctx, raw), "second logout is a no-op")
}

func TestLogin_RehashesWeakHash(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "legacy", "", false, svcNow)
	require.NoError(t, err)

	weak := weakTestHash(t, "old password!")
	require.NoError(t, store.SetPasswordHash(ctx, u.ID, weak, svcNow))

	_, _, err = svc.Login(ctx, "legacy", "old password!", "1.2.3.4")
	require.NoError(t, err)

	got, err := store.UserByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, got.PasswordHash)
	assert.NotEqual(t, weak, *got.PasswordHash, "weak hash upgraded on successful login")
	assert.False(t, NeedsRehash(*got.PasswordHash))
}

func TestLogin_OverlongUsernameRejectedBeforeLimiter(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "correct horse battery", false)

	long := strings.Repeat("a", 300)

	_, _, err := svc.Login(ctx, long, "whatever12", "1.2.3.4")
	require.ErrorIs(t, err, ErrInvalidCredentials)

	svc.limiter.mu.Lock()
	n := len(svc.limiter.entries)
	svc.limiter.mu.Unlock()

	assert.Zero(t, n, "overlong usernames must never become limiter keys")
}

// weakTestHash builds a valid argon2id PHC string with weaker-than-current
// parameters (same technique as TestVerifyPassword_OldParamsStillVerify).
func weakTestHash(t *testing.T, password string) string {
	t.Helper()

	salt := make([]byte, 16)
	key := argon2idKeyForTest(password, salt)

	return formatWeakPHCForTest(salt, key)
}

func argon2idKeyForTest(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
}

func formatWeakPHCForTest(salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=19456,t=2,p=1$%s$%s",
		argon2.Version,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}
