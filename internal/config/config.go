package config

import (
	"bytes"
	"crypto/rand"
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

	"github.com/mhersson/contextmatrix/internal/board"
)

// ModelRate defines per-token cost rates for a model.
type ModelRate struct {
	Prompt     float64 `yaml:"prompt"`
	Completion float64 `yaml:"completion"`
	CacheRead  float64 `yaml:"cache_read"`
	CacheWrite float64 `yaml:"cache_write"`
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

// Backend name constants - the closed set of valid backends mapping keys.
const (
	BackendNameAgent = "agent"
	BackendNameChat  = "chat"
)

// Backend callback paths. The agent and chat repos hardcode these - they
// must not change.
const (
	AgentCallbackPath = "/api/agent"
	ChatCallbackPath  = "/api/chat"
)

// Auth modes. AuthModeMulti (the default) requires login; AuthModeNone is
// the single-user zero-login behavior CM always had.
const (
	AuthModeMulti = "multi"
	AuthModeNone  = "none"
)

// Per-backend CONTEXTMATRIX_BACKEND_<NAME>_* env var suffixes.
// applyAgentBackendEnv / applyChatBackendEnv read each one;
// checkBackendEnvKeys allowlists the same sets - keep them in sync.
var (
	agentBackendEnvSuffixes = []string{
		"_URL",
		"_API_KEY",
		"_ENABLED",
		"_RECONCILE_INTERVAL",
		"_DEFAULT_MODEL",
		"_AA_API_KEY",
		"_MODEL_ALLOWLIST",
		"_CATALOG_QUALITY_FLOOR",
	}
	chatBackendEnvSuffixes = []string{
		"_URL",
		"_API_KEY",
		"_ENABLED",
		"_DEFAULT_MODEL",
	}
)

// Backends declares the execution backends CM drives over the
// contextmatrix-protocol webhook contract. Read once at startup -
// changing backends requires a CM restart.
type Backends struct {
	Agent *AgentBackendConfig
	Chat  *ChatBackendConfig
}

// UnmarshalYAML decodes the backends mapping with a closed key set. Unknown
// names fail loudly instead of being silently ignored, and a leftover
// runner entry gets a dedicated retirement message.
func (b *Backends) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("backends must be a mapping")
	}

	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i].Value, node.Content[i+1]

		switch key {
		case BackendNameAgent:
			b.Agent = &AgentBackendConfig{}
			if err := decodeStrictEntry(val, b.Agent); err != nil {
				return fmt.Errorf("backends.agent: %w", err)
			}
		case BackendNameChat:
			b.Chat = &ChatBackendConfig{}
			if err := decodeStrictEntry(val, b.Chat); err != nil {
				return fmt.Errorf("backends.chat: %w", err)
			}
		case "runner":
			return fmt.Errorf("backends.runner: the runner backend has been removed " +
				"(deprecate-frozen since multi-user; see the runner-eos tag for the last supported commit): " +
				"use backends.agent for task execution and backends.chat for chat")
		default:
			return fmt.Errorf("invalid backend name %q: must be one of \"agent\", \"chat\"", key)
		}
	}

	return nil
}

// decodeStrictEntry strictly decodes one mapping node into target; used by
// every per-entry block whose top-level decoder cannot enforce KnownFields
// on its own.
func decodeStrictEntry(node *yaml.Node, target any) error {
	raw, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("re-marshal entry: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	if err := dec.Decode(target); err != nil {
		return err
	}

	return nil
}

// AgentBackendConfig is the contextmatrix-agent task-execution backend entry.
type AgentBackendConfig struct {
	URL     string `yaml:"url"`     // base URL, e.g. http://localhost:9090
	APIKey  string `yaml:"api_key"` // shared HMAC secret for this backend
	Enabled *bool  `yaml:"enabled"` // nil means enabled (omitting = active)
	// ReconcileInterval - CM's sweep is the agent backend's only reconcile
	// mechanism; defaults to 60s when the entry is enabled.
	ReconcileInterval string `yaml:"reconcile_interval"`
	// DefaultModel is the default orchestrator model slug; per-card pins override it.
	DefaultModel string `yaml:"default_model"`

	// Catalog and selection inputs.
	AAAPIKey       string                         `yaml:"aa_api_key"`
	ModelAllowlist []string                       `yaml:"model_allowlist"`
	Favorites      map[string]board.TierFavorites `yaml:"favorites"`

	// AAModelMap maps each endpoint model slug to its Artificial Analysis model
	// stem, for the openai endpoint type. Empty for the openrouter type (which
	// uses the built-in slug mapping).
	AAModelMap map[string]string `yaml:"aa_model_map"`

	// ModelPriors supplies a direct selection prior for an endpoint slug AA does
	// not rate (a brand-new release, or a private model). When present for a
	// slug, the AA join is skipped for that slug and these priors are used
	// verbatim. openai type only.
	ModelPriors map[string]PriorOverride `yaml:"model_priors"`

	// CatalogQualityFloor is the minimum normalized quality score (0..1) a
	// model's coder or reviewer prior must clear on at least one role to stay
	// in the candidate pool. 0 means unset, and modelcatalog.NewBuilder's
	// existing <=0 fallback keeps it at 0.65. Validated to be in [0, 1).
	CatalogQualityFloor float64 `yaml:"catalog_quality_floor"`
}

