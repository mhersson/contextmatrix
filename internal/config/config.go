package config

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ModelRate defines per-token cost rates for a model.
type ModelRate struct {
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

// MinBackendAPIKeyLength is the minimum required length for a backend
// entry's api_key.
const MinBackendAPIKeyLength = 32

// backendNamePattern restricts backends map keys to lowercase alphanumerics
// so the CONTEXTMATRIX_BACKEND_<NAME>_* env-override scheme stays unambiguous.
var backendNamePattern = regexp.MustCompile(`^[a-z0-9]+$`)

// reservedCallbackPaths is the closed set of webhook callback prefixes the
// router understands. /api/runner is contextmatrix-runner's; /api/agent and
// /api/chat are reserved for future backends.
var reservedCallbackPaths = map[string]bool{
	"/api/runner": true,
	"/api/agent":  true,
	"/api/chat":   true,
}

// BackendConfig is one entry in the backends map: an execution backend CM
// can drive over the contextmatrix-protocol webhook contract. Read once at
// startup — changing backends or selectors requires a CM restart.
type BackendConfig struct {
	URL          string `yaml:"url"`           // base URL, e.g. http://localhost:9090
	APIKey       string `yaml:"api_key"`       // shared HMAC secret for this backend
	CallbackPath string `yaml:"callback_path"` // reserved prefix this backend's callbacks mount at

	// contextmatrix-runner-specific knobs; other backend types leave them empty.
	OrchestratorSonnetModel string `yaml:"orchestrator_sonnet_model"`
	OrchestratorOpusModel   string `yaml:"orchestrator_opus_model"`
	ReconcileInterval       string `yaml:"reconcile_interval"`
}

// ReconcileIntervalDuration parses ReconcileInterval. Zero/unset/invalid
// returns 0, which disables the reconcile sweep.
func (b *BackendConfig) ReconcileIntervalDuration() time.Duration {
	if b.ReconcileInterval == "" {
		return 0
	}

	d, err := time.ParseDuration(b.ReconcileInterval)
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

// ImagesConfig configures the image upload + storage subsystem.
type ImagesConfig struct {
	// DBPath is the SQLite file path for stored images.
	// Defaults to <XDG_STATE_HOME>/contextmatrix/images.db, falling back to
	// ~/.local/state/contextmatrix/images.db.
	DBPath string `yaml:"db_path"`
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
	// containers. 0 means unlimited. Leave unset to use the default (8).
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
	TokenCosts           map[string]ModelRate `yaml:"token_costs"`
	MCPAPIKey            string               `yaml:"mcp_api_key"`
	Runner               RunnerConfig         `yaml:"runner"`
	// DefaultBackend names the backends entry that executes cards. Empty
	// disables task execution. Restart required to change.
	DefaultBackend string `yaml:"default_backend"`
	// ChatBackendName names the backends entry that runs chat containers.
	// Independent of DefaultBackend. Empty disables chat containers.
	ChatBackendName string `yaml:"chat_backend"`
	// Backends maps a backend name to its connection config.
	Backends      map[string]BackendConfig `yaml:"backends"`
	GitHub        GitHubConfig             `yaml:"github"`
	LogFormat     string                   `yaml:"log_format"`      // "json" or "text", default "text"
	LogLevel      string                   `yaml:"log_level"`       // "debug"/"info"/"warn"/"error", default "info"
	AdminPort     int                      `yaml:"admin_port"`      // 0 = disabled
	AdminBindAddr string                   `yaml:"admin_bind_addr"` // listen address for admin server (pprof + /metrics); default "127.0.0.1"
	Chat          ChatConfig               `yaml:"chat"`
	Images        ImagesConfig             `yaml:"images"`
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
			OrchestratorOpusModel:   "claude-opus-4-8",
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

	// applyBackendDefaults fills per-entry knob defaults for backends that
	// omit them. Idempotent; mirrors the applyChatDefaults pattern so
	// callers that bypass Load still get defaults applied.
	applyBackendDefaults(c)

	seenCallbackPaths := map[string]string{}

	for name, b := range c.Backends {
		if !backendNamePattern.MatchString(name) {
			return fmt.Errorf("invalid backend name %q: must match ^[a-z0-9]+$ (the CONTEXTMATRIX_BACKEND_<NAME>_* env scheme depends on it)", name)
		}

		if b.URL == "" {
			return fmt.Errorf("backends[%q].url is required", name)
		}

		if len(b.APIKey) < MinBackendAPIKeyLength {
			return fmt.Errorf("backends[%q].api_key must be at least %d characters", name, MinBackendAPIKeyLength)
		}

		if !reservedCallbackPaths[b.CallbackPath] {
			return fmt.Errorf("backends[%q].callback_path %q must be one of /api/runner, /api/agent, /api/chat", name, b.CallbackPath)
		}

		if other, dup := seenCallbackPaths[b.CallbackPath]; dup {
			return fmt.Errorf("backends[%q].callback_path %q already used by backends[%q]", name, b.CallbackPath, other)
		}

		seenCallbackPaths[b.CallbackPath] = name

		if b.ReconcileInterval != "" {
			if _, err := time.ParseDuration(b.ReconcileInterval); err != nil {
				return fmt.Errorf("invalid backends[%q].reconcile_interval %q: %w", name, b.ReconcileInterval, err)
			}
		}
	}

	if c.DefaultBackend != "" {
		if _, ok := c.Backends[c.DefaultBackend]; !ok {
			return fmt.Errorf("default_backend %q is not in backends", c.DefaultBackend)
		}
	}

	if c.ChatBackendName != "" {
		if _, ok := c.Backends[c.ChatBackendName]; !ok {
			return fmt.Errorf("chat_backend %q is not in backends", c.ChatBackendName)
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
	applyImagesDefaults(c)

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
//
// Returns an empty string when no config file is found in any standard
// location. Callers should treat an empty return as "no config found" and
// either exit with a clear error or require the operator to supply -config
// explicitly.
func FindConfigPath() string {
	var candidates []string

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "contextmatrix", "config.yaml"))
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "contextmatrix", "config.yaml"))
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	slog.Warn("no config file found in standard XDG locations; use -config to specify a path",
		"searched", candidates)

	return ""
}

// Load reads configuration from the given YAML file and applies environment overrides.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyChatDefaults(cfg)
			applyImagesDefaults(cfg)
			applyBackendDefaults(cfg)
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

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyChatDefaults(cfg)
	applyImagesDefaults(cfg)
	applyBackendDefaults(cfg)
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

// defaultSQLiteDBPath returns the conventional XDG-compliant location for a
// per-contextmatrix SQLite database with the given filename. Honors
// $XDG_STATE_HOME first, then falls back to ~/.local/state. Shared between
// every applyXxxDefaults helper so the on-disk layout has a single source of
// truth.
func defaultSQLiteDBPath(filename string) string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(state, "contextmatrix", filename)
}

