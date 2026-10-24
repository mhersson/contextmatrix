import { describe, it, expect } from 'vitest';
import { SECTION_ROW_CAP, sectionRows } from './sessionSections';
import type { ChatSession, ChatStatus, SessionKind } from '../../types';

function s(
  id: string,
  o: {
    status?: ChatStatus;
    pinned?: boolean;
    last_active?: string;
    kind?: SessionKind;
    assistant_working?: boolean;
  } = {},
): ChatSession {
  return {
    id,
    title: id,
    kind: o.kind,
    status: o.status ?? 'cold',
    pinned: o.pinned,
    assistant_working: o.assistant_working,
    created_at: '2026-10-01T00:00:00Z',
    last_active: o.last_active ?? '2026-10-01T00:00:00Z',
    created_by: 'human:alice',
  };
}

// Day n of October 2026, so a higher n is newer.
const day = (n: number) => `2026-10-${String(n).padStart(2, '0')}T12:00:00Z`;

function cold(count: number, firstDay: number): ChatSession[] {
  return Array.from({ length: count }, (_, i) => s(`cold${i}`, { last_active: day(firstDay + i) }));
}

function blueprints(count: number, firstDay: number, status: ChatStatus = 'cold'): ChatSession[] {
  return Array.from({ length: count }, (_, i) =>
    s(`bp${i}`, { kind: 'architect', status, last_active: day(firstDay + i) }),
  );
}

const ids = (rows: ChatSession[]) => rows.map((r) => r.id);

describe('sectionRows', () => {
  it('caps cold sessions at five, newest first', () => {
    const rows = sectionRows(cold(8, 1));
    expect(SECTION_ROW_CAP).toBe(5);
    expect(ids(rows)).toEqual(['cold7', 'cold6', 'cold5', 'cold4', 'cold3']);
  });

  it('puts pinned first, then live, then fills the rest with the newest cold', () => {
    const rows = sectionRows([
      ...cold(8, 10),
      s('pinOld', { pinned: true, last_active: day(1) }),
      s('pinNew', { pinned: true, last_active: day(2) }),
      s('live', { status: 'active', last_active: day(3) }),
    ]);
    expect(ids(rows)).toEqual(['pinNew', 'pinOld', 'live', 'cold7', 'cold6']);
  });

  it('shows every pinned and live row past the cap and no cold row', () => {
    const rows = sectionRows([
      ...cold(6, 20),
      s('p1', { pinned: true, last_active: day(1) }),
      s('p2', { pinned: true, last_active: day(2) }),
      s('p3', { pinned: true, last_active: day(3) }),
      s('p4', { pinned: true, last_active: day(4) }),
      s('l1', { status: 'active', last_active: day(5) }),
      s('l2', { status: 'warm-idle', last_active: day(6) }),
      s('l3', { status: 'active', last_active: day(7) }),
    ]);
    expect(ids(rows)).toEqual(['p4', 'p3', 'p2', 'p1', 'l3', 'l2', 'l1']);
  });

  it('lists a pinned live session once, in the pinned group', () => {
    const rows = sectionRows([
      s('pinnedLive', { pinned: true, status: 'active', last_active: day(1) }),
      s('live', { status: 'warm-idle', last_active: day(9) }),
      s('cold', { last_active: day(5) }),
    ]);
    expect(ids(rows)).toEqual(['pinnedLive', 'live', 'cold']);
  });

  it('treats an ending session as not live', () => {
    const rows = sectionRows([
      ...cold(5, 10),
      s('ending', { status: 'ending', last_active: day(1) }),
    ]);
    expect(ids(rows)).not.toContain('ending');
    expect(rows).toHaveLength(5);
  });

  it('does not depend on input order', () => {
    const input = [
      ...cold(7, 10),
      s('pin', { pinned: true, last_active: day(1) }),
      s('live', { status: 'active', last_active: day(2) }),
      s('tieA', { last_active: day(30) }),
      s('tieB', { last_active: day(30) }),
    ];
    const expected = ids(sectionRows(input));
    expect(ids(sectionRows([...input].reverse()))).toEqual(expected);
    expect(ids(sectionRows([...input.slice(4), ...input.slice(0, 4)]))).toEqual(expected);
  });

  it('caps warm-idle blueprints at five, newest first', () => {
    const rows = sectionRows(blueprints(8, 1, 'warm-idle'));
    expect(ids(rows)).toEqual(['bp7', 'bp6', 'bp5', 'bp4', 'bp3']);
  });

  it('fills a warm-idle blueprint as a recent row by its last activity', () => {
    const rows = sectionRows([
      ...blueprints(5, 10),
      s('warmOld', { kind: 'architect', status: 'warm-idle', last_active: day(1) }),
      s('warmNew', { kind: 'architect', status: 'warm-idle', last_active: day(20) }),
    ]);
    expect(ids(rows)).toEqual(['warmNew', 'bp4', 'bp3', 'bp2', 'bp1']);
  });

  it('always shows an active blueprint and one mid-turn', () => {
    const rows = sectionRows([
      ...blueprints(6, 10),
      s('active', { kind: 'architect', status: 'active', last_active: day(1) }),
      s('working', { kind: 'architect', status: 'warm-idle', assistant_working: true, last_active: day(2) }),
    ]);
    expect(ids(rows)).toEqual(['working', 'active', 'bp5', 'bp4', 'bp3']);
  });

  it('always shows a warm-idle chat', () => {
    const rows = sectionRows([
      ...cold(6, 10),
      s('warmChat', { kind: 'chat', status: 'warm-idle', last_active: day(1) }),
      s('warmLegacy', { status: 'warm-idle', last_active: day(2) }),
    ]);
    expect(ids(rows)).toEqual(['warmLegacy', 'warmChat', 'cold5', 'cold4', 'cold3']);
  });
});