// ChatBackendConfig is the contextmatrix-chat chat-serving backend entry.
type ChatBackendConfig struct {
	URL     string `yaml:"url"`
	APIKey  string `yaml:"api_key"`
	Enabled *bool  `yaml:"enabled"`
	// DefaultModel - required when enabled (contextmatrix-chat has no
	// server-side default; CM supplies it as CM_MODEL).
	DefaultModel string `yaml:"default_model"`
}

// IsEnabled reports whether the agent entry is present and active. A nil
// receiver (entry not declared) is disabled; nil Enabled defaults to true
// (declaring the entry = active; disabled entries are inert placeholders).
func (a *AgentBackendConfig) IsEnabled() bool {
	return a != nil && (a.Enabled == nil || *a.Enabled)
}

// IsEnabled reports whether the chat entry is present and active. Same
// nil-safe semantics as AgentBackendConfig.IsEnabled.
func (c *ChatBackendConfig) IsEnabled() bool {
	return c != nil && (c.Enabled == nil || *c.Enabled)
}

// ReconcileIntervalDuration parses ReconcileInterval. Zero/unset/invalid
// returns 0, which disables the reconcile sweep.
func (a *AgentBackendConfig) ReconcileIntervalDuration() time.Duration {
	if a.ReconcileInterval == "" {
		return 0
	}

	d, err := time.ParseDuration(a.ReconcileInterval)
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

// DefaultBoardsName is the repo name the single-repo map form gets.
const DefaultBoardsName = "boards"

var boardsNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// boardsEnvSuffixes are the CONTEXTMATRIX_BOARDS_* overrides. They apply to
// the single-repo form only; with a list of repos any of them set is an
// error, because a value that silently landed on one entry of many would
// be the hardest misconfiguration to spot.
var boardsEnvSuffixes = []string{
	"_DIR", "_GIT_AUTO_COMMIT", "_GIT_AUTO_PUSH", "_GIT_AUTO_PULL", "_GIT_PULL_INTERVAL",
	"_GIT_DEFERRED_COMMIT", "_GIT_CLONE_ON_EMPTY", "_GIT_REMOTE_URL", "_SHARED",
	"_LEASE_INTERVAL", "_LEASE_TIMEOUT",
}

// BoardsConfig is one boards git repository: where it lives, how it commits
// and syncs, and whether other instances write to it too.
type BoardsConfig struct {
	// Name identifies the repo in the API (boards_repo, /api/sync) and the
	// UI. Required in the list form; the map form is named "boards".
	Name              string `yaml:"name"`
	Dir               string `yaml:"dir"`
	GitAutoCommit     bool   `yaml:"git_auto_commit"`
	GitDeferredCommit bool   `yaml:"git_deferred_commit"`
	GitAutoPush       bool   `yaml:"git_auto_push"`
	GitAutoPull       bool   `yaml:"git_auto_pull"`
	GitPullInterval   string `yaml:"git_pull_interval"`
	GitCloneOnEmpty   bool   `yaml:"git_clone_on_empty"`
	GitRemoteURL      string `yaml:"git_remote_url"`
	// Shared marks a repo that several ContextMatrix instances write to
	// through its remote. Forces auto commit, auto pull and auto push, and
	// switches the syncer to the merge-and-resolve path.
	Shared bool `yaml:"shared"`
	// LeaseInterval (shared only) is how often a live claim's lease is
	// rewritten on the remote. LeaseTimeout is how long a peer's pushed lease
	// may stay unchanged before another instance may stall the card.
	LeaseInterval string `yaml:"lease_interval"`
	LeaseTimeout  string `yaml:"lease_timeout"`
}

func defaultBoardsConfig() BoardsConfig {
	return BoardsConfig{
		Name:            DefaultBoardsName,
		GitAutoCommit:   true,
		GitPullInterval: "60s",
		LeaseInterval:   "5m",
		LeaseTimeout:    "1h",
	}
}

// PullIntervalDuration parses GitPullInterval as a time.Duration.
func (e BoardsConfig) PullIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(e.GitPullInterval)
}

// LeaseIntervalDuration parses LeaseInterval as a time.Duration.
func (e BoardsConfig) LeaseIntervalDuration() (time.Duration, error) {
	return time.ParseDuration(e.LeaseInterval)
}

// LeaseTimeoutDuration parses LeaseTimeout as a time.Duration.
func (e BoardsConfig) LeaseTimeoutDuration() (time.Duration, error) {
	return time.ParseDuration(e.LeaseTimeout)
}

// Boards is the ordered list of boards repositories. Order is significant:
// the first entry is the default target for creation and the earlier repo
// wins when two repos hold a project of the same name. The YAML key accepts
// a mapping (one repo, named "boards") or a list of mappings.
type Boards []BoardsConfig

// UnmarshalYAML accepts the map form and the list form. Each entry is
// re-marshalled through a KnownFields decoder so a typo fails startup.
func (b *Boards) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		entry := defaultBoardsConfig()
		if err := decodeStrictEntry(node, &entry); err != nil {
			return fmt.Errorf("boards: %w", err)
		}

		if entry.Name == "" {
			entry.Name = DefaultBoardsName
		}

		*b = Boards{entry}
	case yaml.SequenceNode:
		out := make(Boards, 0, len(node.Content))

		for i, item := range node.Content {
			entry := defaultBoardsConfig()
			entry.Name = ""

			if err := decodeStrictEntry(item, &entry); err != nil {
				return fmt.Errorf("boards[%d]: %w", i, err)
			}

			if entry.Name == "" {
				return fmt.Errorf("boards[%d]: name is required in the list form", i)
			}

			out = append(out, entry)
		}

		*b = out
	default:
		return fmt.Errorf("boards must be a mapping or a list of mappings")
	}

	return nil
}

