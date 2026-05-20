import type {
  ActiveAgent,
  AgentCost,
  CardCost,
  DashboardData,
  ModelCost,
  ProjectConfig,
} from '../../types';

export interface ProjectRow {
  config: ProjectConfig;
  data: DashboardData | undefined;
  total: number;
  cost: number;
}

export interface DistributionSegment {
  state: string;
  count: number;
  color: string;
}

// Operations-overview state palette. Intentionally diverges from
// `web/src/lib/chip.ts` because that lib encodes kanban-chip semantics
// (e.g. `blocked` = bug-priority orange) while the operations dashboard
// uses status-traffic-light semantics (blocked = "attention" yellow,
// in_progress = "active" aqua) per the design spec.
const STATE_COLOR: Record<string, string> = {
  todo: 'var(--fg)',
  in_progress: 'var(--aqua)',
  hitl: 'var(--aqua)',
  review: 'var(--blue)',
  blocked: 'var(--yellow)',
  done: 'var(--green)',
  stalled: 'var(--red)',
  not_planned: 'var(--grey1)',
};

// Distribution-bar segments need lower-contrast fills for "no progress"
// states so the bar reads as a histogram rather than a colour wash.
const DISTRIBUTION_OVERRIDE: Record<string, string> = {
  todo: 'var(--bg3)',
  not_planned: 'var(--bg4)',
};

/** Foreground colour for a state word (activity feed, status text). */
export function stateColor(state: string): string {
  return STATE_COLOR[state] ?? 'var(--fg)';
}

/** Fill colour for the distribution mini-bar segments in the projects table. */
export function distributionColor(state: string): string {
  return DISTRIBUTION_OVERRIDE[state] ?? STATE_COLOR[state] ?? 'var(--grey0)';
}

const STATE_ORDER = [
  'todo',
  'in_progress',
  'hitl',
  'review',
  'blocked',
  'done',
  'stalled',
  'not_planned',
];

export function distributionSegments(counts: Record<string, number>): DistributionSegment[] {
  const out: DistributionSegment[] = [];
  const seen = new Set<string>();
  for (const state of STATE_ORDER) {
    const count = counts[state] ?? 0;
    if (count > 0) {
      out.push({ state, count, color: distributionColor(state) });
      seen.add(state);
    }
  }
  // Unknown states (custom .board.yaml additions) — sort by state name so
  // the order is deterministic across servers and renders.
  const extras: Array<[string, number]> = [];
  for (const [state, count] of Object.entries(counts)) {
    if (seen.has(state) || count <= 0) continue;
    extras.push([state, count]);
  }
  extras.sort((a, b) => a[0].localeCompare(b[0]));
  for (const [state, count] of extras) {
    out.push({ state, count, color: distributionColor(state) });
  }
  return out;
}

const COST_SERIES_LENGTH = 30;

export function aggregateDashboards(
  summaries: Map<string, DashboardData>,
): DashboardData {
  const stateCounts: Record<string, number> = {};
  const stateCountsParents: Record<string, number> = {};
  let totalCost = 0;
  let costLast30d = 0;
  let costPrior30d = 0;
  const costSeries30d: number[] = Array(COST_SERIES_LENGTH).fill(0);
  let completedToday = 0;
  let completedTodayParents = 0;
  let completedLast7d = 0;
  let completedLast7dParents = 0;
  let completedPrior7d = 0;
  let completedPrior7dParents = 0;
  const allAgents: ActiveAgent[] = [];
  const agentCostMap = new Map<string, AgentCost>();
  const modelCostMap = new Map<string, ModelCost>();
  const allCardCosts: CardCost[] = [];

  for (const data of summaries.values()) {
    for (const [state, count] of Object.entries(data.state_counts)) {
      stateCounts[state] = (stateCounts[state] ?? 0) + count;
    }
    for (const [state, count] of Object.entries(data.state_counts_parents ?? {})) {
      stateCountsParents[state] = (stateCountsParents[state] ?? 0) + count;
    }
    totalCost += data.total_cost_usd;
    costLast30d += data.total_cost_usd_last_30d ?? 0;
    costPrior30d += data.total_cost_usd_prior_30d ?? 0;
    const series = data.cost_series_30d;
    if (series) {
      for (let i = 0; i < COST_SERIES_LENGTH; i++) {
        costSeries30d[i] += series[i] ?? 0;
      }
    }
    completedToday += data.cards_completed_today;
    completedTodayParents += data.cards_completed_today_parents ?? 0;
    completedLast7d += data.cards_completed_last_7d ?? 0;
    completedLast7dParents += data.cards_completed_last_7d_parents ?? 0;
    completedPrior7d += data.cards_completed_prior_7d ?? 0;
    completedPrior7dParents += data.cards_completed_prior_7d_parents ?? 0;
    allAgents.push(...data.active_agents);
    allCardCosts.push(...data.card_costs);
    for (const ac of data.agent_costs) {
      const existing = agentCostMap.get(ac.agent_id);
      if (existing) {
        existing.prompt_tokens += ac.prompt_tokens;
        existing.completion_tokens += ac.completion_tokens;
        existing.estimated_cost_usd += ac.estimated_cost_usd;
        existing.card_count += ac.card_count;
      } else {
        agentCostMap.set(ac.agent_id, { ...ac });
      }
    }
    for (const mc of data.model_costs) {
      const existing = modelCostMap.get(mc.model);
      if (existing) {
        existing.prompt_tokens += mc.prompt_tokens;
        existing.completion_tokens += mc.completion_tokens;
        existing.estimated_cost_usd += mc.estimated_cost_usd;
        existing.card_count += mc.card_count;
      } else {
        modelCostMap.set(mc.model, { ...mc });
      }
    }
  }

  return {
    state_counts: stateCounts,
    state_counts_parents: stateCountsParents,
    active_agents: allAgents,
    total_cost_usd: totalCost,
    total_cost_usd_last_30d: costLast30d,
    total_cost_usd_prior_30d: costPrior30d,
    cost_series_30d: costSeries30d,
    cards_completed_today: completedToday,
    cards_completed_today_parents: completedTodayParents,
    cards_completed_last_7d: completedLast7d,
    cards_completed_last_7d_parents: completedLast7dParents,
    cards_completed_prior_7d: completedPrior7d,
    cards_completed_prior_7d_parents: completedPrior7dParents,
    metric_series: {
      active_agents: [],
      in_flight: [],
      stalled: [],
      shipped: [],
      in_flight_parents: [],
      stalled_parents: [],
      shipped_parents: [],
    },
    agent_costs: Array.from(agentCostMap.values()),
    model_costs: Array.from(modelCostMap.values()),
    card_costs: allCardCosts,
  };
}

