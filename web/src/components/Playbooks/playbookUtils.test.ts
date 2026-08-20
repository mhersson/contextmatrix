import { describe, expect, it } from 'vitest';
import type { PlaybookEntry, PlaybookSummary } from '../../types';
import { arrayMoveLocal, frontierIndex, isFullyComplete, segmentColor } from './playbookUtils';

function entry(complete: boolean): PlaybookEntry {
  return { id: `e-${complete}`, type: 'manual', complete };
}

function summary(complete: number, total: number): PlaybookSummary {
  return {
    id: 'p',
    title: 'Playbook',
    complete,
    total,
    segments: [],
    projects: 0,
    updated_at: '2026-08-20T09:00:00Z',
  };
}

describe('frontierIndex', () => {
  it.each<[string, PlaybookEntry[], number]>([
    ['empty list', [], -1],
    ['all complete', [entry(true), entry(true)], -1],
    ['mixed', [entry(true), entry(false), entry(false)], 1],
  ])('%s', (_label, entries, expected) => {
    expect(frontierIndex(entries)).toBe(expected);
  });
});

describe('isFullyComplete', () => {
  it.each([
    ['0/0', 0, 0, false],
    ['2/3', 2, 3, false],
    ['3/3', 3, 3, true],
  ] as const)('%s', (_label, complete, total, expected) => {
    expect(isFullyComplete(summary(complete, total))).toBe(expected);
  });
});

describe('segmentColor', () => {
  it.each([
    ['complete', 'var(--green)'],
    ['active', 'var(--aqua)'],
    ['missing', 'var(--bg-red)'],
    ['pending', 'var(--bg2)'],
  ])('%s -> %s', (seg, expected) => {
    expect(segmentColor(seg)).toBe(expected);
  });
});

describe('arrayMoveLocal', () => {
  it('moves an item from one index to another without mutating the input', () => {
    const input = ['a', 'b', 'c'];
    const result = arrayMoveLocal(input, 0, 2);
    expect(result).toEqual(['b', 'c', 'a']);
    expect(input).toEqual(['a', 'b', 'c']);
  });

  it('moves an item backward', () => {
    expect(arrayMoveLocal(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
  });
});
