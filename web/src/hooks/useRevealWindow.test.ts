import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useRevealWindow } from './useRevealWindow';

function makeItems(count: number, offset = 0): number[] {
  return Array.from({ length: count }, (_, i) => i + offset);
}

const TAIL = 50;
const CHUNK = 100;

function renderWindow(initial: readonly number[]) {
  return renderHook(
    ({ items }: { items: readonly number[] }) => useRevealWindow(items, TAIL, CHUNK),
    { initialProps: { items: initial } },
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

  it('caps the window size on growth - appends slide the window, not grow it', () => {
    const { result, rerender } = renderWindow(makeItems(120));
    act(() => result.current.revealMore());
    expect(result.current.hiddenCount).toBe(0);

    rerender({ items: makeItems(125) });

    // Window size stays initialTail + revealed (120); the 5 appended items
    // push the 5 oldest back above the window.
    expect(result.current.visible).toHaveLength(120);
    expect(result.current.visible[119]).toBe(124);
    expect(result.current.hiddenCount).toBe(5);
  });

  it('resets the revealed extent when the list shrinks (clear/filter-off)', () => {
    const { result, rerender } = renderWindow(makeItems(300));
    act(() => result.current.revealMore());
    expect(result.current.visible).toHaveLength(150);

    rerender({ items: makeItems(30, 1000) });
    expect(result.current.visible).toHaveLength(30);
    expect(result.current.hiddenCount).toBe(0);

    // A subsequent big list starts from a fresh tail, not the old extent.
    rerender({ items: makeItems(200, 2000) });
    expect(result.current.visible).toHaveLength(50);
    expect(result.current.hiddenCount).toBe(150);
  });
});
