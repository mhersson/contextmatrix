package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ModelCost defines per-token cost rates for a model.
type ModelCost struct {
	Prompt     float64 `yaml:"prompt"`
	Completion float64 `yaml:"completion"`
}

// MinRunnerAPIKeyLength is the minimum required length for runner.api_key.
const MinRunnerAPIKeyLength = 32

// RunnerConfig holds configuration for the remote execution runner.
type RunnerConfig struct {
	Enabled                 bool   `yaml:"enabled"`
	URL                     string `yaml:"url"`                       // base URL, e.g. http://localhost:9090
	APIKey                  string `yaml:"api_key"`                   // shared secret for HMAC signing
	PublicURL               string `yaml:"public_url"`                // public URL for MCP endpoint sent to runner containers
	OrchestratorSonnetModel string `yaml:"orchestrator_sonnet_model"` // model ID for Sonnet orchestrator
	OrchestratorOpusModel   string `yaml:"orchestrator_opus_model"`   // model ID for Opus orchestrator
}

// IssueImportingConfig holds configuration specific to GitHub issue importing.
type IssueImportingConfig struct {
	Enabled      bool   `yaml:"enabled"`
	SyncInterval string `yaml:"sync_interval"`
}

// SyncIntervalDuration parses SyncInterval as a time.Duration.
func (c *IssueImportingConfig) SyncIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(c.SyncInterval)
}

// GitHubConfig holds configuration for GitHub integration.
type GitHubConfig struct {
	Token          string               `yaml:"token"`
	Host           string               `yaml:"host"`
	APIBaseURL     string               `yaml:"api_base_url"`
	IssueImporting IssueImportingConfig `yaml:"issue_importing"`
}

// ResolvedAPIBaseURL returns the effective GitHub API base URL.
// Precedence: APIBaseURL (trimmed) > "https://api." + Host > "https://api.github.com".
func (g *GitHubConfig) ResolvedAPIBaseURL() string {
	if v := strings.TrimSpace(g.APIBaseURL); v != "" {
		return v
	}

	if g.Host != "" {
		return "https://api." + g.Host
	}

	return "https://api.github.com"
}

// AllowedHosts returns the list of GitHub hostnames that are permitted.
// When Host is empty or "github.com", only ["github.com"] is returned.
// For any other Host value, ["github.com", Host] is returned.
func (g *GitHubConfig) AllowedHosts() []string {
	if g.Host == "" || g.Host == "github.com" {
		return []string{"github.com"}
	}

	return []string{"github.com", g.Host}
}

// BoardsConfig holds all configuration related to the boards git repository.
type BoardsConfig struct {
	Dir               string `yaml:"dir"`
	GitAutoCommit     bool   `yaml:"git_auto_commit"`
	GitDeferredCommit bool   `yaml:"git_deferred_commit"`
	GitAutoPush       bool   `yaml:"git_auto_push"`
	GitAutoPull       bool   `yaml:"git_auto_pull"`
	GitPullInterval   string `yaml:"git_pull_interval"`
	GitCloneOnEmpty   bool   `yaml:"git_clone_on_empty"`
	GitRemoteURL      string `yaml:"git_remote_url"`
	GitAuthMode       string `yaml:"git_auth_mode"`
}

// Config holds the application configuration.
type Config struct {
	Port             int                  `yaml:"port"`
	Boards           BoardsConfig         `yaml:"boards"`
	HeartbeatTimeout string               `yaml:"heartbeat_timeout"`
	CORSOrigin       string               `yaml:"cors_origin"`
	SkillsDir        string               `yaml:"skills_dir"`
	Theme            string               `yaml:"theme"`
	TokenCosts       map[string]ModelCost `yaml:"token_costs"`
	MCPAPIKey        string               `yaml:"mcp_api_key"`
	Runner           RunnerConfig         `yaml:"runner"`
	GitHub           GitHubConfig         `yaml:"github"`
	LogFormat        string               `yaml:"log_format"`      // "json" or "text", default "text"
	LogLevel         string               `yaml:"log_level"`       // "debug"/"info"/"warn"/"error", default "info"
	AdminPort        int                  `yaml:"admin_port"`      // 0 = disabled
	AdminBindAddr    string               `yaml:"admin_bind_addr"` // listen address for admin server (pprof + /metrics); default "127.0.0.1"
}

