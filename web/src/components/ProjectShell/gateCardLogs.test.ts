import { describe, it, expect } from 'vitest';
import { gateCardLogs } from './gateCardLogs';
import type { LogEntry } from '../../types';

function entry(cardId: string, content: string): LogEntry {
  return { ts: '2026-07-25T10:00:00Z', card_id: cardId, type: 'text', content };
}

describe('gateCardLogs', () => {
  it('returns matching logs by reference', () => {
    const logs = [entry('CARD-A', 'a'), entry('CARD-A', 'b')];
    expect(gateCardLogs(logs, 'CARD-A')).toBe(logs);
  });

  it('returns empty when any entry belongs to a foreign card', () => {
    const logs = [entry('CARD-A', 'a'), entry('CARD-B', 'stale'), entry('CARD-A', 'c')];
    expect(gateCardLogs(logs, 'CARD-A')).toHaveLength(0);
  });

  it('lets client gap markers (empty card_id) ride with a matching stream', () => {
    const logs = [entry('CARD-A', 'a'), entry('', 'gap marker'), entry('CARD-A', 'b')];
    expect(gateCardLogs(logs, 'CARD-A')).toBe(logs);
  });

  it('drops an all-foreign buffer including its gap markers', () => {
    const logs = [entry('CARD-B', 'stale'), entry('', 'gap marker')];
    expect(gateCardLogs(logs, 'CARD-A')).toHaveLength(0);
  });

  it('returns empty when no card is selected', () => {
    expect(gateCardLogs([entry('CARD-A', 'a')], undefined)).toHaveLength(0);
  });

  it('returns a stable empty reference across calls', () => {
    const a = gateCardLogs([], 'CARD-A');
    const b = gateCardLogs([entry('CARD-B', 'x')], 'CARD-A');
    expect(a).toBe(b);
  });
});
