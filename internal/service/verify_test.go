package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
)

func TestValidateVerifyConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *board.VerifyConfig
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"empty is valid", &board.VerifyConfig{}, false},
		{"simple command", &board.VerifyConfig{Command: "make test"}, false},
		{"command at max length", &board.VerifyConfig{Command: strings.Repeat("a", maxVerifyCommandLen)}, false},
		{"command over max length", &board.VerifyConfig{Command: strings.Repeat("a", maxVerifyCommandLen+1)}, true},
		{"command with newline", &board.VerifyConfig{Command: "make test\nrm -rf /"}, true},
		{"command with carriage return", &board.VerifyConfig{Command: "make test\rfoo"}, true},
		{"command with NUL", &board.VerifyConfig{Command: "make\x00test"}, true},
		{"timeout at max", &board.VerifyConfig{Command: "x", TimeoutSeconds: maxVerifyTimeoutSeconds}, false},
		{"timeout over max", &board.VerifyConfig{Command: "x", TimeoutSeconds: maxVerifyTimeoutSeconds + 1}, true},
		{"timeout negative", &board.VerifyConfig{Command: "x", TimeoutSeconds: -1}, true},
		{"env valid names", &board.VerifyConfig{Command: "x", Env: []string{"JAVA_HOME", "CGO_ENABLED", "_UNDERSCORE"}}, false},
		{"env at max count", &board.VerifyConfig{Command: "x", Env: repeatName("VAR", maxVerifyEnvNames)}, false},
		{"env over max count", &board.VerifyConfig{Command: "x", Env: repeatName("VAR", maxVerifyEnvNames+1)}, true},
		{"env lowercase rejected", &board.VerifyConfig{Command: "x", Env: []string{"java_home"}}, true},
		{"env leading digit rejected", &board.VerifyConfig{Command: "x", Env: []string{"1VAR"}}, true},
		{"env with dash rejected", &board.VerifyConfig{Command: "x", Env: []string{"MY-VAR"}}, true},
		{"secret prefix CM_", &board.VerifyConfig{Command: "x", Env: []string{"CM_API_KEY"}}, true},
		{"secret prefix CMX_", &board.VerifyConfig{Command: "x", Env: []string{"CMX_MASTER"}}, true},
		{"secret prefix LLM_", &board.VerifyConfig{Command: "x", Env: []string{"LLM_ENDPOINT"}}, true},
		{"secret prefix GITHUB_", &board.VerifyConfig{Command: "x", Env: []string{"GITHUB_APP_ID"}}, true},
		{"secret suffix _TOKEN", &board.VerifyConfig{Command: "x", Env: []string{"MY_TOKEN"}}, true},
		{"secret suffix _KEY", &board.VerifyConfig{Command: "x", Env: []string{"API_KEY"}}, true},
		{"secret suffix _SECRET", &board.VerifyConfig{Command: "x", Env: []string{"CLIENT_SECRET"}}, true},
		{"secret suffix _PASSWORD", &board.VerifyConfig{Command: "x", Env: []string{"DB_PASSWORD"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVerifyConfig(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateProjectVerify_WrapsProjectConfigSentinel(t *testing.T) {
	err := validateProjectVerify(&board.VerifyConfig{Command: "x", Env: []string{"API_KEY"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, board.ErrInvalidProjectConfig)
}

func TestValidateCardVerify_WrapsVerifySentinel(t *testing.T) {
	err := validateCardVerify(&board.VerifyConfig{Command: "x", Env: []string{"API_KEY"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidVerify)
}

func TestNormalizeVerify(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		assert.Nil(t, normalizeVerify(nil))
	})

	t.Run("zero value normalizes to nil", func(t *testing.T) {
		assert.Nil(t, normalizeVerify(&board.VerifyConfig{}))
	})

	t.Run("whitespace-only command normalizes to nil", func(t *testing.T) {
		assert.Nil(t, normalizeVerify(&board.VerifyConfig{Command: "   "}))
	})

	t.Run("trims command", func(t *testing.T) {
		got := normalizeVerify(&board.VerifyConfig{Command: "  make test  "})
		require.NotNil(t, got)
		assert.Equal(t, "make test", got.Command)
	})

	t.Run("clones env slice", func(t *testing.T) {
		env := []string{"JAVA_HOME"}
		got := normalizeVerify(&board.VerifyConfig{Command: "x", Env: env})
		require.NotNil(t, got)

		env[0] = "MUTATED"

		assert.Equal(t, "JAVA_HOME", got.Env[0], "normalized env must not alias the input slice")
	})

	t.Run("timeout-only survives", func(t *testing.T) {
		got := normalizeVerify(&board.VerifyConfig{TimeoutSeconds: 300})
		require.NotNil(t, got)
		assert.Equal(t, 300, got.TimeoutSeconds)
	})

	t.Run("non-nil empty env is preserved as override-to-clear", func(t *testing.T) {
		got := normalizeVerify(&board.VerifyConfig{Command: "go test", Env: []string{}})
		require.NotNil(t, got)
		require.NotNil(t, got.Env, "non-nil empty env must survive normalization (override to clear)")
		assert.Empty(t, got.Env)
	})

	t.Run("nil env stays nil (inherit)", func(t *testing.T) {
		got := normalizeVerify(&board.VerifyConfig{Command: "go test"})
		require.NotNil(t, got)
		assert.Nil(t, got.Env, "nil env must stay nil so the card inherits the project's")
	})
}

// repeatName returns n distinct valid env names sharing a base prefix.
func repeatName(base string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = base + "_" + string(rune('A'+i))
	}

	return out
}
