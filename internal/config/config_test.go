package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
boards:
  dir: `+boardsDir+`
  git_auto_commit: false
  git_auto_push: true
  git_deferred_commit: true
heartbeat_timeout: "15m"
cors_origin: "https://example.com"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, boardsDir, cfg.Boards.Dir)
	assert.False(t, cfg.Boards.GitAutoCommit)
	assert.True(t, cfg.Boards.GitAutoPush)
	assert.True(t, cfg.Boards.GitDeferredCommit)
	assert.Equal(t, "15m", cfg.HeartbeatTimeout)
	assert.Equal(t, "https://example.com", cfg.CORSOrigin)
}

func TestLoad_WithGitSyncFields(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_auto_pull: true
  git_pull_interval: "30s"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Boards.GitAutoPull)
	assert.Equal(t, "30s", cfg.Boards.GitPullInterval)

	d, err := cfg.PullIntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}

func TestLoad_MissingFile_FallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Set the required boards.dir via env so validation passes.
	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", boardsDir)

	cfg, err := Load(filepath.Join(dir, "nonexistent.yaml"))
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, boardsDir, cfg.Boards.Dir)
	assert.True(t, cfg.Boards.GitAutoCommit)
	assert.False(t, cfg.Boards.GitAutoPush)
	assert.False(t, cfg.Boards.GitAutoPull)
	assert.Equal(t, "60s", cfg.Boards.GitPullInterval)
	assert.False(t, cfg.Boards.GitDeferredCommit)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
}

func TestLoad_MissingFile_NoBoardsDir_ReturnsError(t *testing.T) {
	// Clear any env that might set boards.dir.
	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", "")

	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards.dir is required")
}