// applyImagesDefaults sets Images fields that were not supplied by YAML.
func applyImagesDefaults(cfg *Config) {
	if cfg.Images.DBPath == "" {
		cfg.Images.DBPath = defaultSQLiteDBPath("images.db")
	}
}

// TaskBackendConfig resolves the default_backend selector. ok is false when
// task execution is disabled (empty selector).
func (c *Config) TaskBackendConfig() (BackendConfig, bool) {
	if c.DefaultBackend == "" {
		return BackendConfig{}, false
	}

	b, ok := c.Backends[c.DefaultBackend]

	return b, ok
}

// ChatBackendConfig resolves the chat_backend selector. ok is false when
// chat execution is disabled (empty selector).
func (c *Config) ChatBackendConfig() (BackendConfig, bool) {
	if c.ChatBackendName == "" {
		return BackendConfig{}, false
	}

	b, ok := c.Backends[c.ChatBackendName]

	return b, ok
}

// applyBackendDefaults fills per-entry defaults for fields not supplied by
// YAML. Idempotent.
func applyBackendDefaults(cfg *Config) {
	for name, b := range cfg.Backends {
		if b.OrchestratorSonnetModel == "" {
			b.OrchestratorSonnetModel = "claude-sonnet-4-6"
		}

		if b.OrchestratorOpusModel == "" {
			b.OrchestratorOpusModel = "claude-opus-4-8"
		}

		if b.ReconcileInterval == "" {
			b.ReconcileInterval = "60s"
		}

		cfg.Backends[name] = b
	}
}

// applyChatDefaults sets Chat fields that were not supplied by YAML.
func applyChatDefaults(cfg *Config) {
	if cfg.Chat.IdleTTL == 0 {
		cfg.Chat.IdleTTL = time.Hour
	}

	if cfg.Chat.DBPath == "" {
		cfg.Chat.DBPath = defaultSQLiteDBPath("chats.db")
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
			"claude-opus-4-8":           {Label: "Opus 4.8", MaxTokens: 1000000},
			"claude-haiku-4-5-20251001": {Label: "Haiku 4.5", MaxTokens: 200000},
		}
	}

	if cfg.Chat.DefaultModel == "" {
		cfg.Chat.DefaultModel = "claude-sonnet-4-6"
	}
}

// parseBoolEnv parses a boolean environment variable. It uses strconv.ParseBool
// so it accepts "1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE",
// "false", "False". On parse error it logs a warning and returns current.
func parseBoolEnv(name string, current bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return current
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("ignoring invalid bool env var", "name", name, "value", v, "error", err)

		return current
	}

	return b
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

	cfg.Boards.GitAutoCommit = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT", cfg.Boards.GitAutoCommit)
	cfg.Boards.GitAutoPush = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH", cfg.Boards.GitAutoPush)
	cfg.Boards.GitAutoPull = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL", cfg.Boards.GitAutoPull)

	if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_PULL_INTERVAL"); v != "" {
		cfg.Boards.GitPullInterval = v
	}

	cfg.Boards.GitDeferredCommit = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT", cfg.Boards.GitDeferredCommit)
	cfg.Boards.GitCloneOnEmpty = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY", cfg.Boards.GitCloneOnEmpty)

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

	cfg.TaskSkills.GitCloneOnEmpty = parseBoolEnv("CONTEXTMATRIX_TASK_SKILLS_GIT_CLONE_ON_EMPTY", cfg.TaskSkills.GitCloneOnEmpty)

	if v := os.Getenv("CONTEXTMATRIX_THEME"); v != "" {
		cfg.Theme = v
	}

	if v := os.Getenv("CONTEXTMATRIX_MCP_API_KEY"); v != "" {
		cfg.MCPAPIKey = v
	}

	cfg.Runner.Enabled = parseBoolEnv("CONTEXTMATRIX_RUNNER_ENABLED", cfg.Runner.Enabled)

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

	cfg.GitHub.IssueImporting.Enabled = parseBoolEnv("CONTEXTMATRIX_GITHUB_ISSUE_IMPORTING_ENABLED", cfg.GitHub.IssueImporting.Enabled)

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

	if v := os.Getenv("CONTEXTMATRIX_IMAGES_DB_PATH"); v != "" {
		cfg.Images.DBPath = v
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
