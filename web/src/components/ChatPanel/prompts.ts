import type { LogEntry } from '../../types';
import { parseRound, type Round } from '../QuestionRound/parseRound';
import { COMMAND_KIND } from '../QuestionRound/rounds';
import { isDecisionEntry } from '../Blueprint/markers';

/** One reply button on an architect prompt. A click sends `reply`; a
 *  feedback action asks for text first and sends `reply` + space + text. */
export interface PromptAction {
  label: string;
  reply: string;
  feedback: boolean;
  /** The architect's label for the feedback box, such as why you approve over critical findings; absent for the default label. */
  ask?: string;
  /** The architect's suggested answer; shown as a chip. */
  recommended: boolean;
}

/** The prompt half of a kind "prompt" row's `{text, prompt}` envelope. */
export interface ChatPrompt {
  id?: string;
  /** Display hint: "question" or "gate"; any other value is neutral. */
  kind?: string;
  /** The prompt's own text without the option list; the envelope text is the fallback. */
  text?: string;
  /** The technical text behind a plain-language prompt; present only when non-blank. */
  detail?: string;
  freeText: boolean;
  actions: PromptAction[];
  /** Set on a kind "round" prompt with an id: a tabbed question round (QuestionRound/rounds.ts derives its state). */
  round?: Round;
}

export interface PromptEnvelope {
  text: string;
  prompt: ChatPrompt;
}

export type PromptState =
  | { status: 'open' }
  | { status: 'paused' }
  | { status: 'answered'; label: string | null }
  | { status: 'superseded' };

const envelopes = new WeakMap<LogEntry, PromptEnvelope | null>();

/** Parses a kind "prompt" row; null for any other row or an envelope that
 *  does not parse, which then renders as an ordinary message. Cached per
 *  entry: ring rows are immutable and reference-stable. */
export function parsePromptEntry(entry: LogEntry): PromptEnvelope | null {
  if (entry.kind !== 'prompt') return null;
  const cached = envelopes.get(entry);
  if (cached !== undefined) return cached;
  const parsed = parseEnvelope(entry.content);
  envelopes.set(entry, parsed);
  return parsed;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function parseEnvelope(content: string): PromptEnvelope | null {
  let raw: unknown;
  try {
    raw = JSON.parse(content);
  } catch {
    return null;
  }
  if (!isRecord(raw) || typeof raw.text !== 'string' || !isRecord(raw.prompt)) return null;
  const p = raw.prompt;
  const actions = Array.isArray(p.actions)
    ? p.actions.flatMap((a): PromptAction[] =>
        isRecord(a) &&
        typeof a.label === 'string' &&
        a.label.trim() !== '' &&
        typeof a.reply === 'string' &&
        a.reply.trim() !== ''
          ? [
              {
                label: a.label,
                reply: a.reply,
                feedback: a.feedback === true,
                recommended: a.recommended === true,
                ...(typeof a.ask === 'string' && a.ask.trim() !== '' ? { ask: a.ask } : {}),
              },
            ]
          : [],
      )
    : [];
  const id = typeof p.id === 'string' && p.id !== '' ? p.id : undefined;
  const kind = typeof p.kind === 'string' && p.kind !== '' ? p.kind : undefined;
  const round = kind === 'round' && id ? parseRound(p.round) : null;
  return {
    text: raw.text,
    prompt: {
      id,
      kind,
      text: typeof p.text === 'string' && p.text !== '' ? p.text : undefined,
      ...(typeof p.detail === 'string' && p.detail.trim() !== '' ? { detail: p.detail } : {}),
      freeText: p.free_text === true,
      actions,
      ...(round ? { round } : {}),
    },
  };
}

/** The action a reply selects, or null. Mirrors the architect, which folds
 *  case: the exact reply (trimmed), or for a feedback action the reply
 *  followed by whitespace and non-empty text. Replies may be several words;
 *  when several actions match, the longest reply wins, so "reject slicing
 *  merge them" is the `reject slicing` action, not `reject`. */
export function matchReply(prompt: ChatPrompt, content: string): PromptAction | null {
  const text = content.trim().toLowerCase();
  let best: PromptAction | null = null;
  let bestLength = -1;
  for (const a of prompt.actions) {
    const reply = a.reply.trim().toLowerCase();
    const hit = a.feedback
      ? text.startsWith(reply) && /^\s+\S/.test(text.slice(reply.length))
      : text === reply;
    if (hit && reply.length > bestLength) {
      best = a;
      bestLength = reply.length;
    }
  }
  return best;
}

/**
 * Every prompt's state, keyed by its row. A later user row answers a prompt
 * when it matches an action (any row does on a free-text prompt), and so does
 * the architect's decision row when it decided the prompt itself; a later
 * prompt supersedes one still unanswered. Only the last unanswered prompt
 * can be open, and only while `live` (the session takes sends and the stream
 * is connected); otherwise it is paused.
 */
export function derivePromptStates(logs: readonly LogEntry[], live: boolean): Map<LogEntry, PromptState> {
  const states = new Map<LogEntry, PromptState>();
  let pending: { entry: LogEntry; prompt: ChatPrompt } | null = null;
  for (const e of logs) {
    const env = parsePromptEntry(e);
    // A round has its own state (deriveRounds); it only ends an unanswered prompt.
    if (env?.prompt.round) {
      if (pending) states.set(pending.entry, { status: 'superseded' });
      pending = null;
      continue;
    }
    if (env) {
      if (pending) states.set(pending.entry, { status: 'superseded' });
      pending = { entry: e, prompt: env.prompt };
      continue;
    }
    if (pending && isDecisionEntry(e)) {
      states.set(pending.entry, { status: 'answered', label: null });
      pending = null;
      continue;
    }
    if (pending && e.type === 'user' && e.kind !== COMMAND_KIND) {
      const action = matchReply(pending.prompt, e.content);
      if (action || pending.prompt.freeText) {
        states.set(pending.entry, { status: 'answered', label: action ? action.label : null });
        pending = null;
      }
    }
  }
  if (pending) states.set(pending.entry, { status: live ? 'open' : 'paused' });
  return states;
}
