import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { HoverPanel } from './HoverPanel';

function renderBelow(rect: Partial<DOMRect>, viewport: { width: number; height: number }, panelHeight = 100) {
  const anchor = document.createElement('button');
  anchor.getBoundingClientRect = () =>
    ({ top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}), ...rect }) as DOMRect;
  vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(viewport.width);
  vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(viewport.height);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(panelHeight);
  render(<HoverPanel id="tip" anchorRef={{ current: anchor }} width={280} placement="below">content</HoverPanel>);
  return screen.getByRole('tooltip');
}

describe('HoverPanel below its trigger', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('sits under the trigger, left-aligned', () => {
    const tip = renderBelow({ top: 100, bottom: 120, left: 300 }, { width: 1600, height: 900 });
    expect(tip).toHaveClass('card-hover');
    expect(tip.style.top).toBe('128px');
    expect(tip.style.left).toBe('300px');
  });

  it('flips above when there is no room below and stays inside the right edge', () => {
    const tip = renderBelow({ top: 820, bottom: 840, left: 1500 }, { width: 1600, height: 900 }, 100);
    // top - gap - height: 820 - 8 - 100; innerWidth - width - gap: 1600 - 280 - 8
    expect(tip.style.top).toBe('712px');
    expect(tip.style.left).toBe('1312px');
  });
});
