import { describe, it, expect } from 'vitest';
import { changedBlocks, changedById, compareBlocks, splitBlocks, wordDiff } from './revisions';

describe('splitBlocks', () => {
  it('splits at blank lines and keeps fenced code whole', () => {
    expect(splitBlocks('One.\n\nTwo\nlines.\n\n```go\na := 1\n\nb := 2\n```\n\n\nEnd.')).toEqual([
      'One.',
      'Two\nlines.',
      '```go\na := 1\n\nb := 2\n```',
      'End.',
    ]);
  });

  it('reads CRLF line ends', () => {
    expect(splitBlocks('A.\r\n\r\nB.')).toEqual(['A.', 'B.']);
  });
});

describe('changedBlocks', () => {
  it('marks new and edited blocks only', () => {
    expect([...changedBlocks(['A.', 'B.', 'C.'], ['A.', 'B changed.', 'C.', 'D.'])]).toEqual([1, 3]);
  });

  it('ignores whitespace-only edits', () => {
    expect(changedBlocks(['A  b.'], ['A b.']).size).toBe(0);
  });
});

describe('compareBlocks', () => {
  it('pairs an edited block and keeps the rest in order', () => {
    expect(compareBlocks(['A.', 'B.', 'C.'], ['A.', 'B2.', 'C.', 'D.'])).toEqual([
      { kind: 'same', text: 'A.' },
      { kind: 'changed', before: 'B.', after: 'B2.' },
      { kind: 'same', text: 'C.' },
      { kind: 'added', text: 'D.' },
    ]);
  });

  it('reports a dropped block as removed', () => {
    expect(compareBlocks(['A.', 'B.'], ['A.'])).toEqual([
      { kind: 'same', text: 'A.' },
      { kind: 'removed', text: 'B.' },
    ]);
  });
});

describe('wordDiff', () => {
  it('separates removed and added words', () => {
    const parts = wordDiff('Call MarshalIndent and append', 'Call SetIndent and append');
    const removed = parts.filter((p) => p.kind === 'removed').map((p) => p.text).join('');
    const added = parts.filter((p) => p.kind === 'added').map((p) => p.text).join('');
    expect(removed).toBe('MarshalIndent');
    expect(added).toBe('SetIndent');
    expect(parts.filter((p) => p.kind !== 'added').map((p) => p.text).join('')).toBe('Call MarshalIndent and append');
  });
});

describe('changedById', () => {
  it('finds edited and new items by id', () => {
    const prev = [{ id: 'D1', text: 'a' }, { id: 'D2', text: 'b' }];
    const next = [{ id: 'D1', text: 'a2' }, { id: 'D2', text: 'b' }, { id: 'D3', text: 'c' }];
    expect([...changedById(prev, next)].sort()).toEqual(['D1', 'D3']);
  });
});
