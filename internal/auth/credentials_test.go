package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func credService(t *testing.T) (*Service, *authstore.Store, *[]CredentialInput) {
	t.Helper()

	svc, store, _ := newTestService(t)

	// Advance the clock by a second on every read: authstore truncates
	// UpdatedAt to whole seconds, and TokenProviderFor's cache is keyed on
	// (name, UpdatedAt) — sequential writes in a test (e.g. create then
	// rotate) must observe distinct timestamps for that self-invalidation
	// contract to be testable against the otherwise-frozen test clock.
	clock := svcNow
	svc.now = func() time.Time {
		now := clock
		clock = clock.Add(time.Second)

		return now
	}

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

func TestCredentialExists(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "acme-pat", Kind: authstore.CredentialKindPAT, Secret: "s"}, "human:root"))

	exists, err := svc.CredentialExists(ctx, "acme-pat")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = svc.CredentialExists(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.False(t, exists)

	// Disabled entries still count as existing — the binding target must
	// exist; whether it's usable is a runtime resolution concern.
	require.NoError(t, svc.SetCredentialDisabled(ctx, "acme-pat", true))

	exists, err = svc.CredentialExists(ctx, "acme-pat")
	require.NoError(t, err)
	assert.True(t, exists, "disabled credentials still count as existing")
}

func TestUpdateCredentialMetadata_ReValidates(t *testing.T) {
	svc, _, checked := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "meta", Kind: authstore.CredentialKindPAT, Secret: "sekret"}, "human:root"))
	require.Len(t, *checked, 1)

	// Metadata change re-runs the checker with the DECRYPTED stored secret
	// and the merged metadata — a host change can invalidate a credential.
	require.NoError(t, svc.UpdateCredentialMetadata(ctx, "meta", "ghe.example", "", 0, 0))
	require.Len(t, *checked, 2)
	assert.Equal(t, "sekret", (*checked)[1].Secret, "checker sees the stored secret")
	assert.Equal(t, "ghe.example", (*checked)[1].Host, "checker sees the new metadata")

	svc.SetCredentialChecker(func(context.Context, CredentialInput) error {
		return errors.New("nope")
	})

	err := svc.UpdateCredentialMetadata(ctx, "meta", "other.example", "", 0, 0)
	require.ErrorIs(t, err, ErrCredentialRejected, "rejected metadata change is not persisted")

	creds, err := svc.ListCredentials(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ghe.example", creds[0].Host)
}

func TestCredentialOps_NoKey(t *testing.T) {
	svc, _, _ := newTestService(t) // no SetCredentialKey

	err := svc.CreateCredential(context.Background(),
		CredentialInput{Name: "x", Kind: authstore.CredentialKindPAT, Secret: "s"}, "human:root")
	assert.ErrorIs(t, err, ErrNoCredentialKey)
}

func TestTokenProviderFor_PAT(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "p", Kind: authstore.CredentialKindPAT, Host: "ghe.example", Secret: "tok-123"}, "human:root"))

	provider, apiBase, host, err := svc.TokenProviderFor(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, "https://api.ghe.example", apiBase)
	assert.Equal(t, "ghe.example", host)

	tok, _, err := provider.GenerateToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tok-123", tok, "PAT providers hand back the decrypted token")
}

func TestTokenProviderFor_CacheAndInvalidation(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "c", Kind: authstore.CredentialKindPAT, Secret: "one"}, "human:root"))

	p1, _, _, err := svc.TokenProviderFor(ctx, "c")
	require.NoError(t, err)

	p2, _, _, err := svc.TokenProviderFor(ctx, "c")
	require.NoError(t, err)
	assert.Same(t, p1, p2, "same generation → cached provider instance")

	// Rotation bumps UpdatedAt → new provider with the new secret.
	require.NoError(t, svc.RotateCredentialSecret(ctx, "c", "two"))

	p3, _, _, err := svc.TokenProviderFor(ctx, "c")
	require.NoError(t, err)
	assert.NotSame(t, p1, p3)

	tok, _, err := p3.GenerateToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "two", tok)
}

// TestTokenProviderFor_SameSecondRotationInvalidatesCache pins the race from
// putCredential: metadata-update then secret-rotate as two sequential writes
// in one request, landing within the same whole-second UpdatedAt tick. A
// cache keyed on UpdatedAt alone would serve the pre-rotation secret forever
// since the entry never sees a timestamp change. Uses newTestService
// directly (frozen clock, no per-read auto-advance) so both writes land on
// literally the same instant — the worst case of the same-second collision.
func TestTokenProviderFor_SameSecondRotationInvalidatesCache(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	svc.SetCredentialKey(key)

	svc.SetCredentialChecker(func(context.Context, CredentialInput) error { return nil })

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "same-second", Kind: authstore.CredentialKindPAT, Secret: "one"}, "human:root"))

	p1, _, _, err := svc.TokenProviderFor(ctx, "same-second")
	require.NoError(t, err)

	tok1, _, err := p1.GenerateToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "one", tok1, "cache warmed with the original secret")

	// Rotate under the frozen clock: UpdatedAt does not change at all, let
	// alone in a way distinguishable from the first write.
	require.NoError(t, svc.RotateCredentialSecret(ctx, "same-second", "two"))

	p2, _, _, err := svc.TokenProviderFor(ctx, "same-second")
	require.NoError(t, err)

	tok2, _, err := p2.GenerateToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "two", tok2, "post-rotation resolve must serve the new secret, not the stale cache entry")
}

