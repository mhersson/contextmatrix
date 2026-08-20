import type { PlaybookEntry, PlaybookSummary } from '../../types';

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
