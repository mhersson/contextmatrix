import type { Card, PatchCardInput, ProjectConfig } from '../../types';

/**
 * Maps a card state to the matching `.chip-state-*` CSS class name.
 * Used by the Info tab's Subtasks/Depends-on lists. Falls back to
 * `chip-state-todo` for unknown states.
 */
export function chipClassForState(state: string): string {
  const known = new Set([
    'todo', 'in_progress', 'hitl', 'review', 'done',
    'blocked', 'stalled', 'not_planned',
  ]);
  return known.has(state) ? `chip-state-${state}` : 'chip-state-todo';
}

/** Shallow equality check for string arrays (used for label comparison). */
function arraysEqual(a: string[] | undefined, b: string[] | undefined): boolean {
  const aa = a ?? [];
  const bb = b ?? [];
  if (aa.length !== bb.length) return false;
  for (let i = 0; i < aa.length; i++) {
    if (aa[i] !== bb[i]) return false;
  }
  return true;
}

/**
 * Three-state equality for `skills`. null and undefined are equivalent
 * ("use project default"); arrays are compared element-wise.
 */
function skillsEqual(
  a: string[] | null | undefined,
  b: string[] | null | undefined,
): boolean {
  const aNorm = a ?? null;
  const bNorm = b ?? null;
  if (aNorm === null && bNorm === null) return true;
  if (aNorm === null || bNorm === null) return false;
  return arraysEqual(aNorm, bNorm);
}

/** True when the edited card differs from the server card in any save-relevant field. */
export function isCardDirty(edited: Card, original: Card): boolean {
  return (
    edited.title !== original.title ||
    edited.state !== original.state ||
    edited.priority !== original.priority ||
    edited.body !== original.body ||
    !arraysEqual(edited.labels, original.labels) ||
    (edited.autonomous ?? false) !== (original.autonomous ?? false) ||
    (edited.feature_branch ?? false) !== (original.feature_branch ?? false) ||
    (edited.create_pr ?? false) !== (original.create_pr ?? false) ||
    (edited.vetted ?? false) !== (original.vetted ?? false) ||
    (edited.base_branch ?? '') !== (original.base_branch ?? '') ||
    !skillsEqual(edited.skills, original.skills)
  );
}

/** Builds a PatchCardInput containing only the fields that changed. */
export function buildCardPatch(edited: Card, original: Card): PatchCardInput {
  const updates: PatchCardInput = {};
  if (edited.title !== original.title) updates.title = edited.title;
  if (edited.state !== original.state) updates.state = edited.state;
  if (edited.priority !== original.priority) updates.priority = edited.priority;
  if (edited.body !== original.body) updates.body = edited.body;
  if (!arraysEqual(edited.labels, original.labels)) {
    updates.labels = edited.labels;
  }
  if ((edited.autonomous ?? false) !== (original.autonomous ?? false)) {
    updates.autonomous = edited.autonomous ?? false;
  }
  if ((edited.feature_branch ?? false) !== (original.feature_branch ?? false)) {
    updates.feature_branch = edited.feature_branch ?? false;
  }
  if ((edited.create_pr ?? false) !== (original.create_pr ?? false)) {
    updates.create_pr = edited.create_pr ?? false;
  }
  if ((edited.vetted ?? false) !== (original.vetted ?? false)) {
    updates.vetted = edited.vetted ?? false;
  }
  if ((edited.base_branch ?? '') !== (original.base_branch ?? '')) {
    updates.base_branch = edited.base_branch ?? '';
  }
  if (!skillsEqual(edited.skills, original.skills)) {
    const next = edited.skills;
    if (next === null || next === undefined) {
      // Explicit clear via the sentinel — pure JSON cannot distinguish
      // absent from null, so the backend needs this flag to know the
      // user actively chose "use project default" rather than just
      // omitting the field.
      updates.skills_clear = true;
    } else {
      updates.skills = next;
    }
  }
  return updates;
}

export function isSafeHttpUrl(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

export function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffMin < 1) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  return `${diffDay}d ago`;
}

/**
 * True when the card is "owned" by a runner or an agent claim — i.e. humans
 * should not perform free-form state transitions or edit metadata that would
 * conflict with the agent's in-flight work.
 *
 * See the workflow-safety rule in the redesign spec: human state-transition
 * UI must only render when `runner_status not in {queued, running}` AND
 * `assigned_agent == null` (or assigned_agent is the current human).
 */
export function isRunnerAttached(card: Card, currentAgentId: string | null): boolean {
  const runnerActive = card.runner_status === 'queued' || card.runner_status === 'running';
  const claimedByOther =
    !!card.assigned_agent &&
    !(currentAgentId && card.assigned_agent === currentAgentId && currentAgentId.startsWith('human:'));
  return runnerActive || claimedByOther;
}

/**
 * Decides which curated primary action button should appear in the top-right
 * of the header for a given card state. Returns null when no curated action
 * applies (fall back to the Move-to cluster in the Info tab).
 *
 * The returned `targetState` is the destination state the button transitions
 * to; used by the Info-tab state picker to filter its options so the same
 * destination isn't offered twice.
 */
export type PrimaryAction =
  | { kind: 'stop' }
  | { kind: 'run'; autonomous: boolean }
  | { kind: 'transition'; label: string; targetState: string }
  | null;

export function primaryAction(
  card: Card,
  editedAutonomous: boolean,
  config: ProjectConfig,
  canRun: boolean,
): PrimaryAction {
  if (card.runner_status === 'queued' || card.runner_status === 'running') {
    return { kind: 'stop' };
  }
  const targets = config.transitions[card.state] || [];
  if (card.state === 'review' && targets.includes('done')) {
    return { kind: 'transition', label: 'Mark done', targetState: 'done' };
  }
  if (card.state === 'blocked' && targets.includes('todo')) {
    return { kind: 'transition', label: 'Unblock', targetState: 'todo' };
  }
  if (card.state === 'stalled' && targets.includes('todo')) {
    return { kind: 'transition', label: 'Resume', targetState: 'todo' };
  }
  if (card.state === 'done' && targets.includes('todo')) {
    return { kind: 'transition', label: 'Re-open', targetState: 'todo' };
  }
  if (canRun) {
    return { kind: 'run', autonomous: editedAutonomous };
  }
  return null;
}
