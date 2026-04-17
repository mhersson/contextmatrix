package gitops

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitAuthEnv_SSHMode(t *testing.T) {
	env := GitAuthEnv("ssh", "ignored-token")
	assert.Nil(t, env, "ssh mode should return nil (preserve caller env)")
}

func TestGitAuthEnv_EmptyMode(t *testing.T) {
	env := GitAuthEnv("", "any-token")
	assert.Nil(t, env, "empty mode should return nil")
}

func TestGitAuthEnv_UnknownMode(t *testing.T) {
	env := GitAuthEnv("kerberos", "any-token")
	assert.Nil(t, env, "unknown mode should return nil")
}

func TestGitAuthEnv_PATMode_FourEntries(t *testing.T) {
	const token = "ghp_supersecrettoken"
	env := GitAuthEnv("pat", token)
	require.Len(t, env, 4, "pat mode must return exactly 4 env entries")
}

func TestGitAuthEnv_PATMode_RequiredEntries(t *testing.T) {
	const token = "ghp_supersecrettoken"
	env := GitAuthEnv("pat", token)
	require.NotNil(t, env)

	assert.Contains(t, env, "GIT_CONFIG_COUNT=1")
	assert.Contains(t, env, "GIT_CONFIG_KEY_0=http.extraheader")
	assert.Contains(t, env, "GIT_CONFIG_VALUE_0=Authorization: Bearer "+token)
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
}

func TestGitAuthEnv_PATMode_BearerHeaderExact(t *testing.T) {
	const token = "ghp_supersecrettoken"
	env := GitAuthEnv("pat", token)
	require.NotNil(t, env)

	var headerValue string
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_VALUE_0=") {
			headerValue = strings.TrimPrefix(e, "GIT_CONFIG_VALUE_0=")
			break
		}
	}
	assert.Equal(t, "Authorization: Bearer "+token, headerValue,
		"http.extraheader value must be exactly 'Authorization: Bearer <token>'")
}

func TestGitAuthEnv_PATMode_PromptSuppressed(t *testing.T) {
	env := GitAuthEnv("pat", "some-token")
	require.NotNil(t, env)
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_PROMPT=0 must be set so missing PAT fails fast")
}

func TestGitAuthEnv_PATMode_TokenNotInKey(t *testing.T) {
	// The token must only appear in GIT_CONFIG_VALUE_0, never in a key name.
	const token = "ghp_supersecrettoken"
	env := GitAuthEnv("pat", token)
	require.NotNil(t, env)

	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_KEY_") {
			assert.NotContains(t, e, token,
				"token must not appear in git config key names")
		}
	}
}
