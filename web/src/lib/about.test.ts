import { describe, it, expect } from 'vitest';
import {
  changeKind,
  changesOf,
  formatBuilt,
  formatMergedDate,
  imageMismatch,
  newCount,
  nextSeen,
  parseSeen,
  unseenCounts,
  versionLabel,
  componentLabel,
  shortSha,
} from './about';
import type { AboutComponent, AboutResponse, BuildChange } from '../types';

const ch = (number: number, merged_at: string, kind = 'feat'): BuildChange => ({
  number,
  kind,
  title: `change ${number}`,
  merged_at,
});

describe('versionLabel', () => {
  it('covers every component state', () => {
    expect(versionLabel({ name: 'agent', status: 'unreachable' })).toBe('-');
    expect(versionLabel({ name: 'chat', status: 'connected' })).toBe('version unknown');
    expect(versionLabel({ name: 'agent', status: 'connected', build: { commit: '', changes: [] } })).toBe('unknown build');
    expect(
      versionLabel({ name: 'agent', status: 'connected', build: { version: 'v0.42.0', commit: 'cfa04af1234', changes: [] } }),
    ).toBe('v0.42.0');
    expect(versionLabel({ name: 'agent', status: 'connected', build: { commit: 'cfa04af1234567', changes: [] } })).toBe('cfa04af');
  });
});

describe('imageMismatch', () => {
  const full = 'a'.repeat(40);
  const other = 'a'.repeat(39) + 'b';
  it('flags only two non-empty different revisions', () => {
    expect(imageMismatch({ name: 'agent', status: 'connected', build: { commit: full, changes: [] }, worker_image_revision: other })).toBe(true);
    expect(imageMismatch({ name: 'agent', status: 'connected', build: { commit: full, changes: [] }, worker_image_revision: full })).toBe(false);
    expect(imageMismatch({ name: 'agent', status: 'connected', build: { commit: full, changes: [] } })).toBe(false);
    expect(imageMismatch({ name: 'agent', status: 'connected', build: { commit: '', changes: [] }, worker_image_revision: full })).toBe(false);
  });
});

describe('small helpers', () => {
  it('maps kinds, changes, labels and shas', () => {
    expect(changeKind('feat')).toBe('feat');
    expect(changeKind('fix')).toBe('fix');
    expect(changeKind('perf')).toBe('change');
    expect(changesOf({ name: 'chat', status: 'connected', build: { changes: null } })).toEqual([]);
    expect(changesOf({ name: 'chat', status: 'unreachable' })).toEqual([]);
    expect(componentLabel('contextmatrix')).toBe('ContextMatrix');
    expect(componentLabel('architect')).toBe('Architect');
    expect(shortSha('0123456789abcdef')).toBe('0123456');
  });

  it('formats dates in local time and falls back', () => {
    expect(formatMergedDate('2026-10-20T12:00:00Z')).toBe('Oct 20');
    expect(formatMergedDate('')).toBe('');
    expect(formatBuilt('2026-10-20T12:14:00Z')).toMatch(/^Oct 20 \d{2}:\d{2}$/);
    expect(formatBuilt(undefined)).toBe('-');
    expect(formatBuilt('garbage')).toBe('-');
  });
});

describe('newCount', () => {
  const L = [ch(220, '2026-10-20T10:00:00Z'), ch(223, '2026-10-19T10:00:00Z'), ch(219, '2026-10-18T10:00:00Z')];
  const rows: [string, BuildChange[], { number: number; merged_at: string } | undefined, number][] = [
    ['nothing seen', L, undefined, 3],
    ['seen in list (by position)', L, { number: 223, merged_at: '2026-10-19T10:00:00Z' }, 1],
    ['newest seen', L, { number: 220, merged_at: '2026-10-20T10:00:00Z' }, 0],
    ['seen fell out of the window', L, { number: 100, merged_at: '2026-10-01T00:00:00Z' }, 3],
    ['rolled back', L, { number: 300, merged_at: '2026-10-30T00:00:00Z' }, 0],
    ['partly newer', L, { number: 100, merged_at: '2026-10-19T12:00:00Z' }, 1],
    ['mark date empty', L, { number: 100, merged_at: '' }, 0],
    [
      'empty entry date stops the count',
      [ch(5, '2026-10-20T10:00:00Z'), ch(4, ''), ch(3, '2026-10-19T10:00:00Z')],
      { number: 1, merged_at: '2026-10-01T00:00:00Z' },
      1,
    ],
  ];
  it.each(rows)('%s', (_name, changes, mark, want) => {
    expect(newCount(changes, mark)).toBe(want);
  });
});

describe('parseSeen', () => {
  it('rejects non-objects', () => {
    for (const raw of [null, 'x', [], 42]) expect(parseSeen(raw)).toEqual({});
  });
  it('keeps only well-formed known entries', () => {
    expect(
      parseSeen({
        agent: { number: 5, merged_at: 'd' },
        chat: { number: '5', merged_at: 'd' },
        bogus: { number: 1, merged_at: 'd' },
        architect: { number: 2 },
      }),
    ).toEqual({ agent: { number: 5, merged_at: 'd' } });
  });
});

describe('nextSeen', () => {
  it('records the newest change per component and keeps the rest', () => {
    const m1 = { number: 1, merged_at: 't1' };
    const comps: AboutComponent[] = [
      { name: 'agent', status: 'connected', build: { changes: [{ ...ch(9, 't9'), title: 'x' }, ch(8, 't8')] } },
      { name: 'chat', status: 'unreachable' },
      { name: 'architect', status: 'connected', build: { changes: [] } },
    ];
    const next = nextSeen({ chat: m1 }, comps);
    expect(next).toEqual({ agent: { number: 9, merged_at: 't9' }, chat: m1 });
    expect(Object.keys(next.agent!).sort()).toEqual(['merged_at', 'number']);
  });
});

describe('unseenCounts', () => {
  it('handles null and omits components without changes', () => {
    expect(unseenCounts(null, {})).toEqual({});
    const about: AboutResponse = {
      components: [
        { name: 'agent', status: 'connected', build: { changes: [ch(2, 't2'), ch(1, 't1')] } },
        { name: 'chat', status: 'connected', build: { changes: [] } },
      ],
    };
    expect(unseenCounts(about, {})).toEqual({ agent: 2 });
  });
});