func TestLoad_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Write a minimal valid config file with boards.dir set.
	path := writeConfigFile(t, dir, `
port: 8080
boards:
  dir: `+boardsDir+`
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
				assert.Equal(t, boardsDir, cfg.Boards.Dir)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT true",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT 1",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT false",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.Boards.GitAutoCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH true",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH 1",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH false",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.Boards.GitAutoPush)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL true",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoPull)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL 1",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitAutoPull)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL false",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.Boards.GitAutoPull)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_PULL_INTERVAL",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_PULL_INTERVAL",
			envValue: "120s",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "120s", cfg.Boards.GitPullInterval)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT true",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT",
			envValue: "true",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitDeferredCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT 1",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT",
			envValue: "1",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.True(t, cfg.Boards.GitDeferredCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT false",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.Boards.GitDeferredCommit)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY false",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY",
			envValue: "false",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.False(t, cfg.Boards.GitCloneOnEmpty)
			},
		},
		{
			name:     "CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL",
			envKey:   "CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL",
			envValue: "git@github.com:user/boards.git",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "git@github.com:user/boards.git", cfg.Boards.GitRemoteURL)
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
boards:
  dir: `+boardsDir+`
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
boards:
  dir: `+boardsDir+`
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
	assert.Equal(t, envBoardsDir, cfg.Boards.Dir)
	assert.Equal(t, "45m", cfg.HeartbeatTimeout)
	assert.Equal(t, "https://override.example.com", cfg.CORSOrigin)
}

func TestValidate_MissingBoardsDir(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: ""},
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards.dir is required")
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
				Boards:           BoardsConfig{Dir: "/some/path"},
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
		Boards:           BoardsConfig{Dir: "/some/path"},
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
boards:
  dir: "~/test-boards"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "test-boards"), cfg.Boards.Dir)
}

func TestLoad_TildeExpansion_MissingFile(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", "~/env-boards")

	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "env-boards"), cfg.Boards.Dir)
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

	// Only set boards.dir; everything else should be defaults.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, boardsDir, cfg.Boards.Dir)
	assert.True(t, cfg.Boards.GitAutoCommit)
	assert.False(t, cfg.Boards.GitAutoPush)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
	assert.Equal(t, filepath.Join(dir, "workflow-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_ValidationFailure_InvalidPullInterval(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_pull_interval: "notaduration"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid boards.git_pull_interval")
}

func TestPullIntervalDuration(t *testing.T) {
	cfg := &Config{Boards: BoardsConfig{GitPullInterval: "90s"}}
	d, err := cfg.PullIntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, d)
}

func TestValidate_EmptyPullInterval_CoercedToDefault(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitPullInterval: "",
		},
		HeartbeatTimeout: "30m",
	}

	require.NoError(t, cfg.Validate())
	assert.Equal(t, "60s", cfg.Boards.GitPullInterval)

	d, err := cfg.PullIntervalDuration()
	require.NoError(t, err)
	assert.NotZero(t, d)
}

func TestLoad_ValidationFailure_InvalidHeartbeat(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
heartbeat_timeout: "notaduration"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid heartbeat_timeout")
}

func TestDefaults(t *testing.T) {
	cfg := defaults()

	assert.Equal(t, 8080, cfg.Port)
	assert.Empty(t, cfg.Boards.Dir)
	assert.True(t, cfg.Boards.GitAutoCommit)
	assert.False(t, cfg.Boards.GitAutoPush)
	assert.False(t, cfg.Boards.GitAutoPull)
	assert.Equal(t, "60s", cfg.Boards.GitPullInterval)
	assert.False(t, cfg.Boards.GitDeferredCommit)
	assert.False(t, cfg.Boards.GitCloneOnEmpty)
	assert.Empty(t, cfg.Boards.GitRemoteURL)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
	assert.Empty(t, cfg.WorkflowSkillsDir)
	assert.Empty(t, cfg.MCPAPIKey)
	assert.False(t, cfg.Runner.Enabled)
	assert.Empty(t, cfg.Runner.URL)
	assert.Empty(t, cfg.Runner.APIKey)
}

func TestLoad_RunnerConfig(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
mcp_api_key: "test-mcp-key"
runner:
  enabled: true
  url: "http://localhost:9090"
  api_key: "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "test-mcp-key", cfg.MCPAPIKey)
	assert.True(t, cfg.Runner.Enabled)
	assert.Equal(t, "http://localhost:9090", cfg.Runner.URL)
	assert.Equal(t, "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh", cfg.Runner.APIKey)
}

func TestLoad_RunnerDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.Runner.Enabled)
	assert.Empty(t, cfg.Runner.URL)
	assert.Empty(t, cfg.Runner.APIKey)
	assert.Empty(t, cfg.MCPAPIKey)
}

func TestValidate_RunnerEnabledMissingURL(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, APIKey: "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.url is required")
}

func TestValidate_RunnerEnabledMissingAPIKey(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, URL: "http://localhost:9090"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.api_key is required")
}

func TestValidate_RunnerEnabledShortAPIKey(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: "too-short"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.api_key must be at least")
}

func TestValidate_RunnerDisabledNoValidation(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: false},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestLoad_RunnerEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_RUNNER_ENABLED", "true")
	t.Setenv("CONTEXTMATRIX_RUNNER_URL", "http://runner:9090")
	t.Setenv("CONTEXTMATRIX_RUNNER_API_KEY", "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh")
	t.Setenv("CONTEXTMATRIX_MCP_API_KEY", "env-mcp-key")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Runner.Enabled)
	assert.Equal(t, "http://runner:9090", cfg.Runner.URL)
	assert.Equal(t, "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh", cfg.Runner.APIKey)
	assert.Equal(t, "env-mcp-key", cfg.MCPAPIKey)
}

func TestFindConfigPath_XDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	xdgDir := filepath.Join(dir, "xdg-config")
	cmDir := filepath.Join(xdgDir, "contextmatrix")
	require.NoError(t, os.MkdirAll(cmDir, 0o755))
	writeConfigFile(t, cmDir, "port: 9090\nboards:\n  dir: /tmp/boards\n")

	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	got := FindConfigPath()
	assert.Equal(t, filepath.Join(cmDir, "config.yaml"), got)
}

func TestFindConfigPath_XDGDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")

	cmDir := filepath.Join(dir, ".config", "contextmatrix")
	require.NoError(t, os.MkdirAll(cmDir, 0o755))
	writeConfigFile(t, cmDir, "port: 9090\nboards:\n  dir: /tmp/boards\n")

	got := FindConfigPath()
	assert.Equal(t, filepath.Join(cmDir, "config.yaml"), got)
}

func TestFindConfigPath_FallbackToCwd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "nonexistent"))

	got := FindConfigPath()
	assert.Equal(t, "config.yaml", got)
}

func TestFindConfigPath_XDGSetButNoFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty-xdg"))

	got := FindConfigPath()
	assert.Equal(t, "config.yaml", got)
}

func TestLoad_WorkflowSkillsDirDerivedFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "workflow-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirExplicitInYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nworkflow_skills_dir: /opt/skills\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "/opt/skills", cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")
	t.Setenv("CONTEXTMATRIX_WORKFLOW_SKILLS_DIR", "/custom/skills")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "/custom/skills", cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nworkflow_skills_dir: \"~/my-skills\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "my-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirMissingFileDerivedFromConfigPath(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", boardsDir)

	// Load from a nonexistent file — workflow_skills_dir derived from its directory
	cfg, err := Load(filepath.Join(dir, "nonexistent.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "workflow-skills"), cfg.WorkflowSkillsDir)
}

// REMOVED IN TASK 8: TestLoad_TaskSkillsDirDefault — replaced by TestLoad_TaskSkills_Defaults
// REMOVED IN TASK 8: TestLoad_TaskSkillsDirExplicit — replaced by TestLoad_TaskSkills_Explicit
// REMOVED IN TASK 8: TestLoad_TaskSkillsDirEnvOverride — rewritten in Task 6/7
// REMOVED IN TASK 8: TestLoad_TaskSkillsDirTildeExpansion — rewritten in Task 7

func TestLoad_CloneOnEmptyFields(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_clone_on_empty: true
  git_remote_url: "git@github.com:user/boards.git"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Boards.GitCloneOnEmpty)
	assert.Equal(t, "git@github.com:user/boards.git", cfg.Boards.GitRemoteURL)
}

func TestLoad_CloneOnEmptyDefaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.Boards.GitCloneOnEmpty)
	assert.Empty(t, cfg.Boards.GitRemoteURL)
}

func TestLoad_CloneOnEmptyEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY", "true")
	t.Setenv("CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL", "git@github.com:user/boards.git")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Boards.GitCloneOnEmpty)
	assert.Equal(t, "git@github.com:user/boards.git", cfg.Boards.GitRemoteURL)
}

func TestValidate_CloneOnEmptyWithoutRemoteURL(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitCloneOnEmpty: true,
			GitRemoteURL:    "",
		},
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards.git_remote_url is required when boards.git_clone_on_empty is enabled")
}

func TestValidate_CloneOnEmptyWithRemoteURL(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitCloneOnEmpty: true,
			GitRemoteURL:    "git@github.com:user/boards.git",
		},
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_RemoteURLWithoutCloneOnEmpty(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitCloneOnEmpty: false,
			GitRemoteURL:    "git@github.com:user/boards.git",
		},
		HeartbeatTimeout: "30m",
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

// REMOVED IN TASK 8: TestLoad_ExampleFile — references cfg.GitHub.Token (removed field);
// rewritten in Task 8 to use the new auth_mode schema.

// ---------- GitHub issue importing config tests ----------

// REMOVED IN TASK 8: TestLoad_GitHubIssueImporting_Enabled — references cfg.GitHub.Token
// REMOVED IN TASK 8: TestLoad_GitHubIssueImporting_Disabled — references cfg.GitHub.Token

func TestLoad_GitHubIssueImporting_DefaultSyncInterval(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// No sync_interval specified — should default to "5m" during Validate.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  token: "ghp_test_token"
  issue_importing:
    enabled: true
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "5m", cfg.GitHub.IssueImporting.SyncInterval)
	d, err := cfg.GitHub.IssueImporting.SyncIntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, d)
}

// REMOVED IN TASK 8: TestLoad_GitHubIssueImporting_NoTokenNoError_WhenDisabled — references cfg.GitHub.Token

// REMOVED IN TASK 8: TestValidate_GitHubIssueImporting_EnabledWithoutToken — references GitHubConfig{Token:}
// REMOVED IN TASK 8: TestValidate_GitHubIssueImporting_InvalidSyncInterval — references GitHubConfig{Token:}
// REMOVED IN TASK 8: TestValidate_GitHubIssueImporting_SyncIntervalTooShort — references GitHubConfig{Token:}
// REMOVED IN TASK 8: TestValidate_GitHubIssueImporting_ValidConfig — references GitHubConfig{Token:}
// REMOVED IN TASK 8: TestValidate_GitHubIssueImporting_TokenWithoutEnabled_NoError — references GitHubConfig{Token:}

// REMOVED IN TASK 8: TestLoad_GitHubIssueImporting_EnvOverrides — references cfg.GitHub.Token

func TestLoad_GitHubIssueImporting_EnvEnabled1(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_GITHUB_TOKEN", "ghp_env_token")
	t.Setenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED", "1")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.GitHub.IssueImporting.Enabled)
}

func TestLoad_GitHubIssueImporting_EnvDisabled(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Start with enabled=true in YAML, override to false via env.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  token: "ghp_test"
  issue_importing:
    enabled: true
    sync_interval: "5m"
`)

	t.Setenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED", "false")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.GitHub.IssueImporting.Enabled)
}

