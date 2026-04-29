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
  estimated_cost_usd: number;
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
  feature_branch?: boolean;
  create_pr?: boolean;
  base_branch?: string;
  skills?: string[] | null;
}

export interface UpdateCardInput {
  title: string;
  type: string;
  state: string;
  priority: string;
  labels?: string[];
  parent?: string;
  subtasks?: string[];
  depends_on?: string[];
  context?: string[];
  custom?: Record<string, unknown>;
  body?: string;
  skills?: string[] | null;
}

export interface PatchCardInput {
  title?: string;
  state?: string;
  priority?: string;
  labels?: string[];
  body?: string;
  autonomous?: boolean;
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

export interface CardContext {
  card: Card;
  project: ProjectConfig;
  template?: string;
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

export interface CardCost {
  card_id: string;
  card_title: string;
  assigned_agent?: string;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_cost_usd: number;
}

export interface DashboardData {
  state_counts: Record<string, number>;
  active_agents: ActiveAgent[];
  total_cost_usd: number;
  cards_completed_today: number;
  agent_costs: AgentCost[];
  card_costs: CardCost[];
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
}

export interface StopAllResponse {
  affected_cards: string[];
}

export type RunnerStatus = NonNullable<Card['runner_status']>;

export type LogEntryType = 'text' | 'thinking' | 'tool_call' | 'stderr' | 'system' | 'user' | 'gap';

export interface LogEntry {
  ts: string;
  card_id: string;
  type: LogEntryType;
  content: string;
  /** Sequence number from server, used for gap detection. */
  seq?: number;
}

export interface AppConfig {
  theme: 'everforest' | 'radix' | 'catppuccin';
  version: string;
}

export const runnerStatusStyles: Record<RunnerStatus, { bg: string; text: string; label: string }> = {
  queued: { bg: 'var(--bg-yellow)', text: 'var(--yellow)', label: 'Queued for runner' },
  running: { bg: 'var(--bg-blue)', text: 'var(--aqua)', label: 'Running on runner' },
  failed: { bg: 'var(--bg-red)', text: 'var(--red)', label: 'Runner failed' },
  killed: { bg: 'var(--bg4)', text: 'var(--grey1)', label: 'Runner killed' },
};
