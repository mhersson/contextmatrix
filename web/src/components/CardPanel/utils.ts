import type { Card, PatchCardInput } from '../../types';

export const typeColors: Record<string, string> = {
  task: 'var(--blue)',
  bug: 'var(--red)',
  feature: 'var(--green)',
  subtask: 'var(--aqua)',
};

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

/** True when the edited card differs from the server card in any save-relevant field. */
export function isCardDirty(edited: Card, original: Card): boolean {
  return (
    edited.title !== original.title ||
    edited.state !== original.state ||
    edited.priority !== original.priority ||
    edited.body !== original.body ||
    !arraysEqual(edited.labels, original.labels) ||
    (edited.autonomous ?? false) !== (original.autonomous ?? false) ||
    (edited.use_opus_orchestrator ?? false) !== (original.use_opus_orchestrator ?? false) ||
    (edited.feature_branch ?? false) !== (original.feature_branch ?? false) ||
    (edited.create_pr ?? false) !== (original.create_pr ?? false) ||
    (edited.vetted ?? false) !== (original.vetted ?? false) ||
    (edited.base_branch ?? '') !== (original.base_branch ?? '')
  );
}

/** Builds a PatchCardInput containing only the fields that changed. */
export function buildCardPatch(edited: Card, original: Card): PatchCardInput {
  const updates: PatchCardInput = {};
  if (edited.title !== original.title) updates.title = edited.title;
  if (edited.state !== original.state) updates.state = edited.state;
  if (edited.priority !== original.priority) updates.priority = edited.priority;
  if (edited.body !== original.body) updates.body = edited.body;
  if (JSON.stringify(edited.labels) !== JSON.stringify(original.labels)) {
    updates.labels = edited.labels;
  }
  if ((edited.autonomous ?? false) !== (original.autonomous ?? false)) {
    updates.autonomous = edited.autonomous ?? false;
  }
  if ((edited.use_opus_orchestrator ?? false) !== (original.use_opus_orchestrator ?? false)) {
    updates.use_opus_orchestrator = edited.use_opus_orchestrator ?? false;
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
