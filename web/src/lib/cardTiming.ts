import type { Card } from '../types';
import { isTerminalState } from './cardState';
import { displayState } from './stateLabels';

/**
 * States in which a holder is not doing work: not started, finished,
 * stalled or blocked. `done` keeps its claim until release, so a holder
 * alone does not mean the card is being worked on.
 */
const NOT_WORKED_STATES = new Set(['todo', 'done', 'stalled', 'not_planned', 'blocked']);
/** States the unclaimed warning watches: work that should have someone on it. */
const WATCHED_STATES = new Set(['in_progress', 'review']);

export function elapsedSeconds(from: string | null | undefined, now: number): number {
  const start = from ? Date.parse(from) : NaN;
  return Number.isFinite(start) ? Math.max(0, (now - start) / 1000) : 0;
}

/** Seconds-precise duration for the timing box: "42s", "14m 5s", "2h 14m". */
export function duration(seconds: number): string {
  const n = Math.max(0, Math.floor(seconds));
  if (n < 60) return `${n}s`;
  if (n < 3600) return `${Math.floor(n / 60)}m ${n % 60}s`;
  return `${Math.floor(n / 3600)}h ${Math.floor((n % 3600) / 60)}m`;
}

/** Minute-precise duration for the tile clock: "<1m", "14m", "2h 14m", "3d 5h". */
export function shortDuration(seconds: number): string {
  const n = Math.max(0, Math.floor(seconds));
  if (n < 60) return '<1m';
  if (n < 3600) return `${Math.floor(n / 60)}m`;
  if (n < 86400) return `${Math.floor(n / 3600)}h ${Math.floor((n % 3600) / 60)}m`;
  return `${Math.floor(n / 86400)}d ${Math.floor((n % 86400) / 3600)}h`;
}

/** A configured limit without trailing zero units: "1h", "30m", "1h 30m". */
export function limitDuration(seconds: number): string {
  return shortDuration(seconds).replace(/ 0[mh]$/, '');
}

const stampFormat = new Intl.DateTimeFormat(undefined, {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
});

/** Short local date and time; empty for a missing or unparsable stamp. */
export function formatStamp(ts: string | null | undefined): string {
  const ms = ts ? Date.parse(ts) : NaN;
  return Number.isFinite(ms) ? stampFormat.format(new Date(ms)) : '';
}

/** An agent holds the card (with a known claim start) in a state where work happens. */
export function isWorkedOn(card: Card): boolean {
  return !!card.assigned_agent && !!card.working_since && !NOT_WORKED_STATES.has(card.state);
}

/**
 * When an unheld in_progress/review card crosses the board's unclaimed
 * warning (epoch ms), or null when the warning is off or does not apply.
 */
export function unclaimedWarningAt(card: Card, warningSeconds?: number): number | null {
  if (!warningSeconds || warningSeconds <= 0) return null;
  if (card.assigned_agent || !WATCHED_STATES.has(card.state)) return null;
  const since = Date.parse(card.unclaimed_since ?? card.state_since ?? card.created);
  return Number.isFinite(since) ? since + warningSeconds * 1000 : null;
}

export interface TimingInterval {
  agent: string;
  from: string;
  to: string | null;
  seconds: number;
}

export interface TimingSummary {
  /** Display label of the current state. */
  state: string;
  stateSince: string;
  stateAge: number;
  /** done / not_planned: the state row shows when, not how long. */
  finished: boolean;
  holder: string | null;
  workingSince: string | null;
  workingAge: number;
  /** Seconds since the holder's last heartbeat; null without a holder or a beat. */
  heartbeatAge: number | null;
  unclaimedSince: string | null;
  unclaimedAge: number;
  /** Total held time of every recorded claim; null when unknown. */
  worked: number | null;
  intervals: TimingInterval[];
  /** The board's unclaimed warning limit when it is on and has passed. */
  warningSeconds: number | null;
}

export function timingSummary(card: Card, now: number, warningSeconds?: number): TimingSummary {
  const holder = card.assigned_agent || null;
  const stateSince = card.state_since ?? card.created;
  const intervals = (card.work_intervals ?? []).map((i) => ({
    ...i,
    seconds: elapsedSeconds(i.from, i.to ? Date.parse(i.to) : now),
  }));
  // Summed here so the held claim keeps counting between reads; the server
  // leaves worked_seconds out when the log cannot account for every claim.
  const worked = card.worked_seconds == null ? null : intervals.reduce((sum, i) => sum + i.seconds, 0);
  const unclaimedSince = holder ? null : card.unclaimed_since ?? stateSince;
  const warningAt = unclaimedWarningAt(card, warningSeconds);

  return {
    state: displayState(card.state),
    stateSince,
    stateAge: elapsedSeconds(stateSince, now),
    finished: isTerminalState(card.state),
    holder,
    workingSince: holder ? card.working_since ?? null : null,
    workingAge: holder ? elapsedSeconds(card.working_since, now) : 0,
    heartbeatAge: holder && card.last_heartbeat ? elapsedSeconds(card.last_heartbeat, now) : null,
    unclaimedSince,
    unclaimedAge: unclaimedSince ? elapsedSeconds(unclaimedSince, now) : 0,
    worked,
    intervals,
    warningSeconds: warningAt != null && now >= warningAt ? (warningSeconds ?? null) : null,
  };
}
