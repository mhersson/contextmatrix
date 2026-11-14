import { isFullyComplete } from '../components/Playbooks/playbookUtils';
import type { PlaybookSummary } from '../types';

export type PlaybookStatus = 'completed' | 'running' | 'waiting' | 'stopped' | 'not started';

/** Entries decide completion; a completed run that predates added entries reads not started. */
export function playbookStatus(p: PlaybookSummary): PlaybookStatus {
  if (isFullyComplete(p)) return 'completed';
  const run = p.run_status;
  return run === 'running' || run === 'waiting' || run === 'stopped' ? run : 'not started';
}
