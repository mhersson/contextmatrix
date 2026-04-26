import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useRunnerLogs } from './useRunnerLogs';

// ---------------------------------------------------------------------------
// Fake EventSource
// ---------------------------------------------------------------------------

type ESListener = (event: MessageEvent) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  url: string;
  readyState: number = 0; // CONNECTING
  onopen: (() => void) | null = null;
  onmessage: ESListener | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  /** Simulate successful open */
  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  /** Push a raw JSON string as a message event */
  simulateMessage(data: unknown) {
    const evt = { data: JSON.stringify(data) } as MessageEvent;
    this.onmessage?.(evt);
  }

  /** Simulate a connection error */
  simulateError() {
    this.readyState = 2;
    this.onerror?.();
  }

  close() {
    this.readyState = 2;
    this.closed = true;
  }
}

// Install fake before each test, restore after
beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function latestES(): FakeEventSource {
  const instances = FakeEventSource.instances;
  if (instances.length === 0) throw new Error('No EventSource created');
  return instances[instances.length - 1];
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useRunnerLogs — dropped frame handling', () => {
  it('(a) dropped frame inserts a gap marker entry and does not add a blank LogEntry', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });
    expect(result.current.connected).toBe(true);

    // Send a dropped frame
    act(() => {
      latestES().simulateMessage({ type: 'dropped', count: 3 });
    });

    const logs = result.current.logs;
    expect(logs).toHaveLength(1);

    // Must be a gap marker — type 'gap'
    expect(logs[0].type).toBe('gap');
    // Content must mention how many were dropped
    expect(logs[0].content).toMatch(/3/);
    // Must NOT have empty/blank content
    expect(logs[0].content.trim()).not.toBe('');
  });

  it('dropped frame with count=0 still produces a gap marker', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    act(() => {
      latestES().simulateMessage({ type: 'dropped' });
    });

    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].type).toBe('gap');
  });
});

describe('useRunnerLogs — terminal frame handling', () => {
  it('(b) terminal frame after log delivery sets connected=false and no reconnect is scheduled', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });
    expect(result.current.connected).toBe(true);

    // Deliver one log entry first so terminal is treated as a clean session end,
    // not as the empty-buffer fast-path race.
    act(() => {
      latestES().simulateMessage({
        type: 'text',
        content: 'hi',
        card_id: 'C-1',
        ts: new Date().toISOString(),
        seq: 1,
      });
    });

    const instancesBefore = FakeEventSource.instances.length;

    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    expect(result.current.connected).toBe(false);

    // Advance time well past the max reconnect delay — no new EventSource should appear
    act(() => { vi.advanceTimersByTime(60_000); });

    expect(FakeEventSource.instances.length).toBe(instancesBefore);
  });

  it('terminal frame does not push any entry to logs', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    expect(result.current.logs).toHaveLength(0);
  });

  it('onerror after a clean terminal does NOT schedule reconnect', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    // Deliver one log entry first so terminal halts reconnects (clean end).
    act(() => {
      latestES().simulateMessage({
        type: 'text',
        content: 'hi',
        card_id: 'C-1',
        ts: new Date().toISOString(),
        seq: 1,
      });
    });

    act(() => { latestES().simulateMessage({ type: 'terminal' }); });

    const countAfterTerminal = FakeEventSource.instances.length;

    // Even if an onerror fires after terminal, no reconnect should happen
    act(() => {
      try { latestES().simulateError(); } catch { /* may be closed */ }
    });
    act(() => { vi.advanceTimersByTime(60_000); });

    expect(FakeEventSource.instances.length).toBe(countAfterTerminal);
    expect(result.current.connected).toBe(false);
  });
});

