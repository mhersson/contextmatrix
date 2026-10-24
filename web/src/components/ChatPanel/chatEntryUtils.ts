import type { LogEntry } from '../../types';

/** Messages visible when the transcript first renders (post-filter). */
export const INITIAL_TAIL = 50;
/** Older messages revealed per "Show older" step. */
export const REVEAL_CHUNK = 100;
/** Server limit on a chat session message, in UTF-8 bytes. */
export const CHAT_MESSAGE_MAX_BYTES = 8192;
/** Server limit on a blueprint session message, in UTF-8 bytes. */
export const BLUEPRINT_MESSAGE_MAX_BYTES = 65536;
/** Server limit on one typed answer in a question round reply, in UTF-8 bytes. */
export const ROUND_ANSWER_MAX_BYTES = 32768;

const utf8Encoder = new TextEncoder();

/** Byte length of s as UTF-8, the unit the server limits messages in. */
export function utf8Length(s: string): number {
  return utf8Encoder.encode(s).length;
}

/** Human size; KiB round up so a size over a limit never reads as at or below it. */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const kib = Math.ceil((n / 1024) * 10) / 10;
  return `${kib.toFixed(1).replace(/\.0$/, '')} KiB`;
}

/** A plain-text payload longer than this collapses to a preview. */
export const COLLAPSE_CHAR_THRESHOLD = 600;
/** ... or one with more than this many newlines near the top. */
export const COLLAPSE_LINE_THRESHOLD = 6;
/** Collapsed preview: at most this many characters ... */
export const PREVIEW_CHAR_LIMIT = 400;
/** ... cut at this many lines. */
export const PREVIEW_LINE_LIMIT = 4;

/** Structured-JSON preview: at most this many pretty-printed lines ... */
export const JSON_PREVIEW_LINE_LIMIT = 6;
/** ... within this many characters. */
export const JSON_PREVIEW_CHAR_LIMIT = 500;
/** A markdown text message longer than this clamps behind Read more ... */
export const TEXT_CLAMP_CHAR_THRESHOLD = 2500;
/** ... or one with more than this many lines (line-dense lists are tall
 *  long before they are long in characters). */
export const TEXT_CLAMP_LINE_THRESHOLD = 30;

/** Count newlines up to `limit`+1, scanning at most the first 2 KiB so a huge
 *  payload never costs a full pass just to decide it collapses. */
export function countNewlines(s: string, limit: number): number {
  let count = 0;
  const scan = s.length > 2048 ? s.slice(0, 2048) : s;
  for (let i = 0; i < scan.length && count <= limit; i++) {
    if (scan[i] === '\n') count++;
  }
  return count;
}

/**
 * Structured-output detector: a text message whose whole body is one JSON
 * object or array (planner output, mob-moderator verdicts) returns its
 * pretty-printed form; conversational text returns null. Scalars don't count -
 * a message that happens to be "true" or a number is still conversation - and
 * neither does anything that fails to parse (e.g. a markdown link line, which
 * also starts with `[`).
 */
export function parseStructured(content: string): string | null {
  const trimmed = content.trim();
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return null;
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (parsed === null || typeof parsed !== 'object') return null;
    return JSON.stringify(parsed, null, 2);
  } catch {
    return null;
  }
}

export function accentFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'tool_result': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--bg3)';
  }
}

export function textFor(type: LogEntry['type']): string {
  switch (type) {
    case 'thinking': return 'var(--grey2)';
    case 'tool_call': return 'var(--aqua)';
    case 'tool_result': return 'var(--aqua)';
    case 'stderr': return 'var(--yellow)';
    case 'system': return 'var(--green)';
    case 'gap': return 'var(--orange)';
    default: return 'var(--fg)';
  }
}
