package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// loadFromYAML writes yamlContent to a temp config file and loads it.
func loadFromYAML(t *testing.T, yamlContent string) (*Config, error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	return Load(path)
}

// loadConfigFromYAML loads a config from a minimal always-valid base
// (boards.dir + github.auth_mode pre-filled so Validate never fails on
// unrelated required fields) plus an extra top-level YAML fragment. Fails
// the test on load error. Lets single-block tests pass just the fragment
// they care about instead of repeating the mandatory boilerplate.
func loadConfigFromYAML(t *testing.T, extra string) *Config {
	t.Helper()

	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	yamlContent := "boards:\n  dir: " + boardsDir + "\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n" + extra

	cfg, err := loadFromYAML(t, yamlContent)
	require.NoError(t, err)

	return cfg
}

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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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

	// Minimal config: only set required fields; everything else should be defaults.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	cfg, err := Load(path)
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
			envValue: "https://github.com/user/boards.git",
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "https://github.com/user/boards.git", cfg.Boards.GitRemoteURL)
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
				GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid heartbeat_timeout")
		})
	}
}

func TestValidate_RejectsNegativeChatIdleTTL(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
		Chat:             ChatConfig{IdleTTL: -time.Minute, MaxConcurrent: 5},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat.idle_ttl")
}

func TestValidate_AcceptsZeroChatIdleTTL(t *testing.T) {
	// Zero IdleTTL means "use the default" — applyChatDefaults bumps it
	// inside Validate so callers that bypass Load still get the default.
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
		Chat:             ChatConfig{IdleTTL: 0, MaxConcurrent: 5},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, time.Hour, cfg.Chat.IdleTTL, "Validate must apply the default IdleTTL")
}

func TestValidate_RejectsNegativeChatMaxConcurrent(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
		Chat:             ChatConfig{IdleTTL: time.Hour, MaxConcurrent: -1},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat.max_concurrent")
}

func TestValidate_AcceptsZeroChatMaxConcurrent(t *testing.T) {
	// MaxConcurrent=0 means "unlimited" per the existing applyChatDefaults
	// semantics; only negative values are rejected.
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
		Chat:             ChatConfig{IdleTTL: time.Hour, MaxConcurrent: 0},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "test-boards"), cfg.Boards.Dir)
}

func TestLoad_TildeExpansion_MissingFile(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards:
  dir: "~/env-boards"
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	cfg, err := Load(path)
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

	// Only set required fields; everything else should be defaults.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
	assert.Empty(t, got, "FindConfigPath must return empty string when no config file is found")
}

func TestFindConfigPath_XDGSetButNoFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty-xdg"))

	got := FindConfigPath()
	assert.Empty(t, got, "FindConfigPath must return empty string when XDG dir has no config file")
}

func TestLoad_WorkflowSkillsDirDerivedFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "workflow-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirExplicitInYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nworkflow_skills_dir: /opt/skills\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "/opt/skills", cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")
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

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\nworkflow_skills_dir: \"~/my-skills\"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "my-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_WorkflowSkillsDirMissingFileDerivedFromConfigPath(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// Use a config file with required auth_mode — workflow_skills_dir derived from its directory
	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "workflow-skills"), cfg.WorkflowSkillsDir)
}

func TestLoad_TaskSkillsDirTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := `
boards: {dir: ` + boardsDir + `}
github: {auth_mode: "pat", pat: {token: "x"}}
task_skills: {dir: "~/my-skills"}
`
	path := writeConfigFile(t, dir, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "my-skills"), cfg.TaskSkills.Dir)
}

func TestLoad_CloneOnEmptyFields(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
  git_clone_on_empty: true
  git_remote_url: "https://github.com/user/boards.git"
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Boards.GitCloneOnEmpty)
	assert.Equal(t, "https://github.com/user/boards.git", cfg.Boards.GitRemoteURL)
}

func TestLoad_CloneOnEmptyDefaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.Boards.GitCloneOnEmpty)
	assert.Empty(t, cfg.Boards.GitRemoteURL)
}

func TestLoad_CloneOnEmptyEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	t.Setenv("CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY", "true")
	t.Setenv("CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL", "https://github.com/user/boards.git")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Boards.GitCloneOnEmpty)
	assert.Equal(t, "https://github.com/user/boards.git", cfg.Boards.GitRemoteURL)
}

func TestValidate_CloneOnEmptyWithoutRemoteURL(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitCloneOnEmpty: true,
			GitRemoteURL:    "",
		},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
			GitRemoteURL:    "https://github.com/user/boards.git",
		},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_RemoteURLWithoutCloneOnEmpty(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{
			Dir:             "/some/path",
			GitCloneOnEmpty: false,
			GitRemoteURL:    "https://github.com/user/boards.git",
		},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

// ---------- GitHub issue importing config tests ----------

func TestLoad_GitHubIssueImporting_DefaultSyncInterval(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	// No sync_interval specified — should default to "5m" during Validate.
	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  auth_mode: "pat"
  pat:
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

func TestLoad_GitHubIssueImporting_EnvEnabled1(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

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
  auth_mode: "pat"
  pat:
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
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Empty(t, cfg.GitHub.Host)
	assert.Empty(t, cfg.GitHub.APIBaseURL)
}

func TestLoad_GitHubHostAndAPIBaseURL_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

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
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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

// ---------- Theme config tests ----------

func TestLoad_Theme_DefaultIsEverforest(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid theme")
}

func TestLoad_Theme_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, "boards:\n  dir: "+boardsDir+"\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n")

	t.Setenv("CONTEXTMATRIX_THEME", "radix")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "radix", cfg.Theme)
}

