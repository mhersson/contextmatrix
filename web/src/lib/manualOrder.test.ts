import { describe, expect, it } from 'vitest';
import { applyMove } from './manualOrder';

describe('applyMove', () => {
  it('moves an item down (active before over)', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'A', 'C')).toEqual(['B', 'C', 'A']);
  });

  it('moves an item up (active after over)', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'C', 'A')).toEqual(['C', 'A', 'B']);
  });

  it('is a no-op when dropped on itself', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'B', 'B')).toEqual(['A', 'B', 'C']);
  });

  it('appends unknown columnIds after stored ids, in visual order', () => {
    expect(applyMove(['A', 'B'], ['A', 'B', 'C', 'D'], 'A', 'A')).toEqual(['A', 'B', 'C', 'D']);
  });

  it('keeps hidden neighbours in place when columnIds is filtered', () => {
    // B is hidden from the caller (e.g. filtered out of view) but stays put.
    expect(applyMove(['A', 'B', 'C'], ['A', 'C'], 'C', 'A')).toEqual(['C', 'A', 'B']);
  });

  it('inserts an id present in neither list immediately before the over card', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'Z', 'B')).toEqual(['A', 'Z', 'B', 'C']);
  });

  it('appends to the end when over-id is not found (e.g. a column/state name)', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'A', 'in_progress')).toEqual([
      'B',
      'C',
      'A',
    ]);
  });

  it('appends to the end when both active and over ids are unknown', () => {
    expect(applyMove(['A', 'B', 'C'], ['A', 'B', 'C'], 'Z', 'unknown')).toEqual([
      'A',
      'B',
      'C',
      'Z',
    ]);
  });

  it('handles an empty stored list', () => {
    expect(applyMove([], ['A', 'B', 'C'], 'A', 'C')).toEqual(['B', 'C', 'A']);
  });

  it('handles an empty columnIds list', () => {
    expect(applyMove(['A', 'B', 'C'], [], 'A', 'C')).toEqual(['B', 'C', 'A']);
  });

  it('de-dupes a repeated id in columnIds and keeps stored ids ahead of newly-seen ones', () => {
    expect(applyMove(['A', 'B'], ['B', 'C', 'C', 'A'], 'A', 'A')).toEqual(['A', 'B', 'C']);
  });

  it('does not mutate the input arrays', () => {
    const stored = ['A', 'B', 'C'];
    const columnIds = ['A', 'B', 'C', 'D'];
    applyMove(stored, columnIds, 'A', 'C');
    expect(stored).toEqual(['A', 'B', 'C']);
    expect(columnIds).toEqual(['A', 'B', 'C', 'D']);
  });

  it.each([
    ['move down by one', ['A', 'B', 'C', 'D'], 'B', 'C', ['A', 'C', 'B', 'D']],
    ['move up by one', ['A', 'B', 'C', 'D'], 'C', 'B', ['A', 'C', 'B', 'D']],
    ['move to the front', ['A', 'B', 'C', 'D'], 'D', 'A', ['D', 'A', 'B', 'C']],
    ['move to the back', ['A', 'B', 'C', 'D'], 'A', 'D', ['B', 'C', 'D', 'A']],
  ])('%s', (_name, stored, activeId, overId, expected) => {
    expect(applyMove(stored, stored, activeId, overId)).toEqual(expected);
  });
});
