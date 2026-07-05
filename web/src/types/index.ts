export interface Source {
  system: string;
  external_id: string;
  external_url: string;
}

export interface ActivityEntry {
  agent: string;
  ts: string;
  action: string;
  message: string;
}

export interface Card {
  id: string;
  title: string;
  project: string;
  type: string;
  state: string;
  priority: string;
  assigned_agent?: string;
  last_heartbeat?: string;
  parent?: string;
  subtasks?: string[];
  depends_on?: string[];
  dependencies_met?: boolean;
  context?: string[];
  labels?: string[];
  source?: Source;
  vetted?: boolean;
  custom?: Record<string, unknown>;
  autonomous?: boolean;
  use_opus_orchestrator?: boolean;
  model_orchestrator?: string;
  model_coder?: string;
  model_reviewer?: string;
  best_of_n?: number;
  phase?: string;
  feature_branch?: boolean;
  create_pr?: boolean;
  branch_name?: string;
  base_branch?: string;
  pr_url?: string;
  review_attempts?: number;
  runner_status?: 'queued' | 'running' | 'failed' | 'killed';
  created: string;
  updated: string;
  activity_log?: ActivityEntry[];
  token_usage?: TokenUsage;
  usage_breakdown?: UsageBucket[];
  body: string;
  // skills uses three-state semantics (matching the backend):
  //   undefined / null — use project default (or full set if project default is null)
  //   []               — mount no skills for this card
  //   [name, ...]      — constrain to listed skills
  skills?: string[] | null;
}

export interface TaskSkillSummary {
  name: string;
  description: string;
}

export interface TokenUsage {
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  estimated_cost_usd: number;
}

export interface UsageBucket {
  agent: string;
  model: string;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  cost_usd: number;
  cost_source: 'actual' | 'estimated';
}

export interface GitHubImportConfig {
  import_issues: boolean;
  owner?: string;
  repo?: string;
  card_type?: string;
  default_priority?: string;
  labels?: string[];
}

export interface ProjectConfig {
  name: string;
  display_name?: string;
  prefix: string;
  next_id: number;
  repo?: string;
  states: string[];
  types: string[];
  priorities: string[];
  transitions: Record<string, string[]>;
  remote_execution?: {
    enabled?: boolean;
    runner_image?: string;
  };
  github?: GitHubImportConfig;
  templates?: Record<string, string>;
  // default_skills uses three-state semantics:
  //   undefined / null — mount the full task-skills set for cards that don't override
  //   []               — mount no skills (cards override per-card if needed)
  //   [name, ...]      — constrain cards to listed skills
  default_skills?: string[] | null;
  /**
   * Reference-only instance credential-pool entry name (multi-user mode).
   * Empty/absent means the instance `github.*` config default applies.
   */
  github_credential?: string;
}

export interface CardFilter {
  state?: string;
  type?: string;
  priority?: string;
  agent?: string;
  label?: string;
  parent?: string;
  external_id?: string;
  vetted?: boolean;
  autonomous?: boolean;
  runner_status?: string;
}

export interface APIError {
  error: string;
  code: string;
  details?: string;
}

export type EventType =
  | 'card.created'
  | 'card.updated'
  | 'card.deleted'
  | 'card.state_changed'
  | 'card.claimed'
  | 'card.released'
  | 'card.stalled'
  | 'card.log_added'
  | 'card.usage_reported'
  | 'project.created'
  | 'project.updated'
  | 'project.deleted'
  | 'sync.started'
  | 'sync.completed'
  | 'sync.conflict'
  | 'sync.error'
  | 'runner.triggered'
  | 'runner.started'
  | 'runner.failed'
  | 'runner.killed';

export interface SyncStatus {
  last_sync_time: string | null;
  last_sync_error?: string;
  syncing: boolean;
  enabled: boolean;
}

export interface BoardEvent {
  type: EventType;
  project: string;
  card_id: string;
  agent?: string;
  timestamp: string;
  data?: Record<string, unknown>;
}

export interface CreateCardInput {
  title: string;
  type: string;
  priority?: string;
  labels?: string[];
  parent?: string;
  body?: string;
  source?: Source;
  autonomous?: boolean;
  use_opus_orchestrator?: boolean;
  model_orchestrator?: string;
  model_coder?: string;
  model_reviewer?: string;
  feature_branch?: boolean;
  create_pr?: boolean;
  base_branch?: string;
  skills?: string[] | null;
}