// REMOVED IN TASK 8: TestDefaults_GitHubIssueImporting — references cfg.GitHub.Token

func TestSyncIntervalDuration(t *testing.T) {
	tests := []struct {
		name      string
		interval  string
		expected  time.Duration
		expectErr bool
	}{
		{name: "5 minutes", interval: "5m", expected: 5 * time.Minute},
		{name: "15 minutes", interval: "15m", expected: 15 * time.Minute},
		{name: "1 hour", interval: "1h", expected: time.Hour},
		{name: "invalid", interval: "notaduration", expectErr: true},
		{name: "empty", interval: "", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IssueImportingConfig{SyncInterval: tt.interval}

			d, err := c.SyncIntervalDuration()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, d)
			}
		})
	}
}

// ---------- GitHubConfig Host and APIBaseURL field tests ----------

func TestLoad_GitHubHostAndAPIBaseURL_YAMLLoading(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  host: "acme.ghe.com"
  api_base_url: "https://api.acme.ghe.com"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "acme.ghe.com", cfg.GitHub.Host)
	assert.Equal(t, "https://api.acme.ghe.com", cfg.GitHub.APIBaseURL)
}

func TestLoad_GitHubHostAndAPIBaseURL_Defaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Empty(t, cfg.GitHub.Host)
	assert.Empty(t, cfg.GitHub.APIBaseURL)
}

