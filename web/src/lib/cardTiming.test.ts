import { describe, expect, it } from 'vitest';
import type { Card } from '../types';
import { isWorkedOn, limitDuration, shortDuration, timingSummary, unclaimedWarningAt } from './cardTiming';

const T0 = Date.parse('2026-01-01T12:00:00Z');
const iso = (offsetMin: number) => new Date(T0 + offsetMin * 60_000).toISOString();

const held = {
  id: 'TEST-002', state: 'review', created: iso(-180), state_since: iso(-60),
  assigned_agent: 'worker-1', working_since: iso(-30), last_heartbeat: iso(-1),
  worked_seconds: 0, work_intervals: [{ agent: 'worker-1', from: iso(-30), to: null }],
} as Card;

const unheld = {
  ...held, assigned_agent: undefined, working_since: undefined, last_heartbeat: undefined,
  unclaimed_since: iso(-90), worked_seconds: 1200,
  work_intervals: [{ agent: 'worker-1', from: iso(-110), to: iso(-90) }],
} as Card;

describe('card timing', () => {
  it('formats the tile time to the minute and limits without zero units', () => {
    expect(shortDuration(42)).toBe('<1m');
    expect(shortDuration(14 * 60 + 59)).toBe('14m');
    expect(shortDuration(2 * 3600 + 14 * 60)).toBe('2h 14m');
    expect(shortDuration(3 * 86400 + 5 * 3600)).toBe('3d 5h');
    expect(limitDuration(3600)).toBe('1h');
    expect(limitDuration(5400)).toBe('1h 30m');
  });

  it('counts a card as worked on only with a holder, a claim start and a working state', () => {
    expect(isWorkedOn(held)).toBe(true);
    expect(isWorkedOn({ ...held, working_since: undefined })).toBe(false);
    expect(isWorkedOn(unheld)).toBe(false);
    for (const state of ['todo', 'done', 'stalled', 'not_planned', 'blocked']) {
      expect(isWorkedOn({ ...held, state })).toBe(false);
    }
  });

  it('keeps the held claim counting between reads', () => {
    const s = timingSummary(held, T0);
    expect(s.workingAge).toBe(30 * 60);
    expect(s.worked).toBe(30 * 60);
    expect(timingSummary(held, T0 + 60_000).worked).toBe(31 * 60);
    expect(s.heartbeatAge).toBe(60);
  });

  it('reads the total as unknown when the server leaves it out', () => {
    expect(timingSummary({ ...held, worked_seconds: undefined }, T0).worked).toBeNull();
  });

  it('turns the unclaimed warning on only when the board sets a limit', () => {
    expect(unclaimedWarningAt(unheld)).toBeNull();
    expect(unclaimedWarningAt(unheld, 0)).toBeNull();
    expect(unclaimedWarningAt(unheld, 3600)).toBe(T0 - 30 * 60_000);
    expect(unclaimedWarningAt(held, 3600)).toBeNull();
    expect(unclaimedWarningAt({ ...unheld, state: 'todo' }, 3600)).toBeNull();
    expect(timingSummary(unheld, T0).warningSeconds).toBeNull();
    expect(timingSummary(unheld, T0, 3600).warningSeconds).toBe(3600);
    expect(timingSummary(unheld, T0, 7200).warningSeconds).toBeNull();
  });

  it('times how long nobody has held a card', () => {
    const s = timingSummary(unheld, T0);
    expect(s.holder).toBeNull();
    expect(s.unclaimedAge).toBe(90 * 60);
    expect(s.finished).toBe(false);
    expect(timingSummary({ ...unheld, state: 'done' }, T0).finished).toBe(true);
  });
});