// defaults returns a Config with default values.
func defaults() *Config {
	return &Config{
		Port: 8080,
		Boards: BoardsConfig{
			Dir:             "", // No default — must be configured
			GitAutoCommit:   true,
			GitAutoPush:     false,
			GitAutoPull:     false,
			GitPullInterval: "60s",
			GitAuthMode:     "ssh",
		},
		HeartbeatTimeout: "30m",
		CORSOrigin:       "http://localhost:5173",
		SkillsDir:        "",
		Theme:            "everforest",
		Runner: RunnerConfig{
			OrchestratorSonnetModel: "claude-sonnet-4-6",
			OrchestratorOpusModel:   "claude-opus-4-7",
		},
		LogFormat:     "text",
		LogLevel:      "info",
		AdminPort:     0,
		AdminBindAddr: "127.0.0.1",
	}
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	if c.Boards.Dir == "" {
		return fmt.Errorf("boards.dir is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR")
	}

	if _, err := time.ParseDuration(c.HeartbeatTimeout); err != nil {
		return fmt.Errorf("invalid heartbeat_timeout %q: %w", c.HeartbeatTimeout, err)
	}

	if c.Boards.GitPullInterval == "" {
		c.Boards.GitPullInterval = "60s"
	}

	if _, err := time.ParseDuration(c.Boards.GitPullInterval); err != nil {
		return fmt.Errorf("invalid boards.git_pull_interval %q: %w", c.Boards.GitPullInterval, err)
	}

	if c.Boards.GitCloneOnEmpty && c.Boards.GitRemoteURL == "" {
		return fmt.Errorf("boards.git_remote_url is required when boards.git_clone_on_empty is enabled")
	}

	if c.Boards.GitAuthMode == "" {
		c.Boards.GitAuthMode = "ssh"
	}

	switch c.Boards.GitAuthMode {
	case "ssh", "pat":
		// valid
	default:
		return fmt.Errorf("invalid boards.git_auth_mode %q: must be \"ssh\" or \"pat\"", c.Boards.GitAuthMode)
	}

	if c.Boards.GitAuthMode == "pat" {
		if c.GitHub.Token == "" {
			return fmt.Errorf("github.token is required when boards.git_auth_mode is \"pat\"")
		}

		if !strings.HasPrefix(c.Boards.GitRemoteURL, "https://") {
			return fmt.Errorf("boards.git_remote_url must start with https:// when boards.git_auth_mode is \"pat\" (got %q)", c.Boards.GitRemoteURL)
		}
	}

	if c.GitHub.IssueImporting.Enabled {
		if c.GitHub.Token == "" {
			return fmt.Errorf("github.token is required when github.issue_importing.enabled is true")
		}

		if c.GitHub.IssueImporting.SyncInterval == "" {
			c.GitHub.IssueImporting.SyncInterval = "5m"
		}

		interval, err := time.ParseDuration(c.GitHub.IssueImporting.SyncInterval)
		if err != nil {
			return fmt.Errorf("invalid github.issue_importing.sync_interval %q: %w", c.GitHub.IssueImporting.SyncInterval, err)
		}

		if interval < 5*time.Minute {
			return fmt.Errorf("github.issue_importing.sync_interval must be at least 5m, got %s", c.GitHub.IssueImporting.SyncInterval)
		}
	}

	if c.Runner.Enabled {
		if c.Runner.URL == "" {
			return fmt.Errorf("runner.url is required when runner is enabled")
		}

		if c.Runner.APIKey == "" {
			return fmt.Errorf("runner.api_key is required when runner is enabled")
		}

		if len(c.Runner.APIKey) < MinRunnerAPIKeyLength {
			return fmt.Errorf("runner.api_key must be at least %d characters", MinRunnerAPIKeyLength)
		}

		if c.Runner.PublicURL == "" {
			return fmt.Errorf("runner.public_url is required when runner is enabled")
		}
	}

	if c.Theme == "" {
		c.Theme = "everforest"
	}

	switch c.Theme {
	case "everforest", "radix", "catppuccin":
		// valid
	default:
		return fmt.Errorf("invalid theme %q: must be one of \"everforest\", \"radix\", \"catppuccin\"", c.Theme)
	}

	if c.LogFormat == "" {
		c.LogFormat = "text"
	}

	switch strings.ToLower(c.LogFormat) {
	case "text", "json":
		// valid
	default:
		return fmt.Errorf("invalid log_format %q: must be \"text\" or \"json\"", c.LogFormat)
	}

	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("invalid log_level %q: must be one of \"debug\", \"info\", \"warn\", \"error\"", c.LogLevel)
	}

	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d: must be in 0..65535", c.Port)
	}

	if c.AdminPort < 0 || c.AdminPort > 65535 {
		return fmt.Errorf("invalid admin_port %d: must be in 0..65535 (0 disables)", c.AdminPort)
	}

	if c.AdminPort != 0 && c.AdminPort == c.Port {
		return fmt.Errorf("admin_port %d collides with port %d", c.AdminPort, c.Port)
	}

	if c.AdminBindAddr == "" {
		c.AdminBindAddr = "127.0.0.1"
	}

	return nil
}

