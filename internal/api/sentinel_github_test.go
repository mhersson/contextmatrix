package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSentinelGitHubParsesThePRURL(t *testing.T) {
	owner, repo, n, err := parsePullURL("https://github.example.net/o/r/pull/7")
	require.NoError(t, err)
	assert.Equal(t, "o", owner)
	assert.Equal(t, "r", repo)
	assert.Equal(t, 7, n)

	for _, bad := range []string{"https://x/o/r/issues/7", "https://x/o/r/pull/x", "https://x/o/pull/7", "https://x/o/r/pull/7/files", ""} {
		_, _, _, err := parsePullURL(bad)
		assert.Error(t, err, bad)
	}
}
