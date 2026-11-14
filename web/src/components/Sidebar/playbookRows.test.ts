import { describe, it, expect } from 'vitest';
import { playbookRows } from './playbookRows';
import type { PlaybookRunStatus, PlaybookSummary } from '../../types';

function pb(id: string, o: { pinned?: boolean; run?: PlaybookRunStatus; day?: number; complete?: number } = {}): PlaybookSummary {
  return {
    id,
    title: id,
    complete: o.complete ?? 0,
    total: 3,
    segments: [],
    projects: 1,
    project_names: [],
    pinned: o.pinned,
    run_status: o.run,
    updated_at: `2026-10-${String(o.day ?? 1).padStart(2, '0')}T12:00:00Z`,
  };
}

const ids = (rows: PlaybookSummary[]) => rows.map((r) => r.id);

describe('playbookRows', () => {
  it('fills to three with the newest, newest first', () => {
    const list = [1, 2, 3, 4, 5].map((d) => pb(`p${d}`, { day: d }));
    expect(ids(playbookRows(list))).toEqual(['p5', 'p4', 'p3']);
  });

  it('orders pinned, then running and waiting, then the newest rest', () => {
    const list = [
      pb('old', { day: 1 }),
      pb('new', { day: 9 }),
      pb('run', { day: 2, run: 'running' }),
      pb('wait', { day: 3, run: 'waiting' }),
      pb('pin', { day: 1, pinned: true }),
    ];
    expect(ids(playbookRows(list, 6))).toEqual(['pin', 'wait', 'run', 'new', 'old']);
  });

  it('shows every pinned and active row past the cap and no filler', () => {
    const list = [
      pb('p1', { pinned: true }),
      pb('p2', { pinned: true }),
      pb('r1', { run: 'running', day: 3 }),
      pb('w1', { run: 'waiting', day: 4 }),
      pb('cold', { day: 9 }),
    ];
    expect(ids(playbookRows(list))).toEqual(['p1', 'p2', 'w1', 'r1']);
  });

  it('does not count a stopped or completed playbook as active', () => {
    const list = [pb('stop', { run: 'stopped', day: 1 }), pb('done', { run: 'completed', complete: 3, day: 2 })];
    expect(ids(playbookRows(list, 1))).toEqual(['done']);
  });

  it('is not tied to the order the server listed', () => {
    const a = [pb('a', { day: 2 }), pb('b', { day: 2 })];
    expect(ids(playbookRows(a))).toEqual(ids(playbookRows([...a].reverse())));
  });
});
