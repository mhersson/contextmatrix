package authstore_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

func TestCreateUser_AndLookup(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "Alice", "Alice A.", true, testNow)
	require.NoError(t, err)

	assert.Equal(t, "alice", u.Username, "usernames are lowercased")
	assert.Equal(t, "Alice A.", u.DisplayName)
	assert.True(t, u.IsAdmin)
	assert.False(t, u.Disabled)
	assert.Nil(t, u.PasswordHash, "no password until invite redeemed")
	assert.Nil(t, u.LastLoginAt)
	assert.Equal(t, testNow, u.CreatedAt)

	byName, err := store.UserByUsername(ctx, "ALICE")
	require.NoError(t, err)
	assert.Equal(t, u.ID, byName.ID, "lookup normalizes case")

	byID, err := store.UserByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", byID.Username)
}

func TestCreateUser_Duplicate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, err := store.CreateUser(ctx, "alice", "", false, testNow)
	require.NoError(t, err)

	_, err = store.CreateUser(ctx, "Alice", "", false, testNow)
	assert.ErrorIs(t, err, authstore.ErrDuplicate)
}

func TestCreateUser_ValidatesUsername(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		username string
		wantErr  error
	}{
		{name: "simple", username: "bob", wantErr: nil},
		{name: "with separators", username: "bob.the_builder-2", wantErr: nil},
		{name: "trims spaces", username: "  carol  ", wantErr: nil},
		{name: "empty", username: "", wantErr: authstore.ErrInvalidUsername},
		{name: "spaces inside", username: "bob smith", wantErr: authstore.ErrInvalidUsername},
		{name: "leading dot", username: ".bob", wantErr: authstore.ErrInvalidUsername},
		{name: "trailing dash", username: "bob-", wantErr: authstore.ErrInvalidUsername},
		{name: "colon breaks identity format", username: "bob:evil", wantErr: authstore.ErrInvalidUsername},
		{name: "too long", username: "a123456789012345678901234567890123", wantErr: authstore.ErrInvalidUsername},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.CreateUser(ctx, tt.username, "", false, testNow)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUserByUsername_NotFound(t *testing.T) {
	store := openTestStore(t)

	_, err := store.UserByUsername(context.Background(), "ghost")
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}

func TestListUsers_OrderedByUsername(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"zed", "alice", "mid"} {
		_, err := store.CreateUser(ctx, name, "", false, testNow)
		require.NoError(t, err)
	}

	users, err := store.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, "alice", users[0].Username)
	assert.Equal(t, "mid", users[1].Username)
	assert.Equal(t, "zed", users[2].Username)
}

func TestUserMutators(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "alice", "", false, testNow)
	require.NoError(t, err)

	later := testNow.Add(time.Hour)

	require.NoError(t, store.SetPasswordHash(ctx, u.ID, "$argon2id$fake", later))
	require.NoError(t, store.SetDisplayName(ctx, u.ID, "Alice!", later))
	require.NoError(t, store.SetAdmin(ctx, u.ID, true, later))
	require.NoError(t, store.SetDisabled(ctx, u.ID, true, later))
	require.NoError(t, store.TouchLastLogin(ctx, u.ID, later))

	got, err := store.UserByID(ctx, u.ID)
	require.NoError(t, err)

	require.NotNil(t, got.PasswordHash)
	assert.Equal(t, "$argon2id$fake", *got.PasswordHash)
	assert.Equal(t, "Alice!", got.DisplayName)
	assert.True(t, got.IsAdmin)
	assert.True(t, got.Disabled)
	require.NotNil(t, got.LastLoginAt)
	assert.Equal(t, later, *got.LastLoginAt)
	assert.Equal(t, later, got.UpdatedAt)
}

func TestUserMutators_NotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.ErrorIs(t, store.SetPasswordHash(ctx, 999, "h", testNow), authstore.ErrNotFound)
	require.ErrorIs(t, store.SetAdmin(ctx, 999, true, testNow), authstore.ErrNotFound)
}

func TestCountActiveAdmins(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	admin, err := store.CreateUser(ctx, "root", "", true, testNow)
	require.NoError(t, err)

	_, err = store.CreateUser(ctx, "user", "", false, testNow)
	require.NoError(t, err)

	n, err := store.CountActiveAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// A disabled admin no longer counts — this backs the last-admin guard.
	require.NoError(t, store.SetDisabled(ctx, admin.ID, true, testNow))

	n, err = store.CountActiveAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestCreateFirstAdmin_AtomicZeroUserGuard(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	u, err := store.CreateFirstAdmin(ctx, "Root", "The Root", testNow)
	require.NoError(t, err)
	assert.Equal(t, "root", u.Username)
	assert.True(t, u.IsAdmin)

	_, err = store.CreateFirstAdmin(ctx, "second", "", testNow)
	assert.ErrorIs(t, err, authstore.ErrNotBootstrappable)
}

func TestCreateFirstAdmin_Concurrent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	const goroutines = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)

	for i := range goroutines {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			if _, err := store.CreateFirstAdmin(ctx, fmt.Sprintf("admin%d", n), "", testNow); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, 1, successes, "exactly one concurrent bootstrap may win")

	users, err := store.ListUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 1)
}

func TestSetAdminGuarded_Concurrent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	a, err := store.CreateUser(ctx, "a", "", true, testNow)
	require.NoError(t, err)

	b, err := store.CreateUser(ctx, "b", "", true, testNow)
	require.NoError(t, err)

	// Two concurrent demotes of the only two admins: at most one may win.
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)

	for _, id := range []int64{a.ID, b.ID} {
		wg.Add(1)

		go func(uid int64) {
			defer wg.Done()

			if err := store.SetAdminGuarded(ctx, uid, testNow); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(id)
	}

	wg.Wait()

	assert.LessOrEqual(t, successes, 1, "guarded demote can never strand zero admins")

	n, err := store.CountActiveAdmins(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)
}

func TestSetAdminGuarded_LastAdminRefused(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	only, err := store.CreateUser(ctx, "only", "", true, testNow)
	require.NoError(t, err)

	require.ErrorIs(t, store.SetAdminGuarded(ctx, only.ID, testNow), authstore.ErrLastAdminStore)
	require.ErrorIs(t, store.SetDisabledGuarded(ctx, only.ID, testNow), authstore.ErrLastAdminStore)
}
