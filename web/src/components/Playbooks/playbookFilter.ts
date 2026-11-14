import { playbookStatus, type PlaybookStatus } from '../../lib/playbookStatus';
import type { PlaybookSummary } from '../../types';

export interface PlaybookFilter {
  query: string;
  project: string;
  status: 'any' | PlaybookStatus;
  pinnedOnly: boolean;
}

/** The Status choices, in the order the toolbar lists them. */
export const PLAYBOOK_STATUSES: PlaybookFilter['status'][] = ['any', 'running', 'waiting', 'stopped', 'not started', 'completed'];

export const EMPTY_PLAYBOOK_FILTER: PlaybookFilter = { query: '', project: '', status: 'any', pinnedOnly: false };

export function isEmptyPlaybookFilter(f: PlaybookFilter): boolean {
  return f.query.trim() === '' && f.project === '' && f.status === 'any' && !f.pinnedOnly;
}

export function filterPlaybooks(playbooks: PlaybookSummary[], f: PlaybookFilter): PlaybookSummary[] {
  const query = f.query.trim().toLowerCase();
  return playbooks.filter((p) => {
    if (query && !p.title.toLowerCase().includes(query)) return false;
    if (f.project && !p.project_names.includes(f.project)) return false;
    if (f.status !== 'any' && playbookStatus(p) !== f.status) return false;
    return !f.pinnedOnly || p.pinned === true;
  });
}

function updatedAt(p: PlaybookSummary): number {
  const t = Date.parse(p.updated_at);
  return Number.isNaN(t) ? 0 : t;
}

export function newestFirst(playbooks: PlaybookSummary[]): PlaybookSummary[] {
  return [...playbooks].sort((a, b) => updatedAt(b) - updatedAt(a));
}