// AnyShared reports whether at least one repo is shared with other instances.
func (b Boards) AnyShared() bool {
	for _, e := range b {
		if e.Shared {
			return true
		}
	}

	return false
}

// Names returns the repo names in config order.
func (b Boards) Names() []string {
	names := make([]string, len(b))
	for i, e := range b {
		names[i] = e.Name
	}

	return names
}

// InstanceConfig identifies this server among the instances sharing a boards
// repo. ID is generated once and persisted when not configured.
type InstanceConfig struct {
	ID string `yaml:"id"`
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
}

// MobGuest is one operator-registered external A2A discussion participant.
// Config-file-only by design - Token is a bearer secret and the env-override
// mechanism is scalar-only, so guests have no CONTEXTMATRIX_* override.
type MobGuest struct {
	Name  string `yaml:"name"`
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

// MobConfig bounds the mob session discussion mode (agent backend). A card
// with mob_participants >= 2 convenes N internal discussion seats in its
// plan/review phases; guests join by registry name.
type MobConfig struct {
	// MaxParticipants is the hard cap on a card's mob_participants value
	// (and the UI selector bound). Default 5, sane range 2..10.
	MaxParticipants int `yaml:"max_participants"`
	// DefaultParticipants is the operator-recommended seat count, surfaced in
	// the UI control's tooltip and shipped via app config. Default 3.
	DefaultParticipants int `yaml:"default_participants"`
	// DefaultRounds is the critique-round count sent on every trigger
	// (round 0, the blind proposal round, never counts). Default 2.
	DefaultRounds int `yaml:"default_rounds"`
	// MaxRounds is the server clamp on critique rounds. Default 3, max 5.
	MaxRounds int `yaml:"max_rounds"`
	// BudgetFactor scales the per-card cost ceiling for discussions:
	// mob budget = budget_factor x max card cost. Default 0.75, range (0, 5].
	BudgetFactor float64 `yaml:"budget_factor"`
	// ExecuteCheckpointsEnabled gates the "execute" mob session phase
	// server-side. nil/omitted = enabled (the default); set false to have a
	// card's "execute" phase dropped at trigger time with a warning.
	ExecuteCheckpointsEnabled *bool `yaml:"execute_checkpoints_enabled"`
	// CheckpointMinTier is the minimum subtask tier that gets an execute
	// checkpoint: simple|moderate|complex|critical. Default "simple" -
	// checkpoint everything until outcome data argues for a higher floor.
	CheckpointMinTier string `yaml:"checkpoint_min_tier"`
	// CheckpointRounds is the critique-round count for execute-checkpoint
	// discussions (plan/review keep default_rounds). Default 3, clamped
	// 1..max_rounds.
	CheckpointRounds int `yaml:"checkpoint_rounds"`
	// Guests is the operator-managed guest registry. Cards reference entries
	// by Name in mob_guests.
	Guests []MobGuest `yaml:"guests"`
}

// ExecuteCheckpoints reports whether the "execute" mob phase is allowed:
// enabled unless the operator set execute_checkpoints_enabled: false.
func (m MobConfig) ExecuteCheckpoints() bool {
	return m.ExecuteCheckpointsEnabled == nil || *m.ExecuteCheckpointsEnabled
}

// AuthConfig controls multi-user authentication.
type AuthConfig struct {
	// Mode is "multi" (default: login required, sessions, admin role) or
	// "none" (single-user, no login - exactly the pre-multi-user behavior).
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
	// containers. 0 (the default when unset) means unlimited.
	MaxConcurrent int `yaml:"max_concurrent"`

	// ResumeBudgetTokens caps the rough token estimate the transcript
	// builder will fit into the rehydration payload on cold-reopen.
	// Default: 40000.
	ResumeBudgetTokens int `yaml:"resume_budget_tokens"`

	// RehydrationTimeout forces the per-session rehydration phase off
	// after this duration, even if the agent never called
	// chat_rehydration_complete and the user never typed. Default: 10m.
	RehydrationTimeout time.Duration `yaml:"rehydration_timeout"`
}

// Config holds the application configuration.
type Config struct {
	Port             int    `yaml:"port"`
	Boards           Boards `yaml:"boards"`
	HeartbeatTimeout string `yaml:"heartbeat_timeout"`
	// AwaitMax bounds how long a single await_subtasks MCP call may block
	// server-side before returning to the caller. Awaiting clients re-call
	// on timeout, so this bounds one HTTP request, not the total wait.
	AwaitMax string `yaml:"await_max"`
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
	// Backends declares the agent (task execution) and chat backend entries.
	// The mapping's key set is closed - see Backends.UnmarshalYAML.
	Backends      Backends       `yaml:"backends"`
	GitHub        GitHubConfig   `yaml:"github"`
	LogFormat     string         `yaml:"log_format"`      // "json" or "text", default "text"
	LogLevel      string         `yaml:"log_level"`       // "debug"/"info"/"warn"/"error", default "info"
	AdminPort     int            `yaml:"admin_port"`      // 0 = disabled
	AdminBindAddr string         `yaml:"admin_bind_addr"` // listen address for admin server (pprof + /metrics); default "127.0.0.1"
	Chat          ChatConfig     `yaml:"chat"`
	Images        ImagesConfig   `yaml:"images"`
	OpStore       OpStoreConfig  `yaml:"op_store"`
	BestOfN       BestOfNConfig  `yaml:"best_of_n"`
	Mob           MobConfig      `yaml:"mob"`
	Auth          AuthConfig     `yaml:"auth"`
	Instance      InstanceConfig `yaml:"instance"`
}

func defaults() *Config {
	return &Config{
		Port:                 8080,
		Boards:               Boards{defaultBoardsConfig()},
		HeartbeatTimeout:     "30m",
		AwaitMax:             "8m",
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
	if len(c.Boards) == 0 {
		return fmt.Errorf("boards is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR")
	}

	for i := range c.Boards {
		if c.Boards[i].Dir == "" {
			return fmt.Errorf("%s.dir is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR", c.boardsLabel(i))
		}
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

	awaitMax, err := time.ParseDuration(c.AwaitMax)
	if err != nil {
		return fmt.Errorf("invalid await_max %q: %w", c.AwaitMax, err)
	}

	// Zero or negative would make every blocking wait return instantly, turning
	// await_subtasks back into the poll it replaces. There is no "disabled"
	// semantics to express here.
	if awaitMax <= 0 {
		return fmt.Errorf("invalid await_max %q: must be positive", c.AwaitMax)
	}

	if c.StalledCheckInterval == "" {
		c.StalledCheckInterval = "1m"
	}

	if d, err := time.ParseDuration(c.StalledCheckInterval); err != nil {
		return fmt.Errorf("invalid stalled_check_interval %q: %w", c.StalledCheckInterval, err)
	} else if d <= 0 {
		return fmt.Errorf("stalled_check_interval must be positive (got %s)", d)
	}

	if err := c.validateBoards(); err != nil {
		return err
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

	// applyBackendDefaults fills per-entry knob defaults for backends that
	// omit them. Idempotent; mirrors the applyChatDefaults pattern so
	// callers that bypass Load still get defaults applied.
	//
	// Unknown backend names, leftover runner entries, and stale per-entry
	// fields are rejected earlier, during Load's YAML parse - see
	// Backends.UnmarshalYAML. Only enabled-entry field checks remain here;
	// disabled entries are inert placeholders.
	applyBackendDefaults(c)

	if a := c.Backends.Agent; a.IsEnabled() {
		if a.URL == "" {
			return fmt.Errorf("backends.agent.url is required")
		}

		if len(a.APIKey) < MinBackendAPIKeyLength {
			return fmt.Errorf("backends.agent.api_key must be at least %d characters", MinBackendAPIKeyLength)
		}

		if a.ReconcileInterval != "" {
			if _, err := time.ParseDuration(a.ReconcileInterval); err != nil {
				return fmt.Errorf("invalid backends.agent.reconcile_interval %q: %w", a.ReconcileInterval, err)
			}
		}

		if !(a.CatalogQualityFloor >= 0 && a.CatalogQualityFloor < 1) {
			return fmt.Errorf("backends.agent.catalog_quality_floor must be in [0, 1) (got %v)", a.CatalogQualityFloor)
		}
	}

	if ch := c.Backends.Chat; ch.IsEnabled() {
		if ch.URL == "" {
			return fmt.Errorf("backends.chat.url is required")
		}

		if len(ch.APIKey) < MinBackendAPIKeyLength {
			return fmt.Errorf("backends.chat.api_key must be at least %d characters", MinBackendAPIKeyLength)
		}

		// The dedicated chat backend (contextmatrix-chat) has no server-side
		// default model - CM supplies it as CM_MODEL on every chat-start - so
		// default_model is mandatory when the entry is enabled.
		if ch.DefaultModel == "" {
			return fmt.Errorf("backends.chat.default_model is required when the chat backend " +
				"is enabled (contextmatrix-chat has no server-side default; it is supplied as CM_MODEL)")
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
	// reject negatives - a negative IdleTTL would have the reaper end every
	// session immediately and a negative MaxConcurrent would reject every
	// open.
	applyChatDefaults(c)
	applyImagesDefaults(c)
	applyOpStoreDefaults(c)
	applyBestOfNDefaults(c)
	applyMobDefaults(c)

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

	// Mob hard checks: numeric fields were normalized by applyMobDefaults
	// above (warn + default, never fatal); explicit bad enum/guest entries
	// fail startup loudly instead.
	switch c.Mob.CheckpointMinTier {
	case "simple", "moderate", "complex", "critical":
		// valid
	default:
		return fmt.Errorf("mob.checkpoint_min_tier must be one of \"simple\", \"moderate\", \"complex\", \"critical\" (got %q)",
			c.Mob.CheckpointMinTier)
	}

	seenGuests := make(map[string]bool, len(c.Mob.Guests))

	for i, g := range c.Mob.Guests {
		if g.Name == "" {
			return fmt.Errorf("mob.guests[%d]: name is required", i)
		}

		if seenGuests[g.Name] {
			return fmt.Errorf("mob.guests[%d]: duplicate guest name %q", i, g.Name)
		}

		seenGuests[g.Name] = true

		if !strings.HasPrefix(g.URL, "http://") && !strings.HasPrefix(g.URL, "https://") {
			return fmt.Errorf("mob.guests[%d] (%s): url must start with http:// or https:// (got %q)", i, g.Name, g.URL)
		}

		if g.Token == "" {
			return fmt.Errorf("mob.guests[%d] (%s): token is required", i, g.Name)
		}
	}

	return nil
}

// boardsLabel names an entry in error messages. A single entry that is
// unnamed or carries the default name is the single-repo form, so its
// messages stay what they always were ("boards"); every other entry -
// including an explicitly-named single-entry list - is named "boards[<name>]".
func (c *Config) boardsLabel(i int) string {
	name := c.Boards[i].Name
	if len(c.Boards) == 1 && (name == "" || name == DefaultBoardsName) {
		return "boards"
	}

	return fmt.Sprintf("boards[%s]", name)
}

// validateBoards checks every repo entry, then the names and directories
// against each other. Runs after resolvePaths, so dirs are tilde-expanded.
// The instance.id check runs last, after every entry has otherwise validated -
// mirroring the single-repo form, where a bad remote or lease field was
// always reported before a bad instance id.
func (c *Config) validateBoards() error {
	heartbeat, _ := time.ParseDuration(c.HeartbeatTimeout)

	seen := make(map[string]bool, len(c.Boards))
	dirs := make([]string, len(c.Boards))

	for i := range c.Boards {
		e := &c.Boards[i]
		label := c.boardsLabel(i)

		if (len(c.Boards) > 1 || (e.Name != "" && e.Name != DefaultBoardsName)) && !boardsNamePattern.MatchString(e.Name) {
			return fmt.Errorf("%s.name %q is invalid: must match %s", label, e.Name, boardsNamePattern)
		}

		if seen[e.Name] {
			return fmt.Errorf("boards: duplicate name %q", e.Name)
		}

		seen[e.Name] = true

		abs, err := filepath.Abs(e.Dir)
		if err != nil {
			return fmt.Errorf("%s.dir: %w", label, err)
		}

		dirs[i] = filepath.Clean(abs)

		for j := range i {
			if dirs[i] == dirs[j] {
				return fmt.Errorf("%s.dir %q is the same directory as %s.dir", label, e.Dir, c.boardsLabel(j))
			}

			if strings.HasPrefix(dirs[i], dirs[j]+string(filepath.Separator)) || strings.HasPrefix(dirs[j], dirs[i]+string(filepath.Separator)) {
				return fmt.Errorf("%s.dir %q nests with %s.dir %q; boards repos may not contain each other", label, e.Dir, c.boardsLabel(j), c.Boards[j].Dir)
			}
		}

		if err := e.validate(label, heartbeat); err != nil {
			return err
		}
	}

	if c.Boards.AnyShared() && !instanceIDPattern.MatchString(c.Instance.ID) {
		return fmt.Errorf("instance.id %q is invalid: must match %s", c.Instance.ID, instanceIDPattern)
	}

	return nil
}

// validate checks one repo entry and fills its defaults. label is the
// prefix its errors carry.
func (e *BoardsConfig) validate(label string, heartbeat time.Duration) error {
	if e.GitPullInterval == "" {
		e.GitPullInterval = "60s"
	}

	if _, err := time.ParseDuration(e.GitPullInterval); err != nil {
		return fmt.Errorf("invalid %s.git_pull_interval %q: %w", label, e.GitPullInterval, err)
	}

	if e.GitCloneOnEmpty && e.GitRemoteURL == "" {
		return fmt.Errorf("%s.git_remote_url is required when %s.git_clone_on_empty is enabled", label, label)
	}

	if e.GitRemoteURL != "" && !strings.HasPrefix(e.GitRemoteURL, "https://") {
		return fmt.Errorf("%s.git_remote_url must start with https:// (got %q)", label, e.GitRemoteURL)
	}

	if !e.Shared {
		return nil
	}

	if e.GitRemoteURL == "" {
		return fmt.Errorf("%s.git_remote_url is required when %s.shared is true", label, label)
	}

	if e.GitDeferredCommit {
		return fmt.Errorf("%s.git_deferred_commit must be false when %s.shared is true", label, label)
	}

	if !e.GitAutoCommit {
		return fmt.Errorf("%s.git_auto_commit must be true when %s.shared is true", label, label)
	}

	e.GitAutoPull = true
	e.GitAutoPush = true

	if e.LeaseInterval == "" {
		e.LeaseInterval = "5m"
	}

	if e.LeaseTimeout == "" {
		e.LeaseTimeout = "1h"
	}

	leaseInterval, err := time.ParseDuration(e.LeaseInterval)
	if err != nil {
		return fmt.Errorf("invalid %s.lease_interval %q: %w", label, e.LeaseInterval, err)
	}

	leaseTimeout, err := time.ParseDuration(e.LeaseTimeout)
	if err != nil {
		return fmt.Errorf("invalid %s.lease_timeout %q: %w", label, e.LeaseTimeout, err)
	}

	if leaseInterval >= heartbeat {
		return fmt.Errorf("%s.lease_interval (%s) must be shorter than heartbeat_timeout (%s), or a live claim could be stalled before its lease is renewed", label, leaseInterval, heartbeat)
	}

	pull, _ := time.ParseDuration(e.GitPullInterval)

	floor := heartbeat + 2*pull + leaseInterval
	if leaseTimeout <= floor {
		return fmt.Errorf("%s.lease_timeout must exceed heartbeat_timeout + 2 * git_pull_interval + lease_interval (%s), got %s", label, floor, leaseTimeout)
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
			applyMobDefaults(cfg)
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
	applyMobDefaults(cfg)
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
	for i := range cfg.Boards {
		dir, err := expandTilde(cfg.Boards[i].Dir)
		if err != nil {
			return err
		}

		cfg.Boards[i].Dir = dir
	}

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

var instanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// LoadOrCreateInstanceID returns the instance ID stored at path, creating
// the file with a fresh <hostname>-<6 hex> value on first use.
func LoadOrCreateInstanceID(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if instanceIDPattern.MatchString(id) {
			return id, nil
		}
	}

	host, _ := os.Hostname()
	host = strings.ToLower(host)
	host = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(host, "-")
	host = strings.Trim(host, "-")

	if host == "" {
		host = "cm"
	}

	if len(host) > 40 {
		host = host[:40]
	}

	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate instance id: %w", err)
	}

	id := fmt.Sprintf("%s-%x", host, raw)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}

	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write instance id: %w", err)
	}

	return id, nil
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
// Idempotent - safe to call again after applyEnvOverrides may have
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
}

// applyMobDefaults sets Mob fields that are zero or out of range.
// Idempotent - safe to call again after applyEnvOverrides may have
// introduced a new out-of-range value (see the call inside Validate, which
// runs after applyEnvOverrides completes in Load). Order matters:
// MaxParticipants before DefaultParticipants, MaxRounds before DefaultRounds
// and before CheckpointRounds - the dependent checks read the
// already-normalized bound.
func applyMobDefaults(cfg *Config) {
	if cfg.Mob.MaxParticipants < 2 || cfg.Mob.MaxParticipants > 10 {
		if cfg.Mob.MaxParticipants != 0 {
			slog.Warn("mob.max_participants outside 2..10; using default", "value", cfg.Mob.MaxParticipants)
		}

		cfg.Mob.MaxParticipants = 5
	}

	if cfg.Mob.DefaultParticipants < 2 || cfg.Mob.DefaultParticipants > cfg.Mob.MaxParticipants {
		if cfg.Mob.DefaultParticipants != 0 {
			slog.Warn("mob.default_participants out of range; using default", "value", cfg.Mob.DefaultParticipants)
		}

		cfg.Mob.DefaultParticipants = min(3, cfg.Mob.MaxParticipants)
	}

	if cfg.Mob.MaxRounds < 1 || cfg.Mob.MaxRounds > 5 {
		if cfg.Mob.MaxRounds != 0 {
			slog.Warn("mob.max_rounds outside 1..5; using default", "value", cfg.Mob.MaxRounds)
		}

		cfg.Mob.MaxRounds = 3
	}

	if cfg.Mob.DefaultRounds < 1 || cfg.Mob.DefaultRounds > cfg.Mob.MaxRounds {
		if cfg.Mob.DefaultRounds != 0 {
			slog.Warn("mob.default_rounds outside 1..max_rounds; using default", "value", cfg.Mob.DefaultRounds)
		}

		cfg.Mob.DefaultRounds = min(2, cfg.Mob.MaxRounds)
	}

	if cfg.Mob.BudgetFactor <= 0 || cfg.Mob.BudgetFactor > 5 {
		if cfg.Mob.BudgetFactor != 0 {
			slog.Warn("mob.budget_factor outside (0, 5]; using default", "value", cfg.Mob.BudgetFactor)
		}

		cfg.Mob.BudgetFactor = 0.75
	}

	if cfg.Mob.CheckpointRounds < 1 || cfg.Mob.CheckpointRounds > cfg.Mob.MaxRounds {
		if cfg.Mob.CheckpointRounds != 0 {
			slog.Warn("mob.checkpoint_rounds outside 1..max_rounds; using default", "value", cfg.Mob.CheckpointRounds)
		}

		cfg.Mob.CheckpointRounds = min(3, cfg.Mob.MaxRounds)
	}

	if cfg.Mob.CheckpointMinTier == "" {
		cfg.Mob.CheckpointMinTier = "simple"
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

// AgentBackend returns the backend responsible for task execution (the agent
// entry), true iff it is declared and enabled.
func (c *Config) AgentBackend() (*AgentBackendConfig, bool) {
	if c.Backends.Agent.IsEnabled() {
		return c.Backends.Agent, true
	}

	return nil, false
}

// ChatBackend returns the backend responsible for chat containers (the chat
// entry), true iff it is declared and enabled.
func (c *Config) ChatBackend() (*ChatBackendConfig, bool) {
	if c.Backends.Chat.IsEnabled() {
		return c.Backends.Chat, true
	}

	return nil, false
}

// applyBackendDefaults fills per-entry defaults for fields not supplied by
// YAML. Idempotent. Load calls it before applyEnvOverrides, so an entry
// flipped on via CONTEXTMATRIX_BACKEND_<NAME>_ENABLED gets its task defaults
// only from the second run inside Validate - keep that re-run if the Load
// sequence is ever reordered (pinned by TestBackendEnvEnableGetsDefaults).
//
// The reconcile default applies to the agent entry only. CM's sweep is the
// agent backend's ONLY reconcile mechanism (the agent has no internal loop),
// so leaving the field empty would silently disable the container backstop.
// The chat entry has no reconcile_interval field by construction.
func applyBackendDefaults(cfg *Config) {
	if a := cfg.Backends.Agent; a.IsEnabled() && a.ReconcileInterval == "" {
		a.ReconcileInterval = "60s"
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

	if len(cfg.Boards) > 1 {
		var set []string

		for _, suffix := range boardsEnvSuffixes {
			if os.Getenv("CONTEXTMATRIX_BOARDS"+suffix) != "" {
				set = append(set, "CONTEXTMATRIX_BOARDS"+suffix)
			}
		}

		if len(set) > 0 {
			return fmt.Errorf("%s: CONTEXTMATRIX_BOARDS_* overrides apply to the single-repo boards form only; this config lists %d boards repos", strings.Join(set, ", "), len(cfg.Boards))
		}
	} else {
		if len(cfg.Boards) == 0 {
			cfg.Boards = Boards{defaultBoardsConfig()}
		}

		b := &cfg.Boards[0]

		if v := os.Getenv("CONTEXTMATRIX_BOARDS_DIR"); v != "" {
			b.Dir = v
		}

		b.GitAutoCommit = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_COMMIT", b.GitAutoCommit)
		b.GitAutoPush = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PUSH", b.GitAutoPush)
		b.GitAutoPull = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_AUTO_PULL", b.GitAutoPull)

		if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_PULL_INTERVAL"); v != "" {
			b.GitPullInterval = v
		}

		b.GitDeferredCommit = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_DEFERRED_COMMIT", b.GitDeferredCommit)
		b.GitCloneOnEmpty = parseBoolEnv("CONTEXTMATRIX_BOARDS_GIT_CLONE_ON_EMPTY", b.GitCloneOnEmpty)

		if v := os.Getenv("CONTEXTMATRIX_BOARDS_GIT_REMOTE_URL"); v != "" {
			b.GitRemoteURL = v
		}

		b.Shared = parseBoolEnv("CONTEXTMATRIX_BOARDS_SHARED", b.Shared)

		if v := os.Getenv("CONTEXTMATRIX_BOARDS_LEASE_INTERVAL"); v != "" {
			b.LeaseInterval = v
		}

		if v := os.Getenv("CONTEXTMATRIX_BOARDS_LEASE_TIMEOUT"); v != "" {
			b.LeaseTimeout = v
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_INSTANCE_ID"); v != "" {
		cfg.Instance.ID = v
	}

	if cfg.Boards.AnyShared() && cfg.Instance.ID == "" {
		id, err := LoadOrCreateInstanceID(defaultSQLiteDBPath("instance_id"))
		if err != nil {
			return fmt.Errorf("resolve instance id: %w", err)
		}

		cfg.Instance.ID = id
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

	if v := os.Getenv("CONTEXTMATRIX_AWAIT_MAX"); v != "" {
		cfg.AwaitMax = v
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

	// The outcome-bias mechanism was retired. A stale YAML outcome_floor key
	// fails startup via strict decoding, but env-configured deployments get
	// no such signal - warn so the variable gets cleaned up.
	if v := os.Getenv("CONTEXTMATRIX_BEST_OF_N_OUTCOME_FLOOR"); v != "" {
		slog.Warn("CONTEXTMATRIX_BEST_OF_N_OUTCOME_FLOOR is removed and ignored; unset it", "value", v)
	}

	for _, o := range []struct {
		env string
		dst *int
	}{
		{"CONTEXTMATRIX_BEST_OF_N_MAX_CANDIDATES", &cfg.BestOfN.MaxCandidates},
		{"CONTEXTMATRIX_BEST_OF_N_DEFAULT_CANDIDATES", &cfg.BestOfN.DefaultCandidates},
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

	for _, o := range []struct {
		env string
		dst *int
	}{
		{"CONTEXTMATRIX_MOB_MAX_PARTICIPANTS", &cfg.Mob.MaxParticipants},
		{"CONTEXTMATRIX_MOB_DEFAULT_PARTICIPANTS", &cfg.Mob.DefaultParticipants},
		{"CONTEXTMATRIX_MOB_DEFAULT_ROUNDS", &cfg.Mob.DefaultRounds},
		{"CONTEXTMATRIX_MOB_MAX_ROUNDS", &cfg.Mob.MaxRounds},
		{"CONTEXTMATRIX_MOB_CHECKPOINT_ROUNDS", &cfg.Mob.CheckpointRounds},
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

	if v := os.Getenv("CONTEXTMATRIX_MOB_BUDGET_FACTOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Mob.BudgetFactor = f
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_MOB_BUDGET_FACTOR", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_MOB_EXECUTE_CHECKPOINTS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Mob.ExecuteCheckpointsEnabled = &b
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_MOB_EXECUTE_CHECKPOINTS_ENABLED", "value", v, "error", err)
		}
	}

	if v := os.Getenv("CONTEXTMATRIX_MOB_CHECKPOINT_MIN_TIER"); v != "" {
		cfg.Mob.CheckpointMinTier = v
	}

	// Backend entries can be configured entirely via env: a variable for one
	// of the two backends creates the entry when YAML does not declare it,
	// so pure-env deployments need no backends stub in the config file.
	if err := applyAgentBackendEnv(cfg); err != nil {
		return err
	}

	if err := applyChatBackendEnv(cfg); err != nil {
		return err
	}

	return nil
}

// anyBackendEnvSet reports whether any prefix+suffix env var is set.
func anyBackendEnvSet(prefix string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if os.Getenv(prefix+suffix) != "" {
			return true
		}
	}

	return false
}

// parseBackendEnabledEnv parses the <prefix>_ENABLED env var, returning nil
// when the variable is unset.
func parseBackendEnabledEnv(prefix string) (*bool, error) {
	v := os.Getenv(prefix + "_ENABLED")
	if v == "" {
		return nil, nil //nolint:nilnil // nil means "env var unset - leave YAML value"
	}

	enabled, err := strconv.ParseBool(v)
	if err != nil {
		return nil, fmt.Errorf("invalid %s_ENABLED %q: must be true/false/1/0: %w", prefix, v, err)
	}

	return &enabled, nil
}

// applyAgentBackendEnv applies the CONTEXTMATRIX_BACKEND_AGENT_* overrides.
// Entry-creating: when the agent entry is not declared in YAML and any of its
// env vars is set, the entry is allocated first.
func applyAgentBackendEnv(cfg *Config) error {
	const prefix = "CONTEXTMATRIX_BACKEND_AGENT"

	if cfg.Backends.Agent == nil {
		if !anyBackendEnvSet(prefix, agentBackendEnvSuffixes) {
			return nil
		}

		cfg.Backends.Agent = &AgentBackendConfig{}
	}

	a := cfg.Backends.Agent

	if v := os.Getenv(prefix + "_URL"); v != "" {
		a.URL = v
	}

	if v := os.Getenv(prefix + "_API_KEY"); v != "" {
		a.APIKey = v
	}

	enabled, err := parseBackendEnabledEnv(prefix)
	if err != nil {
		return err
	}

	if enabled != nil {
		a.Enabled = enabled
	}

	// The env layer just sets the interval; Validate parses it, so a bad
	// value fails loudly with the entry-scoped error.
	if v := os.Getenv(prefix + "_RECONCILE_INTERVAL"); v != "" {
		a.ReconcileInterval = v
	}

	if v := os.Getenv(prefix + "_DEFAULT_MODEL"); v != "" {
		a.DefaultModel = v
	}

	if v := os.Getenv(prefix + "_AA_API_KEY"); v != "" {
		a.AAAPIKey = v
	}

	if v := os.Getenv(prefix + "_MODEL_ALLOWLIST"); v != "" {
		var allow []string

		for s := range strings.SplitSeq(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				allow = append(allow, s)
			}
		}

		a.ModelAllowlist = allow
	}

	if v := os.Getenv(prefix + "_CATALOG_QUALITY_FLOOR"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid %s_CATALOG_QUALITY_FLOOR %q: %w", prefix, v, err)
		}

		a.CatalogQualityFloor = f
	}

	return nil
}

// applyChatBackendEnv applies the CONTEXTMATRIX_BACKEND_CHAT_* overrides.
// Entry-creating like applyAgentBackendEnv.
func applyChatBackendEnv(cfg *Config) error {
	const prefix = "CONTEXTMATRIX_BACKEND_CHAT"

	if cfg.Backends.Chat == nil {
		if !anyBackendEnvSet(prefix, chatBackendEnvSuffixes) {
			return nil
		}

		cfg.Backends.Chat = &ChatBackendConfig{}
	}

	c := cfg.Backends.Chat

	if v := os.Getenv(prefix + "_URL"); v != "" {
		c.URL = v
	}

	if v := os.Getenv(prefix + "_API_KEY"); v != "" {
		c.APIKey = v
	}

	enabled, err := parseBackendEnabledEnv(prefix)
	if err != nil {
		return err
	}

	if enabled != nil {
		c.Enabled = enabled
	}

	if v := os.Getenv(prefix + "_DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}

	return nil
}

// checkBackendEnvKeys rejects CONTEXTMATRIX_BACKEND_* variables that do not
// map to a known (name, suffix) pair: the name must be agent or chat, and the
// suffix must be in that backend's suffix set. A typo'd or stale variable -
// including any CONTEXTMATRIX_BACKEND_RUNNER_* leftover, or an agent-only
// suffix on the chat entry - must fail loudly, not silently configure
// nothing. The entry does not need to be declared in YAML -
// applyEnvOverrides creates it.
func checkBackendEnvKeys() error {
	known := map[string]bool{}

	for _, suffix := range agentBackendEnvSuffixes {
		known["CONTEXTMATRIX_BACKEND_AGENT"+suffix] = true
	}

	for _, suffix := range chatBackendEnvSuffixes {
		known["CONTEXTMATRIX_BACKEND_CHAT"+suffix] = true
	}

	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, "CONTEXTMATRIX_BACKEND_") {
			continue
		}

		if !known[key] {
			return fmt.Errorf("%s is not a recognised backend env var "+
				"(agent suffixes: %s; chat suffixes: %s) - fix the variable name",
				key,
				strings.Join(agentBackendEnvSuffixes, ", "),
				strings.Join(chatBackendEnvSuffixes, ", "))
		}
	}

	return nil
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// AwaitMaxDuration parses AwaitMax as a time.Duration.
func (c *Config) AwaitMaxDuration() (time.Duration, error) {
	return time.ParseDuration(c.AwaitMax)
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