describe('useRunnerLogs — seq gap detection', () => {
  it('(c) out-of-order seq inserts a client-side gap marker between entries', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();

    // First normal entry at seq=5
    act(() => {
      latestES().simulateMessage({
        type: 'text',
        content: 'hello',
        card_id: 'PROJ-001',
        ts,
        seq: 5,
      });
    });

    // Second entry at seq=8 (gap of 2)
    act(() => {
      latestES().simulateMessage({
        type: 'text',
        content: 'world',
        card_id: 'PROJ-001',
        ts,
        seq: 8,
      });
    });

    const logs = result.current.logs;
    // Expect: [entry@5, gap_marker, entry@8]
    expect(logs).toHaveLength(3);
    expect(logs[0].type).toBe('text');
    expect(logs[0].content).toBe('hello');
    expect(logs[1].type).toBe('gap');
    expect(logs[2].type).toBe('text');
    expect(logs[2].content).toBe('world');
  });

  it('consecutive seq numbers do NOT insert a gap marker', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();

    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'a', card_id: 'C-1', ts, seq: 1 });
    });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'b', card_id: 'C-1', ts, seq: 2 });
    });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'c', card_id: 'C-1', ts, seq: 3 });
    });

    expect(result.current.logs).toHaveLength(3);
    expect(result.current.logs.every((e) => e.type === 'text')).toBe(true);
  });

  it('first message with any seq does not produce a gap (no previous seq)', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'first', card_id: 'C-1', ts: new Date().toISOString(), seq: 100 });
    });

    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].type).toBe('text');
  });

  it('seq gap marker content mentions the sequence numbers', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();

    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'a', card_id: 'C-1', ts, seq: 5 });
    });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'b', card_id: 'C-1', ts, seq: 8 });
    });

    const gapEntry = result.current.logs[1];
    expect(gapEntry.type).toBe('gap');
    // Should mention the gap (5 to 8)
    expect(gapEntry.content).toMatch(/5|8|gap/i);
  });
});

describe('useRunnerLogs — terminal-before-snapshot race guard', () => {
  it('terminal frame before any log events schedules a reconnect (not permanent halt)', () => {
    renderHook(() => useRunnerLogs({ project: 'proj', enabled: true }));

    act(() => { latestES().simulateOpen(); });

    const countBefore = FakeEventSource.instances.length;

    // Send terminal with no preceding log events — ring buffer is empty
    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    // No immediate reconnect
    expect(FakeEventSource.instances.length).toBe(countBefore);

    // After the backoff delay, a reconnect SHOULD happen (not a permanent halt)
    act(() => { vi.advanceTimersByTime(1100); });
    expect(FakeEventSource.instances.length).toBeGreaterThan(countBefore);
  });

  it('terminal frame before any log events does NOT set terminalRef (logs stays empty, no halt)', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    // Send terminal with no preceding log events
    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    // logs must remain empty (terminal frame itself never adds a log entry)
    expect(result.current.logs).toHaveLength(0);

    const countAfterTerminal = FakeEventSource.instances.length;

    // Advance time — reconnect should happen since terminalRef was NOT set
    act(() => { vi.advanceTimersByTime(1100); });
    expect(FakeEventSource.instances.length).toBeGreaterThan(countAfterTerminal);
  });

  it('terminal frame after at least one log event still halts reconnects (existing behavior preserved)', () => {
    renderHook(() => useRunnerLogs({ project: 'proj', enabled: true }));

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();

    // Send one log event first
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'some log', card_id: 'C-1', ts, seq: 1 });
    });

    const countBefore = FakeEventSource.instances.length;

    // Now send terminal — logs have been delivered so this should halt reconnects
    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    // Advance time well past max reconnect delay — no new EventSource should appear
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(FakeEventSource.instances.length).toBe(countBefore);
  });

  it('after reconnect triggered by empty-buffer terminal, second connection delivers logs normally', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });

    // First connection: terminal arrives immediately before any logs
    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    // Trigger the reconnect
    act(() => { vi.advanceTimersByTime(1100); });

    // Second connection opens
    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();

    // Second connection delivers a log entry — normal behavior
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'hello from second connect', card_id: 'C-1', ts, seq: 1 });
    });

    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].content).toBe('hello from second connect');

    const countBeforeTerminal = FakeEventSource.instances.length;

    // Now a terminal arrives after logs — should halt reconnects
    act(() => {
      latestES().simulateMessage({ type: 'terminal' });
    });

    act(() => { vi.advanceTimersByTime(60_000); });
    expect(FakeEventSource.instances.length).toBe(countBeforeTerminal);
  });
});

