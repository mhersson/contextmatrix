package config

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mhersson/contextmatrix/internal/board"
)

// ModelRate defines per-token cost rates for a model.
type ModelRate struct {
	Prompt     float64 `yaml:"prompt"`
	Completion float64 `yaml:"completion"`
}

// LLMEndpointConfig is CM's connection to the inference endpoint it reads model
// metadata from (catalog for agent selection + chat picker proxy). type selects
// the wire/catalog dialect; base_url + api_key address the endpoint.
type LLMEndpointConfig struct {
	Type    string `yaml:"type"`     // "openrouter" (default) | "openai"
	BaseURL string `yaml:"base_url"` // required for "openai"; defaults to the OpenRouter models URL for "openrouter"
	APIKey  string `yaml:"api_key"`
}

func (e LLMEndpointConfig) validate() error {
	switch e.Type {
	case "", LLMEndpointTypeOpenRouter:
		return nil
	case LLMEndpointTypeOpenAI:
		if e.BaseURL == "" {
			return fmt.Errorf("llm_endpoint.base_url is required when llm_endpoint.type is \"openai\"")
		}
		if e.APIKey == "" {
			return fmt.Errorf("llm_endpoint.api_key is required when llm_endpoint.type is \"openai\"")
		}
		return nil
	default:
		return fmt.Errorf("llm_endpoint.type must be \"openrouter\" or \"openai\", got %q", e.Type)
	}
}

// PriorOverride is an operator-supplied selection prior for a single endpoint
// slug, used for the openai type when Artificial Analysis does not rate the
// model. Values are on the same normalized 0..1 scale as AA-derived priors.
type PriorOverride struct {
	Coder    float64 `yaml:"coder"`
	Reviewer float64 `yaml:"reviewer"`
}

// MinBackendAPIKeyLength is the minimum required length for a backend
// entry's api_key.
const MinBackendAPIKeyLength = 32

// LLM endpoint type values (llm_endpoint.type).
const (
	LLMEndpointTypeOpenRouter = "openrouter"
	LLMEndpointTypeOpenAI     = "openai"
)

// Backend name constants — the closed set of valid backends map keys.
// Callback paths are derived as /api/<name>.
const (
	BackendNameRunner = "runner"
	BackendNameAgent  = "agent"
	BackendNameChat   = "chat"
)

// allowedBackendNames is the closed set used for name validation and env-key
// allowlisting. Order is stable (runner, agent, chat) for error messages.
var allowedBackendNames = []string{BackendNameRunner, BackendNameAgent, BackendNameChat}

// Auth modes. AuthModeMulti (the default) requires login; AuthModeNone is
// the single-user zero-login behavior CM always had.
const (
	AuthModeMulti = "multi"
	AuthModeNone  = "none"
)

// backendEnvSuffixes are the per-entry CONTEXTMATRIX_BACKEND_<NAME>_* env
// var suffixes. applyEnvOverrides reads each one; checkBackendEnvKeys
// allowlists the same set — keep the two in sync via this list.
var backendEnvSuffixes = []string{
	"_URL",
	"_API_KEY",
	"_ENABLED",
	"_ORCHESTRATOR_SONNET_MODEL",
	"_ORCHESTRATOR_OPUS_MODEL",
	"_RECONCILE_INTERVAL",
	"_DEFAULT_MODEL",
	"_AA_API_KEY",
	"_MODEL_ALLOWLIST",
}

