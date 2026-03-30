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
  context?: string[];
  labels?: string[];
  source?: Source;
  custom?: Record<string, unknown>;
  created: string;
  updated: string;
  activity_log?: ActivityEntry[];
  body: string;
}

export interface ProjectConfig {
  name: string;
  prefix: string;
  next_id: number;
  repo?: string;
  states: string[];
  types: string[];
  priorities: string[];
  transitions: Record<string, string[]>;
  templates?: Record<string, string>;
}

export interface CardFilter {
  state?: string;
  type?: string;
  priority?: string;
  agent?: string;
  label?: string;
  parent?: string;
  external_id?: string;
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
  | 'card.log_added';

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
}

export interface PatchCardInput {
  title?: string;
  state?: string;
  priority?: string;
  labels?: string[];
  body?: string;
}

export interface CardContext {
  card: Card;
  project: ProjectConfig;
  template?: string;
}
