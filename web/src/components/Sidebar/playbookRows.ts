import { SECTION_ROW_CAP } from './sectionRows';
import type { PlaybookSummary } from '../../types';

function updatedAt(p: PlaybookSummary): number {
  const t = Date.parse(p.updated_at);
  return Number.isNaN(t) ? 0 : t;
}

// Ties break on id so the rows never depend on the order the server listed them in.
function newestFirst(a: PlaybookSummary, b: PlaybookSummary): number {
  return updatedAt(b) - updatedAt(a) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
}

const isActive = (p: PlaybookSummary) => p.run_status === 'running' || p.run_status === 'waiting';

/** Pinned, then running and waiting, then the newest of the rest up to `cap` rows; the first two groups always show. */
export function playbookRows(list: PlaybookSummary[], cap: number = SECTION_ROW_CAP): PlaybookSummary[] {
  const sorted = [...list].sort(newestFirst);
  const pinned = sorted.filter((p) => p.pinned === true);
  const active = sorted.filter((p) => p.pinned !== true && isActive(p));
  const rest = sorted.filter((p) => p.pinned !== true && !isActive(p));
  return [...pinned, ...active, ...rest.slice(0, Math.max(0, cap - pinned.length - active.length))];
}
