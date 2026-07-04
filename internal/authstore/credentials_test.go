package authstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCredential(name string) *authstore.Credential {
	return &authstore.Credential{
		Name:            name,
		Kind:            authstore.CredentialKindPAT,
		EncryptedSecret: []byte{0xde, 0xad, 0xbe, 0xef},
		CreatedBy:       "human:morten",
	}
}

func TestCredentialLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	appCred := &authstore.Credential{
		Name:            "acme-org-app",
		Kind:            authstore.CredentialKindApp,
		Host:            "acme.ghe.com",
		APIBaseURL:      "https://api.acme.ghe.com",
		AppID:           1234,
		InstallationID:  5678,
		EncryptedSecret: []byte("encrypted-pem"),
		CreatedBy:       "human:morten",
	}
	require.NoError(t, store.CreateCredential(ctx, appCred, testNow))

	got, err := store.CredentialByName(ctx, "acme-org-app")
	require.NoError(t, err)
	assert.Equal(t, authstore.CredentialKindApp, got.Kind)
	assert.Equal(t, "acme.ghe.com", got.Host)
	assert.Equal(t, int64(1234), got.AppID)
	assert.Equal(t, int64(5678), got.InstallationID)
	assert.Equal(t, []byte("encrypted-pem"), got.EncryptedSecret)
	assert.Equal(t, "human:morten", got.CreatedBy)
	assert.False(t, got.Disabled)
	assert.Nil(t, got.LastUsedAt)
	assert.Equal(t, testNow, got.CreatedAt)
}

func TestCreateCredential_Duplicate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateCredential(ctx, testCredential("dup"), testNow))
	assert.ErrorIs(t, store.CreateCredential(ctx, testCredential("dup"), testNow), authstore.ErrDuplicate)
}

func TestCredentialByName_NotFound(t *testing.T) {
	store := openTestStore(t)

	_, err := store.CredentialByName(context.Background(), "ghost")
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}

func TestListCredentials_OrderedByName(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"zeta", "alpha"} {
		require.NoError(t, store.CreateCredential(ctx, testCredential(name), testNow))
	}

	creds, err := store.ListCredentials(ctx)
	require.NoError(t, err)
	require.Len(t, creds, 2)
	assert.Equal(t, "alpha", creds[0].Name)
	assert.Equal(t, "zeta", creds[1].Name)
}

func TestCredentialMutators(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateCredential(ctx, testCredential("rot"), testNow))

	later := testNow.Add(time.Hour)

	// Rotation replaces the secret under the same name — bindings don't move.
	require.NoError(t, store.UpdateCredentialSecret(ctx, "rot", []byte("new-secret"), later))
	require.NoError(t, store.UpdateCredentialMetadata(ctx, "rot", "ghe.example", "https://api.ghe.example", 9, 10, later))
	require.NoError(t, store.SetCredentialDisabled(ctx, "rot", true, later))

	got, err := store.CredentialByName(ctx, "rot")
	require.NoError(t, err)
	assert.Equal(t, []byte("new-secret"), got.EncryptedSecret)
	assert.Equal(t, "ghe.example", got.Host)
	assert.Equal(t, int64(9), got.AppID)
	assert.True(t, got.Disabled)
	assert.Equal(t, later, got.UpdatedAt)

	// last_used_at tracking for pool hygiene; deliberately no updated_at bump.
	used := later.Add(time.Hour)
	require.NoError(t, store.TouchCredentialUsed(ctx, "rot", used))

	got, err = store.CredentialByName(ctx, "rot")
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	assert.Equal(t, used, *got.LastUsedAt)
	assert.Equal(t, later, got.UpdatedAt, "usage tracking must not bump updated_at")
}

func TestCredentialMutators_NotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.ErrorIs(t, store.UpdateCredentialSecret(ctx, "ghost", []byte("x"), testNow), authstore.ErrNotFound)
	require.ErrorIs(t, store.SetCredentialDisabled(ctx, "ghost", true, testNow), authstore.ErrNotFound)
	assert.ErrorIs(t, store.TouchCredentialUsed(ctx, "ghost", testNow), authstore.ErrNotFound)
}

func TestRotateCredentialSecrets_RewritesAllInOrder(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateCredential(ctx,
		&authstore.Credential{Name: "a", Kind: authstore.CredentialKindPAT, EncryptedSecret: []byte("one"), CreatedBy: "x"}, testNow))
	require.NoError(t, store.CreateCredential(ctx,
		&authstore.Credential{Name: "b", Kind: authstore.CredentialKindPAT, EncryptedSecret: []byte("two"), CreatedBy: "x"}, testNow))

	n, err := store.RotateCredentialSecrets(ctx, func(old []byte) ([]byte, error) {
		return append([]byte("Z"), old...), nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	ga, err := store.CredentialByName(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "Zone", string(ga.EncryptedSecret))

	gb, err := store.CredentialByName(ctx, "b")
	require.NoError(t, err)
	assert.Equal(t, "Ztwo", string(gb.EncryptedSecret))
}

func TestRotateCredentialSecrets_Empty(t *testing.T) {
	store := openTestStore(t)

	n, err := store.RotateCredentialSecrets(context.Background(), func(old []byte) ([]byte, error) {
		return old, nil
	})
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRotateCredentialSecrets_RollsBackOnCallbackError(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateCredential(ctx,
		&authstore.Credential{Name: "aaa", Kind: authstore.CredentialKindPAT, EncryptedSecret: []byte("blob-a"), CreatedBy: "x"}, testNow))
	require.NoError(t, store.CreateCredential(ctx,
		&authstore.Credential{Name: "bbb", Kind: authstore.CredentialKindPAT, EncryptedSecret: []byte("blob-b"), CreatedBy: "x"}, testNow))

	// The callback rewrites the first entry, then fails on the second — a
	// genuine mid-loop abort, not a pre-flight rejection. The already-written
	// first entry must be rolled back too.
	boom := errors.New("boom")
	calls := 0

	n, err := store.RotateCredentialSecrets(ctx, func(old []byte) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, boom
		}

		return append([]byte("X"), old...), nil
	})
	require.ErrorIs(t, err, boom)
	assert.Zero(t, n)

	ga, err := store.CredentialByName(ctx, "aaa")
	require.NoError(t, err)
	assert.Equal(t, "blob-a", string(ga.EncryptedSecret), "first entry rolled back despite being rewritten")

	gb, err := store.CredentialByName(ctx, "bbb")
	require.NoError(t, err)
	assert.Equal(t, "blob-b", string(gb.EncryptedSecret))
}

func TestDeleteCredential(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateCredential(ctx, testCredential("gone"), testNow))
	require.NoError(t, store.DeleteCredential(ctx, "gone"))

	_, err := store.CredentialByName(ctx, "gone")
	require.ErrorIs(t, err, authstore.ErrNotFound)

	assert.ErrorIs(t, store.DeleteCredential(ctx, "gone"), authstore.ErrNotFound)
}