func TestValidate_Theme_InvalidValue(t *testing.T) {
	cfg := &Config{
		Boards:           BoardsConfig{Dir: "/some/path"},
		HeartbeatTimeout: "30m",
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
				GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
				GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
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
		GitHub:           GitHubConfig{AuthMode: "pat", PAT: GitHubPATConfig{Token: "x"}},
		AdminPort:        9091,
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "127.0.0.1", cfg.AdminBindAddr)
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
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

// ---------- auth_mode discriminator validation tests (Task 3) ----------

func TestValidate_AuthModeMissing(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{Dir: "/tmp/x"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.auth_mode is required")
}

func TestValidate_AuthModeInvalid(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{Dir: "/tmp/x"},
		GitHub: GitHubConfig{AuthMode: "bogus"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.auth_mode")
}

func TestValidate_AppMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		app     GitHubAppConfig
		wantMsg string
	}{
		{"no app_id", GitHubAppConfig{InstallationID: 1, PrivateKeyPath: "/k"}, "app_id is required"},
		{"no installation_id", GitHubAppConfig{AppID: 1, PrivateKeyPath: "/k"}, "installation_id is required"},
		{"no private_key_path", GitHubAppConfig{AppID: 1, InstallationID: 1}, "private_key_path is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Boards: BoardsConfig{Dir: "/tmp/x"},
				GitHub: GitHubConfig{AuthMode: "app", App: tc.app},
			}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestValidate_PATMissingToken(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{Dir: "/tmp/x"},
		GitHub: GitHubConfig{AuthMode: "pat"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pat.token is required")
}

func TestLoad_ExampleFile_HasLogFields(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	// The example ships with auth_mode:"app" and placeholder zeros. Override to
	// PAT mode so Validate() passes without real App credentials.
	t.Setenv("CONTEXTMATRIX_GITHUB_AUTH_MODE", "pat")
	t.Setenv("CONTEXTMATRIX_GITHUB_PAT_TOKEN", "test-token")

	cfg, err := Load(examplePath)
	require.NoError(t, err, "config.yaml.example must parse without error")

	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 0, cfg.AdminPort)
}

func TestValidate_AppMode_RejectsPATToken(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{Dir: "/tmp/x"},
		GitHub: GitHubConfig{
			AuthMode: "app",
			App: GitHubAppConfig{
				AppID: 1, InstallationID: 1, PrivateKeyPath: "/k",
			},
			PAT: GitHubPATConfig{Token: "leak"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pat.token must be empty")
}

func TestValidate_PATMode_RejectsAppFields(t *testing.T) {
	cfg := &Config{
		Boards: BoardsConfig{Dir: "/tmp/x"},
		GitHub: GitHubConfig{
			AuthMode: "pat",
			PAT:      GitHubPATConfig{Token: "x"},
			App:      GitHubAppConfig{AppID: 99},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.app.* must be empty")
}

// TestConfigYamlExampleTokenCosts verifies that config.yaml.example ships with
// sane token cost entries for every supported model:
//   - every entry has both prompt and completion > 0 (non-zero rates)
//   - no rate is absurdly high (> $1000/M tokens = > 0.001/token) — catches unit errors
//   - the expected set of model keys is present
func TestConfigYamlExampleTokenCosts(t *testing.T) {
	examplePath := filepath.Join("..", "..", "config.yaml.example")

	// The example ships with auth_mode:"app" and placeholder zeros. Override to
	// PAT mode so Validate() passes without real App credentials.
	t.Setenv("CONTEXTMATRIX_GITHUB_AUTH_MODE", "pat")
	t.Setenv("CONTEXTMATRIX_GITHUB_PAT_TOKEN", "test-token")

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
		"claude-opus-4-8",
	}

	for _, model := range expectedModels {
		_, ok := cfg.TokenCosts[model]
		assert.True(t, ok, "expected model %q to be present in token_costs", model)
	}
}

// ---------- HTTPS-only URL validation tests (Task 5) ----------

// validBaseConfig returns a Config that passes Validate() except for the
// field under test.
func validBaseConfig(t *testing.T) *Config {
	t.Helper()

	return &Config{
		Boards:           BoardsConfig{Dir: "/tmp/boards"},
		HeartbeatTimeout: "30m",
		GitHub: GitHubConfig{
			AuthMode: "pat",
			PAT:      GitHubPATConfig{Token: "x"},
		},
	}
}

func TestValidate_BoardsRemoteURLNotHTTPS(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.Boards.GitRemoteURL = "ssh://git@github.com/foo/bar.git"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards.git_remote_url must start with https://")
}

func TestValidate_BoardsRemoteURL_HTTPS_OK(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.Boards.GitRemoteURL = "https://github.com/foo/bar.git"
	require.NoError(t, cfg.Validate())
}

func TestValidate_BoardsCloneOnEmptyRequiresURL(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.Boards.GitCloneOnEmpty = true
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boards.git_remote_url is required when boards.git_clone_on_empty")
}

func TestValidate_TaskSkillsRemoteURLNotHTTPS(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.TaskSkills.GitRemoteURL = "git@github.com:foo/bar.git"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task_skills.git_remote_url must start with https://")
}

func TestValidate_TaskSkillsCloneOnEmptyRequiresURL(t *testing.T) {
	cfg := validBaseConfig(t)
	cfg.TaskSkills.GitCloneOnEmpty = true
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task_skills.git_remote_url is required when task_skills.git_clone_on_empty")
}

// ---------- Env-var overrides for new fields (Task 6) ----------

func TestEnvOverrides_GitHub(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_GITHUB_AUTH_MODE", "app")
	t.Setenv("CONTEXTMATRIX_GITHUB_APP_ID", "111")
	t.Setenv("CONTEXTMATRIX_GITHUB_INSTALLATION_ID", "222")
	t.Setenv("CONTEXTMATRIX_GITHUB_PRIVATE_KEY_PATH", "/etc/k.pem")
	t.Setenv("CONTEXTMATRIX_GITHUB_PAT_TOKEN", "")

	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `boards: {dir: `+boardsDir+`}`)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "app", cfg.GitHub.AuthMode)
	assert.Equal(t, int64(111), cfg.GitHub.App.AppID)
	assert.Equal(t, int64(222), cfg.GitHub.App.InstallationID)
	assert.Equal(t, "/etc/k.pem", cfg.GitHub.App.PrivateKeyPath)
}

func TestEnvOverrides_GitHubPAT(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_GITHUB_AUTH_MODE", "pat")
	t.Setenv("CONTEXTMATRIX_GITHUB_PAT_TOKEN", "ghp_env")

	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `boards: {dir: `+boardsDir+`}`)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "pat", cfg.GitHub.AuthMode)
	assert.Equal(t, "ghp_env", cfg.GitHub.PAT.Token)
}

func TestEnvOverrides_TaskSkills(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_TASK_SKILLS_DIR", "/var/skills")
	t.Setenv("CONTEXTMATRIX_TASK_SKILLS_GIT_REMOTE_URL", "https://github.com/x/y.git")
	t.Setenv("CONTEXTMATRIX_TASK_SKILLS_GIT_CLONE_ON_EMPTY", "true")

	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "/var/skills", cfg.TaskSkills.Dir)
	assert.Equal(t, "https://github.com/x/y.git", cfg.TaskSkills.GitRemoteURL)
	assert.True(t, cfg.TaskSkills.GitCloneOnEmpty)
}

// ---------- Chat config tests ----------

func TestLoadConfig_ChatDefaults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("boards:\n  dir: /tmp/boards\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n"), 0o644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, time.Hour, cfg.Chat.IdleTTL, "default idle TTL should be 1h")
	assert.Equal(t, 0, cfg.Chat.MaxConcurrent, "unset max_concurrent should be 0 (unlimited)")
	// Chat data lives in the operational store; its DB path is derived there.
	assert.NotEmpty(t, cfg.OpStore.DBPath, "default op-store db path should be derived")
}

