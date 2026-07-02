import { describe, it, expect } from 'vitest';
import { logRowKey } from './RunnerConsoleLog';
import type { LogEntry } from '../../types';

describe('logRowKey', () => {
  it('is derived from the entry alone (independent of array index) and unique per entry', () => {
    const a: LogEntry = {
      ts: '2026-01-01T00:00:01.000Z',
      card_id: 'X',
      type: 'text',
      content: 'a',
      seq: 10,
    };
    const b: LogEntry = {
      ts: '2026-01-01T00:00:02.000Z',
      card_id: 'X',
      type: 'text',
      content: 'b',
      seq: 11,
    };
    // Stable for the same entry regardless of where it sits in the buffer.
    expect(logRowKey(a)).toBe(logRowKey(a));
    expect(logRowKey(a)).not.toBe(logRowKey(b));
    // Client-only gap markers (no seq) still get a distinct key.
    const gap: LogEntry = {
      ts: '2026-01-01T00:00:03.000Z',
      card_id: '',
      type: 'gap',
      content: 'seq gap',
    };
    expect(logRowKey(gap)).not.toBe(logRowKey(a));
  });
});
