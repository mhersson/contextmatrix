package auth_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$"), "PHC format with current params, got %s", hash)

	ok, err := auth.VerifyPassword("correct horse battery staple", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = auth.VerifyPassword("wrong password", hash)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHashPassword_SaltsDiffer(t *testing.T) {
	h1, err := auth.HashPassword("same input")
	require.NoError(t, err)

	h2, err := auth.HashPassword("same input")
	require.NoError(t, err)

	assert.NotEqual(t, h1, h2, "random salt must make identical passwords hash differently")
}

func TestVerifyPassword_Malformed(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "not argon2id", encoded: "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{name: "wrong section count", encoded: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA"},
		{name: "bad params", encoded: "$argon2id$v=19$m=abc,t=3,p=4$c2FsdA$aGFzaA"},
		{name: "bad base64 salt", encoded: "$argon2id$v=19$m=65536,t=3,p=4$!!!$aGFzaA"},
		{name: "bad base64 key", encoded: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := auth.VerifyPassword("anything", tt.encoded)
			assert.False(t, ok)
			assert.ErrorIs(t, err, auth.ErrInvalidHash)
		})
	}
}

func TestVerifyPassword_OldParamsStillVerify(t *testing.T) {
	// A hash produced with weaker parameters must still verify (params are
	// read from the PHC string), and NeedsRehash must flag it. Build the
	// legacy hash programmatically with the OWASP-minimum parameters.
	salt := make([]byte, 16)
	key := argon2.IDKey([]byte("legacy"), salt, 2, 19456, 1, 32)
	legacy := fmt.Sprintf("$argon2id$v=%d$m=19456,t=2,p=1$%s$%s",
		argon2.Version,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)

	ok, err := auth.VerifyPassword("legacy", legacy)
	require.NoError(t, err)
	assert.True(t, ok, "hash with embedded older params must verify")
	assert.True(t, auth.NeedsRehash(legacy))
}

func TestNeedsRehash(t *testing.T) {
	current, err := auth.HashPassword("x")
	require.NoError(t, err)

	assert.False(t, auth.NeedsRehash(current), "freshly produced hash is current")
	assert.True(t, auth.NeedsRehash("garbage"), "malformed forces rehash")
}
