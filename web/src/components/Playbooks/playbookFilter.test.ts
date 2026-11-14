import { describe, it, expect } from 'vitest';
import { EMPTY_PLAYBOOK_FILTER, filterPlaybooks, newestFirst, type PlaybookFilter } from './playbookFilter';
import type { PlaybookSummary } from '../../types';

function pb(o: Partial<PlaybookSummary> & { id: string }): PlaybookSummary {
  return {
    title: o.id,
    complete: 0,
    total: 2,
    segments: ['pending', 'pending'],
    projects: 0,
    project_names: [],
    updated_at: '2026-10-01T10:00:00Z',
    ...o,
  };
}

const ids = (list: PlaybookSummary[]) => list.map((p) => p.id);
const filter = (o: Partial<PlaybookFilter>): PlaybookFilter => ({ ...EMPTY_PLAYBOOK_FILTER, ...o });

describe('filterPlaybooks', () => {
  it('keeps every playbook under the empty filter', () => {
    const list = [pb({ id: 'A' }), pb({ id: 'B', run_status: 'running' })];
    expect(ids(filterPlaybooks(list, EMPTY_PLAYBOOK_FILTER))).toEqual(['A', 'B']);
  });

  it('matches the trimmed query against the title, ignoring case', () => {
    const list = [pb({ id: 'A', title: 'Rollout Q3' }), pb({ id: 'B', title: 'Docs refresh' })];
    expect(ids(filterPlaybooks(list, filter({ query: '  rollout ' })))).toEqual(['A']);
  });

  it('matches a project among the project names', () => {
    const list = [
      pb({ id: 'A', project_names: ['api', 'web'] }),
      pb({ id: 'B', project_names: ['web'] }),
      pb({ id: 'C' }),
    ];
    expect(ids(filterPlaybooks(list, filter({ project: 'api' })))).toEqual(['A']);
  });

  it('reads completed from the entries whatever the run says, and the rest from the run', () => {
    const done: Partial<PlaybookSummary> = { complete: 2, total: 2, segments: ['complete', 'complete'] };
    const list = [
      pb({ id: 'run', run_status: 'running' }),
      pb({ id: 'wait', run_status: 'waiting' }),
      pb({ id: 'stop', run_status: 'stopped' }),
      pb({ id: 'idle' }),
      pb({ id: 'grown', run_status: 'completed' }),
      pb({ id: 'byHand', ...done }),
      pb({ id: 'ranOut', ...done, run_status: 'completed' }),
      pb({ id: 'stoppedDone', ...done, run_status: 'stopped' }),
    ];
    const by = (status: PlaybookFilter['status']) => ids(filterPlaybooks(list, filter({ status })));

    expect(by('running')).toEqual(['run']);
    expect(by('waiting')).toEqual(['wait']);
    expect(by('stopped')).toEqual(['stop']);
    expect(by('not started')).toEqual(['idle', 'grown']);
    expect(by('completed')).toEqual(['byHand', 'ranOut', 'stoppedDone']);
  });

  it('keeps only pinned playbooks for pinned only', () => {
    const list = [pb({ id: 'A', pinned: true }), pb({ id: 'B' }), pb({ id: 'C', pinned: false })];
    expect(ids(filterPlaybooks(list, filter({ pinnedOnly: true })))).toEqual(['A']);
  });

  it('combines the filters with AND', () => {
    const base: Partial<PlaybookSummary> = { title: 'rollout', project_names: ['api'], pinned: true, run_status: 'running' };
    const list = [
      pb({ id: 'hit', ...base }),
      pb({ id: 'otherTitle', ...base, title: 'docs' }),
      pb({ id: 'otherProject', ...base, project_names: ['web'] }),
      pb({ id: 'unpinned', ...base, pinned: false }),
      pb({ id: 'stopped', ...base, run_status: 'stopped' }),
    ];
    const f = filter({ query: 'roll', project: 'api', status: 'running', pinnedOnly: true });
    expect(ids(filterPlaybooks(list, f))).toEqual(['hit']);
  });
});

describe('newestFirst', () => {
  it('sorts by the last update, newest first, without touching its input', () => {
    const list = [
      pb({ id: 'old', updated_at: '2026-10-01T10:00:00Z' }),
      pb({ id: 'new', updated_at: '2026-10-03T10:00:00Z' }),
      pb({ id: 'mid', updated_at: '2026-10-02T10:00:00Z' }),
    ];
    expect(ids(newestFirst(list))).toEqual(['new', 'mid', 'old']);
    expect(ids(list)).toEqual(['old', 'new', 'mid']);
  });
});
