import { describe, it, expect } from 'vitest';
import { deriveHandoffStates, parseHandoffEntry } from './handoff';
import type { ChatSession, LogEntry } from '../../types';

function row(seq: number, content?: string, kind = 'handoff'): LogEntry {
  return {
    ts: '2026-10-23T10:00:00Z',
    card_id: '',
    type: 'system',
    kind,
    seq,
    content: content ?? JSON.stringify({ title: `T${seq}`, need: `need ${seq}`, projects: ['alpha'] }),
  };
}

function session(over: Partial<ChatSession>): ChatSession {
  return {
    id: 'B',
    title: 'Budgets',
    kind: 'architect',
    status: 'running',
    created_at: '2026-10-23T10:00:00Z',
    last_active: '2026-10-23T10:00:00Z',
    created_by: 'human:a',
    ...over,
  } as ChatSession;
}

describe('parseHandoffEntry', () => {
  it('parses a hand-off row and rejects malformed ones', () => {
    expect(parseHandoffEntry(row(3))).toEqual({ title: 'T3', need: 'need 3', projects: ['alpha'] });
    expect(parseHandoffEntry(row(3, 'not json'))).toBeNull();
    expect(parseHandoffEntry(row(3, '{"title":"t","projects":[]}'))).toBeNull();
    expect(parseHandoffEntry(row(3, '{"title":"t","need":"n","projects":[1]}'))).toBeNull();
    expect(parseHandoffEntry(row(3, undefined, 'marker'))).toBeNull();
  });
});

describe('deriveHandoffStates', () => {
  it('marks older hand-offs replaced and the newest open', () => {
    const a = row(3);
    const b = row(9);
    const states = deriveHandoffStates([a, b], 'C', []);
    expect(states.get(a)).toEqual({ status: 'replaced' });
    expect(states.get(b)).toEqual({ status: 'open' });
  });

  it('marks a hand-off started when a session has its origin', () => {
    const a = row(3);
    const s = session({ origin_chat: 'C', origin_seq: 3 });
    expect(deriveHandoffStates([a], 'C', [s]).get(a)).toEqual({
      status: 'started',
      blueprintId: 'B',
      blueprintTitle: 'Budgets',
    });
  });

  it('started wins over replaced', () => {
    const a = row(3);
    const b = row(9);
    const s = session({ origin_chat: 'C', origin_seq: 3 });
    const states = deriveHandoffStates([a, b], 'C', [s]);
    expect(states.get(a)?.status).toBe('started');
    expect(states.get(b)).toEqual({ status: 'open' });
  });

  it('a hand-off whose blueprint is gone is open again when newest', () => {
    const a = row(3);
    const other = session({ origin_chat: 'OTHER', origin_seq: 3 });
    expect(deriveHandoffStates([a], 'C', [other]).get(a)).toEqual({ status: 'open' });
  });
});
