import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatTranscript } from './ChatTranscript';
import type { LogEntry } from '../../types';

/**
 * jsdom has no layout, so scroll geometry is stubbed (same approach as
 * VirtualLogList.test.tsx): a 600px viewport over 1000px of content, a
 * controllable ResizeObserver, and getBoundingClientRect overrides where a
 * test needs anchor math.
 */

const roInstances: MockResizeObserver[] = [];

class MockResizeObserver {
  callback: ResizeObserverCallback;
  constructor(cb: ResizeObserverCallback) {
    this.callback = cb;
    roInstances.push(this);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
  trigger() {
    this.callback([], this);
  }
}

beforeEach(() => {
  roInstances.length = 0;
  (globalThis as unknown as { ResizeObserver: typeof MockResizeObserver }).ResizeObserver =
    MockResizeObserver;

  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return 600;
    },
  });
  Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
    configurable: true,
    get() {
      return 1000;
    },
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function makeEntries(count: number): LogEntry[] {
  return Array.from({ length: count }, (_, i) => ({
    ts: '2026-07-25T10:00:00Z',
    card_id: 'TEST-001',
    type: 'user' as const,
    content: `entry ${i}`,
    seq: i + 1,
  }));
}

function renderedRows(container: HTMLElement): Element[] {
  return Array.from(container.querySelectorAll('[data-logkey]'));
}

function scroller(container: HTMLElement): HTMLElement {
  const el = container.querySelector('[role="log"]');
  if (!(el instanceof HTMLElement)) throw new Error('scroller not found');
  return el;
}

describe('ChatTranscript tail window', () => {
  it('renders only the newest 50 entries with a show-older affordance', () => {
    const { container } = render(<ChatTranscript filteredLogs={makeEntries(120)} />);

    expect(renderedRows(container)).toHaveLength(50);
    expect(screen.getByText('entry 119')).toBeInTheDocument();
    expect(screen.queryByText('entry 69')).not.toBeInTheDocument();
    expect(screen.getByTestId('chat-show-older')).toHaveTextContent(
      'Show older messages (70 hidden)',
    );
  });

  it('reveals the full list on click and removes the affordance', () => {
    const { container } = render(<ChatTranscript filteredLogs={makeEntries(120)} />);

    fireEvent.click(screen.getByTestId('chat-show-older'));

    expect(renderedRows(container)).toHaveLength(120);
    expect(screen.getByText('entry 0')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-show-older')).not.toBeInTheDocument();
  });

  it('renders small lists in full with no affordance', () => {
    const { container } = render(<ChatTranscript filteredLogs={makeEntries(10)} />);

    expect(renderedRows(container)).toHaveLength(10);
    expect(screen.queryByTestId('chat-show-older')).not.toBeInTheDocument();
  });

  it('shows the empty state without logs or working indicator', () => {
    render(<ChatTranscript filteredLogs={[]} />);
    expect(screen.getByText('No messages yet.')).toBeInTheDocument();
  });
});

describe('ChatTranscript scrolling', () => {
  it('pins to the bottom on mount and on append while at the bottom', () => {
    const entries = makeEntries(10);
    const { container, rerender } = render(<ChatTranscript filteredLogs={entries} />);
    const el = scroller(container);
    expect(el.scrollTop).toBe(1000);

    el.scrollTop = 123;
    rerender(<ChatTranscript filteredLogs={[...entries, ...makeEntries(1)]} />);
    expect(el.scrollTop).toBe(1000);
  });

  it('does not yank to the bottom when the user has scrolled up', () => {
    const entries = makeEntries(10);
    const { container, rerender } = render(<ChatTranscript filteredLogs={entries} />);
    const el = scroller(container);

    // 1000 - 100 - 600 = 300 > threshold - user is reading history.
    el.scrollTop = 100;
    fireEvent.scroll(el);

    rerender(<ChatTranscript filteredLogs={[...entries, ...makeEntries(1)]} />);
    expect(el.scrollTop).toBe(100);
  });

  it('restores the anchor row position after revealing older messages', () => {
    const { container } = render(<ChatTranscript filteredLogs={makeEntries(120)} />);
    const el = scroller(container);
    expect(el.scrollTop).toBe(1000);

    // First rect read (trigger): anchor row at top 100. Post-commit read:
    // the same row sits at 500 - the viewport must shift down by the delta.
    const rectSpy = vi.spyOn(Element.prototype, 'getBoundingClientRect');
    rectSpy.mockReturnValueOnce({ top: 100 } as DOMRect);
    rectSpy.mockReturnValue({ top: 500 } as DOMRect);

    fireEvent.click(screen.getByTestId('chat-show-older'));

    expect(el.scrollTop).toBe(1000 + 400);
  });

  it('treats a reveal as reading history - later appends do not pin', () => {
    const entries = makeEntries(120);
    const { container, rerender } = render(<ChatTranscript filteredLogs={entries} />);
    const el = scroller(container);

    const rectSpy = vi.spyOn(Element.prototype, 'getBoundingClientRect');
    rectSpy.mockReturnValue({ top: 0 } as DOMRect);
    fireEvent.click(screen.getByTestId('chat-show-older'));

    el.scrollTop = 42;
    rerender(<ChatTranscript filteredLogs={[...entries, ...makeEntries(1)]} />);
    expect(el.scrollTop).toBe(42);
  });

  it('auto-reveals a chunk when scrolled near the top', () => {
    render(<ChatTranscript filteredLogs={makeEntries(300)} />);
    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('250 hidden');

    const el = scroller(document.body);
    el.scrollTop = 0;
    fireEvent.scroll(el);

    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('150 hidden');
  });

  it('re-pins to the bottom when content height changes while at the bottom', () => {
    const { container } = render(<ChatTranscript filteredLogs={makeEntries(10)} />);
    const el = scroller(container);
    el.scrollTop = 0;

    roInstances.forEach((ro) => ro.trigger());

    expect(el.scrollTop).toBe(1000);
  });
});
