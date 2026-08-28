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

  it('hides slog INFO/DEBUG diagnostics on stderr, keeps WARN/ERROR and non-slog stderr', () => {
    const allOn = { showText: true, showToolCalls: true, showThinking: true };
    const diagnostics: LogEntry[] = [
      entry('stderr', '2026/08/28 05:07:54 INFO selector: tier reachability card_id=CTXMAX-738 role=coder tier=moderate bar=0.76'),
      entry('stderr', '2026/08/28 05:10:34 DEBUG http client retry attempt=2'),
      entry('stderr', 'time=2026-08-28T05:07:54.123Z level=INFO msg="selector: pick" card_id=CTXMAX-738'),
      entry('stderr', 'time=2026-08-28T05:07:54.123Z level=DEBUG msg=cache'),
    ];
    expect(filterLogEntries(diagnostics, allOn)).toEqual([]);

    const kept: LogEntry[] = [
      entry('stderr', '2026/08/28 05:07:54 WARN selector: favorite configured for an unknown tier tier=critical'),
      entry('stderr', '2026/08/28 05:07:54 ERROR release card failed card=CTXMAX-738'),
      entry('stderr', 'time=2026-08-28T05:07:54.123Z level=WARN msg=degraded'),
      entry('stderr', 'panic: runtime error: invalid memory address'),
      entry('stderr', 'model call failed: context deadline exceeded'),
    ];
    expect(filterLogEntries(kept, allOn)).toEqual(kept);
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