// BackendConfig is one entry in the backends map: an execution backend CM
// can drive over the contextmatrix-protocol webhook contract. Read once at
// startup — changing backends requires a CM restart.
type BackendConfig struct {
	URL     string `yaml:"url"`     // base URL, e.g. http://localhost:9090
	APIKey  string `yaml:"api_key"` // shared HMAC secret for this backend
	Enabled *bool  `yaml:"enabled"` // nil means enabled (omitting = active)

	// Name is the map key, set programmatically by applyBackendDefaults.
	// Never parsed from YAML.
	Name string `yaml:"-"`

	// Runner-only task knobs; agent and chat entries must leave these empty.
	OrchestratorSonnetModel string `yaml:"orchestrator_sonnet_model"`
	OrchestratorOpusModel   string `yaml:"orchestrator_opus_model"`
	ReconcileInterval       string `yaml:"reconcile_interval"`

	// DefaultModel is the default OpenRouter model slug.
	//   - agent backend: default orchestrator model; per-card pins override it.
	//   - chat backend: default model for new chats and the cold-open fallback;
	//     required when the chat backend is enabled (contextmatrix-chat has no
	//     server-side default — CM supplies it as CM_MODEL).
	// Rejected on the runner backend (Validate rejects misuse).
	DefaultModel string `yaml:"default_model"`

	// Agent-only: catalog and selection inputs.
	AAAPIKey       string                         `yaml:"aa_api_key"`
	ModelAllowlist []string                       `yaml:"model_allowlist"`
	Favorites      map[string]board.TierFavorites `yaml:"favorites"`

	// AAModelMap maps each endpoint model slug to its Artificial Analysis model
	// stem, for the openai endpoint type. Used only by the agent backend's
	// selection fusion. Empty for the openrouter type (which uses the built-in
	// slug mapping).
	AAModelMap map[string]string `yaml:"aa_model_map"`

	// ModelPriors supplies a direct selection prior for an endpoint slug AA does
	// not rate (a brand-new release, or a private model). When present for a
	// slug, the AA join is skipped for that slug and these priors are used
	// verbatim. openai type only.
	ModelPriors map[string]PriorOverride `yaml:"model_priors"`
}

// IsEnabled reports whether this entry is active. nil Enabled defaults to true
// (presence in the map = active; disabled entries are inert placeholders).
func (b BackendConfig) IsEnabled() bool {
	return b.Enabled == nil || *b.Enabled
}

