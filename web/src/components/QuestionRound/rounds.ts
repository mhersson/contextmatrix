import type { LogEntry } from '../../types';
import { parsePromptEntry } from '../ChatPanel/prompts';
import type { Round } from './parseRound';
import { storedAnswer, storedDecision } from './roundDraft';

/**
 * history: an older frame of a round, never shown; open or paused: the round waits on the operator and docks in place
 * of the composer; responder: the auto responder answers it, shown read-only; closed: the round takes no more answers
 * (all answered and the stage resumed, or the pipeline went past it), a summary; superseded: a later round replaced it
 * before it closed.
 */
export type RoundStatus = 'history' | 'open' | 'paused' | 'responder' | 'closed' | 'superseded';

export interface RoundFrame {
  entry: LogEntry;
  id: string;
  round: Round;
}

/** The kind of a user row typed in a round's chat box; it shows in that round's thread, never in the transcript. */
export const ROUND_CHAT_KIND = 'round_chat';

export interface RoundStates {
  byEntry: Map<LogEntry, RoundStatus>;
  /**
   * The newest frame of the round waiting on the operator, if any, and the round chat rows sent since that frame:
   * the next frame's thread is their record.
   */
  pending: (RoundFrame & { status: 'open' | 'paused'; sent: LogEntry[] }) | null;
}

export function roundFrame(entry: LogEntry): RoundFrame | null {
  const p = parsePromptEntry(entry)?.prompt;
  return p?.round && p.id ? { entry, id: p.id, round: p.round } : null;
}

/**
 * Every round row's status. The architect republishes a round under the same prompt id whenever it changes, so only
 * the newest frame per id counts. Only the newest round can wait, and it is open only while `live`. A round chat row
 * belongs to the round waiting where it sits in the log, as the architect takes a plain message while a round is open.
 */
export function deriveRounds(logs: readonly LogEntry[], live: boolean): RoundStates {
  const byEntry = new Map<LogEntry, RoundStatus>();
  const newest = new Map<string, RoundFrame>();
  let last: RoundFrame | null = null;
  let sent: LogEntry[] = [];
  for (const e of logs) {
    if (e.kind === ROUND_CHAT_KIND) {
      sent.push(e);
      continue;
    }
    const f = roundFrame(e);
    if (!f) continue;
    const prev = newest.get(f.id);
    if (prev) byEntry.set(prev.entry, 'history');
    newest.set(f.id, f);
    last = f;
    sent = [];
  }
  let pending: RoundStates['pending'] = null;
  for (const f of newest.values()) {
    let status: RoundStatus;
    if (f.round.closed) status = 'closed';
    else if (f !== last) status = 'superseded';
    else if (f.round.responder) status = 'responder';
    else status = live ? 'open' : 'paused';
    byEntry.set(f.entry, status);
    if (status === 'open' || status === 'paused') pending = { ...f, status, sent };
  }
  return { byEntry, pending };
}

export interface RoundLeft {
  questions: number;
  assumptions: number;
}

/** The newest frame of the round with this prompt id; null when the loaded transcript does not hold it. */
export function newestRound(logs: readonly LogEntry[], id: string): Round | null {
  for (let i = logs.length - 1; i >= 0; i--) {
    const f = roundFrame(logs[i]);
    if (f?.id === id) return f.round;
  }
  return null;
}

/** What the round still waits on, from what the architect stored. */
export function roundLeft(round: Round): RoundLeft {
  return {
    questions: round.questions.filter((q) => storedAnswer(round, q) === null).length,
    assumptions: round.assumptions.filter((a) => storedDecision(round, a.id) === null).length,
  };
}
