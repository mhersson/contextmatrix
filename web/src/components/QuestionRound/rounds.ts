import type { LogEntry } from '../../types';
import { parsePromptEntry } from '../ChatPanel/prompts';
import type { Round } from './parseRound';

/**
 * history: an older frame of a round, never shown; open or paused: the round waits on the operator and docks in place
 * of the composer; responder: the auto responder answers it, shown read-only; closed: answered and resumed, a summary;
 * superseded: a later round replaced it before it closed.
 */
export type RoundStatus = 'history' | 'open' | 'paused' | 'responder' | 'closed' | 'superseded';

export interface RoundFrame {
  entry: LogEntry;
  id: string;
  round: Round;
}

export interface RoundStates {
  byEntry: Map<LogEntry, RoundStatus>;
  /** The newest frame of the round waiting on the operator, if any. */
  pending: (RoundFrame & { status: 'open' | 'paused' }) | null;
}

export function roundFrame(entry: LogEntry): RoundFrame | null {
  const p = parsePromptEntry(entry)?.prompt;
  return p?.round && p.id ? { entry, id: p.id, round: p.round } : null;
}

/**
 * Every round row's status. The architect republishes a round under the same prompt id whenever it changes, so only
 * the newest frame per id counts. Only the newest round can wait, and it is open only while `live`.
 */
export function deriveRounds(logs: readonly LogEntry[], live: boolean): RoundStates {
  const byEntry = new Map<LogEntry, RoundStatus>();
  const newest = new Map<string, RoundFrame>();
  let last: RoundFrame | null = null;
  for (const e of logs) {
    const f = roundFrame(e);
    if (!f) continue;
    const prev = newest.get(f.id);
    if (prev) byEntry.set(prev.entry, 'history');
    newest.set(f.id, f);
    last = f;
  }
  let pending: RoundStates['pending'] = null;
  for (const f of newest.values()) {
    let status: RoundStatus;
    if (f.round.closed) status = 'closed';
    else if (f !== last) status = 'superseded';
    else if (f.round.responder) status = 'responder';
    else status = live ? 'open' : 'paused';
    byEntry.set(f.entry, status);
    if (status === 'open' || status === 'paused') pending = { ...f, status };
  }
  return { byEntry, pending };
}
