import type { Card } from '../types';

/** States where a holder is not doing work. `done` keeps its claim until release. */
const NOT_WORKED_STATES = new Set(['todo', 'blocked', 'stalled', 'done', 'not_planned']);

export interface ClaimInterval {
  agent: string;
  from: string;
  /** null while the claim is held. */
  to: string | null;
}

export interface CardTiming {
  /** The newest transition into the current state, else creation. */
  stateSince: string;
  /** Every recorded claim, oldest first. */
  intervals: ClaimInterval[];
}

/** Derives the card's timing from its log's claimed, released and state_changed entries. */
export function cardTiming(card: Card): CardTiming {
  const log = [...(card.activity_log ?? [])].sort((a, b) => Date.parse(a.ts) - Date.parse(b.ts));
  const intervals: ClaimInterval[] = [];
  let stateSince = card.created;
  let open = null as ClaimInterval | null;

  for (const e of log) {
    if (e.action === 'state_changed') {
      if (e.message.split(' -> ')[1] === card.state) stateSince = e.ts;
    } else if (e.action === 'claimed' && open?.agent !== e.agent) {
      // A replayed claim by the holder is skipped; another agent's claim ends
      // the open one, as a takeover without a release.
      if (open) open.to = e.ts;
      open = { agent: e.agent, from: e.ts, to: null };
      intervals.push(open);
    } else if (e.action === 'released' && open?.agent === e.agent) {
      open.to = e.ts;
      open = null;
    }
  }

  // An unreleased claim the card no longer holds has no known end.
  if (open && open.agent !== card.assigned_agent) intervals.pop();
  return { stateSince, intervals };
}

/** An agent holds the card in a state where work happens. */
export function isWorkedOn(card: Card): boolean {
  return !!card.assigned_agent && !NOT_WORKED_STATES.has(card.state);
}

export function elapsedSeconds(from: string, now: number): number {
  const start = Date.parse(from);
  return Number.isFinite(start) ? Math.max(0, (now - start) / 1000) : 0;
}

/** Minute-precise duration: "<1m", "14m", "2h 14m", "3d 5h 12m". */
export function duration(seconds: number): string {
  const minutes = Math.floor(Math.max(0, seconds) / 60);
  if (minutes < 1) return '<1m';
  const parts = [`${Math.floor(minutes / 1440)}d`, `${Math.floor(minutes / 60) % 24}h`, `${minutes % 60}m`];
  return parts.slice(minutes < 60 ? 2 : minutes < 1440 ? 1 : 0).join(' ');
}

/** The tile's duration: the two largest units, "3d 5h". */
export function shortDuration(seconds: number): string {
  return duration(seconds).split(' ').slice(0, 2).join(' ');
}

const stampFormat = new Intl.DateTimeFormat(undefined, {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
});

/** Short local date and time; empty for an unparsable stamp. */
export function formatStamp(ts: string): string {
  const ms = Date.parse(ts);
  return Number.isFinite(ms) ? stampFormat.format(new Date(ms)) : '';
}
