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
	assert.Equal(t, filepath.Join(dir, "skills"), cfg.SkillsDir)
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
	assert.Equal(t, "", cfg.Boards.Dir)
	assert.True(t, cfg.Boards.GitAutoCommit)
	assert.False(t, cfg.Boards.GitAutoPush)
	assert.False(t, cfg.Boards.GitAutoPull)
	assert.Equal(t, "60s", cfg.Boards.GitPullInterval)
	assert.False(t, cfg.Boards.GitDeferredCommit)
	assert.False(t, cfg.Boards.GitCloneOnEmpty)
	assert.Equal(t, "", cfg.Boards.GitRemoteURL)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)
	assert.Equal(t, "", cfg.SkillsDir)
	assert.Equal(t, "", cfg.MCPAPIKey)
	assert.False(t, cfg.Runner.Enabled)
	assert.Equal(t, "", cfg.Runner.URL)
	assert.Equal(t, "", cfg.Runner.APIKey)
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
  public_url: "http://contextmatrix:8080"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "test-mcp-key", cfg.MCPAPIKey)
	assert.True(t, cfg.Runner.Enabled)
	assert.Equal(t, "http://localhost:9090", cfg.Runner.URL)
	assert.Equal(t, "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh", cfg.Runner.APIKey)
	assert.Equal(t, "http://contextmatrix:8080", cfg.Runner.PublicURL)
}

func TestLoad_RunnerDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.Runner.Enabled)
	assert.Equal(t, "", cfg.Runner.URL)
	assert.Equal(t, "", cfg.Runner.APIKey)
	assert.Equal(t, "", cfg.MCPAPIKey)
}

func TestValidate_RunnerEnabledMissingURL(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, APIKey: "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh", PublicURL: "http://cm:8080"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.url is required")
}

func TestValidate_RunnerEnabledMissingAPIKey(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, URL: "http://localhost:9090", PublicURL: "http://cm:8080"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.api_key is required")
}

func TestValidate_RunnerEnabledShortAPIKey(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: "too-short", PublicURL: "http://cm:8080"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.api_key must be at least")
}

func TestValidate_RunnerEnabledMissingPublicURL(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		Runner:           RunnerConfig{Enabled: true, URL: "http://localhost:9090", APIKey: "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.public_url is required")
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
	t.Setenv("CONTEXTMATRIX_RUNNER_PUBLIC_URL", "http://contextmatrix:8080")
	t.Setenv("CONTEXTMATRIX_MCP_API_KEY", "env-mcp-key")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Runner.Enabled)
	assert.Equal(t, "http://runner:9090", cfg.Runner.URL)
	assert.Equal(t, "a]3kF#9xL!mQ7nR$2pW^8vZ&5jB+0dYh", cfg.Runner.APIKey)
	assert.Equal(t, "http://contextmatrix:8080", cfg.Runner.PublicURL)
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

func TestLoad_SkillsDirDerivedFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "skills"), cfg.SkillsDir)
}

func TestLoad_SkillsDirExplicitInYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nskills_dir: /opt/skills\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "/opt/skills", cfg.SkillsDir)
}

func TestLoad_SkillsDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")
	t.Setenv("CONTEXTMATRIX_SKILLS_DIR", "/custom/skills")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "/custom/skills", cfg.SkillsDir)
}

func TestLoad_SkillsDirTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nskills_dir: \"~/my-skills\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "my-skills"), cfg.SkillsDir)
}

