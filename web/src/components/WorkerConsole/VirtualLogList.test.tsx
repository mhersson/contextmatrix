import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { VirtualLogList } from './VirtualLogList';
import type { LogEntry } from '../../types';

/**
 * jsdom does not implement layout, so ResizeObserver / getBoundingClientRect
 * return zero. We stub both to provide a deterministic viewport + row height
 * and verify the component only renders a windowed subset of the input.
 *
 * Height measurement path:
 * rowRef returns early on the first render because the ResizeObserver useEffect
 * hasn't run yet (rowObserverRef.current = null). After the useEffect runs and
 * sets rowObserverRef.current, we need rowRef to fire again. Since rowRef(i)
 * returns a NEW closure on each render, React calls the old ref (null) and new
 * ref (el) on every re-render. We force an extra render by changing the items
 * array reference via rerender(). On that second render, rowRef fires with
 * rowObserverRef.current set, calls ro.observe(el), and MockResizeObserver
 * immediately fires the callback with the configured mockRowHeight, causing
 * heightStore.setHeight() to be called and triggering a further re-render
 * where offsets recomputes.
 */

// Shared mock row height — tests that need 40px override this before rendering.
let mockRowHeight = 24;

class MockResizeObserver {
  callback: ResizeObserverCallback;
  targets: Set<Element> = new Set();
  constructor(cb: ResizeObserverCallback) {
    this.callback = cb;
  }
  observe(target: Element) {
    if (this.targets.has(target)) return;
    this.targets.add(target);
    // Fire immediately so height measurements land synchronously within act().
    this.callback(
      [
        {
          target,
          contentRect: {
            height: mockRowHeight,
            width: 100,
            top: 0,
            left: 0,
            right: 100,
            bottom: mockRowHeight,
            x: 0,
            y: 0,
            toJSON() {
              return {};
            },
          } as DOMRectReadOnly,
          borderBoxSize: [],
          contentBoxSize: [],
          devicePixelContentBoxSize: [],
        } as ResizeObserverEntry,
      ],
      this,
    );
  }
  unobserve(target: Element) {
    this.targets.delete(target);
  }
  disconnect() {
    this.targets.clear();
  }
}

