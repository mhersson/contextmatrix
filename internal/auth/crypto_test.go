package auth_test

import (
	"crypto/rand"
	"testing"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKey(t *testing.T) []byte {
	t.Helper()

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)

	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("ghp_secretPATvalue1234")

	blob, err := auth.EncryptSecret(key, plaintext)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), "ghp_", "ciphertext must not contain plaintext")

	got, err := auth.DecryptSecret(key, blob)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestEncryptSecret_NoncesDiffer(t *testing.T) {
	key := testKey(t)

	b1, err := auth.EncryptSecret(key, []byte("same"))
	require.NoError(t, err)

	b2, err := auth.EncryptSecret(key, []byte("same"))
	require.NoError(t, err)

	assert.NotEqual(t, b1, b2, "random nonce must make identical plaintexts encrypt differently")
}

func TestDecryptSecret_WrongKey(t *testing.T) {
	blob, err := auth.EncryptSecret(testKey(t), []byte("secret"))
	require.NoError(t, err)

	_, err = auth.DecryptSecret(testKey(t), blob)
	assert.ErrorIs(t, err, auth.ErrDecrypt)
}

func TestDecryptSecret_Tampered(t *testing.T) {
	key := testKey(t)

	blob, err := auth.EncryptSecret(key, []byte("secret"))
	require.NoError(t, err)

	blob[len(blob)-1] ^= 0x01

	_, err = auth.DecryptSecret(key, blob)
	assert.ErrorIs(t, err, auth.ErrDecrypt)
}

func TestDecryptSecret_TooShort(t *testing.T) {
	_, err := auth.DecryptSecret(testKey(t), []byte{0x01, 0x02})
	assert.ErrorIs(t, err, auth.ErrDecrypt)
}

func TestEncryptSecret_BadKeyLength(t *testing.T) {
	_, err := auth.EncryptSecret([]byte("short"), []byte("x"))
	assert.Error(t, err)
}