func TestLoad_SkillsDirMissingFileDerivedFromConfigPath(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	t.Setenv("CONTEXTMATRIX_BOARDS_DIR", boardsDir)

	// Load from a nonexistent file — skills_dir derived from its directory
	cfg, err := Load(filepath.Join(dir, "nonexistent.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "skills"), cfg.SkillsDir)
}

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
	assert.Equal(t, "", cfg.Boards.GitRemoteURL)
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

func TestLoad_ExampleFile(t *testing.T) {
	// config.yaml.example lives in the repo root, two directories above this
	// package (internal/config → repo root).
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	cfg, err := Load(examplePath)
	require.NoError(t, err, "config.yaml.example must parse and validate without error")

	// Verify key field values match the documented defaults in the example file.
	assert.Equal(t, 8080, cfg.Port)
	assert.False(t, cfg.Boards.GitDeferredCommit)
	assert.True(t, cfg.Boards.GitAutoCommit)
	assert.False(t, cfg.Boards.GitAutoPush)
	assert.False(t, cfg.Boards.GitAutoPull)
	assert.Equal(t, "60s", cfg.Boards.GitPullInterval)
	assert.Equal(t, "30m", cfg.HeartbeatTimeout)
	assert.Equal(t, "http://localhost:5173", cfg.CORSOrigin)

	// boards.dir must be non-empty (tilde-expanded from ~/contextmatrix-boards).
	assert.NotEmpty(t, cfg.Boards.Dir)

	// heartbeat_timeout must be a valid duration.
	d, err := cfg.HeartbeatDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, d)

	// token_costs section should have at least one entry.
	assert.NotEmpty(t, cfg.TokenCosts)

	// GitHub issue importing should be disabled by default in the example file.
	assert.False(t, cfg.GitHub.IssueImporting.Enabled)
	assert.Equal(t, "5m", cfg.GitHub.IssueImporting.SyncInterval)
	assert.Equal(t, "", cfg.GitHub.Token)
}

// ---------- GitHub issue importing config tests ----------

func TestLoad_GitHubIssueImporting_Enabled(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  token: "ghp_test_token"
  issue_importing:
    enabled: true
    sync_interval: "10m"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "ghp_test_token", cfg.GitHub.Token)
	assert.True(t, cfg.GitHub.IssueImporting.Enabled)
	assert.Equal(t, "10m", cfg.GitHub.IssueImporting.SyncInterval)
}

func TestLoad_GitHubIssueImporting_Disabled(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Token set but issue importing explicitly disabled — should still load cleanly.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  token: "ghp_test_token"
  issue_importing:
    enabled: false
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "ghp_test_token", cfg.GitHub.Token)
	assert.False(t, cfg.GitHub.IssueImporting.Enabled)
}

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

func TestLoad_GitHubIssueImporting_NoTokenNoError_WhenDisabled(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// issue_importing disabled and no token — should not error.
	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "", cfg.GitHub.Token)
	assert.False(t, cfg.GitHub.IssueImporting.Enabled)
}

func TestValidate_GitHubIssueImporting_EnabledWithoutToken(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			Token: "",
			IssueImporting: IssueImportingConfig{
				Enabled:      true,
				SyncInterval: "5m",
			},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.token is required when github.issue_importing.enabled is true")
}

func TestValidate_GitHubIssueImporting_InvalidSyncInterval(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			Token: "ghp_test",
			IssueImporting: IssueImportingConfig{
				Enabled:      true,
				SyncInterval: "notaduration",
			},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid github.issue_importing.sync_interval")
}

func TestValidate_GitHubIssueImporting_SyncIntervalTooShort(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			Token: "ghp_test",
			IssueImporting: IssueImportingConfig{
				Enabled:      true,
				SyncInterval: "1m",
			},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.issue_importing.sync_interval must be at least 5m")
}

func TestValidate_GitHubIssueImporting_ValidConfig(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			Token: "ghp_test",
			IssueImporting: IssueImportingConfig{
				Enabled:      true,
				SyncInterval: "5m",
			},
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_GitHubIssueImporting_TokenWithoutEnabled_NoError(t *testing.T) {
	// Token present but issue importing disabled — valid (token used for branches).
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			Token: "ghp_test",
			IssueImporting: IssueImportingConfig{
				Enabled: false,
			},
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestLoad_GitHubIssueImporting_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	t.Setenv("CONTEXTMATRIX_GITHUB_TOKEN", "ghp_env_token")
	t.Setenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED", "true")
	t.Setenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_SYNC_INTERVAL", "15m")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "ghp_env_token", cfg.GitHub.Token)
	assert.True(t, cfg.GitHub.IssueImporting.Enabled)
	assert.Equal(t, "15m", cfg.GitHub.IssueImporting.SyncInterval)
}

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

func TestDefaults_GitHubIssueImporting(t *testing.T) {
	cfg := defaults()

	assert.Equal(t, "", cfg.GitHub.Token)
	assert.False(t, cfg.GitHub.IssueImporting.Enabled)
	assert.Equal(t, "", cfg.GitHub.IssueImporting.SyncInterval)
}

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

	assert.Equal(t, "", cfg.GitHub.Host)
	assert.Equal(t, "", cfg.GitHub.APIBaseURL)
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
	assert.Equal(t, "", cfg.GitHub.Host)
	assert.Equal(t, "", cfg.GitHub.APIBaseURL)
}

