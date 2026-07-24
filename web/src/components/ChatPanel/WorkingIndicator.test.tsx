import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { WorkingIndicator, formatElapsed } from './WorkingIndicator';

describe('formatElapsed', () => {
  it('renders sub-minute values as bare seconds', () => {
    expect(formatElapsed(0)).toBe('0s');
    expect(formatElapsed(40)).toBe('40s');
    expect(formatElapsed(59)).toBe('59s');
  });

  it('renders minute values with zero-padded seconds', () => {
    expect(formatElapsed(60)).toBe('1m 00s');
    expect(formatElapsed(65)).toBe('1m 05s');
    expect(formatElapsed(600)).toBe('10m 00s');
  });
});

describe('WorkingIndicator', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-24T10:00:40Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the verb and elapsed time from since', () => {
    render(<WorkingIndicator verb="Beboppin'" since={Date.parse('2026-07-24T10:00:00Z')} />);
    const el = screen.getByTestId('working-indicator');
    expect(el.textContent).toContain("Beboppin'");
    expect(el.textContent).toContain('(40s)');
  });

  it('ticks the timer every second', () => {
    render(<WorkingIndicator verb="Noodling" since={Date.parse('2026-07-24T10:00:00Z')} />);
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.getByTestId('working-indicator').textContent).toContain('(1m 10s)');
  });

  it('announces a static working status without exposing the ticking text', () => {
    render(<WorkingIndicator verb="Brewing" since={Date.now()} />);
    expect(screen.getByRole('status').textContent).toBe('Assistant is working');
    const el = screen.getByTestId('working-indicator');
    expect(el.querySelector('[aria-hidden="true"]')?.textContent).toContain('Brewing');
  });
});
