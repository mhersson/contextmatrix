package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func credService(t *testing.T) (*Service, *authstore.Store, *[]CredentialInput) {
	t.Helper()

	svc, store, _ := newTestService(t)

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	svc.SetCredentialKey(key)

	var checked []CredentialInput

	svc.SetCredentialChecker(func(_ context.Context, in CredentialInput) error {
		checked = append(checked, in)

		return nil
	})

	return svc, store, &checked
}

func TestCreateCredential_ValidatesEncryptsStores(t *testing.T) {
	svc, store, checked := credService(t)
	ctx := context.Background()

	in := CredentialInput{Name: "acme-pat", Kind: authstore.CredentialKindPAT, Secret: "ghp_supersecret"}
	require.NoError(t, svc.CreateCredential(ctx, in, "human:root"))

	require.Len(t, *checked, 1, "GitHub check ran before save")

	stored, err := store.CredentialByName(ctx, "acme-pat")
	require.NoError(t, err)
	assert.Equal(t, "human:root", stored.CreatedBy)
	assert.NotContains(t, string(stored.EncryptedSecret), "ghp_supersecret", "secret is encrypted at rest")

	// The service can round-trip it (S5's resolver depends on this).
	plain, err := DecryptSecret(svc.credKey, stored.EncryptedSecret)
	require.NoError(t, err)
	assert.Equal(t, "ghp_supersecret", string(plain))
}

func TestCreateCredential_ShapeValidation(t *testing.T) {
	svc, _, checked := credService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		in   CredentialInput
	}{
		{name: "bad name", in: CredentialInput{Name: "Bad Name!", Kind: authstore.CredentialKindPAT, Secret: "x"}},
		{name: "empty secret", in: CredentialInput{Name: "ok", Kind: authstore.CredentialKindPAT}},
		{name: "unknown kind", in: CredentialInput{Name: "ok", Kind: "ssh", Secret: "x"}},
		{name: "app missing ids", in: CredentialInput{Name: "ok", Kind: authstore.CredentialKindApp, Secret: "pem"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, svc.CreateCredential(ctx, tt.in, "human:root"), ErrInvalidCredential)
		})
	}

	assert.Empty(t, *checked, "shape failures never reach the GitHub check")
}

func TestCreateCredential_GitHubRejection(t *testing.T) {
	svc, store, _ := credService(t)
	ctx := context.Background()

	svc.SetCredentialChecker(func(context.Context, CredentialInput) error {
		return errors.New("401 bad credentials")
	})

	err := svc.CreateCredential(ctx, CredentialInput{Name: "bad", Kind: authstore.CredentialKindPAT, Secret: "x"}, "human:root")
	require.ErrorIs(t, err, ErrCredentialRejected)

	_, err = store.CredentialByName(ctx, "bad")
	assert.ErrorIs(t, err, authstore.ErrNotFound, "rejected credentials are not stored")
}

func TestRotateCredentialSecret(t *testing.T) {
	svc, store, checked := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "rot", Kind: authstore.CredentialKindPAT, Secret: "old-secret"}, "human:root"))

	require.NoError(t, svc.RotateCredentialSecret(ctx, "rot", "new-secret"))
	assert.Len(t, *checked, 2, "rotation re-validates")

	stored, err := store.CredentialByName(ctx, "rot")
	require.NoError(t, err)

	plain, err := DecryptSecret(svc.credKey, stored.EncryptedSecret)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", string(plain))
}

func TestListCredentials_SecretsZeroed(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "a", Kind: authstore.CredentialKindPAT, Secret: "s"}, "human:root"))

	creds, err := svc.ListCredentials(ctx)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Nil(t, creds[0].EncryptedSecret, "list strips even the ciphertext")
}

func TestCredentialOps_NoKey(t *testing.T) {
	svc, _, _ := newTestService(t) // no SetCredentialKey

	err := svc.CreateCredential(context.Background(),
		CredentialInput{Name: "x", Kind: authstore.CredentialKindPAT, Secret: "s"}, "human:root")
	assert.ErrorIs(t, err, ErrNoCredentialKey)
}
