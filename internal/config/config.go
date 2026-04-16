package config

import (
	"fmt"
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
	Enabled   bool   `yaml:"enabled"`
	URL       string `yaml:"url"`        // base URL, e.g. http://localhost:9090
	APIKey    string `yaml:"api_key"`    // shared secret for HMAC signing
	PublicURL string `yaml:"public_url"` // public URL for MCP endpoint sent to runner containers
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

// Config holds the application configuration.
type Config struct {
	Port                int                  `yaml:"port"`
	BoardsDir           string               `yaml:"boards_dir"`
	GitAutoCommit       bool                 `yaml:"git_auto_commit"`
	GitAutoPush         bool                 `yaml:"git_auto_push"`
	GitAutoPull         bool                 `yaml:"git_auto_pull"`
	GitPullInterval     string               `yaml:"git_pull_interval"`
	GitDeferredCommit   bool                 `yaml:"git_deferred_commit"`
	GitCloneOnEmpty     bool                 `yaml:"git_clone_on_empty"`
	GitRemoteURL        string               `yaml:"git_remote_url"`
	HeartbeatTimeout    string               `yaml:"heartbeat_timeout"`
	CORSOrigin          string               `yaml:"cors_origin"`
	SkillsDir           string               `yaml:"skills_dir"`
	TokenCosts          map[string]ModelCost `yaml:"token_costs"`
	MCPAPIKey           string               `yaml:"mcp_api_key"`
	Runner              RunnerConfig         `yaml:"runner"`
	GitHub              GitHubConfig         `yaml:"github"`
}

// defaults returns a Config with default values.
func defaults() *Config {
	return &Config{
		Port:             8080,
		BoardsDir:        "", // No default — must be configured
		GitAutoCommit:    true,
		GitAutoPush:      false,
		GitAutoPull:      false,
		GitPullInterval:  "60s",
		HeartbeatTimeout: "30m",
		CORSOrigin:       "http://localhost:5173",
		SkillsDir:        "",
	}
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	if c.BoardsDir == "" {
		return fmt.Errorf("boards_dir is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR")
	}
	if _, err := time.ParseDuration(c.HeartbeatTimeout); err != nil {
		return fmt.Errorf("invalid heartbeat_timeout %q: %w", c.HeartbeatTimeout, err)
	}
	if c.GitPullInterval != "" {
		if _, err := time.ParseDuration(c.GitPullInterval); err != nil {
			return fmt.Errorf("invalid git_pull_interval %q: %w", c.GitPullInterval, err)
		}
	}
	if c.GitCloneOnEmpty && c.GitRemoteURL == "" {
		return fmt.Errorf("git_remote_url is required when git_clone_on_empty is enabled")
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
	boardsDir, err := expandTilde(cfg.BoardsDir)
	if err != nil {
		return err
	}
	cfg.BoardsDir = boardsDir

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
		cfg.BoardsDir = v
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_AUTO_COMMIT"); v != "" {
		cfg.GitAutoCommit = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_AUTO_PUSH"); v != "" {
		cfg.GitAutoPush = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_AUTO_PULL"); v != "" {
		cfg.GitAutoPull = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_PULL_INTERVAL"); v != "" {
		cfg.GitPullInterval = v
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_DEFERRED_COMMIT"); v != "" {
		cfg.GitDeferredCommit = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_CLONE_ON_EMPTY"); v != "" {
		cfg.GitCloneOnEmpty = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_REMOTE_URL"); v != "" {
		cfg.GitRemoteURL = v
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
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// PullIntervalDuration parses GitPullInterval as a time.Duration.
func (c *Config) PullIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(c.GitPullInterval)
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
