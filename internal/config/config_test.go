package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfigFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

func TestLoad_WithYAMLFile(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
port: 9090
boards_dir: `+boardsDir+`
git_auto_commit: false
git_auto_push: true
heartbeat_timeout: "15m"
cors_origin: "https://example.com"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, boardsDir, cfg.BoardsDir)
	assert.False(t, cfg.GitAutoCommit)
	assert.True(t, cfg.GitAutoPush)
	assert.Equal(t, "15m", cfg.HeartbeatTimeout)
	assert.Equal(t, "https://example.com", cfg.CORSOrigin)
}

func TestLoad_MissingFile_FallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Set the required boards_dir via env so validation passes.
	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", boardsDir)

	cfg, err := Load(filepath.Join(dir, "nonexistent.yaml"))
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, boardsDir, cfg.BoardsDir)
	assert.True(t, cfg.GitAutoCommit)
	assert.False(t, cfg.GitAutoPush)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
}

func TestLoad_MissingFile_NoBoardsDir_ReturnsError(t *testing.T) {
	// Clear any env that might set boards_dir.
	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", "")

	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards_dir is required")
}

func TestLoad_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Write a minimal valid config file with boards_dir set.
	path := writeConfigFile(t, dir, `
port: 8080
boards_dir: `+boardsDir+`
git_auto_commit: true
git_auto_push: false
heartbeat_timeout: "30m"
cors_origin: "http://localhost:5173"
`)

	tests := []struct {
		name     string
		envKey   string
		envValue string
		check    func(t *testing.T, cfg *Config)
	}{
		{
			name:     "CONTEXTMATRIX_PORT",
			envKey:   "CONTEXTMATRIX_PORT",
			envValue: "3000",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, 3000, cfg.Port)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_DIR",
			envKey:   "CONTEXTMATRIX_BOARDS_DIR",
			envValue: boardsDir,
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, boardsDir, cfg.BoardsDir)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_COMMIT true",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_COMMIT",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_COMMIT 1",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_COMMIT",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_COMMIT false",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_COMMIT",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_PUSH true",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_PUSH",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_PUSH 1",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_PUSH",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_GIT_AUTO_PUSH false",
			envKey:   "CONTEXTMATRIX_GIT_AUTO_PUSH",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_HEARTBEAT_TIMEOUT",
			envKey:   "CONTEXTMATRIX_HEARTBEAT_TIMEOUT",
			envValue: "1h",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "1h", cfg.HeartbeatTimeout)
			},
		},
		{
			name:     "CONTEXTMATRIX_CORS_ORIGIN",
			envKey:   "CONTEXTMATRIX_CORS_ORIGIN",
			envValue: "https://myapp.example.com",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "https://myapp.example.com", cfg.CORSOrigin)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envValue)

			cfg, err := Load(path)
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestLoad_InvalidPortEnv_SilentlyIgnored(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
port: 9090
boards_dir: `+boardsDir+`
`)

	t.Setenv("CONTEXTMATRIX_PORT", "abc")

	cfg, err := Load(path)
	require.NoError(t, err)
	// Original value from YAML should be preserved since env override was invalid.
	assert.Equal(t, 9090, cfg.Port)
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	envBoardsDir := filepath.Join(dir, "env-boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))
	require.NoError(t, os.MkdirAll(envBoardsDir, 0o755))

	path := writeConfigFile(t, dir, `
port: 9090
boards_dir: `+boardsDir+`
heartbeat_timeout: "15m"
cors_origin: "http://localhost:5173"
`)

	t.Setenv("CONTEXTMATRIX_PORT", "4000")
	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", envBoardsDir)
	t.Setenv("CONTEXTMATRIX_HEARTBEAT_TIMEOUT", "45m")
	t.Setenv("CONTEXTMATRIX_CORS_ORIGIN", "https://override.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 4000, cfg.Port)
	assert.Equal(t, envBoardsDir, cfg.BoardsDir)
	assert.Equal(t, "45m", cfg.HeartbeatTimeout)
	assert.Equal(t, "https://override.example.com", cfg.CORSOrigin)
}

func TestValidate_MissingBoardsDir(t *testing.T) {
	cfg := &Config{
		BoardsDir:        "",
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards_dir is required")
}

func TestValidate_InvalidHeartbeatTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{name: "garbage string", timeout: "notaduration"},
		{name: "missing unit", timeout: "30"},
		{name: "empty string", timeout: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BoardsDir:        "/some/path",
				HeartbeatTimeout: tt.timeout,
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid heartbeat_timeout")
		})
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		BoardsDir:        "/some/path",
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "tilde only",
			input:    "~",
			expected: home,
		},
		{
			name:     "tilde with subpath",
			input:    "~/boards/contextmatrix",
			expected: filepath.Join(home, "boards/contextmatrix"),
		},
		{
			name:     "absolute path unchanged",
			input:    "/var/data/boards",
			expected: "/var/data/boards",
		},
		{
			name:     "relative path unchanged",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "tilde in middle unchanged",
			input:    "/home/user/~stuff",
			expected: "/home/user/~stuff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandTilde(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoad_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards_dir: "~/test-boards"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "test-boards"), cfg.BoardsDir)
}

func TestLoad_TildeExpansion_MissingFile(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", "~/env-boards")

	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "env-boards"), cfg.BoardsDir)
}

func TestHeartbeatDuration(t *testing.T) {
	tests := []struct {
		name      string
		timeout   string
		expected  time.Duration
		expectErr bool
	}{
		{
			name:     "30 minutes",
			timeout:  "30m",
			expected: 30 * time.Minute,
		},
		{
			name:     "1 hour",
			timeout:  "1h",
			expected: time.Hour,
		},
		{
			name:     "90 seconds",
			timeout:  "90s",
			expected: 90 * time.Second,
		},
		{
			name:     "complex duration",
			timeout:  "1h30m",
			expected: 90 * time.Minute,
		},
		{
			name:      "invalid string",
			timeout:   "notaduration",
			expectErr: true,
		},
		{
			name:      "empty string",
			timeout:   "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{HeartbeatTimeout: tt.timeout}
			d, err := cfg.HeartbeatDuration()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, d)
			}
		})
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, `
port: [invalid yaml
  this is broken
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse config")
}

func TestLoad_PartialYAML_DefaultsFillIn(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Only set boards_dir; everything else should be defaults.
	path := writeConfigFile(t, dir, `
boards_dir: `+boardsDir+`
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, boardsDir, cfg.BoardsDir)
	assert.True(t, cfg.GitAutoCommit)
	assert.False(t, cfg.GitAutoPush)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
}

func TestLoad_ValidationFailure_InvalidHeartbeat(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards_dir: `+boardsDir+`
heartbeat_timeout: "notaduration"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid heartbeat_timeout")
}

func TestDefaults(t *testing.T) {
	cfg := defaults()

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "", cfg.BoardsDir)
	assert.True(t, cfg.GitAutoCommit)
	assert.False(t, cfg.GitAutoPush)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
}
