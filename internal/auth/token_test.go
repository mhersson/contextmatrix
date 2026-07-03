package auth_test

import (
	"testing"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewToken(t *testing.T) {
	raw, hash, err := auth.NewToken()
	require.NoError(t, err)

	assert.Len(t, raw, 43, "32 bytes base64url-encoded without padding")
	assert.Len(t, hash, 64, "hex sha-256")
	assert.Equal(t, auth.HashToken(raw), hash, "returned hash matches HashToken of raw")

	raw2, hash2, err := auth.NewToken()
	require.NoError(t, err)
	assert.NotEqual(t, raw, raw2, "tokens are random")
	assert.NotEqual(t, hash, hash2)
}

func TestHashToken_Deterministic(t *testing.T) {
	hash1 := auth.HashToken("abc")
	hash2 := auth.HashToken("abc")
	assert.Equal(t, hash1, hash2)
	assert.NotEqual(t, hash1, auth.HashToken("abd"))
}
