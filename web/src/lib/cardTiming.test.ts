import { describe, expect, it } from 'vitest';
import type { ActivityEntry, Card } from '../types';
import { cardTiming, duration, elapsedSeconds, formatStamp, isWorkedOn, shortDuration } from './cardTiming';

const T0 = Date.parse('2026-01-01T12:00:00Z');
const iso = (offsetMin: number) => new Date(T0 + offsetMin * 60_000).toISOString();

const claimed = (agent: string, min: number): ActivityEntry => ({ agent, ts: iso(min), action: 'claimed', message: 'card claimed' });
const released = (agent: string, min: number): ActivityEntry => ({ agent, ts: iso(min), action: 'released', message: 'claim ended' });
const moved = (from: string, to: string, min: number): ActivityEntry => ({
  agent: 'human:alice', ts: iso(min), action: 'state_changed', message: `${from} -> ${to}`,
});

const card = (patch: Partial<Card>): Card => ({
  id: 'TEST-001', title: 't', project: 'p', type: 'task', state: 'in_progress', priority: 'low',
  created: iso(-120), updated: iso(0), body: '', autonomous: false, ...patch,
} as Card);

describe('cardTiming', () => {
  it('follows a claim lifecycle across a release and a new holder', () => {
    const t = cardTiming(card({
      state: 'review',
      assigned_agent: 'worker-b',
      activity_log: [
        claimed('worker-a', 0),
        moved('in_progress', 'review', 10),
        released('worker-a', 15),
        claimed('worker-b', 60),
      ],
    }));
    expect(t.stateSince).toBe(iso(10));
    expect(t.intervals).toEqual([
      { agent: 'worker-a', from: iso(0), to: iso(15) },
      { agent: 'worker-b', from: iso(60), to: null },
    ]);
  });

  it('ignores a replayed claim by the same holder and a duplicated release', () => {
    const t = cardTiming(card({
      activity_log: [claimed('worker', -2), claimed('worker', -1), released('worker', 0), released('worker', 0)],
    }));
    expect(t.intervals).toEqual([{ agent: 'worker', from: iso(-2), to: iso(0) }]);
  });

  it('ends the previous claim when another agent takes over without a release', () => {
    const t = cardTiming(card({
      assigned_agent: 'worker-b',
      activity_log: [claimed('worker-a', -10), claimed('worker-b', -4)],
    }));
    expect(t.intervals).toEqual([
      { agent: 'worker-a', from: iso(-10), to: iso(-4) },
      { agent: 'worker-b', from: iso(-4), to: null },
    ]);
  });

  it('ignores a release by an agent that does not hold the open claim', () => {
    const t = cardTiming(card({
      assigned_agent: 'worker-a',
      activity_log: [released('worker-b', -20), claimed('worker-a', -10), released('worker-b', -5)],
    }));
    expect(t.intervals).toEqual([{ agent: 'worker-a', from: iso(-10), to: null }]);
  });

  it('drops an open claim the card no longer holds', () => {
    const log = [claimed('worker', -30)];
    expect(cardTiming(card({ activity_log: log })).intervals).toEqual([]);
    expect(cardTiming(card({ assigned_agent: 'other', activity_log: log })).intervals).toEqual([]);
  });

  it('keeps every claim of a long-running card', () => {
    const log = Array.from({ length: 60 }, (_, i) => [claimed('worker', i), released('worker', i + 0.5)]).flat();
    const t = cardTiming(card({ activity_log: log }));
    expect(t.intervals).toHaveLength(60);
    expect(t.intervals[59]).toEqual({ agent: 'worker', from: iso(59), to: iso(59.5) });
  });

  it('reads the log in time order', () => {
    const t = cardTiming(card({
      assigned_agent: 'worker',
      activity_log: [claimed('worker', -5), released('worker', -20), claimed('worker', -30)],
    }));
    expect(t.intervals).toEqual([
      { agent: 'worker', from: iso(-30), to: iso(-20) },
      { agent: 'worker', from: iso(-5), to: null },
    ]);
  });

  it('dates the state from the newest transition into it, else from creation', () => {
    expect(cardTiming(card({})).stateSince).toBe(iso(-120));
    expect(cardTiming(card({ activity_log: [moved('todo', 'review', -50)] })).stateSince).toBe(iso(-120));
    const log = [moved('todo', 'in_progress', -90), moved('in_progress', 'review', -60), moved('review', 'in_progress', -30)];
    expect(cardTiming(card({ activity_log: log })).stateSince).toBe(iso(-30));
    expect(cardTiming(card({ state: 'review', activity_log: log })).stateSince).toBe(iso(-60));
  });
});

describe('isWorkedOn', () => {
  it('needs a holder and a state where work happens', () => {
    expect(isWorkedOn(card({ assigned_agent: 'worker' }))).toBe(true);
    expect(isWorkedOn(card({ state: 'review', assigned_agent: 'worker' }))).toBe(true);
    expect(isWorkedOn(card({}))).toBe(false);
    for (const state of ['todo', 'blocked', 'stalled', 'done', 'not_planned']) {
      expect(isWorkedOn(card({ state, assigned_agent: 'worker' }))).toBe(false);
    }
  });
});

describe('timing formatters', () => {
  it('formats durations to the minute, the tile keeping the two largest units', () => {
    expect(duration(42)).toBe('<1m');
    expect(duration(14 * 60 + 59)).toBe('14m');
    expect(duration(2 * 3600 + 14 * 60)).toBe('2h 14m');
    expect(duration(3 * 86400 + 5 * 3600 + 12 * 60)).toBe('3d 5h 12m');
    expect(duration(-5)).toBe('<1m');
    expect(shortDuration(42)).toBe('<1m');
    expect(shortDuration(14 * 60)).toBe('14m');
    expect(shortDuration(2 * 3600 + 14 * 60)).toBe('2h 14m');
    expect(shortDuration(3 * 86400 + 5 * 3600 + 12 * 60)).toBe('3d 5h');
  });

  it('measures elapsed seconds, never negative', () => {
    expect(elapsedSeconds(iso(-5), T0)).toBe(300);
    expect(elapsedSeconds(iso(5), T0)).toBe(0);
    expect(elapsedSeconds('not a time', T0)).toBe(0);
  });

  it('formats a stamp, empty when it does not parse', () => {
    expect(formatStamp(iso(0))).not.toBe('');
    expect(formatStamp('not a time')).toBe('');
  });
});
