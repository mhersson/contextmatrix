package authstore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOneTimeToken_ConsumeOnce(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	uid := createTestUser(t, store, "alice")

	expiry := testNow.Add(48 * time.Hour)
	require.NoError(t, store.CreateOneTimeToken(ctx, "tok-1", authstore.TokenPurposeInvite, &uid, testNow, expiry))

	got, err := store.OneTimeTokenByHash(ctx, "tok-1")
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeInvite, got.Purpose)
	require.NotNil(t, got.UserID)
	assert.Equal(t, uid, *got.UserID)
	assert.Nil(t, got.UsedAt)

	used := testNow.Add(time.Hour)

	consumed, err := store.ConsumeOneTimeToken(ctx, "tok-1", used)
	require.NoError(t, err)
	require.NotNil(t, consumed.UsedAt)
	assert.Equal(t, used, *consumed.UsedAt)

	_, err = store.ConsumeOneTimeToken(ctx, "tok-1", used.Add(time.Minute))
	assert.ErrorIs(t, err, authstore.ErrTokenSpent, "second redemption must fail")
}

func TestOneTimeToken_Expired(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateOneTimeToken(ctx, "tok-b", authstore.TokenPurposeBootstrap, nil, testNow, testNow.Add(time.Hour)))

	_, err := store.ConsumeOneTimeToken(ctx, "tok-b", testNow.Add(2*time.Hour))
	assert.ErrorIs(t, err, authstore.ErrTokenExpired)
}

func TestOneTimeToken_NotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, err := store.OneTimeTokenByHash(ctx, "ghost")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	_, err = store.ConsumeOneTimeToken(ctx, "ghost", testNow)
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}

func TestOneTimeToken_BootstrapHasNoUser(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateOneTimeToken(ctx, "tok-b", authstore.TokenPurposeBootstrap, nil, testNow, testNow.Add(time.Hour)))

	got, err := store.OneTimeTokenByHash(ctx, "tok-b")
	require.NoError(t, err)
	assert.Nil(t, got.UserID)
}

func TestInvalidateTokensForUser(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	uid := createTestUser(t, store, "alice")

	expiry := testNow.Add(48 * time.Hour)
	require.NoError(t, store.CreateOneTimeToken(ctx, "inv-1", authstore.TokenPurposeInvite, &uid, testNow, expiry))
	require.NoError(t, store.CreateOneTimeToken(ctx, "res-1", authstore.TokenPurposeReset, &uid, testNow, expiry))

	// Regenerating an invite invalidates prior unused invites but not resets.
	n, err := store.InvalidateTokensForUser(ctx, uid, authstore.TokenPurposeInvite)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	_, err = store.OneTimeTokenByHash(ctx, "inv-1")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	_, err = store.OneTimeTokenByHash(ctx, "res-1")
	assert.NoError(t, err)
}

func TestConsumeOneTimeToken_Concurrent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateOneTimeToken(ctx, "tok-race", authstore.TokenPurposeBootstrap, nil, testNow, testNow.Add(time.Hour)))

	const goroutines = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)

	for range goroutines {
		wg.Go(func() {
			if _, err := store.ConsumeOneTimeToken(ctx, "tok-race", testNow.Add(time.Minute)); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	assert.Equal(t, 1, successes, "exactly one concurrent redemption may win")
}

func TestDeleteExpiredOneTimeTokens(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	uid := createTestUser(t, store, "alice")

	require.NoError(t, store.CreateOneTimeToken(ctx, "old-unused", authstore.TokenPurposeInvite, &uid, testNow, testNow.Add(time.Hour)))
	require.NoError(t, store.CreateOneTimeToken(ctx, "fresh", authstore.TokenPurposeInvite, &uid, testNow, testNow.Add(72*time.Hour)))
	require.NoError(t, store.CreateOneTimeToken(ctx, "old-used", authstore.TokenPurposeReset, &uid, testNow, testNow.Add(time.Hour)))

	_, err := store.ConsumeOneTimeToken(ctx, "old-used", testNow.Add(time.Minute))
	require.NoError(t, err)

	n, err := store.DeleteExpiredOneTimeTokens(ctx, testNow.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "only the expired UNUSED token is swept")

	_, err = store.OneTimeTokenByHash(ctx, "old-unused")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	_, err = store.OneTimeTokenByHash(ctx, "fresh")
	require.NoError(t, err)

	_, err = store.OneTimeTokenByHash(ctx, "old-used")
	assert.NoError(t, err, "redeemed tokens are kept as audit trail")
}