beforeEach(() => {
  mockRowHeight = 24;
  (globalThis as unknown as { ResizeObserver: typeof MockResizeObserver }).ResizeObserver = MockResizeObserver;

  // Force a deterministic viewport height so clientHeight reads as 600px.
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return 600;
    },
  });

  // Row element getBoundingClientRect returns a fixed row height so the
  // virtualization decisions become deterministic. The mockRowHeight variable
  // lets individual tests override the height without re-spying.
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    // Row containers get a fixed height; other elements fall through.
    if (this instanceof HTMLElement && (this as HTMLElement).dataset.testid === 'log-line') {
      return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: mockRowHeight, width: 100, height: mockRowHeight, toJSON: () => ({}) } as DOMRect;
    }
    return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON: () => ({}) } as DOMRect;
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

  // R1: No row overlap after row heights are measured.
  // With 50 items and measured row height of 40px (> ESTIMATE_ROW_HEIGHT=24px),
  // each row's top offset must be >= previous top + 40px. Before the fix,
  // offsets use the 24px estimate even after heights are measured, so adjacent
  // rows overlap (top delta = 24 < measured height 40).
  it('R1: rows do not overlap after measured heights are applied', async () => {
    // Use 40px row height — strictly greater than ESTIMATE_ROW_HEIGHT=24 so the
    // estimate-based and measured-based paths produce different offsets.
    mockRowHeight = 40;

    const items = makeEntries(50);
    const { rerender } = render(
      <VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />,
    );

    // Force a second render so rowRef fires after useEffect has set
    // rowObserverRef.current. On the second render, rowRef's closure calls
    // ro.observe(el) which triggers the MockResizeObserver callback immediately,
    // storing 40px heights in the heightStore. This triggers a third render
    // where offsets recomputes using the measured heights.
    await act(async () => {
      rerender(<VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />);
    });

    // Wait for offsets to reflect measured heights.
    await waitFor(() => {
      const rows = screen.queryAllByTestId('log-line');
      expect(rows.length).toBeGreaterThan(1);
      const secondTop = parseFloat((rows[1] as HTMLElement).style.top);
      // After the fix: 40px. Before the fix: 24px.
      expect(secondTop).toBeGreaterThanOrEqual(40);
    });

    // Verify the full set of rendered rows — each adjacent pair must not overlap.
    const rows = screen.queryAllByTestId('log-line');
    for (let i = 1; i < rows.length; i++) {
      const prev = rows[i - 1] as HTMLElement;
      const curr = rows[i] as HTMLElement;
      const prevTop = parseFloat(prev.style.top);
      const currTop = parseFloat(curr.style.top);
      expect(currTop).toBeGreaterThanOrEqual(prevTop + mockRowHeight);
    }
  });

  // R2: Viewport scrolls to the true end of the log (based on measured heights),
  // not the estimate-based bottom. With 10 rows × 40px measured height and a
  // 600px viewport (10×40=400 < 600, so all rows fit), totalHeight should be
  // 400px after measurement. Before the fix, totalHeight stays at 10×24=240
  // even after row measurements land.
  //
  // We use 10 items so all fit in the 600px viewport and all get their heights
  // measured in a single rerender cycle. With 50 items only a windowed subset
  // would be visible and measured.
  //
  // jsdom does not run layout, so scrollHeight is 0. We mock it on the viewport
  // element to return the inner content div's totalHeight (from data-total-height),
  // mirroring real browser behaviour so the auto-scroll layout effect can set a
  // non-zero scrollTop.
  it('R2: auto-scroll lands at the true end after measured heights are applied', async () => {
    mockRowHeight = 40;
    const ITEM_COUNT = 10; // 10 × 40 = 400px < 600px viewport → all rows visible

    // Mock scrollHeight to return the inner content div's declared height.
    // jsdom sets scrollHeight to 0 (no layout). This mock lets the auto-scroll
    // layout effect (el.scrollTop = el.scrollHeight) produce a verifiable value.
    Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
      configurable: true,
      get(this: HTMLElement) {
        const firstChild = this.firstElementChild as HTMLElement | null;
        if (firstChild?.dataset.totalHeight) {
          return parseFloat(firstChild.dataset.totalHeight);
        }
        return 0;
      },
    });

    const items = makeEntries(ITEM_COUNT);
    const { rerender } = render(
      <VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />,
    );

    // Force a second render so rowRef fires with rowObserverRef.current set,
    // triggering height measurements via MockResizeObserver.
    await act(async () => {
      rerender(<VirtualLogList items={items} getKey={(_, idx) => `k-${idx}`} />);
    });

    // Wait for totalHeight to reflect measured heights (400 = 10 × 40).
    // Before the fix it stays at 240 (10 × 24). After the fix it updates to 400.
    await waitFor(() => {
      const el = document.querySelector('[data-total-height]') as HTMLElement;
      expect(el).not.toBeNull();
      expect(parseFloat(el.dataset.totalHeight!)).toBe(ITEM_COUNT * mockRowHeight);
    });

    const contentDiv = document.querySelector('[data-total-height]') as HTMLElement;
    const totalHeight = parseFloat(contentDiv.dataset.totalHeight!);
    expect(totalHeight).toBe(400); // 10 × 40

    // The auto-scroll layout effect (el.scrollTop = el.scrollHeight) fires after
    // every totalHeight change. With scrollHeight mocked to return totalHeight,
    // scrollTop should equal 400 after the fix (versus 240 before the fix).
    const viewport = screen.getByTestId('log-viewport');
    expect(viewport.scrollTop).toBe(totalHeight);
  });
});
