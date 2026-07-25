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
  const el = container.querySelector('[data-testid="chat-scroller"]');
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

  it('re-pin observer works when the transcript mounted empty', () => {
    // Regression: the observed wrapper hosts the empty state too, so an
    // empty first commit must still end up with a working observer.
    const { container, rerender } = render(<ChatTranscript filteredLogs={[]} />);
    expect(screen.getByText('No messages yet.')).toBeInTheDocument();

    rerender(<ChatTranscript filteredLogs={makeEntries(5)} />);
    const el = scroller(container);
    el.scrollTop = 0;

    roInstances.forEach((ro) => ro.trigger());

    expect(el.scrollTop).toBe(1000);
  });

  it('holds the window top on live appends while the user reads history', () => {
    const entries = makeEntries(120);
    const { container, rerender } = render(<ChatTranscript filteredLogs={entries} />);
    const el = scroller(container);
    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('70 hidden');

    // Scroll up into history (well below the auto-reveal band).
    el.scrollTop = 300;
    fireEvent.scroll(el);

    rerender(<ChatTranscript filteredLogs={[...entries, ...makeEntries(10)]} />);

    // Window start held: hidden count unchanged, appended rows visible.
    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('70 hidden');
    expect(renderedRows(container)).toHaveLength(60);
  });

  it('slides the window on appends while pinned at the bottom', () => {
    const entries = makeEntries(120);
    const { container, rerender } = render(<ChatTranscript filteredLogs={entries} />);

    rerender(<ChatTranscript filteredLogs={[...entries, ...makeEntries(10)]} />);

    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('80 hidden');
    expect(renderedRows(container)).toHaveLength(50);
  });

  it('offers Load earlier when persisted history remains and no rows are hidden', () => {
    const onLoadOlder = vi.fn();
    render(
      <ChatTranscript
        filteredLogs={makeEntries(10)}
        hasMoreHistory
        onLoadOlder={onLoadOlder}
      />,
    );

    const button = screen.getByTestId('chat-show-older');
    expect(button).toHaveTextContent('Load earlier messages');

    fireEvent.click(button);
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it('prefers revealing in-memory rows before fetching persisted history', () => {
    const onLoadOlder = vi.fn();
    const { container } = render(
      <ChatTranscript
        filteredLogs={makeEntries(120)}
        hasMoreHistory
        onLoadOlder={onLoadOlder}
      />,
    );

    fireEvent.click(screen.getByTestId('chat-show-older'));

    expect(onLoadOlder).not.toHaveBeenCalled();
    expect(renderedRows(container)).toHaveLength(120);
    // All in-memory rows revealed - the button now offers the fetch path.
    expect(screen.getByTestId('chat-show-older')).toHaveTextContent('Load earlier messages');
  });

  it('disables the affordance while a history page is loading', () => {
    const onLoadOlder = vi.fn();
    render(
      <ChatTranscript
        filteredLogs={makeEntries(10)}
        hasMoreHistory
        loadingOlder
        onLoadOlder={onLoadOlder}
      />,
    );

    const button = screen.getByTestId('chat-show-older');
    expect(button).toBeDisabled();
    expect(button).toHaveTextContent('Loading earlier messages…');

    fireEvent.click(button);
    expect(onLoadOlder).not.toHaveBeenCalled();
  });

  it('restores the anchor position when a fetched history page prepends', () => {
    const onLoadOlder = vi.fn();
    const entries = makeEntries(10);
    const { container, rerender } = render(
      <ChatTranscript filteredLogs={entries} hasMoreHistory onLoadOlder={onLoadOlder} />,
    );
    const el = scroller(container);
    expect(el.scrollTop).toBe(1000);

    const rectSpy = vi.spyOn(Element.prototype, 'getBoundingClientRect');
    rectSpy.mockReturnValueOnce({ top: 100 } as DOMRect); // arm: first row top
    rectSpy.mockReturnValue({ top: 500 } as DOMRect); // post-prepend position

    fireEvent.click(screen.getByTestId('chat-show-older'));
    expect(onLoadOlder).toHaveBeenCalledTimes(1);

    // The page lands asynchronously as a prepend (older seqs before the
    // existing rows). The anchor must absorb the height delta.
    const older = Array.from({ length: 5 }, (_, i) => ({
      ts: '2026-07-25T09:59:00Z',
      card_id: 'TEST-001',
      type: 'user' as const,
      content: `older ${i}`,
      seq: -5 + i, // sorts before seq 1..10 in list order
    }));
    rerender(
      <ChatTranscript
        filteredLogs={[...older, ...entries]}
        hasMoreHistory
        onLoadOlder={onLoadOlder}
      />,
    );

    expect(el.scrollTop).toBe(1000 + 400);
  });

  it('renders unique row keys for seq-less (worker-shaped) entries', () => {
    // Worker-log frames may arrive without a usable seq; keys must fall back
    // to ts+content instead of collapsing onto one duplicate key.
    const entries: LogEntry[] = Array.from({ length: 10 }, (_, i) => ({
      ts: `2026-07-25T10:00:${String(i).padStart(2, '0')}Z`,
      card_id: 'TEST-001',
      type: 'user' as const,
      content: `entry ${i}`,
    }));
    const { container } = render(<ChatTranscript filteredLogs={entries} />);

    const keys = renderedRows(container).map((r) => r.getAttribute('data-logkey'));
    expect(new Set(keys).size).toBe(10);
  });
});