export interface PatchCardInput {
  title?: string;
  type?: string;
  state?: string;
  priority?: string;
  labels?: string[];
  body?: string;
  autonomous?: boolean;
  use_opus_orchestrator?: boolean;
  model_orchestrator?: string;
  model_coder?: string;
  model_reviewer?: string;
  best_of_n?: number;
  feature_branch?: boolean;
  create_pr?: boolean;
  base_branch?: string;
  vetted?: boolean;
  // skills: explicit list (or empty) goes here; pure JSON cannot
  // distinguish absent from null, so use skills_clear to express
  // "set back to nil so the project default applies".
  skills?: string[] | null;
  skills_clear?: boolean;
}

export interface ActiveAgent {
  agent_id: string;
  card_id: string;
  card_title: string;
  since: string;
  last_heartbeat: string;
}

export interface AgentCost {
  agent_id: string;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_cost_usd: number;
  card_count: number;
}

export interface ModelCost {
  model: string;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_cost_usd: number;
  card_count: number;
}

export interface CardCost {
  card_id: string;
  card_title: string;
  assigned_agent?: string;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_cost_usd: number;
}

export interface MetricSeries {
  active_agents: number[];
  in_flight: number[];
  stalled: number[];
  shipped: number[];
  in_flight_parents: number[];
  stalled_parents: number[];
  shipped_parents: number[];
}

export interface DashboardData {
  state_counts: Record<string, number>;
  state_counts_parents: Record<string, number>;
  active_agents: ActiveAgent[];
  total_cost_usd: number;
  total_cost_usd_last_30d?: number;
  total_cost_usd_prior_30d?: number;
  cost_series_30d?: number[];
  cards_completed_today: number;
  cards_completed_today_parents: number;
  cards_completed_last_7d: number;
  cards_completed_last_7d_parents: number;
  cards_completed_prior_7d: number;
  cards_completed_prior_7d_parents: number;
  metric_series: MetricSeries;
  agent_costs: AgentCost[];
  model_costs: ModelCost[];
  card_costs: CardCost[];
  // Server-wide chat cost aggregates (not per-project; cached 30s server-side).
  chat_cost_usd_last_30d?: number;
  chat_cost_usd_prior_30d?: number;
  chat_cost_series_30d?: number[];
}

export interface ActivityFeedEntry {
  agent: string;
  action: string;
  message?: string;
  card_id: string;
  ts: string;
}

export interface ActivityFeedResponse {
  items: ActivityFeedEntry[];
}

export interface RunnerHealth {
  ok: boolean;
  running_containers: number;
  max_concurrent: number;
}

export interface CreateProjectInput {
  name: string;
  display_name?: string;
  prefix: string;
  repo?: string;
  states: string[];
  types: string[];
  priorities: string[];
  transitions: Record<string, string[]>;
}

export interface UpdateProjectInput {
  repo?: string;
  states: string[];
  types: string[];
  priorities: string[];
  transitions: Record<string, string[]>;
  github?: GitHubImportConfig;
  default_skills?: string[] | null;
  /**
   * Pointer-application semantics on the server: key omitted (undefined)
   * preserves the current binding; "" clears it back to the instance
   * default; a name sets/replaces it (422 if unknown, multi mode only).
   */
  github_credential?: string;
}

export interface StopAllResponse {
  affected_cards: string[];
}

export type RunnerStatus = NonNullable<Card['runner_status']>;

export type LogEntryType =
  | 'text'
  | 'thinking'
  | 'tool_call'
  | 'stderr'
  | 'system'
  | 'user'
  | 'gap';

export interface LogEntry {
  ts: string;
  card_id: string;
  type: LogEntryType;
  content: string;
  /** Sequence number from server, used for gap detection. */
  seq?: number;
  /** True when this message was produced during a chat-mode rehydration phase. */
  rehydration_phase?: boolean;
  /**
   * Structural marker for chat messages — e.g. "divider" for the "Context
   * cleared" sentinel appended on Clear Context. Empty / absent means a
   * regular message. The frontend matches on this rather than content
   * strings so the rendering rule is unambiguous and survives reloads.
   */
  kind?: string;
}

export type AuthMode = 'multi' | 'none';

export interface AppConfig {
  theme: 'everforest' | 'radix' | 'catppuccin';
  version: string;
  /**
   * Active auth mode: "multi" (login required) or "none" (single-tenant,
   * no auth). Absent on servers older than the multi-user rollout — treat
   * as "none". The slim pre-login shape (unauthenticated multi-mode
   * response) omits task_backend/favorites, hence both are optional below.
   */
  auth_mode?: AuthMode;
  /**
   * Active task-execution backend: "runner" or "agent" (may be "" when no
   * task backend is configured). Drives which automation controls render.
   */
  task_backend?: string;
  /**
   * Operator-configured favorite model slugs per tier (key = tier name,
   * value = All slugs for that tier). Only present when the agent backend
   * has favorites configured. Tiers with only ByRole slugs are excluded.
   */
  favorites?: Record<string, string[]>;
  /**
   * Best-of-N bounds for the card selector: the hard cap (`max_candidates`)
   * and the operator-recommended candidate count, surfaced in the control's
   * tooltip. Only present on the full (post-login) payload — absent from the
   * slim pre-login multi-mode response, same as task_backend/favorites.
   */
  best_of_n_max?: number;
  best_of_n_default?: number;
}