func TestLoad_GitHubHostAndAPIBaseURL_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_GITHUB_HOST", "enterprise.example.com")
	t.Setenv("CONTEXTMATRIX_GITHUB_API_BASE_URL", "https://api.enterprise.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "enterprise.example.com", cfg.GitHub.Host)
	assert.Equal(t, "https://api.enterprise.example.com", cfg.GitHub.APIBaseURL)
}

func TestLoad_GitHubHostEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  host: "yaml-host.example.com"
  api_base_url: "https://yaml-api.example.com"
`)

	t.Setenv("CONTEXTMATRIX_GITHUB_HOST", "env-host.example.com")
	t.Setenv("CONTEXTMATRIX_GITHUB_API_BASE_URL", "https://env-api.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "env-host.example.com", cfg.GitHub.Host)
	assert.Equal(t, "https://env-api.example.com", cfg.GitHub.APIBaseURL)
}

// ---------- ResolvedAPIBaseURL tests ----------

func TestResolvedAPIBaseURL_DefaultWhenEmpty(t *testing.T) {
	gh := GitHubConfig{}
	assert.Equal(t, "https://api.github.com", gh.ResolvedAPIBaseURL())
}

func TestResolvedAPIBaseURL_HostDerivedWhenAPIBaseURLEmpty(t *testing.T) {
	gh := GitHubConfig{Host: "acme.ghe.com"}
	assert.Equal(t, "https://api.acme.ghe.com", gh.ResolvedAPIBaseURL())
}

func TestResolvedAPIBaseURL_APIBaseURLTakesPrecedence(t *testing.T) {
	gh := GitHubConfig{
		Host:       "acme.ghe.com",
		APIBaseURL: "https://custom-api.acme.ghe.com",
	}
	assert.Equal(t, "https://custom-api.acme.ghe.com", gh.ResolvedAPIBaseURL())
}

func TestResolvedAPIBaseURL_APIBaseURLTrimmed(t *testing.T) {
	gh := GitHubConfig{APIBaseURL: "  https://api.example.com  "}
	assert.Equal(t, "https://api.example.com", gh.ResolvedAPIBaseURL())
}

func TestResolvedAPIBaseURL_APIBaseURLSetWithoutHost(t *testing.T) {
	gh := GitHubConfig{APIBaseURL: "https://api.custom.com"}
	assert.Equal(t, "https://api.custom.com", gh.ResolvedAPIBaseURL())
}

func TestResolvedAPIBaseURL_HostGitHubCom(t *testing.T) {
	// Host set to github.com explicitly — should still derive correctly.
	gh := GitHubConfig{Host: "github.com"}
	assert.Equal(t, "https://api.github.com", gh.ResolvedAPIBaseURL())
}

// ---------- AllowedHosts tests ----------

func TestAllowedHosts_EmptyHost(t *testing.T) {
	gh := GitHubConfig{}
	assert.Equal(t, []string{"github.com"}, gh.AllowedHosts())
}

func TestAllowedHosts_DefaultGitHubComHost(t *testing.T) {
	gh := GitHubConfig{Host: "github.com"}
	assert.Equal(t, []string{"github.com"}, gh.AllowedHosts())
}

func TestAllowedHosts_CustomHost(t *testing.T) {
	gh := GitHubConfig{Host: "acme.ghe.com"}
	hosts := gh.AllowedHosts()
	assert.Equal(t, []string{"github.com", "acme.ghe.com"}, hosts)
}

func TestAllowedHosts_CustomHostNotDuplicated(t *testing.T) {
	gh := GitHubConfig{Host: "acme.ghe.com"}
	hosts := gh.AllowedHosts()
	assert.Len(t, hosts, 2)
}

func TestDefaults_GitHubHostAndAPIBaseURL(t *testing.T) {
	cfg := defaults()
	assert.Empty(t, cfg.GitHub.Host)
	assert.Empty(t, cfg.GitHub.APIBaseURL)
}

// ---------- GitAuthMode tests — REMOVED IN TASK 8 ----------
// All tests below reference BoardsConfig.GitAuthMode and GitHubConfig.Token,
// which were removed in Task 2. Replacements written in Tasks 3–5 and deleted here in Task 8.
//
// REMOVED IN TASK 8: TestDefaults_GitAuthMode
// REMOVED IN TASK 8: TestLoad_GitAuthMode_DefaultIsSSH
// REMOVED IN TASK 8: TestLoad_GitAuthMode_EnvOverride
// REMOVED IN TASK 8: TestValidate_GitAuthMode_UnknownValueRejected
// REMOVED IN TASK 8: TestValidate_GitAuthMode_PATMissingToken
// REMOVED IN TASK 8: TestValidate_GitAuthMode_PATWithSSHURL
// REMOVED IN TASK 8: TestValidate_GitAuthMode_PATWithHTTPSURLAndToken
// REMOVED IN TASK 8: TestValidate_GitAuthMode_SSHIsValidWithAnyRemote
// REMOVED IN TASK 8: TestLoad_GitAuthMode_YAMLField
// REMOVED IN TASK 8: TestLoad_GitAuthMode_PATFromYAML
// REMOVED IN TASK 8: TestLoad_GitAuthMode_ExampleFileHasSSHDefault

// ---------- Theme config tests ----------

func TestDefaults_Theme(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, "everforest", cfg.Theme)
}

func TestLoad_Theme_DefaultIsEverforest(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "everforest", cfg.Theme)
}

func TestLoad_Theme_ValidRadix(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
theme: "radix"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "radix", cfg.Theme)
}

func TestLoad_Theme_InvalidValueRejected(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
theme: "dracula"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid theme")
}

func TestLoad_Theme_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_THEME", "radix")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "radix", cfg.Theme)
}

func TestValidate_Theme_InvalidValue(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Theme:            "monokai",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid theme")
}

func TestValidate_Theme_ValidValues(t *testing.T) {
	for _, theme := range []string{"everforest", "radix", "catppuccin"} {
		t.Run(theme, func(t *testing.T) {
			cfg := &Config{
				Boards:           BoardsConfig{Dir: "/some/path"},
				HeartbeatTimeout: "30m",
				Theme:            theme,
			}
			err := cfg.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestValidate_LogFormat_InvalidValue(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		LogFormat:        "yaml",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log_format")
}

func TestValidate_LogLevel_InvalidValue(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		LogLevel:         "warm", // typo for "warn"
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log_level")
}

func TestValidate_AdminPort_OutOfRange(t *testing.T) {
	for _, p := range []int{-1, 65536, 99999} {
		t.Run(strconv.Itoa(p), func(t *testing.T) {
			cfg := &Config{
				Boards:           BoardsConfig{Dir: "/some/path"},
				HeartbeatTimeout: "30m",
				AdminPort:        p,
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid admin_port")
		})
	}
}

func TestValidate_AdminPort_CollisionWithPort(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Port:             8080,
		AdminPort:        8080,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides")
}

func TestValidate_AdminBindAddr_DefaultsToLoopback(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		AdminPort:        9091,
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "127.0.0.1", cfg.AdminBindAddr)
}

// ---------- OrchestratorModel config tests ----------

func TestLoad_OrchestratorModels_YAMLProvidesBothValues(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
runner:
  orchestrator_sonnet_model: "claude-sonnet-4-99"
  orchestrator_opus_model: "claude-opus-4-99"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "claude-sonnet-4-99", cfg.Runner.OrchestratorSonnetModel)
	assert.Equal(t, "claude-opus-4-99", cfg.Runner.OrchestratorOpusModel)
}

func TestLoad_OrchestratorModels_DefaultsApplyWhenYAMLOmits(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "claude-sonnet-4-6", cfg.Runner.OrchestratorSonnetModel)
	assert.Equal(t, "claude-opus-4-7", cfg.Runner.OrchestratorOpusModel)
}

func TestLoad_OrchestratorModels_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
runner:
  orchestrator_sonnet_model: "claude-sonnet-4-yaml"
  orchestrator_opus_model: "claude-opus-4-yaml"
`)

	t.Setenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_SONNET_MODEL", "claude-sonnet-4-env")
	t.Setenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_OPUS_MODEL", "claude-opus-4-env")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "claude-sonnet-4-env", cfg.Runner.OrchestratorSonnetModel)
	assert.Equal(t, "claude-opus-4-env", cfg.Runner.OrchestratorOpusModel)
}

