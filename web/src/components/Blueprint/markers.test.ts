import { describe, it, expect } from 'vitest';
import { isDecisionEntry, parseMarkerEntry } from './markers';
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

  it('reads a decision the architect made itself, and tells it from other markers', () => {
    const decided = row(JSON.stringify({ text: 'Sign-off approved automatically.', marker: { kind: 'decision', stage: 'signoff', version: 1 } }), 'marker', 'system');
    expect(parseMarkerEntry(decided)?.marker?.kind).toBe('decision');
    expect(isDecisionEntry(decided)).toBe(true);
    expect(isDecisionEntry(row(JSON.stringify({ text: 't', marker: { kind: 'stage', stage: 'intake' } })))).toBe(false);
    expect(isDecisionEntry(row(JSON.stringify({ text: 't', marker: { kind: 'decision', stage: '' } })))).toBe(false);
    expect(isDecisionEntry(row('approve', '', 'user'))).toBe(false);
  });

  it('reads the slice a marker belongs to', () => {
    const r = row(JSON.stringify({ text: 't', marker: { kind: 'artifact', stage: 'solution_design', slice: 'SL2', version: 1 } }));
    expect(parseMarkerEntry(r)?.marker).toMatchObject({ stage: 'solution_design', slice: 'SL2', version: 1 });
  });

  it('reads a review marker with its slice, version and reviewer', () => {
    const r = row(JSON.stringify({
      text: 'The tester scored acceptance (SL2) version 2 4 of 5.',
      marker: { kind: 'review', stage: 'acceptance', slice: 'SL2', version: 2, persona: 'tester' },
    }));
    expect(parseMarkerEntry(r)?.marker).toMatchObject({ kind: 'review', stage: 'acceptance', slice: 'SL2', version: 2, persona: 'tester' });
  });
});