func TestLoadConfig_ChatEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)

	t.Setenv("CONTEXTMATRIX_CHAT_IDLE_TTL", "30m")
	t.Setenv("CONTEXTMATRIX_CHAT_MAX_CONCURRENT", "10")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 30*time.Minute, cfg.Chat.IdleTTL)
	assert.Equal(t, 10, cfg.Chat.MaxConcurrent)
}

func TestLoadConfig_ChatInvalidIdleTTL_Ignored(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)

	t.Setenv("CONTEXTMATRIX_CHAT_IDLE_TTL", "notaduration")

	cfg, err := Load(path)
	require.NoError(t, err)
	// Should retain the default value since the env override was invalid.
	assert.Equal(t, time.Hour, cfg.Chat.IdleTTL)
}

func TestLoadConfig_ChatInvalidMaxConcurrent_Ignored(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)

	t.Setenv("CONTEXTMATRIX_CHAT_MAX_CONCURRENT", "abc")

	cfg, err := Load(path)
	require.NoError(t, err)
	// YAML did not set max_concurrent → field is zero (unlimited); the invalid
	// env override is ignored, so the zero value is preserved.
	assert.Equal(t, 0, cfg.Chat.MaxConcurrent)
}

func TestLoadConfig_ChatYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
chat:
  idle_ttl: 2h
  max_concurrent: 3
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Hour, cfg.Chat.IdleTTL)
	assert.Equal(t, 3, cfg.Chat.MaxConcurrent)
}

// ---------- Operational store (ops.db) config tests ----------
//
// The operational store holds both the chat schema and the model blacklist in
// a single ops.db. These tests cover the default-derived path, the env
// override, the YAML field, and the XDG-state-home default.

func TestLoadConfig_OpStoreEnvOverride(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)

	t.Setenv("CONTEXTMATRIX_OP_STORE_DB_PATH", "/var/lib/contextmatrix/ops.db")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "/var/lib/contextmatrix/ops.db", cfg.OpStore.DBPath)
}

func TestLoadConfig_OpStoreYAML(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
op_store:
  db_path: /custom/ops.db
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "/custom/ops.db", cfg.OpStore.DBPath)
}

func TestLoadConfig_OpStoreDBPath_XDGStateHome(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, `
boards: {dir: `+boardsDir+`}
github: {auth_mode: "pat", pat: {token: "x"}}
`)

	stateDir := filepath.Join(dir, "state")
	t.Setenv("XDG_STATE_HOME", stateDir)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(stateDir, "contextmatrix", "ops.db"), cfg.OpStore.DBPath)
}

// ---------- Backends config tests ----------

// minValidBase returns a minimal YAML string that passes Validate() on its own.
// Tests append their own backends stanzas.
func minValidBase(boardsDir string) string {
	return `boards:
  dir: ` + boardsDir + `
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`
}

// validBackendsBlock is the canonical backends YAML block: a single enabled
// agent entry. Used by the "valid agent entry" case and by
// TestBackendsConfigDefaults.
const validBackendsBlock = `
backends:
  agent:
    url: http://localhost:9090
    api_key: "0123456789abcdef0123456789abcdef"
`

func TestBackendsConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string // empty = Load must succeed
	}{
		{
			name:    "valid agent entry",
			yaml:    validBackendsBlock,
			wantErr: "",
		},
		{
			name: "short api key",
			yaml: `
backends:
  agent:
    url: http://localhost:9090
    api_key: "short"
`,
			wantErr: "api_key",
		},
		{
			name: "missing url",
			yaml: `
backends:
  agent:
    url: ""
    api_key: "0123456789abcdef0123456789abcdef"
`,
			wantErr: "url",
		},
		{
			// Unknown names fail during Load's YAML parse (Backends.UnmarshalYAML),
			// not Validate.
			name: "unknown backend name",
			yaml: `
backends:
  foo:
    url: http://localhost:9090
    api_key: "0123456789abcdef0123456789abcdef"
`,
			wantErr: `invalid backend name "foo": must be one of "agent", "chat"`,
		},
		{
			// Unknown name rejected even when disabled (typo guard).
			name: "unknown name disabled entry still rejected",
			yaml: `
backends:
  typo:
    enabled: false
`,
			wantErr: "backend name",
		},
		{
			// A valid agent entry does not shield an unknown sibling.
			name: "valid agent entry plus unknown name",
			yaml: `
backends:
  agent:
    url: http://localhost:9090
    api_key: "0123456789abcdef0123456789abcdef"
  unknown: {}
`,
			wantErr: `invalid backend name "unknown"`,
		},
		{
			name:    "no backends configured",
			yaml:    "",
			wantErr: "",
		},
		{
			// agent + chat → valid (the two backends serve disjoint roles).
			name: "agent and chat both enabled",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "0123456789abcdef0123456789abcdef"
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    default_model: "anthropic/claude-sonnet-4"
`,
			wantErr: "",
		},
		{
			// Disabled entry with missing url/api_key → valid (incomplete
			// placeholder is fine when disabled).
			name: "disabled entry with missing url",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "0123456789abcdef0123456789abcdef"
  chat:
    enabled: false
`,
			wantErr: "",
		},
		{
			// ChatBackendConfig has no reconcile_interval field by
			// construction; the strict per-entry decode rejects it as an
			// unknown field during Load's YAML parse.
			name: "reconcile_interval on chat entry",
			yaml: `
backends:
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    reconcile_interval: "30s"
`,
			wantErr: "field reconcile_interval not found",
		},
		{
			// The value never matters — the field itself is unknown.
			name: "unparseable reconcile_interval on chat entry",
			yaml: `
backends:
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    reconcile_interval: "bad-value"
`,
			wantErr: "field reconcile_interval not found",
		},
		{
			name: "orchestrator_sonnet_model on chat entry",
			yaml: `
backends:
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    orchestrator_sonnet_model: "claude-sonnet-4-6"
`,
			wantErr: "field orchestrator_sonnet_model not found",
		},
		{
			// default_model is agent-only; parses cleanly on an agent entry.
			name: "default_model parses on agent entry",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "0123456789abcdef0123456789abcdef"
    default_model: "deepseek/deepseek-v4-flash"
`,
			wantErr: "",
		},
		{
			// default_model IS allowed on the chat entry (it is the OpenRouter
			// slug CM supplies as CM_MODEL); satisfies the required check too.
			name: "default_model on chat entry",
			yaml: `
backends:
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    default_model: "deepseek/deepseek-v4-flash"
`,
			wantErr: "",
		},
		{
			// orchestrator_sonnet_model was a knob of the removed runner
			// backend; unknown field on the agent entry (agent uses
			// default_model), rejected by the strict per-entry decode.
			name: "orchestrator_sonnet_model on agent entry",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "0123456789abcdef0123456789abcdef"
    orchestrator_sonnet_model: "anthropic/claude-sonnet-4-6"
`,
			wantErr: "field orchestrator_sonnet_model not found",
		},
		{
			// orchestrator_opus_model was a knob of the removed runner
			// backend; unknown field on the agent entry.
			name: "orchestrator_opus_model on agent entry",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "0123456789abcdef0123456789abcdef"
    orchestrator_opus_model: "anthropic/claude-opus-4-8"
`,
			wantErr: "field orchestrator_opus_model not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+tc.yaml)

			_, err := Load(path)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestValidateRejectsRunnerBackend pins the removal of the runner backend:
// any declared backends.runner entry — enabled or not — fails Load with the
// dedicated removal error, never a silent skip or a generic name error. The
// rejection fires during Load's YAML parse (Backends.UnmarshalYAML carries
// the closed key set), not in Validate.
func TestValidateRejectsRunnerBackend(t *testing.T) {
	apiKey := strings.Repeat("k", 32)

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "enabled runner entry",
			yaml: `
backends:
  runner:
    url: http://localhost:9090
    api_key: "` + apiKey + `"
`,
		},
		{
			// The name check applies to ALL entries, enabled or not — a
			// disabled leftover entry must still fail loudly.
			name: "disabled runner entry still rejected",
			yaml: `
backends:
  runner:
    enabled: false
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+tc.yaml)

			_, err := Load(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "the runner backend has been removed")
		})
	}
}