// ---------- GitAuthMode tests ----------

func TestDefaults_GitAuthMode(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, "ssh", cfg.Boards.GitAuthMode)
}

func TestLoad_GitAuthMode_DefaultIsSSH(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ssh", cfg.Boards.GitAuthMode)
}

func TestLoad_GitAuthMode_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_remote_url: "https://github.com/user/boards.git"
github:
  token: "ghp_test_pat_token"
`)

	t.Setenv("CONTEXTMATRIX_BOARDS_GIT_AUTH_MODE", "pat")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "pat", cfg.Boards.GitAuthMode)
}

func TestValidate_GitAuthMode_UnknownValueRejected(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "typo", mode: "SSH"},
		{name: "kebab", mode: "ssh-key"},
		{name: "empty via explicit set", mode: "token"},
		{name: "garbage", mode: "ftp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Boards:           BoardsConfig{Dir: "/some/path", GitAuthMode: tt.mode},
				HeartbeatTimeout: "30m",
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid boards.git_auth_mode")
		})
	}
}

func TestValidate_GitAuthMode_PATMissingToken(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:          "/some/path",
			GitAuthMode:  "pat",
			GitRemoteURL: "https://github.com/user/boards.git",
		},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{Token: ""},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.token is required when boards.git_auth_mode is \"pat\"")
}

func TestValidate_GitAuthMode_PATWithSSHURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "ssh scheme", url: "ssh://git@github.com/user/boards.git"},
		{name: "scp-style", url: "git@github.com:user/boards.git"},
		{name: "empty URL", url: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Boards: BoardsConfig{
					Dir:          "/some/path",
					GitAuthMode:  "pat",
					GitRemoteURL: tt.url,
				},
				HeartbeatTimeout: "30m",
				GitHub:           GitHubConfig{Token: "ghp_test_token"},
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "boards.git_remote_url must start with https://")
		})
	}
}

func TestValidate_GitAuthMode_PATWithHTTPSURLAndToken(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:          "/some/path",
			GitAuthMode:  "pat",
			GitRemoteURL: "https://github.com/user/boards.git",
		},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{Token: "ghp_test_valid_token"},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_GitAuthMode_SSHIsValidWithAnyRemote(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "scp-style", url: "git@github.com:user/boards.git"},
		{name: "ssh scheme", url: "ssh://git@github.com/user/boards.git"},
		{name: "https", url: "https://github.com/user/boards.git"},
		{name: "empty", url: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Boards: BoardsConfig{
					Dir:          "/some/path",
					GitAuthMode:  "ssh",
					GitRemoteURL: tt.url,
				},
				HeartbeatTimeout: "30m",
			}
			err := cfg.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestLoad_GitAuthMode_YAMLField(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_auth_mode: "ssh"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ssh", cfg.Boards.GitAuthMode)
}

func TestLoad_GitAuthMode_PATFromYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_remote_url: "https://github.com/user/boards.git"
  git_auth_mode: "pat"
github:
  token: "ghp_yaml_pat_token"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "pat", cfg.Boards.GitAuthMode)
	assert.Equal(t, "https://github.com/user/boards.git", cfg.Boards.GitRemoteURL)
}

func TestLoad_GitAuthMode_ExampleFileHasSSHDefault(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	cfg, err := Load(examplePath)
	require.NoError(t, err, "config.yaml.example must parse and validate without error")
	assert.Equal(t, "ssh", cfg.Boards.GitAuthMode)
}

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
	for _, theme := range []string{"everforest", "radix"} {
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
