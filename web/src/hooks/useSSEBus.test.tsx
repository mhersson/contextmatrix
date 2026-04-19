import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, act } from '@testing-library/react';
import { SSEProvider, useSSEBus } from './useSSEBus';
import type { BoardEvent } from '../types';
import { useEffect } from 'react';

// ---- EventSource mock -------------------------------------------------------

interface MockES {
  url: string;
  onopen: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
  close: () => void;
  _triggerOpen: () => void;
  _triggerMessage: (data: unknown) => void;
  _triggerError: () => void;
  _closed: boolean;
}

let instances: MockES[] = [];

class MockEventSource implements MockES {
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  _closed = false;

  constructor(url: string) {
    this.url = url;
    instances.push(this);
  }

  close() {
    this._closed = true;
  }

  _triggerOpen() {
    this.onopen?.(new Event('open'));
  }

  _triggerMessage(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }));
  }

  _triggerError() {
    this.onerror?.(new Event('error'));
  }
}

// Assign to globalThis so the module under test picks it up
Object.defineProperty(globalThis, 'EventSource', {
  value: MockEventSource,
  writable: true,
  configurable: true,
});

// ---- helpers ----------------------------------------------------------------

function latestInstance(): MockES {
  if (instances.length === 0) throw new Error('No EventSource instances');
  return instances[instances.length - 1];
}

// Simple wrapper that calls useSSEBus and forwards the subscribe function to a
// callback so tests can use it without needing additional wiring.
function TestConsumer({ onMount }: { onMount: (ctx: ReturnType<typeof useSSEBus>) => void }) {
  const ctx = useSSEBus();
  useEffect(() => {
    onMount(ctx);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}

function renderWithProvider(onMount: (ctx: ReturnType<typeof useSSEBus>) => void) {
  return render(
    <SSEProvider>
      <TestConsumer onMount={onMount} />
    </SSEProvider>,
  );
}

// ---- test suite -------------------------------------------------------------

beforeEach(() => {
  instances = [];
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

const sampleEvent: BoardEvent = {
  type: 'card.updated',
  project: 'alpha',
  card_id: 'ALPHA-001',
  timestamp: '2026-01-01T00:00:00Z',
};

// ── 1. Multiple subscribers receive the same event ───────────────────────────

describe('fan-out', () => {
  it('delivers the same event to all active subscribers', () => {
    const received1: BoardEvent[] = [];
    const received2: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe((e) => received1.push(e));
        ctx.subscribe((e) => received2.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
    });

    act(() => {
      latestInstance()._triggerMessage(sampleEvent);
    });

    expect(received1).toHaveLength(1);
    expect(received1[0]).toEqual(sampleEvent);
    expect(received2).toHaveLength(1);
    expect(received2[0]).toEqual(sampleEvent);
  });
});

// ── 2. Unsubscribe removes only that listener ─────────────────────────────────

describe('unsubscribe', () => {
  it('stops delivering events to the unsubscribed listener only', () => {
    const received1: BoardEvent[] = [];
    const received2: BoardEvent[] = [];
    let unsub1!: () => void;

    act(() => {
      renderWithProvider((ctx) => {
        unsub1 = ctx.subscribe((e) => received1.push(e));
        ctx.subscribe((e) => received2.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
      latestInstance()._triggerMessage(sampleEvent);
    });

    // Both received the first event
    expect(received1).toHaveLength(1);
    expect(received2).toHaveLength(1);

    // Unsubscribe only subscriber 1
    act(() => {
      unsub1();
    });

    act(() => {
      latestInstance()._triggerMessage(sampleEvent);
    });

    // subscriber 1 did NOT receive the second event
    expect(received1).toHaveLength(1);
    // subscriber 2 still receives events
    expect(received2).toHaveLength(2);
  });
});

// ── 3. Reconnect backoff ──────────────────────────────────────────────────────

describe('reconnect backoff', () => {
  it('doubles the delay on each error up to 30 s', () => {
    act(() => {
      renderWithProvider(() => {});
    });

    // Initial connection created
    expect(instances).toHaveLength(1);

    // Error → reconnect after 1 s
    act(() => {
      instances[0]._triggerError();
    });
    expect(instances).toHaveLength(1); // reconnect not fired yet

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(2); // reconnected after 1 s

    // Second error → reconnect after 2 s
    act(() => {
      instances[1]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(instances).toHaveLength(3);

    // Third error → reconnect after 4 s
    act(() => {
      instances[2]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(instances).toHaveLength(4);

    // Fourth error → reconnect after 8 s
    act(() => {
      instances[3]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(8000);
    });
    expect(instances).toHaveLength(5);

    // Fifth error → reconnect after 16 s
    act(() => {
      instances[4]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(16000);
    });
    expect(instances).toHaveLength(6);

    // Sixth error → delay would be 32 s but capped at 30 s
    act(() => {
      instances[5]._triggerError();
    });
    // Not fired after 29 s
    act(() => {
      vi.advanceTimersByTime(29000);
    });
    expect(instances).toHaveLength(6);
    // Fires after the remaining 1 s (total 30 s)
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(7);
  });

  it('resets delay to 1 s after a successful reconnect', () => {
    act(() => {
      renderWithProvider(() => {});
    });

    // Trigger one error cycle to advance delay to 2 s
    act(() => {
      instances[0]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(2);

    // Open succeeds → delay resets to 1 s
    act(() => {
      instances[1]._triggerOpen();
    });

    // Next error should reconnect after 1 s again
    act(() => {
      instances[1]._triggerError();
    });
    // Should NOT have reconnected after only 500 ms
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(instances).toHaveLength(2);
    // Should reconnect after the remaining 500 ms (total 1 s)
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(instances).toHaveLength(3);
  });
});
