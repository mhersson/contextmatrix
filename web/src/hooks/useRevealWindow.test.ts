import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useRevealWindow } from './useRevealWindow';

function makeItems(count: number, offset = 0): number[] {
  return Array.from({ length: count }, (_, i) => i + offset);
}

const TAIL = 50;
const CHUNK = 100;

function renderWindow(initial: readonly number[], holdTop = false, expectTop = false) {
  return renderHook(
    ({ items, hold, expect: exp }: { items: readonly number[]; hold: boolean; expect: boolean }) =>
      useRevealWindow(items, TAIL, CHUNK, hold, exp),
    { initialProps: { items: initial, hold: holdTop, expect: expectTop } },
  );
}

describe('useRevealWindow', () => {
  it('shows the newest initialTail items and reports the rest hidden', () => {
    const items = makeItems(120);
    const { result } = renderWindow(items);

    expect(result.current.visible).toHaveLength(50);
    expect(result.current.visible[0]).toBe(70);
    expect(result.current.visible[49]).toBe(119);
    expect(result.current.hiddenCount).toBe(70);
  });

  it('returns the whole list when it fits in the tail', () => {
    const items = makeItems(20);
    const { result } = renderWindow(items);

    expect(result.current.visible).toBe(items);
    expect(result.current.hiddenCount).toBe(0);
  });

  it('revealMore widens by a chunk and clamps at the full list', () => {
    const { result } = renderWindow(makeItems(120));

    act(() => result.current.revealMore());
    // chunk 100 > 70 hidden - clamps to everything.
    expect(result.current.visible).toHaveLength(120);
    expect(result.current.hiddenCount).toBe(0);

    act(() => result.current.revealMore());
    expect(result.current.visible).toHaveLength(120);
    expect(result.current.hiddenCount).toBe(0);
  });

  it('reveals in steps when more than one chunk is hidden', () => {
    const { result } = renderWindow(makeItems(300));
    expect(result.current.hiddenCount).toBe(250);

    act(() => result.current.revealMore());
    expect(result.current.visible).toHaveLength(150);
    expect(result.current.hiddenCount).toBe(150);

    act(() => result.current.revealMore());
    expect(result.current.hiddenCount).toBe(50);
  });

  it('slides the window on growth while at the bottom (holdTop false)', () => {
    const { result, rerender } = renderWindow(makeItems(120));
    act(() => result.current.revealMore());
    expect(result.current.hiddenCount).toBe(0);

    rerender({ items: makeItems(125), hold: false, expect: false });

    // Window size stays initialTail + revealed (120); the 5 appended items
    // push the 5 oldest back above the window.
    expect(result.current.visible).toHaveLength(120);
    expect(result.current.visible[119]).toBe(124);
    expect(result.current.hiddenCount).toBe(5);
  });

  it('holds the window start on growth while reading history (holdTop true)', () => {
    const { result, rerender } = renderWindow(makeItems(120));
    expect(result.current.hiddenCount).toBe(70);

    rerender({ items: makeItems(120), hold: true, expect: false });
    rerender({ items: makeItems(130), hold: true, expect: false });

    // Start pinned: hidden count unchanged, appended rows extend the bottom.
    expect(result.current.hiddenCount).toBe(70);
    expect(result.current.visible).toHaveLength(60);
    expect(result.current.visible[0]).toBe(70);
    expect(result.current.visible[59]).toBe(129);

    // Back at the bottom, later appends slide again from the widened window.
    rerender({ items: makeItems(130), hold: false, expect: false });
    rerender({ items: makeItems(140), hold: false, expect: false });
    expect(result.current.hiddenCount).toBe(80);
    expect(result.current.visible).toHaveLength(60);
  });

  it('resets the revealed extent when the list shrinks (clear/filter-off)', () => {
    const { result, rerender } = renderWindow(makeItems(300));
    act(() => result.current.revealMore());
    expect(result.current.visible).toHaveLength(150);

    rerender({ items: makeItems(30, 1000), hold: false, expect: false });
    expect(result.current.visible).toHaveLength(30);
    expect(result.current.hiddenCount).toBe(0);

    // A cleared-then-refilled stream (length passes through 0, as clear()
    // always produces) starts from a fresh tail, not the old extent.
    rerender({ items: [], hold: false, expect: false });
    rerender({ items: makeItems(200, 2000), hold: false, expect: false });
    expect(result.current.visible).toHaveLength(50);
    expect(result.current.hiddenCount).toBe(150);
  });

  it('reveals expected top-growth (history-page prepend) even while at the bottom', () => {
    const items = makeItems(40, 100);
    const { result, rerender } = renderWindow(items);
    expect(result.current.hiddenCount).toBe(0);

    // A fetched history page prepends 20 older items; holdTop is false
    // (user at the bottom clicked Load earlier) but the caller flagged a
    // fetch in flight. The page must be visible, not behind the fold.
    rerender({ items: [...makeItems(20, 0), ...items], hold: false, expect: true });

    expect(result.current.visible).toHaveLength(60);
    expect(result.current.visible[0]).toBe(0);
    expect(result.current.hiddenCount).toBe(0);
  });

  it('does NOT widen on unexpected top-growth (filter toggle re-including old rows)', () => {
    const { result, rerender } = renderWindow(makeItems(300, 1000));
    expect(result.current.hiddenCount).toBe(250);

    // Toggling a filter back on regrows the list with a different first
    // element - same signature as a prepend, but no fetch is in flight.
    // The window must slide (stay capped), not mount the whole backlog.
    rerender({
      items: [...makeItems(100, 0), ...makeItems(300, 1000)],
      hold: false,
      expect: false,
    });

    expect(result.current.visible).toHaveLength(50);
    expect(result.current.hiddenCount).toBe(350);
  });

  it('refreshes the baseline on identity shifts at constant length (ring at capacity)', () => {
    // A: 60 items. B: ring-full append - one evicted at the head, one added
    // at the tail, LENGTH UNCHANGED but items[0] identity changed.
    const a = makeItems(60);
    const { result, rerender } = renderWindow(a, false, true);
    expect(result.current.hiddenCount).toBe(10);

    const b = [...makeItems(59, 1), 100];
    rerender({ items: b, hold: false, expect: true });
    expect(result.current.hiddenCount).toBe(10);

    // C: genuine bottom growth whose first element matches B's. With a
    // stale (length-gated) baseline this would misclassify as top-growth
    // and widen; the refreshed baseline keeps the window sliding.
    const c = [...b, 101, 102, 103, 104, 105];
    rerender({ items: c, hold: false, expect: true });

    expect(result.current.hiddenCount).toBe(15);
    expect(result.current.visible).toHaveLength(50);
  });
});
