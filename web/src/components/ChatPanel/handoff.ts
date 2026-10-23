import type { ChatSession, LogEntry } from '../../types';

/** Transcript kind of the system row a chat gets when it hands a need to the architect. */
export const HANDOFF_KIND = 'handoff';
/** Transcript kind of the user row that carries the need into the blueprint. */
export const HANDOFF_NEED_KIND = 'handoff_need';

export interface Handoff {
  title: string;
  need: string;
  projects: string[];
}

/** What the New Chat dialog opens with for a hand-off: the origin row and its parsed content. */
export interface HandoffPrefill {
  chatId: string;
  chatTitle: string;
  seq: number;
  title: string;
  need: string;
  projects: string[];
}

export type HandoffState =
  | { status: 'open' }
  | { status: 'replaced' }
  | { status: 'started'; blueprintId: string; blueprintTitle: string };

const parsed = new WeakMap<LogEntry, Handoff | null>();

function parse(content: string): Handoff | null {
  let raw: unknown;
  try {
    raw = JSON.parse(content);
  } catch {
    return null;
  }
  if (typeof raw !== 'object' || raw === null) return null;
  const { title, need, projects } = raw as Record<string, unknown>;
  if (typeof title !== 'string' || typeof need !== 'string') return null;
  if (!Array.isArray(projects) || !projects.every((p) => typeof p === 'string')) return null;
  return { title, need, projects: projects as string[] };
}

/** The hand-off a row carries, or null when it is not a hand-off row or cannot be read. */
export function parseHandoffEntry(entry: LogEntry): Handoff | null {
  if (entry.kind !== HANDOFF_KIND) return null;
  if (parsed.has(entry)) return parsed.get(entry) ?? null;
  const result = parse(entry.content);
  parsed.set(entry, result);
  return result;
}

/**
 * State of every hand-off row in the loaded logs. A blueprint whose origin is
 * the row means started (and wins over replaced); otherwise a later hand-off
 * row means replaced. A deleted blueprint is simply absent from the sessions,
 * so its hand-off is open again when it is the newest.
 */
export function deriveHandoffStates(
  logs: readonly LogEntry[],
  chatId: string | undefined,
  sessions: readonly ChatSession[],
): ReadonlyMap<LogEntry, HandoffState> {
  const states = new Map<LogEntry, HandoffState>();
  const rows = logs.filter((e) => e.kind === HANDOFF_KIND);
  rows.forEach((row, i) => {
    const started = chatId
      ? sessions.find((s) => s.origin_chat === chatId && s.origin_seq === row.seq)
      : undefined;
    if (started) {
      states.set(row, { status: 'started', blueprintId: started.id, blueprintTitle: started.title });
    } else if (i < rows.length - 1) {
      states.set(row, { status: 'replaced' });
    } else {
      states.set(row, { status: 'open' });
    }
  });
  return states;
}