// TestBackendEntryUnknownFieldFailsAtLoad pins the strict per-entry decode:
// entries are decoded with KnownFields(true), so a stale or typo'd field on
// either backend entry fails Load's YAML parse loudly instead of being
// silently dropped. This preserves the migration-tripwire posture at the
// field level (e.g. a leftover backends.chat.reconcile_interval).
func TestBackendEntryUnknownFieldFailsAtLoad(t *testing.T) {
	apiKey := strings.Repeat("k", 32)

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "stale reconcile_interval on chat entry",
			yaml: `
backends:
  chat:
    url: http://localhost:9092
    api_key: "` + apiKey + `"
    default_model: "anthropic/claude-sonnet-4"
    reconcile_interval: "60s"
`,
			wantErr: "backends.chat",
		},
		{
			name: "typo'd field on agent entry",
			yaml: `
backends:
  agent:
    url: http://localhost:9091
    api_key: "` + apiKey + `"
    models_allowlist: ["deepseek"]
`,
			wantErr: "backends.agent",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+tc.yaml)

			_, err := Load(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "not found in type")
		})
	}
}

// TestConfigExampleParses pins config.yaml.example against the Config schema:
// the shipped template must stay byte-compatible with the strict
// KnownFields(true) decode Load performs, including the typed backends
// mapping.
func TestConfigExampleParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.yaml.example"))
	require.NoError(t, err)

	cfg := defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	require.NoError(t, dec.Decode(cfg), "config.yaml.example must decode cleanly")
}

func TestBackendsConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir)+validBackendsBlock)

	cfg, err := Load(path)
	require.NoError(t, err)

	// Per-entry defaults filled by applyBackendDefaults on the enabled agent
	// entry: reconcile interval only.
	b := cfg.Backends.Agent
	require.NotNil(t, b)
	assert.Equal(t, "60s", b.ReconcileInterval)

	// AgentBackend: agent present+enabled → returns the entry.
	tb, ok := cfg.AgentBackend()
	require.True(t, ok)
	assert.Equal(t, "http://localhost:9090", tb.URL)

	// ChatBackend: the agent backend does not serve chat.
	_, ok = cfg.ChatBackend()
	assert.False(t, ok)

	// No backends → both accessors return false.
	emptyDir := t.TempDir()
	emptyBoardsDir := t.TempDir()
	emptyCfg, err := Load(writeConfigFile(t, emptyDir, minValidBase(emptyBoardsDir)))
	require.NoError(t, err)

	_, ok = emptyCfg.AgentBackend()
	assert.False(t, ok)

	_, ok = emptyCfg.ChatBackend()
	assert.False(t, ok)
}

func TestBackendsConfigDefaults_ChatEntryParses(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  chat:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    default_model: "anthropic/claude-sonnet-4"
`
	path := writeConfigFile(t, dir, yaml)

	cfg, err := Load(path)
	require.NoError(t, err)

	// The chat entry resolves; no agent entry appears as a side effect (the
	// old map code stamped task defaults per entry — the typed chat entry has
	// no task knobs by construction).
	cb, ok := cfg.ChatBackend()
	require.True(t, ok)
	assert.Equal(t, "http://localhost:9092", cb.URL)
	assert.Nil(t, cfg.Backends.Agent)
}

// TestBackendsConfigDefaults_AgentReconcileInterval pins the agent entry's
// reconcile default: CM's backend-agnostic sweep is the agent backend's ONLY
// reconcile mechanism (docs/remote-execution.md), so an enabled agent
// entry defaults reconcile_interval to 60s.
func TestBackendsConfigDefaults_AgentReconcileInterval(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  agent:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
`
	path := writeConfigFile(t, dir, yaml)

	cfg, err := Load(path)
	require.NoError(t, err)

	b := cfg.Backends.Agent
	require.NotNil(t, b)
	assert.Equal(t, "60s", b.ReconcileInterval)

	tb, ok := cfg.AgentBackend()
	require.True(t, ok)
	assert.Equal(t, 60*time.Second, tb.ReconcileIntervalDuration())
}

// TestBackendsConfigDefaults_AgentReconcileIntervalVariants pins that an
// explicit value wins over the default and a disabled entry gets no default.
func TestBackendsConfigDefaults_AgentReconcileIntervalVariants(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "explicit value wins",
			yaml: `
backends:
  agent:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    reconcile_interval: "5m"
`,
			want: "5m",
		},
		{
			name: "disabled entry gets no default",
			yaml: `
backends:
  agent:
    url: http://localhost:9092
    api_key: "0123456789abcdef0123456789abcdef"
    enabled: false
`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+tc.yaml)

			cfg, err := Load(path)
			require.NoError(t, err)
			require.NotNil(t, cfg.Backends.Agent)
			assert.Equal(t, tc.want, cfg.Backends.Agent.ReconcileInterval)
		})
	}
}

