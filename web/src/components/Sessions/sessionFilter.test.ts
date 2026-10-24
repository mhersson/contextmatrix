import { describe, it, expect } from 'vitest';
import { EMPTY_FILTER, filterSessions, type SessionFilter } from './sessionFilter';
import type { ChatSession } from '../../types';

function sess(o: Partial<ChatSession> & { id: string }): ChatSession {
  return {
    title: o.id,
    status: 'cold',
    created_at: '2026-10-01T10:00:00Z',
    last_active: '2026-10-01T10:00:00Z',
    created_by: 'human:alice',
    ...o,
  };
}

const ids = (list: ChatSession[]) => list.map((s) => s.id);
const filter = (o: Partial<SessionFilter>): SessionFilter => ({ ...EMPTY_FILTER, ...o });

describe('filterSessions', () => {
  it('keeps every session under the empty filter', () => {
    const list = [sess({ id: 'A' }), sess({ id: 'B', kind: 'architect' })];
    expect(ids(filterSessions(list, EMPTY_FILTER))).toEqual(['A', 'B']);
  });

  it('matches the trimmed query against the title, ignoring case', () => {
    const list = [sess({ id: 'A', title: 'worker-auth' }), sess({ id: 'B', title: 'billing' })];
    expect(ids(filterSessions(list, filter({ query: '  AUTH ' })))).toEqual(['A']);
  });

  it("matches a project in a blueprint's workspace or a chat's project", () => {
    const list = [
      sess({ id: 'BP', kind: 'architect', workspace: ['web', 'api'] }),
      sess({ id: 'C', project: 'api' }),
      sess({ id: 'X', project: 'other' }),
      sess({ id: 'BP2', kind: 'architect', workspace: ['web'] }),
    ];
    expect(ids(filterSessions(list, filter({ project: 'api' })))).toEqual(['BP', 'C']);
  });

  it('keeps active and warm-idle for live and the rest for idle', () => {
    const list = [
      sess({ id: 'act', status: 'active' }),
      sess({ id: 'warm', status: 'warm-idle' }),
      sess({ id: 'cold', status: 'cold' }),
      sess({ id: 'end', status: 'ending' }),
    ];
    expect(ids(filterSessions(list, filter({ status: 'live' })))).toEqual(['act', 'warm']);
    expect(ids(filterSessions(list, filter({ status: 'idle' })))).toEqual(['cold', 'end']);
  });

  it('keeps only pinned sessions for pinned only', () => {
    const list = [sess({ id: 'A', pinned: true }), sess({ id: 'B' }), sess({ id: 'C', pinned: false })];
    expect(ids(filterSessions(list, filter({ pinnedOnly: true })))).toEqual(['A']);
  });

  it('combines the filters with AND', () => {
    const list = [
      sess({ id: 'hit', title: 'auth fix', project: 'api', pinned: true, status: 'active' }),
      sess({ id: 'unpinned', title: 'auth docs', project: 'api', status: 'active' }),
      sess({ id: 'otherProject', title: 'auth', project: 'web', pinned: true, status: 'active' }),
      sess({ id: 'otherTitle', title: 'billing', project: 'api', pinned: true, status: 'active' }),
      sess({ id: 'idle', title: 'auth', project: 'api', pinned: true, status: 'cold' }),
    ];
    const f = filter({ query: 'auth', project: 'api', status: 'live', pinnedOnly: true });
    expect(ids(filterSessions(list, f))).toEqual(['hit']);
  });
});