// CallbackPath returns the webhook callback prefix derived from the entry name:
// /api/<name>. Name must be set (by applyBackendDefaults or the constructor
// helpers) before calling this.
func (b BackendConfig) CallbackPath() string {
	return "/api/" + b.Name
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

// OpStoreConfig configures the operational SQLite database. This single store
// holds chat sessions/transcripts and the model blacklist (ops.db).
type OpStoreConfig struct {
	// DBPath is the SQLite file path for the op store.
	// Defaults to <XDG_STATE_HOME>/contextmatrix/ops.db, falling back to
	// ~/.local/state/contextmatrix/ops.db.
	DBPath string `yaml:"db_path"`
}

// BestOfNConfig bounds the Best-of-N run mode (agent backend).
type BestOfNConfig struct {
	// MaxCandidates is the hard cap on a card's best_of_n value (and the UI
	// selector bound). Default 5.
	MaxCandidates int `yaml:"max_candidates"`
	// DefaultCandidates is the operator-recommended candidate count, surfaced in
	// the UI control's tooltip and shipped via app config. Default 3.
	DefaultCandidates int `yaml:"default_candidates"`
	// OutcomeFloor is the per-model recorded-outcome count required before
	// win-rates bias model selection. Default 20.
	OutcomeFloor int `yaml:"outcome_floor"`
}

// AuthConfig controls multi-user authentication.
type AuthConfig struct {
	// Mode is "multi" (default: login required, sessions, admin role) or
	// "none" (single-user, no login — exactly the pre-multi-user behavior).
	Mode string `yaml:"mode"`
	// MasterKeyFile is the hex-encoded 32-byte key that encrypts credential
	// secrets at rest. Auto-generated (0600) when absent.
	MasterKeyFile string `yaml:"master_key_file"`
	// SessionIdleTTL is the sliding session lifetime (renewed on use).
	SessionIdleTTL string `yaml:"session_idle_ttl"`
	// DBPath is the auth.db SQLite file (users, sessions, tokens, credentials).
	DBPath string `yaml:"db_path"`
}

// ChatConfig configures the global chat panel feature. Chat data is persisted
// in the shared operational store (op_store.db_path), not a separate DB.
type ChatConfig struct {
	// IdleTTL is how long a chat container survives after the browser
	// disconnects. Default: 1h.
	IdleTTL time.Duration `yaml:"idle_ttl"`

	// MaxConcurrent caps the number of simultaneously-running chat
	// containers. 0 means unlimited. Leave unset to use the default (8).
	MaxConcurrent int `yaml:"max_concurrent"`

	// DefaultModel is the Claude model ID used when a chat is created
	// without an explicit selection. Must be a key in Models. Default:
	// "claude-sonnet-4-6". Applies only when the runner serves chat; when
	// the dedicated chat backend (contextmatrix-chat, OpenRouter) serves
	// chat, the default comes from backends.chat.default_model instead.
	DefaultModel string `yaml:"default_model"`

	// Models is the allowlist of selectable models for new chats, keyed
	// by model ID. The values carry the human label shown in the picker
	// and the context-window denominator used by the UI usage indicator.
	// Used only when the runner serves chat (native Anthropic slugs via
	// Claude Code); ignored when the dedicated chat backend serves chat,
	// which offers the live OpenRouter catalog instead.
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
	LLMEndpoint          LLMEndpointConfig    `yaml:"llm_endpoint"`
	MCPAPIKey            string               `yaml:"mcp_api_key"`
	// Backends maps a backend name (runner, agent, chat) to its connection
	// config. Roles and callback paths are derived from the entry name.
	Backends      map[string]BackendConfig `yaml:"backends"`
	GitHub        GitHubConfig             `yaml:"github"`
	LogFormat     string                   `yaml:"log_format"`      // "json" or "text", default "text"
	LogLevel      string                   `yaml:"log_level"`       // "debug"/"info"/"warn"/"error", default "info"
	AdminPort     int                      `yaml:"admin_port"`      // 0 = disabled
	AdminBindAddr string                   `yaml:"admin_bind_addr"` // listen address for admin server (pprof + /metrics); default "127.0.0.1"
	Chat          ChatConfig               `yaml:"chat"`
	Images        ImagesConfig             `yaml:"images"`
	OpStore       OpStoreConfig            `yaml:"op_store"`
	BestOfN       BestOfNConfig            `yaml:"best_of_n"`
	Auth          AuthConfig               `yaml:"auth"`
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
		LogFormat:            "text",
		LogLevel:             "info",
		AdminPort:            0,
		AdminBindAddr:        "127.0.0.1",
		Auth: AuthConfig{
			Mode:           AuthModeMulti,
			SessionIdleTTL: "720h",
		},
	}
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	if c.Boards.Dir == "" {
		return fmt.Errorf("boards.dir is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR")
	}

	if err := c.LLMEndpoint.validate(); err != nil {
		return err
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
	// All git remote URLs must be HTTPS — SSH is not supported.
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

	applyAuthDefaults(c)

	switch c.Auth.Mode {
	case AuthModeMulti, AuthModeNone:
	default:
		return fmt.Errorf("auth.mode must be %q or %q (got %q)", AuthModeMulti, AuthModeNone, c.Auth.Mode)
	}

	if d, err := time.ParseDuration(c.Auth.SessionIdleTTL); err != nil {
		return fmt.Errorf("invalid auth.session_idle_ttl %q: %w", c.Auth.SessionIdleTTL, err)
	} else if d <= 0 {
		return fmt.Errorf("auth.session_idle_ttl must be positive (got %s)", d)
	}

	// A missing "runner" key returns a zero-value BackendConfig whose
	// Enabled pointer is nil, and IsEnabled() treats nil as enabled — so
	// map presence must be checked too (same gotcha guarded against by
	// hasRunner in the runner/agent/chat exclusivity check below).
	if runner, declared := c.Backends[BackendNameRunner]; c.Auth.Mode == AuthModeMulti && declared && runner.IsEnabled() {
		return fmt.Errorf("backends.runner is not supported when auth.mode is %q (the runner backend is deprecate-frozen): "+
			"switch the task backend to \"agent\" or set auth.mode: %q for single-user operation", AuthModeMulti, AuthModeNone)
	}

	// applyBackendDefaults fills per-entry knob defaults for backends that
	// omit them. Idempotent; mirrors the applyChatDefaults pattern so
	// callers that bypass Load still get defaults applied.
	applyBackendDefaults(c)

	allowedSet := map[string]bool{}
	for _, n := range allowedBackendNames {
		allowedSet[n] = true
	}

	for name, b := range c.Backends {
		// Name check applies to ALL entries, enabled or not (typo guard).
		if !allowedSet[name] {
			return fmt.Errorf("invalid backend name %q: must be one of \"runner\", \"agent\", \"chat\"", name)
		}

		// Disabled entries are inert placeholders — skip all further checks.
		if !b.IsEnabled() {
			continue
		}

		if b.URL == "" {
			return fmt.Errorf("backends[%q].url is required", name)
		}

		if len(b.APIKey) < MinBackendAPIKeyLength {
			return fmt.Errorf("backends[%q].api_key must be at least %d characters", name, MinBackendAPIKeyLength)
		}

		// chat is a pure chat-serving backend; task-execution knobs don't
		// apply. Checked before the duration-format check so the operator
		// sees "must not be set" rather than a misleading format error.
		if name == BackendNameChat {
			if b.OrchestratorSonnetModel != "" {
				return fmt.Errorf("backends[%q].orchestrator_sonnet_model must not be set on the chat backend", name)
			}

			if b.OrchestratorOpusModel != "" {
				return fmt.Errorf("backends[%q].orchestrator_opus_model must not be set on the chat backend", name)
			}

			if b.ReconcileInterval != "" {
				return fmt.Errorf("backends[%q].reconcile_interval must not be set on the chat backend", name)
			}

			// default_model IS allowed on the chat backend: contextmatrix-chat
			// has no server-side default, so CM supplies the OpenRouter slug as
			// CM_MODEL. It is required when the chat backend is enabled — checked
			// after the mutual-exclusivity gate below so a runner+chat misconfig
			// reports the exclusivity error first.

			continue
		}

		// agent is a task-execution-only backend; runner-only steering-wheel
		// fields (sonnet/opus model selection) are not applicable. agent uses
		// default_model instead. runner entries must not have default_model.
		if name == BackendNameAgent {
			if b.OrchestratorSonnetModel != "" {
				return fmt.Errorf("backends[%q].orchestrator_sonnet_model must not be set on the agent backend: agent backend uses default_model; orchestrator_*_model fields are runner-only", name)
			}

			if b.OrchestratorOpusModel != "" {
				return fmt.Errorf("backends[%q].orchestrator_opus_model must not be set on the agent backend: agent backend uses default_model; orchestrator_*_model fields are runner-only", name)
			}
		}

		if name == BackendNameRunner {
			if b.DefaultModel != "" {
				return fmt.Errorf("backends[%q].default_model must not be set on the runner backend: default_model is agent-only", name)
			}
		}

		if b.ReconcileInterval != "" {
			if _, err := time.ParseDuration(b.ReconcileInterval); err != nil {
				return fmt.Errorf("invalid backends[%q].reconcile_interval %q: %w", name, b.ReconcileInterval, err)
			}
		}
	}

	// runner is mutually exclusive with agent and chat: runner already serves
	// both task execution and chat, so mixing the roles creates ambiguity.
	runnerEnabled := c.Backends[BackendNameRunner].IsEnabled() && c.Backends[BackendNameRunner].URL != ""
	agentEnabled := c.Backends[BackendNameAgent].IsEnabled() && c.Backends[BackendNameAgent].URL != ""
	chatEnabled := c.Backends[BackendNameChat].IsEnabled() && c.Backends[BackendNameChat].URL != ""

	// Only treat an entry as "present+enabled" when it was actually declared
	// in the map (a missing key returns a zero BackendConfig with Enabled==nil,
	// which IsEnabled() would report true — so we must check map presence too).
	_, hasRunner := c.Backends[BackendNameRunner]
	_, hasAgent := c.Backends[BackendNameAgent]
	_, hasChat := c.Backends[BackendNameChat]

	runnerActive := hasRunner && runnerEnabled
	agentActive := hasAgent && agentEnabled
	chatActive := hasChat && chatEnabled

	if runnerActive && (agentActive || chatActive) {
		return fmt.Errorf("backends: runner is mutually exclusive with agent and chat " +
			"(runner already serves both task execution and chat); " +
			"set enabled: false on the entries you are not using")
	}

	// The dedicated chat backend (contextmatrix-chat) has no server-side default
	// model — CM supplies it as CM_MODEL on every chat-start — so default_model
	// is mandatory when that backend is enabled. Checked here (after the
	// exclusivity gate) so the error order is deterministic.
	if chatActive && c.Backends[BackendNameChat].DefaultModel == "" {
		return fmt.Errorf(`backends["chat"].default_model is required when the chat backend ` +
			`is enabled (contextmatrix-chat has no server-side default; it is supplied as CM_MODEL)`)
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
	applyOpStoreDefaults(c)
	applyBestOfNDefaults(c)

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
			applyOpStoreDefaults(cfg)
			applyBestOfNDefaults(cfg)
			applyAuthDefaults(cfg)
			applyBackendDefaults(cfg)

			if err := applyEnvOverrides(cfg); err != nil {
				return nil, err
			}

			if err := checkBackendEnvKeys(); err != nil {
				return nil, err
			}

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
	applyOpStoreDefaults(cfg)
	applyBestOfNDefaults(cfg)
	applyAuthDefaults(cfg)
	applyBackendDefaults(cfg)

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	if err := checkBackendEnvKeys(); err != nil {
		return nil, err
	}

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

	authDB, err := expandTilde(cfg.Auth.DBPath)
	if err != nil {
		return err
	}

	cfg.Auth.DBPath = authDB

	masterKey, err := expandTilde(cfg.Auth.MasterKeyFile)
	if err != nil {
		return err
	}

	cfg.Auth.MasterKeyFile = masterKey

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

// applyOpStoreDefaults sets OpStore fields that were not supplied by YAML.
func applyOpStoreDefaults(cfg *Config) {
	if cfg.OpStore.DBPath == "" {
		cfg.OpStore.DBPath = defaultSQLiteDBPath("ops.db")
	}
}

// applyBestOfNDefaults sets BestOfN fields that are zero or out of range.
// Idempotent — safe to call again after applyEnvOverrides may have
// introduced a new out-of-range value (see the call inside Validate, which
// runs after applyEnvOverrides completes in Load).
func applyBestOfNDefaults(cfg *Config) {
	if cfg.BestOfN.MaxCandidates < 2 {
		if cfg.BestOfN.MaxCandidates != 0 {
			slog.Warn("best_of_n.max_candidates < 2; using default", "value", cfg.BestOfN.MaxCandidates)
		}

		cfg.BestOfN.MaxCandidates = 5
	}

	if cfg.BestOfN.DefaultCandidates < 2 || cfg.BestOfN.DefaultCandidates > cfg.BestOfN.MaxCandidates {
		if cfg.BestOfN.DefaultCandidates != 0 {
			slog.Warn("best_of_n.default_candidates out of range; using default", "value", cfg.BestOfN.DefaultCandidates)
		}

		cfg.BestOfN.DefaultCandidates = min(3, cfg.BestOfN.MaxCandidates)
	}

	if cfg.BestOfN.OutcomeFloor < 1 {
		if cfg.BestOfN.OutcomeFloor != 0 {
			slog.Warn("best_of_n.outcome_floor < 1; using default", "value", cfg.BestOfN.OutcomeFloor)
		}

		cfg.BestOfN.OutcomeFloor = 20
	}
}

// applyAuthDefaults sets Auth fields that were not supplied by YAML. The
// mode default is set here (not only in defaults()) so callers that build a
// Config by hand and then Validate still land on multi.
func applyAuthDefaults(cfg *Config) {
	if cfg.Auth.Mode == "" {
		cfg.Auth.Mode = AuthModeMulti
	}

	if cfg.Auth.SessionIdleTTL == "" {
		cfg.Auth.SessionIdleTTL = "720h"
	}

	if cfg.Auth.DBPath == "" {
		cfg.Auth.DBPath = defaultSQLiteDBPath("auth.db")
	}

	if cfg.Auth.MasterKeyFile == "" {
		cfg.Auth.MasterKeyFile = defaultSQLiteDBPath("master.key")
	}
}

// TaskBackendConfig returns the backend responsible for task execution.
// Precedence: runner (if present+enabled) → agent (if present+enabled) → not found.
// The returned copy has Name set defensively so callers that bypass Load still
// get a usable value.
func (c *Config) TaskBackendConfig() (BackendConfig, bool) {
	for _, name := range []string{BackendNameRunner, BackendNameAgent} {
		b, ok := c.Backends[name]
		if ok && b.IsEnabled() {
			b.Name = name

			return b, true
		}
	}

	return BackendConfig{}, false
}

// ChatBackendConfig returns the backend responsible for chat containers.
// Precedence: runner (if present+enabled) → chat (if present+enabled) → not found.
// The returned copy has Name set defensively so callers that bypass Load still
// get a usable value.
func (c *Config) ChatBackendConfig() (BackendConfig, bool) {
	for _, name := range []string{BackendNameRunner, BackendNameChat} {
		b, ok := c.Backends[name]
		if ok && b.IsEnabled() {
			b.Name = name

			return b, true
		}
	}

	return BackendConfig{}, false
}

// TaskBackendServesChat reports whether the active task backend is also the
// active chat backend — true only for the runner, which serves both roles.
// The reconcile sweep's chat half cross-references CM chat sessions against
// the TASK backend's /containers list, so it must only be wired when this is
// true: with a dedicated chat backend (contextmatrix-chat, which exposes no
// /containers), every live session is absent from the task backend's list by
// construction and would be flipped to cold — killing the worker
// mid-conversation — on every tick.
func (c *Config) TaskBackendServesChat() bool {
	tb, taskOK := c.TaskBackendConfig()
	cb, chatOK := c.ChatBackendConfig()

	return taskOK && chatOK && tb.Name == cb.Name
}

// applyBackendDefaults fills per-entry defaults for fields not supplied by
// YAML. Idempotent. Load calls it before applyEnvOverrides, so an entry
// flipped on via CONTEXTMATRIX_BACKEND_<NAME>_ENABLED gets its task defaults
// only from the second run inside Validate — keep that re-run if the Load
// sequence is ever reordered (pinned by TestBackendEnvEnableGetsDefaults).
func applyBackendDefaults(cfg *Config) {
	for name, b := range cfg.Backends {
		// Name is always set from the map key — cheap and correct even on
		// disabled placeholders.
		b.Name = name

		// Orchestrator model steering is runner-only: Validate rejects the
		// sonnet/opus fields on agent and chat entries.
		if b.IsEnabled() && name == BackendNameRunner {
			if b.OrchestratorSonnetModel == "" {
				b.OrchestratorSonnetModel = "claude-sonnet-4-6"
			}

			if b.OrchestratorOpusModel == "" {
				b.OrchestratorOpusModel = "claude-opus-4-8"
			}
		}

		// The reconcile default applies to every task backend. For the agent
		// backend CM's sweep is the ONLY reconcile mechanism (the agent has no
		// internal loop — docs/agent-backend-parity.md), so leaving the entry
		// at 0 would silently disable the container backstop. Chat entries
		// must leave the field empty (Validate rejects it there).
		if b.IsEnabled() && (name == BackendNameRunner || name == BackendNameAgent) {
			if b.ReconcileInterval == "" {
				b.ReconcileInterval = "60s"
			}
		}

		cfg.Backends[name] = b
	}
}

// applyChatDefaults sets Chat fields that were not supplied by YAML.
func applyChatDefaults(cfg *Config) {
	if cfg.Chat.IdleTTL == 0 {
		cfg.Chat.IdleTTL = time.Hour
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
func applyEnvOverrides(cfg *Config) error {
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

	if v := os.Getenv("CONTEXTMATRIX_IMAGES_DB_PATH"); v != "" {
		cfg.Images.DBPath = v
	}

	if v := os.Getenv("CONTEXTMATRIX_OP_STORE_DB_PATH"); v != "" {
		cfg.OpStore.DBPath = v
	}

	if v := os.Getenv("CONTEXTMATRIX_AUTH_MODE"); v != "" {
		cfg.Auth.Mode = v
	}

	if v := os.Getenv("CONTEXTMATRIX_AUTH_MASTER_KEY_FILE"); v != "" {
		cfg.Auth.MasterKeyFile = v
	}

	if v := os.Getenv("CONTEXTMATRIX_AUTH_SESSION_IDLE_TTL"); v != "" {
		cfg.Auth.SessionIdleTTL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_AUTH_DB_PATH"); v != "" {
		cfg.Auth.DBPath = v
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

	if v := os.Getenv("CONTEXTMATRIX_LLM_ENDPOINT_TYPE"); v != "" {
		cfg.LLMEndpoint.Type = v
	}

	if v := os.Getenv("CONTEXTMATRIX_LLM_ENDPOINT_BASE_URL"); v != "" {
		cfg.LLMEndpoint.BaseURL = v
	}

	if v := os.Getenv("CONTEXTMATRIX_LLM_ENDPOINT_API_KEY"); v != "" {
		cfg.LLMEndpoint.APIKey = v
	}

	for _, o := range []struct {
		env string
		dst *int
	}{
		{"CONTEXTMATRIX_BEST_OF_N_MAX_CANDIDATES", &cfg.BestOfN.MaxCandidates},
		{"CONTEXTMATRIX_BEST_OF_N_DEFAULT_CANDIDATES", &cfg.BestOfN.DefaultCandidates},
		{"CONTEXTMATRIX_BEST_OF_N_OUTCOME_FLOOR", &cfg.BestOfN.OutcomeFloor},
	} {
		if v := os.Getenv(o.env); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				slog.Warn("invalid int env var; ignoring", "var", o.env, "value", v)

				continue
			}

			*o.dst = n
		}
	}

	// Backend entries can be configured entirely via env: a variable for one
	// of the allowed names creates the entry when YAML does not declare it,
	// so pure-env deployments need no backends stub in the config file.
	for _, name := range allowedBackendNames {
		prefix := "CONTEXTMATRIX_BACKEND_" + strings.ToUpper(name)

		anySet := false

		for _, suffix := range backendEnvSuffixes {
			if os.Getenv(prefix+suffix) != "" {
				anySet = true

				break
			}
		}

		b, declared := cfg.Backends[name]
		if !declared && !anySet {
			continue
		}

		if v := os.Getenv(prefix + "_URL"); v != "" {
			b.URL = v
		}

		if v := os.Getenv(prefix + "_API_KEY"); v != "" {
			b.APIKey = v
		}

		if v := os.Getenv(prefix + "_ENABLED"); v != "" {
			enabled, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid %s_ENABLED %q: must be true/false/1/0: %w", prefix, v, err)
			}

			b.Enabled = &enabled
		}

		// Task-only fields: the env layer just sets them; Validate rejects
		// them on the chat entry and parses the interval, so misuse fails
		// loudly with the entry-scoped error.
		if v := os.Getenv(prefix + "_ORCHESTRATOR_SONNET_MODEL"); v != "" {
			b.OrchestratorSonnetModel = v
		}

		if v := os.Getenv(prefix + "_ORCHESTRATOR_OPUS_MODEL"); v != "" {
			b.OrchestratorOpusModel = v
		}

		if v := os.Getenv(prefix + "_RECONCILE_INTERVAL"); v != "" {
			b.ReconcileInterval = v
		}

		if v := os.Getenv(prefix + "_DEFAULT_MODEL"); v != "" {
			b.DefaultModel = v
		}

		if v := os.Getenv(prefix + "_AA_API_KEY"); v != "" {
			b.AAAPIKey = v
		}

		if v := os.Getenv(prefix + "_MODEL_ALLOWLIST"); v != "" {
			var allow []string

			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					allow = append(allow, s)
				}
			}

			b.ModelAllowlist = allow
		}

		if cfg.Backends == nil {
			cfg.Backends = map[string]BackendConfig{}
		}

		cfg.Backends[name] = b
	}

	return nil
}

// checkBackendEnvKeys rejects CONTEXTMATRIX_BACKEND_<NAME>_* variables that
// do not map to a known (name, suffix) pair: the name must be in the closed
// set (runner, agent, chat) and the suffix in backendEnvSuffixes. A typo'd
// or stale variable must fail loudly, not silently configure nothing. The
// entry does not need to be declared in YAML — applyEnvOverrides creates it.
func checkBackendEnvKeys() error {
	known := map[string]bool{}

	for _, name := range allowedBackendNames {
		pfx := "CONTEXTMATRIX_BACKEND_" + strings.ToUpper(name)
		for _, suffix := range backendEnvSuffixes {
			known[pfx+suffix] = true
		}
	}

	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, "CONTEXTMATRIX_BACKEND_") {
			continue
		}

		if !known[key] {
			return fmt.Errorf("%s is not a recognised backend env var "+
				"(names: %s; suffixes: %s) — fix the variable name",
				key,
				strings.Join(allowedBackendNames, ", "),
				strings.Join(backendEnvSuffixes, ", "))
		}
	}

	return nil
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// SessionIdleTTLDuration parses the sliding session lifetime.
func (c *Config) SessionIdleTTLDuration() (time.Duration, error) {
	return time.ParseDuration(c.Auth.SessionIdleTTL)
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