func TestBackendsRoleDerivation(t *testing.T) {
	apiKey := strings.Repeat("k", 32)

	cases := []struct {
		name        string
		yaml        string
		wantTaskURL string
		wantTaskOK  bool
		wantChatURL string
		wantChatOK  bool
	}{
		{
			name: "agent only",
			yaml: `backends:
  agent:
    url: http://a:9091
    api_key: "` + apiKey + `"
`,
			wantTaskURL: "http://a:9091", wantTaskOK: true,
			wantChatOK: false,
		},
		{
			name: "chat only",
			yaml: `backends:
  chat:
    url: http://c:9092
    api_key: "` + apiKey + `"
    default_model: "anthropic/claude-sonnet-4"
`,
			wantTaskOK:  false,
			wantChatURL: "http://c:9092", wantChatOK: true,
		},
		{
			name: "agent and chat",
			yaml: `backends:
  agent:
    url: http://a:9091
    api_key: "` + apiKey + `"
  chat:
    url: http://c:9092
    api_key: "` + apiKey + `"
    default_model: "anthropic/claude-sonnet-4"
`,
			wantTaskURL: "http://a:9091", wantTaskOK: true,
			wantChatURL: "http://c:9092", wantChatOK: true,
		},
		{
			name:       "empty backends",
			yaml:       "",
			wantTaskOK: false, wantChatOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+tc.yaml)

			cfg, err := Load(path)
			require.NoError(t, err)

			tb, taskOK := cfg.AgentBackend()
			assert.Equal(t, tc.wantTaskOK, taskOK)

			if tc.wantTaskOK {
				assert.Equal(t, tc.wantTaskURL, tb.URL)
			}

			cb, chatOK := cfg.ChatBackend()
			assert.Equal(t, tc.wantChatOK, chatOK)

			if tc.wantChatOK {
				assert.Equal(t, tc.wantChatURL, cb.URL)
			}
		})
	}
}

// validBackendsBlock (minimal agent entry) is the fixture for env override
// tests — no selectors to set anymore, URL/API key come from env.
func TestBackendEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir)+validBackendsBlock)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_URL", "http://override:9999")
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_API_KEY", strings.Repeat("x", 32))
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_RECONCILE_INTERVAL", "90s")

	cfg, err := Load(path)
	require.NoError(t, err)

	require.NotNil(t, cfg.Backends.Agent)
	assert.Equal(t, "http://override:9999", cfg.Backends.Agent.URL)
	assert.Equal(t, strings.Repeat("x", 32), cfg.Backends.Agent.APIKey)
	assert.Equal(t, "90s", cfg.Backends.Agent.ReconcileInterval)

	tb, ok := cfg.AgentBackend()
	require.True(t, ok, "AgentBackend must resolve after env URL override")
	assert.Equal(t, "http://override:9999", tb.URL)
}

func TestBackendEnvReconcileIntervalInvalid(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir)+validBackendsBlock)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_RECONCILE_INTERVAL", "soon")

	// The env layer just sets the field; Validate's duration parse rejects it
	// with the entry-scoped error.
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconcile_interval")
}

func TestBackendEnvTaskOnlyFieldOnChatErrors(t *testing.T) {
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  chat:
    url: http://localhost:9092
    api_key: "` + apiKey + `"
    default_model: "anthropic/claude-sonnet-4"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_RECONCILE_INTERVAL", "60s")

	// RECONCILE_INTERVAL is not in the chat entry's env suffix set, so
	// checkBackendEnvKeys rejects the variable by name — same loud-failure
	// posture as the YAML unknown-field decode.
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CONTEXTMATRIX_BACKEND_CHAT_RECONCILE_INTERVAL")
	assert.Contains(t, err.Error(), "not a recognised backend env var")
}

func TestBackendEnvAgentDefaultModel(t *testing.T) {
	// CONTEXTMATRIX_BACKEND_AGENT_DEFAULT_MODEL sets default_model on the
	// agent entry; Validate accepts it and the value is visible on the entry.
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  agent:
    url: http://localhost:9091
    api_key: "` + apiKey + `"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_DEFAULT_MODEL", "deepseek/deepseek-v4-flash")

	cfg, err := Load(path)
	require.NoError(t, err)

	require.NotNil(t, cfg.Backends.Agent)
	assert.Equal(t, "deepseek/deepseek-v4-flash", cfg.Backends.Agent.DefaultModel)
}

func TestBackendEnvChatDefaultModel(t *testing.T) {
	// CONTEXTMATRIX_BACKEND_CHAT_DEFAULT_MODEL sets default_model on the chat
	// entry; Validate accepts it and the value is visible on the entry.
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  chat:
    url: http://localhost:9092
    api_key: "` + apiKey + `"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_DEFAULT_MODEL", "anthropic/claude-sonnet-4")

	cfg, err := Load(path)
	require.NoError(t, err)

	require.NotNil(t, cfg.Backends.Chat)
	assert.Equal(t, "anthropic/claude-sonnet-4", cfg.Backends.Chat.DefaultModel)
}

func TestValidate_ChatBackendRequiresDefaultModel(t *testing.T) {
	// An enabled chat backend with no default_model fails fast: contextmatrix-chat
	// has no server-side default, so an empty CM_MODEL would break chat init.
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  chat:
    url: http://localhost:9092
    api_key: "` + apiKey + `"
`
	path := writeConfigFile(t, dir, yaml)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_model")
}

func TestValidate_Auth(t *testing.T) {
	base := func() *Config {
		c := defaults()
		c.Boards.Dir = "/tmp/boards"
		c.GitHub.AuthMode = "pat"
		c.GitHub.PAT.Token = "x"
		applyAuthDefaults(c)

		return c
	}

	t.Run("default mode is multi", func(t *testing.T) {
		c := base()
		require.NoError(t, c.Validate())
		assert.Equal(t, AuthModeMulti, c.Auth.Mode)
	})

	t.Run("none is accepted", func(t *testing.T) {
		c := base()
		c.Auth.Mode = AuthModeNone
		assert.NoError(t, c.Validate())
	})

	t.Run("unknown mode rejected", func(t *testing.T) {
		c := base()
		c.Auth.Mode = "both"
		assert.ErrorContains(t, c.Validate(), "auth.mode")
	})

	t.Run("bad ttl rejected", func(t *testing.T) {
		c := base()
		c.Auth.SessionIdleTTL = "soon"
		assert.ErrorContains(t, c.Validate(), "auth.session_idle_ttl")
	})

	t.Run("non-positive ttl rejected", func(t *testing.T) {
		c := base()
		c.Auth.SessionIdleTTL = "0s"
		assert.ErrorContains(t, c.Validate(), "auth.session_idle_ttl")
	})

	t.Run("defaults fill paths", func(t *testing.T) {
		c := base()
		assert.Contains(t, c.Auth.DBPath, "auth.db")
		assert.Contains(t, c.Auth.MasterKeyFile, "master.key")
		assert.Equal(t, "720h", c.Auth.SessionIdleTTL)
	})
}

func TestBackendEnvEnabled(t *testing.T) {
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  agent:
    url: http://localhost:9090
    api_key: "` + apiKey + `"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_ENABLED", "false")

	cfg, err := Load(path)
	require.NoError(t, err)

	// agent disabled via env → AgentBackend returns false.
	_, ok := cfg.AgentBackend()
	assert.False(t, ok)
}

func TestBackendEnvEnableGetsDefaults(t *testing.T) {
	// An entry disabled in YAML but enabled via env must come out of Load
	// fully defaulted. This pins the ordering: Validate re-runs
	// applyBackendDefaults after applyEnvOverrides, so the env-enabled entry
	// picks up the reconcile default.
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  agent:
    enabled: false
    url: http://localhost:9090
    api_key: "` + apiKey + `"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_ENABLED", "true")

	cfg, err := Load(path)
	require.NoError(t, err)

	tb, ok := cfg.AgentBackend()
	require.True(t, ok, "env-enabled agent must resolve as task backend")
	assert.Equal(t, "60s", tb.ReconcileInterval)
}

func TestBackendEnvEnabledInvalidValue(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir)+validBackendsBlock)

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_ENABLED", "maybe")

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CONTEXTMATRIX_BACKEND_AGENT_ENABLED")
}

func TestBackendChatEnvURL(t *testing.T) {
	apiKey := strings.Repeat("k", 32)
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  chat:
    url: http://localhost:9092
    api_key: "` + apiKey + `"
    default_model: "anthropic/claude-sonnet-4"
`
	path := writeConfigFile(t, dir, yaml)

	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_URL", "http://override:9993")

	cfg, err := Load(path)
	require.NoError(t, err)

	cb, ok := cfg.ChatBackend()
	require.True(t, ok)
	assert.Equal(t, "http://override:9993", cb.URL)
}