describe('useRunnerLogs — reconnect on error (non-terminal)', () => {
  it('onerror triggers reconnect after delay when not terminal', () => {
    renderHook(() => useRunnerLogs({ project: 'proj', enabled: true }));

    act(() => { latestES().simulateOpen(); });

    const countBefore = FakeEventSource.instances.length;

    act(() => { latestES().simulateError(); });

    // No immediate reconnect
    expect(FakeEventSource.instances.length).toBe(countBefore);

    // After delay, reconnect should happen
    act(() => { vi.advanceTimersByTime(1100); });
    expect(FakeEventSource.instances.length).toBeGreaterThan(countBefore);
  });
});

describe('useRunnerLogs — close→reopen does not duplicate entries', () => {
  it('buffer is cleared on reopen so server snapshot replay does not produce duplicates', () => {
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        useRunnerLogs({ project: 'proj', enabled, cardId: 'CARD-1' }),
      { initialProps: { enabled: true } },
    );

    act(() => { latestES().simulateOpen(); });
    expect(result.current.connected).toBe(true);

    const ts = new Date().toISOString();
    const N = 3;

    // Step 2: push N messages
    act(() => {
      for (let i = 0; i < N; i++) {
        latestES().simulateMessage({ type: 'text', content: `msg-${i}`, card_id: 'CARD-1', ts, seq: i + 1 });
      }
    });

    // Step 3: assert N entries in buffer
    expect(result.current.logs).toHaveLength(N);

    // Step 4: close the console (enabled=false)
    act(() => { rerender({ enabled: false }); });

    // Step 5: reopen the console (enabled=true)
    act(() => { rerender({ enabled: true }); });
    act(() => { latestES().simulateOpen(); });

    // Step 6: push the same N messages again (server snapshot replay)
    act(() => {
      for (let i = 0; i < N; i++) {
        latestES().simulateMessage({ type: 'text', content: `msg-${i}`, card_id: 'CARD-1', ts, seq: i + 1 });
      }
    });

    // Step 7: regression assertion — must be N, not 2N
    expect(result.current.logs).toHaveLength(N);
  });
});

describe('useRunnerLogs — stream identity changes', () => {
  it('clears buffer when cardId changes so entries from previous card do not bleed', () => {
    const { result, rerender } = renderHook(
      ({ cardId }: { cardId: string }) =>
        useRunnerLogs({ project: 'proj', enabled: true, cardId }),
      { initialProps: { cardId: 'CARD-A' } },
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'a-1', card_id: 'CARD-A', ts, seq: 1 });
    });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'a-2', card_id: 'CARD-A', ts, seq: 2 });
    });

    expect(result.current.logs).toHaveLength(2);
    expect(result.current.logs[0].content).toBe('a-1');

    // Switch to a different card — buffer must be cleared before new stream fills.
    act(() => { rerender({ cardId: 'CARD-B' }); });
    expect(result.current.logs).toHaveLength(0);

    act(() => { latestES().simulateOpen(); });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'b-1', card_id: 'CARD-B', ts, seq: 10 });
    });

    // Only the new card's entry should be visible; previous card's entries must be gone.
    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].content).toBe('b-1');
  });

  it('clears buffer when project changes', () => {
    const { result, rerender } = renderHook(
      ({ project }: { project: string }) =>
        useRunnerLogs({ project, enabled: true }),
      { initialProps: { project: 'proj-1' } },
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'from-1', card_id: 'X-1', ts, seq: 1 });
    });
    expect(result.current.logs).toHaveLength(1);

    act(() => { rerender({ project: 'proj-2' }); });
    expect(result.current.logs).toHaveLength(0);
  });

  it('does NOT emit a spurious seq gap marker on the first message after a card switch', () => {
    const { result, rerender } = renderHook(
      ({ cardId }: { cardId: string }) =>
        useRunnerLogs({ project: 'proj', enabled: true, cardId }),
      { initialProps: { cardId: 'CARD-A' } },
    );

    act(() => { latestES().simulateOpen(); });

    const ts = new Date().toISOString();
    // Push entries on card A, advancing seq high.
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'a', card_id: 'CARD-A', ts, seq: 100 });
    });

    // Switch to card B whose first message has seq=1 — must NOT trigger a gap.
    act(() => { rerender({ cardId: 'CARD-B' }); });
    act(() => { latestES().simulateOpen(); });
    act(() => {
      latestES().simulateMessage({ type: 'text', content: 'b', card_id: 'CARD-B', ts, seq: 1 });
    });

    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].type).toBe('text');
    expect(result.current.logs[0].content).toBe('b');
  });
});
