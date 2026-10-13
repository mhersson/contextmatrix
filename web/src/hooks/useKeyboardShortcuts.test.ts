import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useKeyboardShortcuts } from './useKeyboardShortcuts';

function press(key: string, { prevent = false } = {}): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
  if (prevent) e.preventDefault();
  document.body.dispatchEvent(e);
  return e;
}

describe('useKeyboardShortcuts', () => {
  it('runs the handler and consumes the event', () => {
    const handler = vi.fn();
    renderHook(() => useKeyboardShortcuts([{ key: 'Escape', handler }]));

    const e = press('Escape');

    expect(handler).toHaveBeenCalledTimes(1);
    expect(e.defaultPrevented).toBe(true);
  });

  it('skips an event another handler already consumed', () => {
    const handler = vi.fn();
    renderHook(() => useKeyboardShortcuts([{ key: 'Escape', handler }]));

    press('Escape', { prevent: true });

    expect(handler).not.toHaveBeenCalled();
  });

  it('leaves the event unconsumed when the handler declines', () => {
    const handler = vi.fn(() => false);
    renderHook(() => useKeyboardShortcuts([{ key: 'Escape', handler }]));

    const e = press('Escape');

    expect(handler).toHaveBeenCalledTimes(1);
    expect(e.defaultPrevented).toBe(false);
  });
});
