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
  it('delivers the same event to all active wildcard subscribers', () => {
    const received1: BoardEvent[] = [];
    const received2: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe('*', (e) => received1.push(e));
        ctx.subscribe('*', (e) => received2.push(e));
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

// ── 1b. Pattern-filtered subscribers only receive matching events ────────────

describe('pattern filtering', () => {
  it('does not deliver card.* events to project.* subscribers', () => {
    const cardReceived: BoardEvent[] = [];
    const projectReceived: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe('card.*', (e) => cardReceived.push(e));
        ctx.subscribe('project.*', (e) => projectReceived.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
    });

    act(() => {
      latestInstance()._triggerMessage({
        type: 'card.created',
        project: 'alpha',
        card_id: 'ALPHA-001',
        timestamp: '2026-01-01T00:00:00Z',
      } as BoardEvent);
    });

    expect(cardReceived).toHaveLength(1);
    expect(cardReceived[0].type).toBe('card.created');
    expect(projectReceived).toHaveLength(0);
  });

  it('delivers exact-match subscriptions only for that event type', () => {
    const exactReceived: BoardEvent[] = [];
    const prefixReceived: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe('project.updated', (e) => exactReceived.push(e));
        ctx.subscribe('card.*', (e) => prefixReceived.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
    });

    // project.created should not reach the 'project.updated' exact subscriber
    act(() => {
      latestInstance()._triggerMessage({
        type: 'project.created',
        project: 'alpha',
        card_id: '',
        timestamp: '2026-01-01T00:00:00Z',
      } as BoardEvent);
    });

    expect(exactReceived).toHaveLength(0);
    expect(prefixReceived).toHaveLength(0);

    // project.updated should reach the exact subscriber
    act(() => {
      latestInstance()._triggerMessage({
        type: 'project.updated',
        project: 'alpha',
        card_id: '',
        timestamp: '2026-01-01T00:00:00Z',
      } as BoardEvent);
    });

    expect(exactReceived).toHaveLength(1);
    expect(prefixReceived).toHaveLength(0);
  });

  it('does not collide an exact-match "card" subscription with the "card.*" prefix bucket', () => {
    // Regression: bucket keys were previously the raw prefix string ('card'),
    // so a literal exact subscription to 'card' (no dot) would share its
    // bucket with the 'card.*' prefix subscription and receive every card.X
    // event. Sigil-prefixed buckets ('p:card' vs 'e:card') keep them apart.
    const exactReceived: BoardEvent[] = [];
    const prefixReceived: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe('card', (e) => exactReceived.push(e));
        ctx.subscribe('card.*', (e) => prefixReceived.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
    });

    // 'card.created' should reach the prefix subscriber only, not the exact
    // subscriber (which is waiting for the literal type 'card').
    act(() => {
      latestInstance()._triggerMessage({
        type: 'card.created',
        project: 'alpha',
        card_id: 'ALPHA-001',
        timestamp: '2026-01-01T00:00:00Z',
      } as BoardEvent);
    });

    expect(exactReceived).toHaveLength(0);
    expect(prefixReceived).toHaveLength(1);

    // A literal type 'card' (no dot) should reach the exact subscriber only;
    // the prefix subscriber is filtering on 'card.*' and never matches a bare
    // type with no suffix. The cast goes through `unknown` because 'card' is
    // deliberately not in the EventType union — this test exists to prove
    // that even a hypothetical future event with such a shape would not
    // cross-fire the 'card.*' bucket.
    act(() => {
      latestInstance()._triggerMessage({
        type: 'card',
        project: 'alpha',
        card_id: '',
        timestamp: '2026-01-01T00:00:00Z',
      } as unknown as BoardEvent);
    });

    expect(exactReceived).toHaveLength(1);
    expect(prefixReceived).toHaveLength(1); // unchanged from previous assertion
  });

  it('delivers events to both wildcard and matching prefix subscribers', () => {
    const wild: BoardEvent[] = [];
    const card: BoardEvent[] = [];
    const runner: BoardEvent[] = [];

    act(() => {
      renderWithProvider((ctx) => {
        ctx.subscribe('*', (e) => wild.push(e));
        ctx.subscribe('card.*', (e) => card.push(e));
        ctx.subscribe('runner.*', (e) => runner.push(e));
      });
    });

    act(() => {
      latestInstance()._triggerOpen();
    });

    act(() => {
      latestInstance()._triggerMessage({
        type: 'runner.started',
        project: 'alpha',
        card_id: 'ALPHA-001',
        timestamp: '2026-01-01T00:00:00Z',
      } as BoardEvent);
    });

    expect(wild).toHaveLength(1);
    expect(card).toHaveLength(0);
    expect(runner).toHaveLength(1);
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
        unsub1 = ctx.subscribe('*', (e) => received1.push(e));
        ctx.subscribe('*', (e) => received2.push(e));
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

// ── 2b. Provider value is stable: consumers do not re-render on every event ──

describe('provider value memoization', () => {
  it('does not re-render useSSEBus consumers when an SSE event arrives', () => {
    const counter = { n: 0 };

    function Consumer({ onRender }: { onRender: () => void }) {
      useSSEBus();
      // Side effect in render is the point of this test — we want to count
      // every render of this consumer. Intentional bypass of the usual lint
      // rule, applied narrowly.
      onRender();
      return null;
    }

    const onRender = () => {
      counter.n += 1;
    };

    act(() => {
      render(
        <SSEProvider>
          <Consumer onRender={onRender} />
        </SSEProvider>,
      );
    });

    // Initial render + onopen flips `connected` → one extra render.
    act(() => {
      latestInstance()._triggerOpen();
    });
    const baseline = counter.n;

    // Firing several SSE messages must not re-render the consumer because the
    // provider value (subscribe / connected / error) does not change.
    act(() => {
      latestInstance()._triggerMessage(sampleEvent);
      latestInstance()._triggerMessage(sampleEvent);
      latestInstance()._triggerMessage(sampleEvent);
    });

    expect(counter.n).toBe(baseline);
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

  it('resets delay to 1 s after a successful reconnect (open + first message)', () => {
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

    // Open succeeds AND first message arrives → delay resets to 1 s.
    // A bare onopen is no longer enough — accept-then-close upstreams
    // would tight-loop reconnect if it were.
    act(() => {
      instances[1]._triggerOpen();
      instances[1]._triggerMessage(sampleEvent);
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

  it('keeps escalating backoff when server accepts then closes without sending', () => {
    act(() => {
      renderWithProvider(() => {});
    });
    expect(instances).toHaveLength(1);

    // Cycle 1: server opens then closes immediately. delay starts at
    // 1 s — onopen alone must NOT reset it, otherwise an
    // accept-then-close upstream tight-loops reconnect.
    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(2); // first reconnect after 1 s
    // Backoff is now 2 s.

    // Cycle 2: same accept-then-close pattern. Without the onopen
    // reset bug the backoff would have rolled back to 1 s and the
    // next reconnect would fire after 1 s.
    act(() => {
      instances[1]._triggerOpen();
      instances[1]._triggerError();
    });
    // 1 s is NOT enough; the next reconnect waits the full 2 s.
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(2);
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(instances).toHaveLength(3);
  });
});