export interface SessionUser {
  username: string;
  display_name: string;
  is_admin: boolean;
}

export interface TokenInfo {
  purpose: 'bootstrap' | 'invite' | 'reset';
  username: string;
}

export interface RedeemTokenInput {
  username?: string;
  display_name?: string;
  password: string;
}

// Admin — user and credential management (multi-user mode, admin-only).
export interface AdminUser {
  username: string;
  display_name: string;
  is_admin: boolean;
  disabled: boolean;
  has_password: boolean;
  last_login_at?: string;
}

export interface InviteInfo {
  token: string;
  purpose: 'invite' | 'reset';
  expires_at: string;
}

export interface CredentialInfo {
  name: string;
  kind: 'pat' | 'app';
  host: string;
  api_base_url: string;
  app_id: number;
  installation_id: number;
  created_by: string;
  disabled: boolean;
  created_at: string;
  updated_at: string;
  last_used_at?: string;
}

// Request-only input — carries the plaintext secret up to the server. Never
// mirrored back in CredentialInfo (the response type has no secret field).
export interface CreateCredentialInput {
  name: string;
  kind: 'pat' | 'app';
  host?: string;
  api_base_url?: string;
  app_id?: number;
  installation_id?: number;
  secret: string;
}

// Admin — Best-of-N model-outcome stats. Registered in both auth modes (see
// docs/api-reference.md § GET /api/admin/model-outcomes); `win_rate` is a
// fraction (0..1), not a percentage.
export interface ModelOutcomeEntry {
  model: string;
  samples: number;
  wins: number;
  win_rate: number;
  expected_wins: number;
  total_cost_usd: number;
  active: boolean;
}

export interface ModelOutcomeStats {
  outcome_floor: number;
  total_samples: number;
  models: ModelOutcomeEntry[];
}

export type ChatStatus = 'cold' | 'active' | 'warm-idle' | 'ending';

export interface ChatSession {
  id: string;
  title: string;
  project?: string;
  status: ChatStatus;
  created_at: string;
  last_active: string;
  created_by: string;
  container_id?: string;
  workspace?: string[];
  model?: string;
  context_tokens?: number;
  context_tokens_updated_at?: string;
  rehydration_active?: boolean;
  // Token counters and cost — cumulative totals from all usage frames.
  prompt_tokens?: number;
  completion_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  /** Running total in USD. Precision floor ~$0.0001. */
  estimated_cost_usd?: number;
}

export interface ChatMessage {
  id: number;
  session_id: string;
  seq: number;
  role: string;
  content: string;
  created_at: string;
  rehydration_phase?: boolean;
  /** Structural marker (e.g. "divider"). See LogEntry.kind. */
  kind?: string;
}

export interface ChatModel {
  id: string;
  label: string;
  max_tokens: number;
}

export interface ChatModelList {
  // source tells the New Chat picker which mode to render:
  //  - 'config': runner serves chat → `models` is the chat.models allowlist.
  //  - 'openrouter': dedicated chat backend serves chat → `models` is CM's
  //    vendor-screened OpenRouter catalog (id/label = slug, max_tokens =
  //    context window); empty only when the server catalog is unfetched.
  //  - 'endpoint': server-provided list from the configured OpenAI-compatible
  //    endpoint; rendered like 'config' (a <select> over the server models[]).
  source: 'config' | 'openrouter' | 'endpoint';
  models: ChatModel[];
  default: string;
}

export interface ModelCatalogEntry {
  id: string;
  max_tokens: number;
}

export interface ModelCatalogResponse {
  // 'openrouter' = CM's vendor-screened OpenRouter list; 'endpoint' = the
  // endpoint's served list; 'none' = no catalog builder configured server-side.
  source: 'openrouter' | 'endpoint' | 'none';
  models: ModelCatalogEntry[];
}

export interface ChatSessionUpdate {
  context_tokens?: number;
  context_tokens_updated_at?: string;
  model?: string;
  rehydration_active?: boolean;
  status?: ChatStatus;
  // Cost and token counters — new running totals after each usage frame.
  estimated_cost_usd?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
}
