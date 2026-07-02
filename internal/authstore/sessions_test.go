package authstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestUser(t *testing.T, store *authstore.Store, username string) int64 {
	t.Helper()

	u, err := store.CreateUser(context.Background(), username, "", false, testNow)
	require.NoError(t, err)

	return u.ID
}

func TestSessionLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	uid := createTestUser(t, store, "alice")

	expiry := testNow.Add(30 * 24 * time.Hour)
	require.NoError(t, store.CreateSession(ctx, "hash-1", uid, testNow, expiry))

	sess, err := store.SessionByTokenHash(ctx, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, uid, sess.UserID)
	assert.Equal(t, testNow, sess.CreatedAt)
	assert.Equal(t, expiry, sess.ExpiresAt)
	assert.Equal(t, testNow, sess.LastSeenAt)

	// Sliding renewal bumps expiry and last-seen, not created-at.
	later := testNow.Add(time.Hour)
	newExpiry := later.Add(30 * 24 * time.Hour)
	require.NoError(t, store.RenewSession(ctx, "hash-1", later, newExpiry))

	sess, err = store.SessionByTokenHash(ctx, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, newExpiry, sess.ExpiresAt)
	assert.Equal(t, later, sess.LastSeenAt)
	assert.Equal(t, testNow, sess.CreatedAt)

	require.NoError(t, store.DeleteSession(ctx, "hash-1"))

	_, err = store.SessionByTokenHash(ctx, "hash-1")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	// Deleting again is idempotent.
	assert.NoError(t, store.DeleteSession(ctx, "hash-1"))
}

func TestRenewSession_NotFound(t *testing.T) {
	store := openTestStore(t)

	err := store.RenewSession(context.Background(), "ghost", testNow, testNow.Add(time.Hour))
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}

func TestDeleteSessionsForUser(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	alice := createTestUser(t, store, "alice")
	bob := createTestUser(t, store, "bob")

	expiry := testNow.Add(time.Hour)
	require.NoError(t, store.CreateSession(ctx, "a-1", alice, testNow, expiry))
	require.NoError(t, store.CreateSession(ctx, "a-2", alice, testNow, expiry))
	require.NoError(t, store.CreateSession(ctx, "b-1", bob, testNow, expiry))

	n, err := store.DeleteSessionsForUser(ctx, alice)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	_, err = store.SessionByTokenHash(ctx, "b-1")
	assert.NoError(t, err, "other users' sessions survive")
}

func TestDeleteSessionsForUserExcept(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	alice := createTestUser(t, store, "alice")

	expiry := testNow.Add(time.Hour)
	require.NoError(t, store.CreateSession(ctx, "keep", alice, testNow, expiry))
	require.NoError(t, store.CreateSession(ctx, "drop-1", alice, testNow, expiry))
	require.NoError(t, store.CreateSession(ctx, "drop-2", alice, testNow, expiry))

	n, err := store.DeleteSessionsForUserExcept(ctx, alice, "keep")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	_, err = store.SessionByTokenHash(ctx, "keep")
	assert.NoError(t, err, "the acting session survives a password change")
}

func TestDeleteExpiredSessions(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	alice := createTestUser(t, store, "alice")

	require.NoError(t, store.CreateSession(ctx, "old", alice, testNow, testNow.Add(time.Minute)))
	require.NoError(t, store.CreateSession(ctx, "fresh", alice, testNow, testNow.Add(time.Hour)))

	n, err := store.DeleteExpiredSessions(ctx, testNow.Add(30*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	_, err = store.SessionByTokenHash(ctx, "old")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	_, err = store.SessionByTokenHash(ctx, "fresh")
	assert.NoError(t, err)
}