func TestDefaults_OrchestratorModels(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, "claude-sonnet-4-6", cfg.Runner.OrchestratorSonnetModel)
	assert.Equal(t, "claude-opus-4-7", cfg.Runner.OrchestratorOpusModel)
}

func TestDefaults_ReconcileInterval(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, "60s", cfg.Runner.ReconcileInterval)
	assert.Equal(t, 60*time.Second, cfg.Runner.ReconcileIntervalDuration())
}

func TestReconcileIntervalDuration_EmptyReturnsZero(t *testing.T) {
	r := &RunnerConfig{ReconcileInterval: ""}
	assert.Equal(t, time.Duration(0), r.ReconcileIntervalDuration())
}

func TestReconcileIntervalDuration_InvalidReturnsZero(t *testing.T) {
	r := &RunnerConfig{ReconcileInterval: "not-a-duration"}
	assert.Equal(t, time.Duration(0), r.ReconcileIntervalDuration())
}

func TestLoad_ReconcileInterval_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
runner:
  reconcile_interval: "120s"
`)

	t.Setenv("CONTEXTMATRIX_RUNNER_RECONCILE_INTERVAL", "30s")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "30s", cfg.Runner.ReconcileInterval)
	assert.Equal(t, 30*time.Second, cfg.Runner.ReconcileIntervalDuration())
}

func TestLoad_ReconcileInterval_InvalidRejectedWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
runner:
  enabled: true
  url: "http://localhost:9090"
  api_key: "0123456789012345678901234567890123"
  reconcile_interval: "not-a-duration"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.reconcile_interval")
}

// ---------- LogFormat / LogLevel / AdminPort config tests ----------

func TestDefaults_LogFormatLevelAdminPort(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 0, cfg.AdminPort)
}

func TestLoad_LogFormatLevelAdminPort_YAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
log_format: "json"
log_level: "debug"
admin_port: 6060
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 6060, cfg.AdminPort)
}

func TestLoad_LogFormatLevelAdminPort_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
log_format: "text"
log_level: "info"
admin_port: 0
`)

	t.Setenv("CONTEXTMATRIX_LOG_FORMAT", "json")
	t.Setenv("CONTEXTMATRIX_LOG_LEVEL", "warn")
	t.Setenv("CONTEXTMATRIX_ADMIN_PORT", "9090")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, 9090, cfg.AdminPort)
}

