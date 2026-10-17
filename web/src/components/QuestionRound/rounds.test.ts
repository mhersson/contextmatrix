import { describe, expect, it } from 'vitest';
import type { LogEntry } from '../../types';
import { rawRound, roundChatEntry, roundEntry } from '../../test-utils/roundFixtures';
import { deriveRounds, newestRound, roundLeft } from './rounds';

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

  it('keeps the newest round docked when a late frame of an older round arrives', () => {
    const one = roundEntry(1, 'round:1', rawRound());
    const two = roundEntry(2, 'round:2', rawRound());
    const lateClose = roundEntry(3, 'round:1', rawRound({ closed: true }));
    const closed = deriveRounds([one, two, lateClose], true);
    expect(closed.byEntry.get(lateClose)).toBe('closed');
    expect(closed.byEntry.get(two)).toBe('open');
    expect(closed.pending?.id).toBe('round:2');

    const lateOpen = roundEntry(3, 'round:1', rawRound({ answers: [{ number: 1, option: 1 }] }));
    const reopened = deriveRounds([one, two, lateOpen], true);
    expect(reopened.byEntry.get(lateOpen)).toBe('superseded');
    expect(reopened.pending?.id).toBe('round:2');
  });

  it('supersedes an open round with a later closed responder round and docks nothing', () => {
    const one = roundEntry(1, 'round:1', rawRound());
    const auto = roundEntry(2, 'round:2', rawRound({ responder: true, closed: true }));
    const s = deriveRounds([one, auto], true);
    expect(s.byEntry.get(one)).toBe('superseded');
    expect(s.byEntry.get(auto)).toBe('closed');
    expect(s.pending).toBeNull();
  });

  it('shows a round chat row no round was waiting for in the transcript, and only that one', () => {
    const open = roundEntry(1, 'round:1', rawRound());
    const held = roundChatEntry(2, 'held');
    const closed = roundEntry(3, 'round:1', rawRound({ closed: true }));
    const late = roundChatEntry(4, 'late');
    const s = deriveRounds([open, held, closed, late], true);
    expect([...s.stray]).toEqual([late]);

    const auto = roundEntry(5, 'round:2', rawRound({ responder: true }));
    const afterAuto = roundChatEntry(6, 'after the responder');
    expect(deriveRounds([open, auto, afterAuto], true).stray.has(afterAuto)).toBe(true);
  });

  it('keeps a round chat row out of the transcript when its round is not loaded yet', () => {
    const early = roundChatEntry(1, 'about an older round');
    const s = deriveRounds([early, roundEntry(2, 'round:1', rawRound({ chat: [{ from: 'user', text: 'about an older round' }] }))], true);
    expect(s.stray.size).toBe(0);
    expect(s.pending?.sent).toEqual([]);
  });

  it('keeps the waiting round its chat rows across a late frame of an older round', () => {
    const one = roundEntry(1, 'round:1', rawRound());
    const two = roundEntry(2, 'round:2', rawRound());
    const sent = roundChatEntry(3, 'about round 2');
    const s = deriveRounds([one, two, sent, roundEntry(4, 'round:1', rawRound({ closed: true }))], true);
    expect(s.pending?.sent).toEqual([sent]);
    expect(s.stray.size).toBe(0);
  });

  it('shows a responder round read-only and never docks it', () => {
    const a = roundEntry(1, 'round:1', rawRound({ responder: true }));
    const s = deriveRounds([a], true);
    expect(s.byEntry.get(a)).toBe('responder');
    expect(s.pending).toBeNull();
  });

  it('holds the round chat rows sent since the waiting frame, and none for a later round', () => {
    const a = roundEntry(1, 'round:1', rawRound());
    const early = roundChatEntry(2, 'early');
    const b = roundEntry(3, 'round:1', rawRound());
    const late = roundChatEntry(4, 'late');
    expect(deriveRounds([a, early, b, late], true).pending?.sent).toEqual([late]);
    const logs = [a, early, b, late, roundEntry(5, 'round:1', rawRound({ closed: true })), roundEntry(6, 'round:2', rawRound())];
    expect(deriveRounds(logs, true).pending?.sent).toEqual([]);
  });

  it('ignores a round frame without an id', () => {
    const s = deriveRounds([roundEntry(1, '', rawRound())], true);
    expect(s.byEntry.size).toBe(0);
    expect(s.pending).toBeNull();
  });
});

describe('newestRound and roundLeft', () => {
  it('counts what the newest frame of a round still lacks', () => {
    const logs = [
      roundEntry(1, 'round:1', rawRound()),
      roundEntry(2, 'round:1', rawRound({ answers: [{ number: 1, option: 1 }], assumptions: [{ id: 'A1', text: 'x' }] })),
    ];
    const r = newestRound(logs, 'round:1');
    expect(r && roundLeft(r)).toEqual({ questions: 2, assumptions: 1 });
    expect(newestRound(logs, 'round:9')).toBeNull();
  });
});
