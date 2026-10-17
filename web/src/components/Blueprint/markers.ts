import type { LogEntry, LogMarker } from '../../types';

export interface MarkerRow {
  /** The plain line the architect sends with every marker. */
  text: string;
  /** Null when the marker does not parse; the row then shows its text. */
  marker: LogMarker | null;
}

const rows = new WeakMap<LogEntry, MarkerRow>();

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** Parses a kind "marker" row's {text, marker} envelope; null for every other row. Cached per ring row. */
export function parseMarkerEntry(entry: LogEntry): MarkerRow | null {
  if (entry.kind !== 'marker') return null;
  const cached = rows.get(entry);
  if (cached) return cached;
  const parsed = parseEnvelope(entry.content);
  rows.set(entry, parsed);
  return parsed;
}

function parseEnvelope(content: string): MarkerRow {
  let raw: unknown;
  try {
    raw = JSON.parse(content);
  } catch {
    return { text: content, marker: null };
  }
  if (!isRecord(raw)) return { text: content, marker: null };
  const text = typeof raw.text === 'string' ? raw.text : '';
  const m = isRecord(raw.marker) ? raw.marker : null;
  if (!m || (m.kind !== 'stage' && m.kind !== 'artifact') || typeof m.stage !== 'string' || m.stage === '') {
    return { text: text || content, marker: null };
  }
  return {
    text,
    marker: {
      kind: m.kind,
      stage: m.stage,
      persona: typeof m.persona === 'string' && m.persona !== '' ? m.persona : undefined,
      version: typeof m.version === 'number' && m.version > 0 ? m.version : undefined,
      note: typeof m.note === 'string' && m.note !== '' ? m.note : undefined,
    },
  };
}
