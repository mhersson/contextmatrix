import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { VirtualLogList } from './VirtualLogList';
import type { LogEntry } from '../../types';

/**
 * jsdom does not implement layout, so ResizeObserver / getBoundingClientRect
 * return zero. We stub both to provide a deterministic viewport + row height
 * and verify the component only renders a windowed subset of the input.
 */

class MockResizeObserver {
  callback: ResizeObserverCallback;
  targets: Set<Element> = new Set();
  constructor(cb: ResizeObserverCallback) {
    this.callback = cb;
  }
  observe(target: Element) {
    this.targets.add(target);
  }
  unobserve(target: Element) {
    this.targets.delete(target);
  }
  disconnect() {
    this.targets.clear();
  }
}

beforeEach(() => {
  (globalThis as unknown as { ResizeObserver: typeof MockResizeObserver }).ResizeObserver = MockResizeObserver;

  // Force a deterministic viewport height so clientHeight reads as 600px.
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return 600;
    },
  });

  // Row element getBoundingClientRect returns a fixed row height so the
  // virtualization decisions become deterministic.
  const originalGetBCR = Element.prototype.getBoundingClientRect;
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    // Row containers get a fixed height; other elements fall through.
    if (this instanceof HTMLElement && (this as HTMLElement).dataset.testid === 'log-line') {
      return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 24, width: 100, height: 24, toJSON: () => ({}) } as DOMRect;
    }
    return originalGetBCR.call(this);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function makeEntries(count: number): LogEntry[] {
  const out: LogEntry[] = [];
  for (let i = 0; i < count; i++) {
    out.push({
      ts: `2026-01-01T00:00:${String(i % 60).padStart(2, '0')}.000Z`,
      card_id: 'TEST-001',
      type: 'text',
      content: `entry ${i}`,
      seq: i,
    });
  }
  return out;
}

describe('VirtualLogList', () => {
  it('renders substantially fewer rows than items with 5000 entries', () => {
    const items = makeEntries(5000);
    render(<VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />);

    const rendered = screen.queryAllByTestId('log-line');
    // Viewport is 600px, ESTIMATE_ROW_HEIGHT is 24px → ~25 rows + 2 × 10 buffer.
    // Auto-scroll to bottom kicks in layout effect. Either way, count must be
    // vastly below 5000.
    expect(rendered.length).toBeLessThan(100);
    expect(rendered.length).toBeGreaterThan(0);
  });

  it('renders all items when the list is small', () => {
    const items = makeEntries(5);
    render(<VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />);

    const rendered = screen.queryAllByTestId('log-line');
    expect(rendered.length).toBe(5);
  });

  it('shows empty state when items is empty', () => {
    render(
      <VirtualLogList
        items={[]}
        getKey={(_, idx) => `k-${idx}`}
        emptyState={<div data-testid="empty">nothing</div>}
      />,
    );
    expect(screen.getByTestId('empty')).toBeInTheDocument();
  });
});
