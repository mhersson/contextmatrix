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

/** The kind of a user row typed in a round's chat box; it shows in the waiting round's thread, not the transcript. */
export const ROUND_CHAT_KIND = 'round_chat';

/** The kind of a user row that answers a round (final or partial); the round's block and summary are its record. */
export const ROUND_REPLY_KIND = 'round_reply';

/** The kind of a user row that carries a command to the architect (e.g. a cost cap change); it never shows and answers nothing. */
export const COMMAND_KIND = 'command';

export interface RoundStates {
  byEntry: Map<LogEntry, RoundStatus>;
  /**
   * The newest frame of the round waiting on the operator, if any, and the round chat rows sent since that frame:
   * the next frame's thread is their record.
   */
  pending: (RoundFrame & { status: 'open' | 'paused'; sent: LogEntry[] }) | null;
  /**
   * Round chat rows stored after a round frame that was not waiting (closed, or a responder's); they show in the
   * transcript as ordinary messages. A row before the first loaded frame stays out: its round may be in older history.
   */
  stray: Set<LogEntry>;
  /**
   * Round reply rows that follow a round frame: the reply answered a round loaded here, so the transcript omits it.
   * A reply before the first loaded frame answered a round in older history and stays a plain message.
   */
  answered: Set<LogEntry>;
}

export function roundFrame(entry: LogEntry): RoundFrame | null {
  const p = parsePromptEntry(entry)?.prompt;
  return p?.round && p.id ? { entry, id: p.id, round: p.round } : null;
}

/**
 * Every round row's status. The architect republishes a round under the same prompt id whenever it changes, so only
 * the newest frame per id counts. The current round is the one whose id appeared last: a late frame of an older round
 * never displaces it. Only the current round can wait, and it is open only while `live`. A round chat row belongs to
 * the round waiting where it sits in the log, as the architect takes a plain message while a round is open.
 */
export function deriveRounds(logs: readonly LogEntry[], live: boolean): RoundStates {
  const byEntry = new Map<LogEntry, RoundStatus>();
  const newest = new Map<string, RoundFrame>();
  const stray = new Set<LogEntry>();
  const answered = new Set<LogEntry>();
  let current: string | null = null;
  let sent: LogEntry[] = [];
  for (const e of logs) {
    if (e.kind === ROUND_REPLY_KIND) {
      if (newest.size > 0) answered.add(e);
      continue;
    }
    if (e.kind === ROUND_CHAT_KIND) {
      const round = current === null ? undefined : newest.get(current)?.round;
      if (round && !round.closed && !round.responder) sent.push(e);
      else if (round) stray.add(e);
      continue;
    }
    const f = roundFrame(e);
    if (!f) continue;
    const prev = newest.get(f.id);
    if (prev) byEntry.set(prev.entry, 'history');
    else current = f.id;
    newest.set(f.id, f);
    if (f.id === current) sent = [];
  }
  let pending: RoundStates['pending'] = null;
  for (const f of newest.values()) {
    let status: RoundStatus;
    if (f.round.closed) status = 'closed';
    else if (f.id !== current) status = 'superseded';
    else if (f.round.responder) status = 'responder';
    else status = live ? 'open' : 'paused';
    byEntry.set(f.entry, status);
    if (status === 'open' || status === 'paused') pending = { ...f, status, sent };
  }
  return { byEntry, pending, stray, answered };
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