func TestBackendEnvOnlyConfiguration(t *testing.T) {
	// No backends block in YAML at all: env vars for a valid name create
	// the entry, so pure-env deployments need no YAML stub.
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir))

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_URL", "http://env-only:9090")
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_API_KEY", strings.Repeat("e", 32))
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_ENABLED", "true")

	cfg, err := Load(path)
	require.NoError(t, err)

	tb, ok := cfg.AgentBackend()
	require.True(t, ok, "env-only agent must resolve as task backend")
	assert.Equal(t, "http://env-only:9090", tb.URL)
	// The env-created entry gets the task defaults like a YAML-declared one.
	assert.Equal(t, "60s", tb.ReconcileInterval)
}

func TestBackendEnvOnlyChatConfiguration(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir))

	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_URL", "http://env-only:9092")
	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_API_KEY", strings.Repeat("e", 32))
	// Required when the chat backend is enabled (contextmatrix-chat has no
	// server-side default model).
	t.Setenv("CONTEXTMATRIX_BACKEND_CHAT_DEFAULT_MODEL", "anthropic/claude-sonnet-4")

	cfg, err := Load(path)
	require.NoError(t, err)

	cb, ok := cfg.ChatBackend()
	require.True(t, ok, "env-only chat must resolve as chat backend")
	assert.Equal(t, "http://env-only:9092", cb.URL)

	_, ok = cfg.AgentBackend()
	assert.False(t, ok, "chat-only config must not resolve a task backend")
}

func TestBackendEnvOnlyIncompleteErrors(t *testing.T) {
	// An env-created entry goes through full validation: enabling a backend
	// via env without a URL fails loudly instead of half-configuring.
	dir := t.TempDir()
	boardsDir := t.TempDir()
	path := writeConfigFile(t, dir, minValidBase(boardsDir))

	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_ENABLED", "true")

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url")
}

func TestBackendEnvUnknownNameErrors(t *testing.T) {
	cases := []struct {
		name   string
		envKey string
	}{
		{
			// Env references a name not in the backends map.
			name:   "unknown backend name",
			envKey: "CONTEXTMATRIX_BACKEND_NOPE_URL",
		},
		{
			// The removed runner backend's name is no longer in the closed
			// set, so its env vars fail loudly instead of configuring nothing.
			name:   "retired runner name rejected",
			envKey: "CONTEXTMATRIX_BACKEND_RUNNER_URL",
		},
		{
			// Valid backend name (agent declared in YAML), but suffix not in
			// the agent suffix allowlist.
			name:   "known backend name with unknown suffix",
			envKey: "CONTEXTMATRIX_BACKEND_AGENT_MODEL",
		},
		{
			// The suffix sets are per-name: AA_API_KEY is agent-only, so the
			// chat-prefixed variant fails loudly instead of being ignored.
			name:   "agent-only suffix on chat entry",
			envKey: "CONTEXTMATRIX_BACKEND_CHAT_AA_API_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			boardsDir := t.TempDir()
			path := writeConfigFile(t, dir, minValidBase(boardsDir)+validBackendsBlock)

			t.Setenv(tc.envKey, "http://x")

			_, err := Load(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.envKey)
		})
	}
}

func TestBackendsConfigValidation_InvalidReconcileInterval(t *testing.T) {
	dir := t.TempDir()
	boardsDir := t.TempDir()
	yaml := minValidBase(boardsDir) + `
backends:
  agent:
    url: http://localhost:9090
    api_key: "0123456789abcdef0123456789abcdef"
    reconcile_interval: "soon"
`
	path := writeConfigFile(t, dir, yaml)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconcile_interval")
}

// ---------- Favorites + AA key config tests ----------

func TestBackendFavoritesAndAAKey(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_AA_API_KEY", "aa-env")
	t.Setenv("CONTEXTMATRIX_BACKEND_AGENT_MODEL_ALLOWLIST", "a, b")

	cfg, err := loadFromYAML(t, `
boards:
  dir: /tmp
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
backends:
  agent:
    url: "http://x"
    api_key: "aaaabbbbccccddddeeeeffffgggghhhh"
    default_model: "deepseek/deepseek-v4-flash"
    favorites:
      complex: ["anthropic/claude-opus-4.8"]
      critical:
        reviewer: ["openai/gpt-5.5"]
`)
	if err != nil {
		t.Fatal(err)
	}

	b := cfg.Backends.Agent
	if b == nil {
		t.Fatal("agent entry must be present")
	}

	if b.AAAPIKey != "aa-env" {
		t.Errorf("AA key env override failed: %q", b.AAAPIKey)
	}

	if got := b.ModelAllowlist; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("allowlist env override failed (want [a b], no leading space): %v", got)
	}

	if got := b.Favorites["complex"].All; len(got) != 1 || got[0] != "anthropic/claude-opus-4.8" {
		t.Errorf("complex favorites: %v", got)
	}

	if got := b.Favorites["critical"].ByRole["reviewer"]; len(got) != 1 || got[0] != "openai/gpt-5.5" {
		t.Errorf("critical reviewer favorites: %v", got)
	}
}

// ---------- LLM Endpoint config tests ----------