func TestLoad_LogFormat_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
log_format: "text"
`)

	t.Setenv("CONTEXTMATRIX_LOG_FORMAT", "json")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "json", cfg.LogFormat)
}

func TestLoad_InvalidAdminPortEnv_SilentlyIgnored(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
admin_port: 6060
`)

	t.Setenv("CONTEXTMATRIX_ADMIN_PORT", "notanumber")

	cfg, err := Load(path)
	require.NoError(t, err)
	// YAML value preserved when env override is invalid.
	assert.Equal(t, 6060, cfg.AdminPort)
}

// ---------- BuildSlogHandler tests ----------

func TestBuildSlogHandler_TextFormat(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: "info"}
	h := cfg.BuildSlogHandler(&bytes.Buffer{})
	_, ok := h.(*slog.TextHandler)
	assert.True(t, ok, "expected *slog.TextHandler for format=text")
}

func TestBuildSlogHandler_JSONFormat(t *testing.T) {
	cfg := &Config{LogFormat: "json", LogLevel: "info"}
	h := cfg.BuildSlogHandler(&bytes.Buffer{})
	_, ok := h.(*slog.JSONHandler)
	assert.True(t, ok, "expected *slog.JSONHandler for format=json")
}

func TestBuildSlogHandler_UnknownFormatDefaultsToText(t *testing.T) {
	cfg := &Config{LogFormat: "", LogLevel: "info"}
	h := cfg.BuildSlogHandler(&bytes.Buffer{})
	_, ok := h.(*slog.TextHandler)
	assert.True(t, ok, "expected *slog.TextHandler for empty format")
}

