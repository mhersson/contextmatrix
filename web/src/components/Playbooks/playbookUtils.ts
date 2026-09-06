import { api } from '../../api/client';
import { displayState } from '../../lib/stateLabels';
import type { PlaybookDetail, PlaybookEntry, PlaybookRun, PlaybookRunStatus, PlaybookSummary } from '../../types';

// First incomplete entry - the frontier marker's target. -1 when all done.
export function frontierIndex(entries: PlaybookEntry[]): number {
  return entries.findIndex((e) => !e.complete);
}

export function isFullyComplete(p: PlaybookSummary): boolean {
  return p.total > 0 && p.complete >= p.total;
}

export function segmentColor(seg: string): string {
  switch (seg) {
    case 'complete':
      return 'var(--green)';
    case 'active':
      return 'var(--aqua)';
    case 'missing':
      return 'var(--bg-red)';
    default:
      return 'var(--bg2)';
  }
}

// Local-only reorder for optimistic UI - the caller still persists the move
// via patchPlaybookEntry and reconciles with the server response.
export function arrayMoveLocal<T>(items: T[], from: number, to: number): T[] {
  const next = [...items];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

const PROJECT_ACCENTS = ['var(--blue)', 'var(--orange)', 'var(--purple)', 'var(--yellow)', 'var(--aqua)'];

// Stable per-project accent, hashed from the name so a project's badge color
// stays consistent across rows and renders without a server-side mapping.
export function projectColor(project: string): string {
  let hash = 0;
  for (let i = 0; i < project.length; i++) {
    hash = (hash * 31 + project.charCodeAt(i)) | 0;
  }
  return PROJECT_ACCENTS[Math.abs(hash) % PROJECT_ACCENTS.length];
}

export interface EntryStateChip {
  label: string;
  bg: string;
  color: string;
}

// State chip for a card entry, per the locked visual direction. Manual
// entries carry no card_state and get no chip - their checkbox is the state.
export function entryStateChip(entry: PlaybookEntry): EntryStateChip | null {
  if (entry.type !== 'card') return null;
  if (entry.missing) return { label: 'missing', bg: 'var(--bg-red)', color: 'var(--red)' };
  const state = entry.card_state;
  if (!state) return null;
  switch (state) {
    case 'done':
      return { label: displayState(state), bg: 'var(--bg-green)', color: 'var(--green)' };
    case 'in_progress':
      return { label: 'agent active', bg: 'var(--bg-aqua)', color: 'var(--aqua)' };
    case 'review':
      return { label: displayState(state), bg: 'var(--bg-purple)', color: 'var(--purple)' };
    case 'not_planned':
      return { label: 'not planned', bg: 'var(--bg1)', color: 'var(--grey1)' };
    default:
      return { label: displayState(state), bg: 'var(--bg1)', color: 'var(--grey1)' };
  }
}

// Resolves a drag from `activeId` onto `overId` to the patch args a reorder
// needs - position is the dropped-on entry's index. Pure, so
// PlaybookDetailPage's reorder logic is testable without dnd machinery.
export function computeReorderPatch(
  detail: PlaybookDetail,
  activeId: string,
  overId: string,
): { entryId: string; position: number } | null {
  if (activeId === overId) return null;
  const from = detail.entries.findIndex((e) => e.id === activeId);
  const to = detail.entries.findIndex((e) => e.id === overId);
  if (from < 0 || to < 0) return null;
  return { entryId: activeId, position: to };
}

// Persists a reorder computed by computeReorderPatch.
export async function persistReorder(
  playbookId: string,
  detail: PlaybookDetail,
  activeId: string,
  overId: string,
): Promise<PlaybookDetail | null> {
  const patch = computeReorderPatch(detail, activeId, overId);
  if (!patch) return null;
  return api.patchPlaybookEntry(playbookId, patch.entryId, { position: patch.position });
}

// Active runs lock the playbook's cards and refuse a second Play.
export function isRunActive(run?: PlaybookRun | null): boolean {
  return run?.status === 'running' || run?.status === 'waiting';
}

// Run status chip in the entryStateChip idiom: foreground on its bg token.
export function runStatusChip(status: PlaybookRunStatus): EntryStateChip {
  switch (status) {
    case 'running':
      return { label: 'running', bg: 'var(--bg-aqua)', color: 'var(--aqua)' };
    case 'waiting':
      return { label: 'waiting for you', bg: 'var(--bg-yellow)', color: 'var(--yellow)' };
    case 'completed':
      return { label: 'completed', bg: 'var(--bg-green)', color: 'var(--green)' };
    default:
      return { label: 'stopped', bg: 'var(--bg1)', color: 'var(--grey1)' };
  }
}

// Index of the entry the run is on, -1 when the run has none.
export function runEntryIndex(detail: PlaybookDetail): number {
  const entry = detail.run?.entry;
  if (!entry) return -1;
  return detail.entries.findIndex((e) => e.id === entry);
}

// One-line status for the side panel. Only claims the run block makes.
export function describeRun(detail: PlaybookDetail): string {
  const run = detail.run;
  if (!run) return 'Not started';
  switch (run.status) {
    case 'running': {
      const i = runEntryIndex(detail);
      const e = i >= 0 ? detail.entries[i] : undefined;
      const name = e?.type === 'card' ? e.card : e?.text;
      return name ? `Running ${name}, ${i + 1} of ${detail.entries.length}` : 'Running';
    }
    case 'waiting':
      return run.reason ? `Waiting for you: ${run.reason}` : 'Waiting for you';
    case 'stopped':
      return run.reason ? `Stopped: ${run.reason}` : 'Stopped';
    default:
      return 'Completed';
  }
}