// FindConfigPath discovers the config file using XDG Base Directory conventions.
// Search order:
//  1. $XDG_CONFIG_HOME/contextmatrix/config.yaml (if XDG_CONFIG_HOME is set)
//  2. ~/.config/contextmatrix/config.yaml (XDG default)
//  3. config.yaml (relative to cwd — legacy fallback)
func FindConfigPath() string {
	var candidates []string

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "contextmatrix", "config.yaml"))
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "contextmatrix", "config.yaml"))
		}
	}

	candidates = append(candidates, "config.yaml")

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return candidates[len(candidates)-1]
}

// Load reads configuration from the given YAML file and applies environment overrides.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)

			if err := resolvePaths(cfg, path); err != nil {
				return nil, err
			}

			if err := cfg.Validate(); err != nil {
				return nil, err
			}

			return cfg, nil
		}

		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(cfg)

	if err := resolvePaths(cfg, path); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// resolvePaths expands tildes and derives default paths relative to the config file location.
func resolvePaths(cfg *Config, configPath string) error {
	boardsDir, err := expandTilde(cfg.Boards.Dir)
	if err != nil {
		return err
	}

	cfg.Boards.Dir = boardsDir

	skillsDir, err := expandTilde(cfg.SkillsDir)
	if err != nil {
		return err
	}

	cfg.SkillsDir = skillsDir

	if cfg.SkillsDir == "" {
		cfg.SkillsDir = filepath.Join(filepath.Dir(configPath), "skills")
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CONTEXTMATRIX_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Port = port
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_PORT", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_DIR"); v != "" {
		cfg.Boards.Dir = v
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT"); v != "" {
		cfg.Boards.GitAutoCommit = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH"); v != "" {
		cfg.Boards.GitAutoPush = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL"); v != "" {
		cfg.Boards.GitAutoPull = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_PULL_INTERVAL"); v != "" {
		cfg.Boards.GitPullInterval = v
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT"); v != "" {
		cfg.Boards.GitDeferredCommit = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY"); v != "" {
		cfg.Boards.GitCloneOnEmpty = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL"); v != "" {
		cfg.Boards.GitRemoteURL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_AUTH_MODE"); v != "" {
		cfg.Boards.GitAuthMode = v
	}

	if v := os.Getenv("CONTEXTMATRIX_HEARTBEAT_TIMEOUT"); v != "" {
		cfg.HeartbeatTimeout = v
	}

	if v := os.Getenv("CONTEXTMATRIX_CORS_ORIGIN"); v != "" {
		cfg.CORSOrigin = v
	}

	if v := os.Getenv("CONTEXTMATRIX_SKILLS_DIR"); v != "" {
		cfg.SkillsDir = v
	}

	if v := os.Getenv("CONTEXTMATRIX_THEME"); v != "" {
		cfg.Theme = v
	}

	if v := os.Getenv("CONTEXTMATRIX_MCP_API_KEY"); v != "" {
		cfg.MCPAPIKey = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_ENABLED"); v != "" {
		cfg.Runner.Enabled = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_URL"); v != "" {
		cfg.Runner.URL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_API_KEY"); v != "" {
		cfg.Runner.APIKey = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_PUBLIC_URL"); v != "" {
		cfg.Runner.PublicURL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_SONNET_MODEL"); v != "" {
		cfg.Runner.OrchestratorSonnetModel = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_OPUS_MODEL"); v != "" {
		cfg.Runner.OrchestratorOpusModel = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_TOKEN"); v != "" {
		cfg.GitHub.Token = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_HOST"); v != "" {
		cfg.GitHub.Host = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_API_BASE_URL"); v != "" {
		cfg.GitHub.APIBaseURL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED"); v != "" {
		cfg.GitHub.IssueImporting.Enabled = v == "true" || v == "1"
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_SYNC_INTERVAL"); v != "" {
		cfg.GitHub.IssueImporting.SyncInterval = v
	}

	if v := os.Getenv("CONTEXTMATRIX_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}

	if v := os.Getenv("CONTEXTMATRIX_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("CONTEXTMATRIX_ADMIN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.AdminPort = port
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_ADMIN_PORT", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_ADMIN_BIND_ADDR"); v != "" {
		cfg.AdminBindAddr = v
	}
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// PullIntervalDuration parses Boards.GitPullInterval as a time.Duration.
func (c *Config) PullIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(c.Boards.GitPullInterval)
}

// BuildSlogHandler constructs a slog.Handler from the LogFormat and LogLevel fields.
// Unknown level strings default to slog.LevelInfo. Unknown format strings default to text.
func (c *Config) BuildSlogHandler(w io.Writer) slog.Handler {
	var level slog.Level

	switch strings.ToLower(c.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	if strings.ToLower(c.LogFormat) == "json" {
		return slog.NewJSONHandler(w, opts)
	}

	return slog.NewTextHandler(w, opts)
}

// expandTilde expands a leading ~ in a path to the user's home directory.
func expandTilde(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}

		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}

		return filepath.Join(home, path[2:]), nil
	}

	return path, nil
}