export function totalCardCount(counts: Record<string, number>): number {
  return Object.values(counts).reduce((a, b) => a + b, 0);
}

export function projectRow(
  config: ProjectConfig,
  data: DashboardData | undefined,
): ProjectRow {
  const total = data ? totalCardCount(data.state_counts) : 0;
  const cost = data?.total_cost_usd ?? 0;
  return { config, data, total, cost };
}

export function isHumanAgent(agentId: string): boolean {
  return agentId.startsWith('human:');
}

export function agentInitials(agentId: string): string {
  const id = agentId.startsWith('human:') ? agentId.slice(6) : agentId;
  if (!id) return '··';
  const parts = id.split(/[-_:.\s]+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toLowerCase();
  }
  const word = parts[0] ?? id;
  return word.slice(0, 2).toLowerCase();
}

export function formatUsd(amount: number): string {
  return `$${amount.toFixed(2)}`;
}

export function summarySentence(
  projectCount: number,
  totalCards: number,
  agentCount: number,
  stalled: number,
  blockedProjects: number,
): string {
  const projects = `${projectCount} ${projectCount === 1 ? 'project' : 'projects'}`;
  const cards = `${totalCards} ${totalCards === 1 ? 'card' : 'cards'}`;
  const agents = `${agentCount} ${agentCount === 1 ? 'agent' : 'agents'} on duty`;
  const lead = `${projects} active across ${cards}, with ${agents}.`;
  const tails: string[] = [];
  if (stalled > 0) {
    tails.push(`${stalled} ${stalled === 1 ? 'card is' : 'cards are'} currently stalled`);
  }
  if (blockedProjects > 0) {
    tails.push(
      `${blockedProjects} ${blockedProjects === 1 ? 'project has' : 'projects have'} cards in the blocked state`,
    );
  }
  if (tails.length === 0) {
    return `${lead} No stalls or blockers detected.`;
  }
  return `${lead} ${tails.join(' and ')}.`;
}

export function buildPrefixMap(projects: ProjectConfig[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const p of projects) {
    if (p.prefix) map.set(p.prefix.toUpperCase(), p.name);
  }
  return map;
}

export function projectForCardId(
  cardId: string,
  prefixMap: Map<string, string>,
): string | null {
  const dash = cardId.indexOf('-');
  if (dash <= 0) return null;
  const prefix = cardId.slice(0, dash).toUpperCase();
  return prefixMap.get(prefix) ?? null;
}

/**
 * Median in seconds of "now - last_heartbeat" over the active-agent list.
 * Returns null for an empty list.
 */
export function medianHeartbeatSeconds(agents: ActiveAgent[], now: number = Date.now()): number | null {
  if (agents.length === 0) return null;
  const deltas = agents
    .map((a) => {
      const t = a.last_heartbeat ? new Date(a.last_heartbeat).getTime() : NaN;
      if (Number.isNaN(t)) return null;
      return Math.max(0, (now - t) / 1000);
    })
    .filter((d): d is number => d !== null)
    .sort((a, b) => a - b);
  if (deltas.length === 0) return null;
  const mid = Math.floor(deltas.length / 2);
  if (deltas.length % 2 === 0) return (deltas[mid - 1] + deltas[mid]) / 2;
  return deltas[mid];
}

/**
 * Oldest claim duration, formatted as "Nh Mm" / "Nm" / "Ns". Falls back
 * to "—" when no agent has a valid `since` timestamp.
 */
export function oldestClaim(agents: ActiveAgent[], now: number = Date.now()): string {
  let oldestMs = 0;
  for (const a of agents) {
    if (!a.since) continue;
    const t = new Date(a.since).getTime();
    if (Number.isNaN(t)) continue;
    const delta = now - t;
    if (delta > oldestMs) oldestMs = delta;
  }
  if (oldestMs === 0) return '—';
  const sec = Math.floor(oldestMs / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const hour = Math.floor(min / 60);
  const remMin = min % 60;
  return remMin === 0 ? `${hour}h` : `${hour}h ${remMin}m`;
}

export function compactSeconds(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const min = seconds / 60;
  if (min < 60) return `${min.toFixed(min < 10 ? 1 : 0)}m`;
  const hour = min / 60;
  return `${hour.toFixed(hour < 10 ? 1 : 0)}h`;
}
