package auth_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateMasterKey_CreatesThenLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "master.key")

	key, created, err := auth.LoadOrCreateMasterKey(path)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Len(t, key, 32)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	again, created, err := auth.LoadOrCreateMasterKey(path)
	require.NoError(t, err)
	assert.False(t, created, "second call loads, not creates")
	assert.Equal(t, key, again, "loaded key matches created key")
}

func TestLoadOrCreateMasterKey_RejectsBadContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "not hex", content: "zz not hex zz"},
		{name: "wrong length", content: "abcdef1234"},
		{name: "empty", content: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			_, _, err := auth.LoadOrCreateMasterKey(path)
			assert.Error(t, err)
		})
	}
}

func TestLoadOrCreateMasterKey_TrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	require.NoError(t, os.WriteFile(path, []byte(hexKey+"\n"), 0o600))

	key, created, err := auth.LoadOrCreateMasterKey(path)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Len(t, key, 32)
	assert.Equal(t, byte(0x1f), key[31])
}

func TestDeriveKey_PurposesDiffer(t *testing.T) {
	master := make([]byte, 32)

	k1, err := auth.DeriveKey(master, auth.KeyPurposeCredentials)
	require.NoError(t, err)
	assert.Len(t, k1, 32)

	k2, err := auth.DeriveKey(master, "some-future-purpose")
	require.NoError(t, err)

	assert.NotEqual(t, k1, k2, "distinct purposes must never share key material")

	k1again, err := auth.DeriveKey(master, auth.KeyPurposeCredentials)
	require.NoError(t, err)
	assert.Equal(t, k1, k1again, "derivation is deterministic")
}