func TestLLMEndpointConfigParses(t *testing.T) {
	cfg, err := loadFromYAML(t, `
boards:
  dir: `+t.TempDir()+`
github:
  auth_mode: pat
  pat:
    token: ghp_test
llm_endpoint:
  type: openai
  base_url: https://your-llm-endpoint.example/v1
  api_key: test-key
backends:
  agent:
    url: http://localhost:9092
    api_key: 0123456789012345678901234567890123456789
    default_model: model-a
    aa_api_key: aa-key
    aa_model_map:
      model-a: vendor-x-1
    model_priors:
      model-b: { coder: 0.91, reviewer: 0.88 }
`)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.LLMEndpoint.Type)
	assert.Equal(t, "https://your-llm-endpoint.example/v1", cfg.LLMEndpoint.BaseURL)
	assert.Equal(t, "test-key", cfg.LLMEndpoint.APIKey)
	require.NotNil(t, cfg.Backends.Agent)
	assert.Equal(t, "vendor-x-1", cfg.Backends.Agent.AAModelMap["model-a"])
	assert.InDelta(t, 0.91, cfg.Backends.Agent.ModelPriors["model-b"].Coder, 1e-9)
	assert.InDelta(t, 0.88, cfg.Backends.Agent.ModelPriors["model-b"].Reviewer, 1e-9)
}

func TestLLMEndpointValidationRequiresBaseURLForOpenAI(t *testing.T) {
	c := &Config{LLMEndpoint: LLMEndpointConfig{Type: "openai", APIKey: "k"}}
	assert.Error(t, c.LLMEndpoint.validate())
}

func TestLLMEndpointValidationRequiresAPIKeyForOpenAI(t *testing.T) {
	c := &Config{LLMEndpoint: LLMEndpointConfig{Type: "openai", BaseURL: "https://your-llm-endpoint.example/v1"}}
	assert.Error(t, c.LLMEndpoint.validate())
}

func TestLLMEndpointValidationRejectsUnknownType(t *testing.T) {
	c := &Config{LLMEndpoint: LLMEndpointConfig{Type: "gemini"}}
	assert.Error(t, c.LLMEndpoint.validate())
}

func TestLLMEndpointValidationAcceptsValidTypes(t *testing.T) {
	for _, typ := range []string{"", "openrouter"} {
		t.Run(typ, func(t *testing.T) {
			c := &Config{LLMEndpoint: LLMEndpointConfig{Type: typ}}
			assert.NoError(t, c.LLMEndpoint.validate())
		})
	}
}

// TestLLMEndpointEnvOverrides verifies that the three CONTEXTMATRIX_LLM_ENDPOINT_*
// variables documented in config.yaml.example are wired in applyEnvOverrides.
func TestLLMEndpointEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	boardsDir := filepath.Join(dir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	path := writeConfigFile(t, dir, `
boards:
  dir: `+boardsDir+`
github:
  auth_mode: "pat"
  pat:
    token: "ghp_test"
`)

	t.Setenv("CONTEXTMATRIX_LLM_ENDPOINT_TYPE", "openai")
	t.Setenv("CONTEXTMATRIX_LLM_ENDPOINT_BASE_URL", "https://my-llm.example/v1")
	t.Setenv("CONTEXTMATRIX_LLM_ENDPOINT_API_KEY", "sk-test-override")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.LLMEndpoint.Type)
	assert.Equal(t, "https://my-llm.example/v1", cfg.LLMEndpoint.BaseURL)
	assert.Equal(t, "sk-test-override", cfg.LLMEndpoint.APIKey)
}

// ---------- BestOfNConfig tests ----------

func TestBestOfNConfigDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "port: 8080\n") // use the file's existing helper; if named differently, adapt
		assert.Equal(t, 5, cfg.BestOfN.MaxCandidates)
		assert.Equal(t, 3, cfg.BestOfN.DefaultCandidates)
		assert.Equal(t, 20, cfg.BestOfN.OutcomeFloor)
	})

	t.Run("yaml values", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "best_of_n:\n  max_candidates: 4\n  default_candidates: 2\n  outcome_floor: 10\n")
		assert.Equal(t, 4, cfg.BestOfN.MaxCandidates)
		assert.Equal(t, 2, cfg.BestOfN.DefaultCandidates)
		assert.Equal(t, 10, cfg.BestOfN.OutcomeFloor)
	})

	t.Run("env overrides", func(t *testing.T) {
		t.Setenv("CONTEXTMATRIX_BEST_OF_N_MAX_CANDIDATES", "3")
		t.Setenv("CONTEXTMATRIX_BEST_OF_N_DEFAULT_CANDIDATES", "2")
		t.Setenv("CONTEXTMATRIX_BEST_OF_N_OUTCOME_FLOOR", "5")
		cfg := loadConfigFromYAML(t, "port: 8080\n")
		assert.Equal(t, 3, cfg.BestOfN.MaxCandidates)
		assert.Equal(t, 2, cfg.BestOfN.DefaultCandidates)
		assert.Equal(t, 5, cfg.BestOfN.OutcomeFloor)
	})

	t.Run("invalid values normalized", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "best_of_n:\n  max_candidates: 1\n  default_candidates: 9\n  outcome_floor: 0\n")
		assert.Equal(t, 5, cfg.BestOfN.MaxCandidates, "max < 2 falls back to default")
		assert.Equal(t, 3, cfg.BestOfN.DefaultCandidates, "default > max falls back")
		assert.Equal(t, 20, cfg.BestOfN.OutcomeFloor, "floor < 1 falls back")
	})

	t.Run("invalid env values normalized", func(t *testing.T) {
		// Judgment point: applyEnvOverrides runs AFTER the pre-env defaults
		// pass in Load, so an out-of-range env value must still be caught by
		// the second applyBestOfNDefaults call inside Validate (which runs
		// after applyEnvOverrides completes) — mirroring how AuthConfig's
		// defaults survive env overrides via the same Validate-internal call.
		t.Setenv("CONTEXTMATRIX_BEST_OF_N_MAX_CANDIDATES", "1")
		cfg := loadConfigFromYAML(t, "port: 8080\n")
		assert.Equal(t, 5, cfg.BestOfN.MaxCandidates, "env-supplied max < 2 must still fall back to default")
	})
}

// ---------- MobConfig tests ----------

