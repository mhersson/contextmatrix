import { describe, it, expect } from 'vitest';
import { parseMarkerEntry } from './markers';
import type { LogEntry } from '../../types';

function row(content: string, kind = 'marker', type: LogEntry['type'] = 'text'): LogEntry {
  return { ts: '2026-10-17T10:00:00Z', card_id: '', type, kind, content, seq: 1 };
}

describe('parseMarkerEntry', () => {
  it('reads a sheet reference with its version and note', () => {
    const r = row(JSON.stringify({
      text: 'Design rev 2 stored.',
      marker: { kind: 'artifact', stage: 'solution_design', persona: 'system_designer', version: 2, note: 'Revised for the 3 findings' },
    }));
    expect(parseMarkerEntry(r)).toEqual({
      text: 'Design rev 2 stored.',
      marker: { kind: 'artifact', stage: 'solution_design', persona: 'system_designer', version: 2, note: 'Revised for the 3 findings' },
    });
  });

  it('reads a stage divider without the omitted fields', () => {
    const r = row(JSON.stringify({ text: 'Grounding started.', marker: { kind: 'stage', stage: 'grounding' } }), 'marker', 'system');
    expect(parseMarkerEntry(r)?.marker).toEqual({ kind: 'stage', stage: 'grounding', slice: undefined, persona: undefined, version: undefined, note: undefined });
  });

  it('falls back to the text line for a marker it cannot read', () => {
    expect(parseMarkerEntry(row(JSON.stringify({ text: 'Fallback.', marker: { kind: 'mystery', stage: 'x' } })))).toEqual({
      text: 'Fallback.',
      marker: null,
    });
    expect(parseMarkerEntry(row('not json'))).toEqual({ text: 'not json', marker: null });
  });

  it('ignores other rows and parses each row once', () => {
    expect(parseMarkerEntry(row('{}', 'prompt'))).toBeNull();
    const r = row(JSON.stringify({ text: 't', marker: { kind: 'stage', stage: 'intake' } }));
    expect(parseMarkerEntry(r)).toBe(parseMarkerEntry(r));
  });

  it('reads the slice a marker belongs to', () => {
    const r = row(JSON.stringify({ text: 't', marker: { kind: 'artifact', stage: 'solution_design', slice: 'SL2', version: 1 } }));
    expect(parseMarkerEntry(r)?.marker).toMatchObject({ stage: 'solution_design', slice: 'SL2', version: 1 });
  });
});
