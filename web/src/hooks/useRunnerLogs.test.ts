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
  it('(b) terminal frame sets connected=false and no reconnect is scheduled', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });
    expect(result.current.connected).toBe(true);

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

  it('onerror after terminal does NOT schedule reconnect', () => {
    const { result } = renderHook(() =>
      useRunnerLogs({ project: 'proj', enabled: true }),
    );

    act(() => { latestES().simulateOpen(); });
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
