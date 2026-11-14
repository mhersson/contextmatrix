import { describe, it, expect } from 'vitest';
import { playbookStatus } from './playbookStatus';
import type { PlaybookRunStatus, PlaybookSummary } from '../types';

function pb(complete: number, total: number, run_status?: PlaybookRunStatus): PlaybookSummary {
  return { id: 'p', title: 'p', complete, total, segments: [], projects: 1, updated_at: '', run_status };
}

describe('playbookStatus', () => {
  it.each([
    ['running', pb(1, 3, 'running'), 'running'],
    ['waiting', pb(1, 3, 'waiting'), 'waiting'],
    ['stopped', pb(1, 3, 'stopped'), 'stopped'],
    ['no run', pb(0, 3), 'not started'],
    ['completed run with entries added since', pb(2, 3, 'completed'), 'not started'],
    ['all entries done, no run', pb(3, 3), 'completed'],
    ['all entries done, completed run', pb(3, 3, 'completed'), 'completed'],
    ['all entries done while the run still reads running', pb(3, 3, 'running'), 'completed'],
    ['empty playbook', pb(0, 0), 'not started'],
  ])('%s', (_name, p, want) => {
    expect(playbookStatus(p)).toBe(want);
  });
});
