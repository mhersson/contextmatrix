import { describe, expect, it } from 'vitest';
import type { LogEntry } from '../../types';
import { rawRound, roundEntry } from '../../test-utils/roundFixtures';
import { deriveRounds } from './rounds';

const text = (seq: number, content: string): LogEntry => ({ ts: '2026-10-17T10:00:00Z', card_id: '', type: 'text', content, seq });

describe('deriveRounds', () => {
  it('keeps only the newest frame of a round; older frames are history', () => {
    const a = roundEntry(1, 'round:1', rawRound());
    const b = roundEntry(3, 'round:1', rawRound({ answers: [{ number: 1, option: 1 }] }));
    const s = deriveRounds([a, text(2, 'noted'), b], true);
    expect(s.byEntry.get(a)).toBe('history');
    expect(s.byEntry.get(b)).toBe('open');
    expect(s.pending?.entry).toBe(b);
    expect(s.pending?.id).toBe('round:1');
    expect(s.pending?.round.answers).toEqual([{ number: 1, option: 1 }]);
  });

  it('pauses the waiting round while the session is not live', () => {
    const a = roundEntry(1, 'round:1', rawRound());
    const s = deriveRounds([a], false);
    expect(s.byEntry.get(a)).toBe('paused');
    expect(s.pending?.status).toBe('paused');
  });

  it('collapses a closed round and leaves nothing waiting', () => {
    const a = roundEntry(1, 'round:1', rawRound());
    const b = roundEntry(2, 'round:1', rawRound({ closed: true }));
    const s = deriveRounds([a, b], true);
    expect(s.byEntry.get(b)).toBe('closed');
    expect(s.pending).toBeNull();
  });

  it('marks an unclosed round superseded once a later round arrives', () => {
    const one = roundEntry(1, 'round:1', rawRound());
    const two = roundEntry(2, 'round:2', rawRound());
    const s = deriveRounds([one, two], true);
    expect(s.byEntry.get(one)).toBe('superseded');
    expect(s.byEntry.get(two)).toBe('open');
    expect(s.pending?.id).toBe('round:2');
  });

  it('shows a responder round read-only and never docks it', () => {
    const a = roundEntry(1, 'round:1', rawRound({ responder: true }));
    const s = deriveRounds([a], true);
    expect(s.byEntry.get(a)).toBe('responder');
    expect(s.pending).toBeNull();
  });

  it('ignores a round frame without an id', () => {
    const s = deriveRounds([roundEntry(1, '', rawRound())], true);
    expect(s.byEntry.size).toBe(0);
    expect(s.pending).toBeNull();
  });
});