func TestBuildSlogHandler_LevelDebug(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: "debug"}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Debug("test debug message")

	assert.Contains(t, buf.String(), "test debug message")
}

func TestBuildSlogHandler_LevelInfo_FiltersDebug(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: "info"}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Debug("should not appear")
	logger.Info("should appear")

	assert.NotContains(t, buf.String(), "should not appear")
	assert.Contains(t, buf.String(), "should appear")
}

// ---------- New auth-mode schema tests (Task 2) ----------

func TestLoad_GitHubAuthModeApp_Parses(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := `
boards:
  dir: ` + boardsDir + `
github:
  auth_mode: "app"
  app:
    app_id: 12345
    installation_id: 67890
    private_key_path: /etc/keys/app.pem
`
	path := writeConfigFile(t, dir, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "app", cfg.GitHub.AuthMode)
	assert.Equal(t, int64(12345), cfg.GitHub.App.AppID)
	assert.Equal(t, int64(67890), cfg.GitHub.App.InstallationID)
	assert.Equal(t, "/etc/keys/app.pem", cfg.GitHub.App.PrivateKeyPath)
	assert.Empty(t, cfg.GitHub.PAT.Token)
}

func TestLoad_GitHubAuthModePAT_Parses(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := `
boards:
  dir: ` + boardsDir + `
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`
	path := writeConfigFile(t, dir, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "pat", cfg.GitHub.AuthMode)
	assert.Equal(t, "ghp_test", cfg.GitHub.PAT.Token)
}

func TestLoad_BoardsHasNoAuthMode(t *testing.T) {
	cfg := defaults()
	v := reflect.TypeOf(cfg.Boards)
	for i := 0; i < v.NumField(); i++ {
		assert.NotEqual(t, "GitAuthMode", v.Field(i).Name,
			"BoardsConfig.GitAuthMode must be removed")
	}
}

