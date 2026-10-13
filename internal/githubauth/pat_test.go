package githubauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPATProvider_Empty(t *testing.T) {
	_, err := NewPATProvider("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token is required")
}

func TestNewPATProvider_OK(t *testing.T) {
	p, err := NewPATProvider("ghp_example")
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestPATProvider_GenerateToken(t *testing.T) {
	p, err := NewPATProvider("ghp_example")
	require.NoError(t, err)

	token, expiresAt, err := p.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ghp_example", token)
	assert.Greater(t, time.Until(expiresAt), 24*time.Hour,
		"PAT expiry sentinel should be far in the future, got %s", expiresAt)
}
