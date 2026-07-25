import { describe, it, expect } from 'vitest';
import { decorateLogs, filterLogEntries } from './decorateLogs';
import type { LogEntry } from '../../types';

function entry(type: LogEntry['type'], content: string, ts = '2026-07-25T10:00:00Z'): LogEntry {
  return { ts, card_id: 'C-1', type, content };
}

describe('filterLogEntries', () => {
  const logs: LogEntry[] = [
    entry('text', 'a'),
    entry('tool_call', 'call'),
    entry('tool_result', 'result'),
    entry('thinking', 'hmm'),
    entry('stderr', 'oops'),
    entry('user', 'hi'),
  ];

  it('applies the three toggles and gates tool_result with tool_call', () => {
    const shown = filterLogEntries(logs, { showText: true, showToolCalls: false, showThinking: false });
    expect(shown.map((e) => e.type)).toEqual(['text', 'stderr', 'user']);

    const withTools = filterLogEntries(logs, { showText: false, showToolCalls: true, showThinking: false });
    expect(withTools.map((e) => e.type)).toEqual(['tool_call', 'tool_result', 'stderr', 'user']);
  });
});

describe('decorateLogs', () => {
  it('stamps the first eligible entry of any slice', () => {
    const decorated = decorateLogs([entry('text', 'a')]);
    expect(decorated[0].showStamp).toBe(true);
  });

  it('dedupes stamps for same type within the same minute', () => {
    const decorated = decorateLogs([
      entry('text', 'a', '2026-07-25T10:00:00Z'),
      entry('text', 'b', '2026-07-25T10:00:30Z'),
      entry('text', 'c', '2026-07-25T10:01:00Z'),
    ]);
    expect(decorated.map((d) => d.showStamp)).toEqual([true, false, true]);
  });

  it('stamps again when the speaker side flips', () => {
    const decorated = decorateLogs([
      entry('text', 'a', '2026-07-25T10:00:00Z'),
      entry('user', 'b', '2026-07-25T10:00:10Z'),
      entry('text', 'c', '2026-07-25T10:00:20Z'),
    ]);
    expect(decorated.map((d) => d.showStamp)).toEqual([true, true, true]);
  });

  it('never stamps non-conversational entries and skips bad timestamps', () => {
    const decorated = decorateLogs([
      entry('tool_call', 'call'),
      entry('text', 'bad-ts', 'not-a-date'),
    ]);
    expect(decorated.map((d) => d.showStamp)).toEqual([false, false]);
  });
});