func TestLoad_TaskSkills_Defaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := `
boards:
  dir: ` + boardsDir + `
github:
  auth_mode: "pat"
  pat:
    token: "x"
`
	path := writeConfigFile(t, dir, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "task-skills"), cfg.TaskSkills.Dir)
	assert.False(t, cfg.TaskSkills.GitCloneOnEmpty)
	assert.Empty(t, cfg.TaskSkills.GitRemoteURL)
}

func TestLoad_TaskSkills_Explicit(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := `
boards:
  dir: ` + boardsDir + `
github:
  auth_mode: "pat"
  pat:
    token: "x"
task_skills:
  dir: /opt/skills
  git_clone_on_empty: true
  git_remote_url: https://github.com/example/skills.git
`
	path := writeConfigFile(t, dir, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "/opt/skills", cfg.TaskSkills.Dir)
	assert.True(t, cfg.TaskSkills.GitCloneOnEmpty)
	assert.Equal(t, "https://github.com/example/skills.git", cfg.TaskSkills.GitRemoteURL)
}

func TestBuildSlogHandler_LevelEmptyDefaultsToInfo(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: ""}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Debug("debug msg")
	logger.Info("info msg")

	assert.NotContains(t, buf.String(), "debug msg")
	assert.Contains(t, buf.String(), "info msg")
}

func TestBuildSlogHandler_LevelWarn(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: "warn"}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Info("info msg")
	logger.Warn("warn msg")

	assert.NotContains(t, buf.String(), "info msg")
	assert.Contains(t, buf.String(), "warn msg")
}

func TestBuildSlogHandler_LevelError(t *testing.T) {
	cfg := &Config{LogFormat: "text", LogLevel: "error"}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Warn("warn msg")
	logger.Error("error msg")

	assert.NotContains(t, buf.String(), "warn msg")
	assert.Contains(t, buf.String(), "error msg")
}

func TestBuildSlogHandler_JSONEmitsValidStructure(t *testing.T) {
	cfg := &Config{LogFormat: "json", LogLevel: "info"}

	var buf bytes.Buffer

	h := cfg.BuildSlogHandler(&buf)

	logger := slog.New(h)
	logger.Info("hello world", "key", "value")

	output := buf.String()
	assert.Contains(t, output, `"msg"`)
	assert.Contains(t, output, `"hello world"`)
	assert.Contains(t, output, `"key"`)
	assert.Contains(t, output, `"value"`)
}

func TestLoad_ExampleFile_HasLogFields(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	cfg, err := Load(examplePath)
	require.NoError(t, err, "config.yaml.example must parse without error")

	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 0, cfg.AdminPort)
}

// TestConfigYamlExampleTokenCosts verifies that config.yaml.example ships with
// sane token cost entries for every supported model:
//   - every entry has both prompt and completion > 0 (non-zero rates)
//   - no rate is absurdly high (> $1000/M tokens = > 0.001/token) — catches unit errors
//   - the expected set of model keys is present
func TestConfigYamlExampleTokenCosts(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	cfg, err := Load(examplePath)
	require.NoError(t, err, "config.yaml.example must parse without error")

	require.NotEmpty(t, cfg.TokenCosts, "token_costs must not be empty in config.yaml.example")

	const maxRatePerToken = 0.001 // $1000 per million tokens — unit-error sentinel

	for model, cost := range cfg.TokenCosts {
		t.Run(model, func(t *testing.T) {
			assert.Greater(t, cost.Prompt, 0.0, "prompt rate must be > 0 for %s", model)
			assert.Greater(t, cost.Completion, 0.0, "completion rate must be > 0 for %s", model)
			assert.Less(t, cost.Prompt, maxRatePerToken, "prompt rate suspiciously high for %s (units error?)", model)
			assert.Less(t, cost.Completion, maxRatePerToken, "completion rate suspiciously high for %s (units error?)", model)
		})
	}

	// Assert the expected model keys are present. Update this list when new models ship.
	expectedModels := []string{
		"claude-haiku-4-5",
		"claude-sonnet-4-6",
		"claude-opus-4-6",
		"claude-opus-4-7",
	}

	for _, model := range expectedModels {
		_, ok := cfg.TokenCosts[model]
		assert.True(t, ok, "expected model %q to be present in token_costs", model)
	}
}