func TestTokenProviderFor_Unavailable(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	_, _, _, err := svc.TokenProviderFor(ctx, "ghost")
	require.ErrorIs(t, err, ErrCredentialUnavailable)

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "off", Kind: authstore.CredentialKindPAT, Secret: "x"}, "human:root"))
	require.NoError(t, svc.SetCredentialDisabled(ctx, "off", true))

	_, _, _, err = svc.TokenProviderFor(ctx, "off")
	assert.ErrorIs(t, err, ErrCredentialUnavailable, "disabled entries fail closed")
}

// randomMaster returns a fresh 32-byte master key for rotation tests.
func randomMaster(t *testing.T) []byte {
	t.Helper()

	m := make([]byte, 32)
	_, err := rand.Read(m)
	require.NoError(t, err)

	return m
}

// rotateService builds a Service on a frozen clock with the credential subkey
// derived from master, plus a no-op GitHub checker so CreateCredential never
// reaches the network.
func rotateService(t *testing.T, master []byte) (*Service, *authstore.Store) {
	t.Helper()

	svc, store, _ := newTestService(t)

	key, err := DeriveKey(master, KeyPurposeCredentials)
	require.NoError(t, err)
	svc.SetCredentialKey(key)

	svc.SetCredentialChecker(func(context.Context, CredentialInput) error { return nil })

	return svc, store
}

func TestRotateMasterKey_ReencryptsAllUnderNewKey(t *testing.T) {
	masterA := randomMaster(t)
	masterB := randomMaster(t)

	svc, store := rotateService(t, masterA)
	ctx := context.Background()

	keyA, err := DeriveKey(masterA, KeyPurposeCredentials)
	require.NoError(t, err)
	keyB, err := DeriveKey(masterB, KeyPurposeCredentials)
	require.NoError(t, err)

	// Obviously-fake, per-entry markers so a swap bug (writing one entry's
	// ciphertext under another's name) surfaces as a decrypt mismatch.
	markers := map[string]string{
		"alpha":   "plaintext-alpha",
		"bravo":   "plaintext-bravo",
		"charlie": "plaintext-charlie",
	}
	for name, marker := range markers {
		require.NoError(t, svc.CreateCredential(ctx,
			CredentialInput{Name: name, Kind: authstore.CredentialKindPAT, Secret: marker}, "human:root"))
	}

	n, err := svc.RotateMasterKey(ctx, masterA, masterB)
	require.NoError(t, err)
	assert.Equal(t, len(markers), n, "every pool entry is re-encrypted")

	for name, marker := range markers {
		stored, err := store.CredentialByName(ctx, name)
		require.NoError(t, err)

		// Decrypts under the NEW derived key back to its own marker...
		plain, err := DecryptSecret(keyB, stored.EncryptedSecret)
		require.NoError(t, err)
		assert.Equal(t, marker, string(plain), "entry keeps its own plaintext after rotation")

		// ...and authenticated decryption under the OLD key now fails.
		_, err = DecryptSecret(keyA, stored.EncryptedSecret)
		assert.ErrorIs(t, err, ErrDecrypt, "old key no longer opens the blob")
	}
}

func TestRotateMasterKey_NoCredentials(t *testing.T) {
	masterA := randomMaster(t)
	masterB := randomMaster(t)

	svc, _ := rotateService(t, masterA)

	n, err := svc.RotateMasterKey(context.Background(), masterA, masterB)
	require.NoError(t, err)
	assert.Zero(t, n, "an empty pool still rotates cleanly")
}

func TestRotateMasterKey_WrongOldKeyRollsBack(t *testing.T) {
	masterA := randomMaster(t)
	masterB := randomMaster(t)
	masterWrong := randomMaster(t)

	svc, store := rotateService(t, masterA)
	ctx := context.Background()

	keyA, err := DeriveKey(masterA, KeyPurposeCredentials)
	require.NoError(t, err)
	keyB, err := DeriveKey(masterB, KeyPurposeCredentials)
	require.NoError(t, err)

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "keep", Kind: authstore.CredentialKindPAT, Secret: "plaintext-keep"}, "human:root"))

	// decrypt-with-old is the transaction guard: a wrong old key fails on the
	// first entry and must leave the pool byte-identical (all-or-nothing).
	_, err = svc.RotateMasterKey(ctx, masterWrong, masterB)
	require.Error(t, err)

	stored, err := store.CredentialByName(ctx, "keep")
	require.NoError(t, err)

	plain, err := DecryptSecret(keyA, stored.EncryptedSecret)
	require.NoError(t, err, "original key still opens the untouched blob")
	assert.Equal(t, "plaintext-keep", string(plain))

	_, err = DecryptSecret(keyB, stored.EncryptedSecret)
	assert.ErrorIs(t, err, ErrDecrypt, "nothing was re-encrypted under the new key")
}

func TestTokenProviderFor_WarmCacheThenDisable(t *testing.T) {
	svc, _, _ := credService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateCredential(ctx,
		CredentialInput{Name: "warm", Kind: authstore.CredentialKindPAT, Secret: "s"}, "human:root"))

	_, _, _, err := svc.TokenProviderFor(ctx, "warm")
	require.NoError(t, err, "cache warmed")

	require.NoError(t, svc.SetCredentialDisabled(ctx, "warm", true))

	_, _, _, err = svc.TokenProviderFor(ctx, "warm")
	assert.ErrorIs(t, err, ErrCredentialUnavailable, "disable wins over a warm cache")
}
