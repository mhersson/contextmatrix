import type { LogEntry } from '../../types';

/** A plain-text payload longer than this collapses to a preview. */
export const COLLAPSE_CHAR_THRESHOLD = 600;
/** ... or one with more than this many newlines near the top. */
export const COLLAPSE_LINE_THRESHOLD = 6;
/** Collapsed preview: at most this many characters ... */
export const PREVIEW_CHAR_LIMIT = 400;
/** ... cut at this many lines. */
export const PREVIEW_LINE_LIMIT = 4;

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