// mobBaseYAML mirrors loadConfigFromYAML's always-valid base so
// expected-error tests can go through loadFromYAML directly.
func mobBaseYAML(t *testing.T) string {
	t.Helper()

	boardsDir := filepath.Join(t.TempDir(), "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	return "boards:\n  dir: " + boardsDir + "\ngithub:\n  auth_mode: \"pat\"\n  pat:\n    token: \"ghp_test\"\n"
}

func TestMobConfigDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "port: 8080\n")
		assert.Equal(t, 5, cfg.Mob.MaxParticipants)
		assert.Equal(t, 3, cfg.Mob.DefaultParticipants)
		assert.Equal(t, 2, cfg.Mob.DefaultRounds)
		assert.Equal(t, 3, cfg.Mob.MaxRounds)
		assert.InDelta(t, 0.75, cfg.Mob.BudgetFactor, 1e-9)
		assert.False(t, cfg.Mob.ExecuteCheckpointsEnabled)
		assert.Equal(t, "complex", cfg.Mob.CheckpointMinTier)
		assert.Nil(t, cfg.Mob.Guests)
	})

	t.Run("yaml values", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "mob:\n"+
			"  max_participants: 4\n"+
			"  default_participants: 2\n"+
			"  default_rounds: 1\n"+
			"  max_rounds: 2\n"+
			"  budget_factor: 1.5\n"+
			"  execute_checkpoints_enabled: true\n"+
			"  checkpoint_min_tier: critical\n"+
			"  guests:\n"+
			"    - name: laptop\n"+
			"      url: http://192.0.2.1:8484\n"+
			"      token: guest-secret\n")
		assert.Equal(t, 4, cfg.Mob.MaxParticipants)
		assert.Equal(t, 2, cfg.Mob.DefaultParticipants)
		assert.Equal(t, 1, cfg.Mob.DefaultRounds)
		assert.Equal(t, 2, cfg.Mob.MaxRounds)
		assert.InDelta(t, 1.5, cfg.Mob.BudgetFactor, 1e-9)
		assert.True(t, cfg.Mob.ExecuteCheckpointsEnabled)
		assert.Equal(t, "critical", cfg.Mob.CheckpointMinTier)
		require.Len(t, cfg.Mob.Guests, 1)
		assert.Equal(t, MobGuest{Name: "laptop", URL: "http://192.0.2.1:8484", Token: "guest-secret"}, cfg.Mob.Guests[0])
	})

	t.Run("env overrides (scalars only)", func(t *testing.T) {
		t.Setenv("CONTEXTMATRIX_MOB_MAX_PARTICIPANTS", "6")
		t.Setenv("CONTEXTMATRIX_MOB_DEFAULT_PARTICIPANTS", "4")
		t.Setenv("CONTEXTMATRIX_MOB_DEFAULT_ROUNDS", "3")
		t.Setenv("CONTEXTMATRIX_MOB_MAX_ROUNDS", "4")
		t.Setenv("CONTEXTMATRIX_MOB_BUDGET_FACTOR", "1.25")
		t.Setenv("CONTEXTMATRIX_MOB_EXECUTE_CHECKPOINTS_ENABLED", "true")
		t.Setenv("CONTEXTMATRIX_MOB_CHECKPOINT_MIN_TIER", "moderate")
		cfg := loadConfigFromYAML(t, "port: 8080\n")
		assert.Equal(t, 6, cfg.Mob.MaxParticipants)
		assert.Equal(t, 4, cfg.Mob.DefaultParticipants)
		assert.Equal(t, 3, cfg.Mob.DefaultRounds)
		assert.Equal(t, 4, cfg.Mob.MaxRounds)
		assert.InDelta(t, 1.25, cfg.Mob.BudgetFactor, 1e-9)
		assert.True(t, cfg.Mob.ExecuteCheckpointsEnabled)
		assert.Equal(t, "moderate", cfg.Mob.CheckpointMinTier)
	})

	t.Run("invalid values normalized", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "mob:\n"+
			"  max_participants: 1\n"+ // < 2 → default 5
			"  default_participants: 9\n"+ // > max → min(3, max)
			"  default_rounds: 0\n"+ // < 1 → min(2, max_rounds)
			"  max_rounds: 6\n"+ // > 5 → default 3
			"  budget_factor: -1\n") // <= 0 → default 0.75
		assert.Equal(t, 5, cfg.Mob.MaxParticipants, "max_participants outside 2..10 falls back")
		assert.Equal(t, 3, cfg.Mob.DefaultParticipants, "default_participants outside 2..max falls back")
		assert.Equal(t, 2, cfg.Mob.DefaultRounds, "default_rounds outside 1..max_rounds falls back")
		assert.Equal(t, 3, cfg.Mob.MaxRounds, "max_rounds outside 1..5 falls back")
		assert.InDelta(t, 0.75, cfg.Mob.BudgetFactor, 1e-9, "budget_factor outside (0, 5] falls back")
	})

	t.Run("max_participants above 10 normalized", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "mob:\n  max_participants: 11\n")
		assert.Equal(t, 5, cfg.Mob.MaxParticipants)
	})

	t.Run("default_rounds above max_rounds normalized", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "mob:\n  max_rounds: 2\n  default_rounds: 3\n")
		assert.Equal(t, 2, cfg.Mob.MaxRounds)
		assert.Equal(t, 2, cfg.Mob.DefaultRounds, "default_rounds clamps into 1..max_rounds via min(2, max)")
	})

	t.Run("budget_factor above 5 normalized", func(t *testing.T) {
		cfg := loadConfigFromYAML(t, "mob:\n  budget_factor: 6.0\n")
		assert.InDelta(t, 0.75, cfg.Mob.BudgetFactor, 1e-9)
	})

	t.Run("invalid env values normalized", func(t *testing.T) {
		// applyEnvOverrides runs AFTER the pre-env defaults pass in Load, so
		// an out-of-range env value must be caught by the second
		// applyMobDefaults call inside Validate — same two-pass contract as
		// applyBestOfNDefaults.
		t.Setenv("CONTEXTMATRIX_MOB_MAX_PARTICIPANTS", "1")
		cfg := loadConfigFromYAML(t, "port: 8080\n")
		assert.Equal(t, 5, cfg.Mob.MaxParticipants, "env-supplied max < 2 must still fall back to default")
	})
}

func TestMobConfigValidation(t *testing.T) {
	load := func(t *testing.T, mobFragment string) error {
		t.Helper()

		_, err := loadFromYAML(t, mobBaseYAML(t)+mobFragment)

		return err
	}

	t.Run("guest missing name rejected", func(t *testing.T) {
		err := load(t, "mob:\n  guests:\n    - url: http://192.0.2.1:8484\n      token: s\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("guest missing url rejected", func(t *testing.T) {
		err := load(t, "mob:\n  guests:\n    - name: laptop\n      token: s\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http://")
	})

	t.Run("guest non-http url rejected", func(t *testing.T) {
		err := load(t, "mob:\n  guests:\n    - name: laptop\n      url: ssh://192.0.2.1\n      token: s\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http://")
	})

	t.Run("guest https url accepted", func(t *testing.T) {
		_, err := loadFromYAML(t, mobBaseYAML(t)+
			"mob:\n  guests:\n    - name: laptop\n      url: https://guest.example:8484\n      token: s\n")
		require.NoError(t, err)
	})

	t.Run("guest missing token rejected", func(t *testing.T) {
		err := load(t, "mob:\n  guests:\n    - name: laptop\n      url: http://192.0.2.1:8484\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("duplicate guest name rejected", func(t *testing.T) {
		err := load(t, "mob:\n  guests:\n"+
			"    - name: laptop\n      url: http://192.0.2.1:8484\n      token: a\n"+
			"    - name: laptop\n      url: http://192.0.2.2:8484\n      token: b\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate guest name")
	})

	t.Run("invalid checkpoint_min_tier rejected", func(t *testing.T) {
		err := load(t, "mob:\n  checkpoint_min_tier: extreme\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint_min_tier")
	})
}
