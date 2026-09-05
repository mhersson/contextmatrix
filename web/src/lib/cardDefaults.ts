import type { CardDefaults } from '../types';

/** Every field concrete - what the settings form edits and the create form seeds from. */
export interface ResolvedCardDefaults {
  autonomous: boolean;
  max_capability: boolean;
  mob_participants: number;
  mob_phases: string[];
  create_pr: boolean;
  await_ci: boolean;
  await_copilot_review: boolean;
}

export const BUILTIN_CARD_DEFAULTS: ResolvedCardDefaults = {
  autonomous: false,
  max_capability: false,
  mob_participants: 0,
  mob_phases: [],
  create_pr: true,
  await_ci: false,
  await_copilot_review: false,
};

export function resolveCardDefaults(raw?: CardDefaults | null): ResolvedCardDefaults {
  if (!raw) return { ...BUILTIN_CARD_DEFAULTS, mob_phases: [] };
  return {
    autonomous: raw.autonomous ?? false,
    max_capability: raw.max_capability ?? false,
    mob_participants: raw.mob_participants ?? 0,
    mob_phases: [...(raw.mob_phases ?? [])],
    // Absent means the built-in true - never let an omitted key read as off.
    create_pr: raw.create_pr ?? true,
    await_ci: raw.await_ci ?? false,
    await_copilot_review: raw.await_copilot_review ?? false,
  };
}

/** Stable diff key; phase order is irrelevant to the server. */
export function cardDefaultsKey(d: ResolvedCardDefaults): string {
  return JSON.stringify({ ...d, mob_phases: [...d.mob_phases].sort() });
}

/**
 * Wire shape for PUT: every flag explicit (the server normalizes built-ins
 * away), phases only when seats are on so a stale list cannot linger.
 */
export function toWireCardDefaults(d: ResolvedCardDefaults): CardDefaults {
  return {
    autonomous: d.autonomous,
    max_capability: d.max_capability,
    mob_participants: d.mob_participants,
    ...(d.mob_participants >= 2 && d.mob_phases.length > 0 ? { mob_phases: d.mob_phases } : {}),
    create_pr: d.create_pr,
    await_ci: d.await_ci,
    await_copilot_review: d.await_copilot_review,
  };
}
