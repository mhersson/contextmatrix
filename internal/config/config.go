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
	OrchestratorSonnetModel string `yaml:"orchestrator_sonnet_model"` // model ID for Sonnet orchestrator
	OrchestratorOpusModel   string `yaml:"orchestrator_opus_model"`   // model ID for Opus orchestrator
	ReconcileInterval       string `yaml:"reconcile_interval"`        // how often the backstop sweep scans for leaked containers; "0s" disables
}

// ReconcileIntervalDuration parses ReconcileInterval as a time.Duration. A
// zero or unset value returns 0, which disables the sweep in
// StartReconciliationSweep.
func (r *RunnerConfig) ReconcileIntervalDuration() time.Duration {
	if r.ReconcileInterval == "" {
		return 0
	}

	d, err := time.ParseDuration(r.ReconcileInterval)
	if err != nil {
		return 0
	}

	return d
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

// GitHubAppConfig holds GitHub App credentials.
type GitHubAppConfig struct {
	AppID          int64  `yaml:"app_id"`
	InstallationID int64  `yaml:"installation_id"`
	PrivateKeyPath string `yaml:"private_key_path"`
}

// GitHubPATConfig holds a Personal Access Token credential.
type GitHubPATConfig struct {
	Token string `yaml:"token"`
}

// GitHubConfig holds configuration for GitHub integration.
type GitHubConfig struct {
	AuthMode       string               `yaml:"auth_mode"`
	Host           string               `yaml:"host"`
	APIBaseURL     string               `yaml:"api_base_url"`
	App            GitHubAppConfig      `yaml:"app"`
	PAT            GitHubPATConfig      `yaml:"pat"`
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
}

// TaskSkillsConfig holds configuration for the task-skills directory and its optional git backing.
type TaskSkillsConfig struct {
	Dir             string `yaml:"dir"`
	GitCloneOnEmpty bool   `yaml:"git_clone_on_empty"`
	GitRemoteURL    string `yaml:"git_remote_url"`
}

// ChatConfig configures the global chat panel feature.
type ChatConfig struct {
	// DBPath is the SQLite file path for chat sessions and transcripts.
	// Defaults to <XDG_STATE_HOME>/contextmatrix/chats.db, falling back to
	// ~/.local/state/contextmatrix/chats.db.
	DBPath string `yaml:"db_path"`

	// IdleTTL is how long a chat container survives after the browser
	// disconnects. Default: 1h.
	IdleTTL time.Duration `yaml:"idle_ttl"`

	// MaxConcurrent caps the number of simultaneously-running chat
	// containers. Default: 5.
	MaxConcurrent int `yaml:"max_concurrent"`

	// DefaultModel is the Claude model ID used when a chat is created
	// without an explicit selection. Must be a key in Models. Default:
	// "claude-sonnet-4-6".
	DefaultModel string `yaml:"default_model"`

	// Models is the allowlist of selectable models for new chats, keyed
	// by model ID. The values carry the human label shown in the picker
	// and the context-window denominator used by the UI usage indicator.
	Models map[string]ChatModelConfig `yaml:"models"`

	// ResumeBudgetTokens caps the rough token estimate the transcript
	// builder will fit into the rehydration payload on cold-reopen.
	// Default: 40000.
	ResumeBudgetTokens int `yaml:"resume_budget_tokens"`

	// RehydrationTimeout forces the per-session rehydration phase off
	// after this duration, even if the agent never called
	// chat_rehydration_complete and the user never typed. Default: 10m.
	RehydrationTimeout time.Duration `yaml:"rehydration_timeout"`
}

// ChatModelConfig is one entry in ChatConfig.Models.
type ChatModelConfig struct {
	// Label is the human-readable name shown in the picker, e.g. "Sonnet 4.6".
	Label string `yaml:"label"`

	// MaxTokens is the context-window denominator used by the UI usage
	// indicator. The picker also surfaces it (e.g. "(200k context)").
	MaxTokens int64 `yaml:"max_tokens"`
}

// Config holds the application configuration.
type Config struct {
	Port             int          `yaml:"port"`
	Boards           BoardsConfig `yaml:"boards"`
	HeartbeatTimeout string       `yaml:"heartbeat_timeout"`
	// StalledCheckInterval is how often the lock manager scans for
	// cards whose last heartbeat is older than HeartbeatTimeout and
	// transitions them to `stalled`. Empty defaults to 1m, which is
	// fine for production (heartbeat is typically a few minutes).
	// Test harnesses shrink it to seconds so heartbeat-timeout
	// scenarios don't have to wait a full tick.
	StalledCheckInterval string               `yaml:"stalled_check_interval"`
	CORSOrigin           string               `yaml:"cors_origin"`
	WorkflowSkillsDir    string               `yaml:"workflow_skills_dir"`
	TaskSkills           TaskSkillsConfig     `yaml:"task_skills"`
	Theme                string               `yaml:"theme"`
	TokenCosts           map[string]ModelCost `yaml:"token_costs"`
	MCPAPIKey            string               `yaml:"mcp_api_key"`
	Runner               RunnerConfig         `yaml:"runner"`
	GitHub               GitHubConfig         `yaml:"github"`
	LogFormat            string               `yaml:"log_format"`      // "json" or "text", default "text"
	LogLevel             string               `yaml:"log_level"`       // "debug"/"info"/"warn"/"error", default "info"
	AdminPort            int                  `yaml:"admin_port"`      // 0 = disabled
	AdminBindAddr        string               `yaml:"admin_bind_addr"` // listen address for admin server (pprof + /metrics); default "127.0.0.1"
	Chat                 ChatConfig           `yaml:"chat"`
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
		},
		HeartbeatTimeout:     "30m",
		StalledCheckInterval: "1m",
		CORSOrigin:           "http://localhost:5173",
		WorkflowSkillsDir:    "",
		TaskSkills:           TaskSkillsConfig{},
		Theme:                "everforest",
		Runner: RunnerConfig{
			OrchestratorSonnetModel: "claude-sonnet-4-6",
			OrchestratorOpusModel:   "claude-opus-4-7",
			ReconcileInterval:       "60s",
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

	switch c.GitHub.AuthMode {
	case "app":
		if c.GitHub.App.AppID == 0 {
			return fmt.Errorf("github.app.app_id is required when github.auth_mode is \"app\"")
		}

		if c.GitHub.App.InstallationID == 0 {
			return fmt.Errorf("github.app.installation_id is required when github.auth_mode is \"app\"")
		}

		if c.GitHub.App.PrivateKeyPath == "" {
			return fmt.Errorf("github.app.private_key_path is required when github.auth_mode is \"app\"")
		}

		if c.GitHub.PAT.Token != "" {
			return fmt.Errorf("github.pat.token must be empty when github.auth_mode is \"app\"")
		}
	case "pat":
		if c.GitHub.PAT.Token == "" {
			return fmt.Errorf("github.pat.token is required when github.auth_mode is \"pat\"")
		}

		if c.GitHub.App.AppID != 0 || c.GitHub.App.InstallationID != 0 || c.GitHub.App.PrivateKeyPath != "" {
			return fmt.Errorf("github.app.* must be empty when github.auth_mode is \"pat\"")
		}
	default:
		return fmt.Errorf("github.auth_mode is required: must be \"app\" or \"pat\" (got %q)", c.GitHub.AuthMode)
	}

	if _, err := time.ParseDuration(c.HeartbeatTimeout); err != nil {
		return fmt.Errorf("invalid heartbeat_timeout %q: %w", c.HeartbeatTimeout, err)
	}

	if c.StalledCheckInterval == "" {
		c.StalledCheckInterval = "1m"
	}

	if d, err := time.ParseDuration(c.StalledCheckInterval); err != nil {
		return fmt.Errorf("invalid stalled_check_interval %q: %w", c.StalledCheckInterval, err)
	} else if d <= 0 {
		return fmt.Errorf("stalled_check_interval must be positive (got %s)", d)
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
	// All git remote URLs must be HTTPS — SSH is no longer supported.
	if c.Boards.GitRemoteURL != "" && !strings.HasPrefix(c.Boards.GitRemoteURL, "https://") {
		return fmt.Errorf("boards.git_remote_url must start with https:// (got %q)", c.Boards.GitRemoteURL)
	}

	if c.TaskSkills.GitCloneOnEmpty && c.TaskSkills.GitRemoteURL == "" {
		return fmt.Errorf("task_skills.git_remote_url is required when task_skills.git_clone_on_empty is enabled")
	}

	if c.TaskSkills.GitRemoteURL != "" && !strings.HasPrefix(c.TaskSkills.GitRemoteURL, "https://") {
		return fmt.Errorf("task_skills.git_remote_url must start with https:// (got %q)", c.TaskSkills.GitRemoteURL)
	}

	if c.GitHub.IssueImporting.Enabled {
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

		if c.Runner.ReconcileInterval != "" {
			if _, err := time.ParseDuration(c.Runner.ReconcileInterval); err != nil {
				return fmt.Errorf("invalid runner.reconcile_interval %q: %w", c.Runner.ReconcileInterval, err)
			}
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

	// Chat: applyChatDefaults turns zero values into safe defaults during
	// Load. Run it again here so callers that bypass Load (tests, embedded
	// uses) still get defaults applied; the function is idempotent. Then
	// reject negatives — a negative IdleTTL would have the reaper end every
	// session immediately and a negative MaxConcurrent would reject every
	// open.
	applyChatDefaults(c)

	if c.Chat.IdleTTL <= 0 {
		return fmt.Errorf("chat.idle_ttl must be positive (got %s)", c.Chat.IdleTTL)
	}

	if c.Chat.MaxConcurrent < 0 {
		return fmt.Errorf("chat.max_concurrent must be >= 0 (got %d)", c.Chat.MaxConcurrent)
	}

	if c.Chat.ResumeBudgetTokens < 0 {
		return fmt.Errorf("chat.resume_budget_tokens must be >= 0 (got %d)", c.Chat.ResumeBudgetTokens)
	}

	if c.Chat.RehydrationTimeout <= 0 {
		return fmt.Errorf("chat.rehydration_timeout must be positive (got %s)", c.Chat.RehydrationTimeout)
	}

	if c.Chat.DefaultModel == "" {
		return fmt.Errorf("chat.default_model is required")
	}

	if _, ok := c.Chat.Models[c.Chat.DefaultModel]; !ok {
		return fmt.Errorf("chat.default_model %q is not in chat.models", c.Chat.DefaultModel)
	}

	for id, m := range c.Chat.Models {
		if m.Label == "" {
			return fmt.Errorf("chat.models[%q].label is required", id)
		}

		if m.MaxTokens <= 0 {
			return fmt.Errorf("chat.models[%q].max_tokens must be positive (got %d)", id, m.MaxTokens)
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
			applyChatDefaults(cfg)
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

	applyChatDefaults(cfg)
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

	workflowSkillsDir, err := expandTilde(cfg.WorkflowSkillsDir)
	if err != nil {
		return err
	}

	cfg.WorkflowSkillsDir = workflowSkillsDir

	if cfg.WorkflowSkillsDir == "" {
		cfg.WorkflowSkillsDir = filepath.Join(filepath.Dir(configPath), "workflow-skills")
	}

	taskSkillsDir, err := expandTilde(cfg.TaskSkills.Dir)
	if err != nil {
		return err
	}

	cfg.TaskSkills.Dir = taskSkillsDir

	if cfg.TaskSkills.Dir == "" {
		cfg.TaskSkills.Dir = filepath.Join(filepath.Dir(configPath), "task-skills")
	}

	return nil
}

// applyChatDefaults sets Chat fields that were not supplied by YAML.
func applyChatDefaults(cfg *Config) {
	if cfg.Chat.IdleTTL == 0 {
		cfg.Chat.IdleTTL = time.Hour
	}

	if cfg.Chat.MaxConcurrent == 0 {
		cfg.Chat.MaxConcurrent = 8
	}

	if cfg.Chat.DBPath == "" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			home, _ := os.UserHomeDir()
			state = filepath.Join(home, ".local", "state")
		}

		cfg.Chat.DBPath = filepath.Join(state, "contextmatrix", "chats.db")
	}

	if cfg.Chat.ResumeBudgetTokens == 0 {
		cfg.Chat.ResumeBudgetTokens = 40000
	}

	if cfg.Chat.RehydrationTimeout == 0 {
		cfg.Chat.RehydrationTimeout = 10 * time.Minute
	}

	if len(cfg.Chat.Models) == 0 {
		cfg.Chat.Models = map[string]ChatModelConfig{
			"claude-sonnet-4-6":         {Label: "Sonnet 4.6", MaxTokens: 1000000},
			"claude-opus-4-7":           {Label: "Opus 4.7", MaxTokens: 1000000},
			"claude-haiku-4-5-20251001": {Label: "Haiku 4.5", MaxTokens: 200000},
		}
	}

	if cfg.Chat.DefaultModel == "" {
		cfg.Chat.DefaultModel = "claude-sonnet-4-6"
	}
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

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_AUTH_MODE"); v != "" {
		cfg.GitHub.AuthMode = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_APP_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.GitHub.App.AppID = id
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_GITHUB_APP_ID", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_INSTALLATION_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.GitHub.App.InstallationID = id
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_GITHUB_INSTALLATION_ID", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_PRIVATE_KEY_PATH"); v != "" {
		cfg.GitHub.App.PrivateKeyPath = v
	}

	if v := os.Getenv("CONTEXTMATRIX_GITHUB_PAT_TOKEN"); v != "" {
		cfg.GitHub.PAT.Token = v
	}

	if v := os.Getenv("CONTEXTMATRIX_HEARTBEAT_TIMEOUT"); v != "" {
		cfg.HeartbeatTimeout = v
	}

	if v := os.Getenv("CONTEXTMATRIX_CORS_ORIGIN"); v != "" {
		cfg.CORSOrigin = v
	}

	if v := os.Getenv("CONTEXTMATRIX_WORKFLOW_SKILLS_DIR"); v != "" {
		cfg.WorkflowSkillsDir = v
	}

	if v := os.Getenv("CONTEXTMATRIX_TASK_SKILLS_DIR"); v != "" {
		cfg.TaskSkills.Dir = v
	}

	if v := os.Getenv("CONTEXTMATRIX_TASK_SKILLS_GIT_REMOTE_URL"); v != "" {
		cfg.TaskSkills.GitRemoteURL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_TASK_SKILLS_GIT_CLONE_ON_EMPTY"); v != "" {
		cfg.TaskSkills.GitCloneOnEmpty = v == "true" || v == "1"
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

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_SONNET_MODEL"); v != "" {
		cfg.Runner.OrchestratorSonnetModel = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_ORCHESTRATOR_OPUS_MODEL"); v != "" {
		cfg.Runner.OrchestratorOpusModel = v
	}

	if v := os.Getenv("CONTEXTMATRIX_RUNNER_RECONCILE_INTERVAL"); v != "" {
		cfg.Runner.ReconcileInterval = v
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

	if v := os.Getenv("CONTEXTMATRIX_CHAT_DB_PATH"); v != "" {
		cfg.Chat.DBPath = v
	}

	if v := os.Getenv("CONTEXTMATRIX_CHAT_IDLE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Chat.IdleTTL = d
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_CHAT_IDLE_TTL", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_CHAT_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Chat.MaxConcurrent = n
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_CHAT_MAX_CONCURRENT", "value", v, "error", err)
		}
	}
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// StalledCheckIntervalDuration parses StalledCheckInterval as a
// time.Duration. Validate ensures the string parses and is positive,
// so this returns nil error in normal flow.
func (c *Config) StalledCheckIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(c.StalledCheckInterval)
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
