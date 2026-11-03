import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { SelectorCandidate, SelectorLadders } from '../../types';
import { CHART_LAYOUT } from './chart';
import { LadderPanel, type LadderPanelProps } from './LadderPanel';

const DEFAULTS = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function cand(slug: string, coder: number, reviewer: number, perMillion = 2): SelectorCandidate {
  const blended = perMillion / 1e6;
  return { slug, creator: slug.split('/')[0], coder_prior: coder, reviewer_prior: reviewer, prompt_price_per_tok: blended / 2, completion_price_per_tok: blended / 2, context_window: 200000, price_source: 'gateway', scored_from: '', prior_source: 'aa' };
}

const CANDIDATES = [cand('a/top', 0.95, 0.95, 2), cand('a/mid', 0.85, 0.85, 4), cand('b/low', 0.7, 0.7, 8), cand('c/floor', 0.5, 0.5, 16)];

const LADDERS: SelectorLadders = { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } };

// Real backing store + spy-able methods so remount and throwing-storage tests can drive one object.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    clear: vi.fn(() => {
      store = {};
    }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

beforeAll(() => {
  Object.defineProperty(SVGElement.prototype, 'getBoundingClientRect', {
    value: () => ({ top: 0, left: 0, height: CHART_LAYOUT.height, width: 760, bottom: CHART_LAYOUT.height, right: 760, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect,
    configurable: true,
  });
  Object.defineProperty(Element.prototype, 'setPointerCapture', { value: () => {}, configurable: true });
  Object.defineProperty(Element.prototype, 'releasePointerCapture', { value: () => {}, configurable: true });
});

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

function props(over: Partial<LadderPanelProps> = {}): LadderPanelProps {
  return {
    candidates: CANDIDATES,
    ladders: LADDERS,
    separate: false,
    onSeparateChange: vi.fn(),
    onChange: vi.fn(),
    headroom: 1.5,
    onHeadroomChange: vi.fn(),
    floor: 0.65,
    bestAAScore: 59.4,
    indexVersion: '5',
    selectedTier: 'complex',
    tierPreview: null,
    appliedHeadroom: 1.5,
    blacklist: new Set<string>(),
    ...over,
  };
}

function renderPanel(over: Partial<LadderPanelProps> = {}) {
  const p = props(over);
  return { p, ...render(<LadderPanel {...p} />) };
}

const dot = (slug: string) => screen.queryByTestId(`tl-pt-${slug}`);

describe('LadderPanel', () => {
  it('toggling the switch reports the next state', () => {
    const { p, rerender } = renderPanel();
    const sw = screen.getByRole('switch', { name: 'Separate reviewer ladder' });
    expect(sw).toHaveAttribute('aria-checked', 'false');

    fireEvent.click(sw);
    expect(p.onSeparateChange).toHaveBeenLastCalledWith(true);

    rerender(<LadderPanel {...p} separate />);
    fireEvent.click(screen.getByRole('switch', { name: 'Separate reviewer ladder' }));
    expect(p.onSeparateChange).toHaveBeenLastCalledWith(false);
  });

  it('hiding a provider removes its dots and leaves the scales and callbacks alone', () => {
    const { p } = renderPanel();
    const lowX = dot('b/low')?.getAttribute('cx');
    expect(screen.queryByText(/candidates shown/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'a 2' }));

    expect(dot('a/top')).not.toBeInTheDocument();
    expect(dot('a/mid')).not.toBeInTheDocument();
    expect(dot('b/low')).toBeInTheDocument();
    expect(dot('b/low')?.getAttribute('cx')).toBe(lowX);
    expect(screen.getByText('2 of 4 candidates shown')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'none' }));
    expect(screen.queryByTestId(/^tl-pt-/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'b 1' }));
    expect(dot('b/low')).toBeInTheDocument();
    expect(dot('c/floor')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'all' }));
    expect(dot('a/top')).toBeInTheDocument();
    expect(dot('c/floor')).toBeInTheDocument();
    expect(screen.queryByText(/candidates shown/)).not.toBeInTheDocument();

    expect(p.onChange).not.toHaveBeenCalled();
    expect(p.onHeadroomChange).not.toHaveBeenCalled();
    expect(p.onSeparateChange).not.toHaveBeenCalled();
  });

  it('the selection survives a remount through localStorage', () => {
    const { unmount } = renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'a 2' }));
    unmount();

    renderPanel();

    expect(dot('a/top')).not.toBeInTheDocument();
    expect(dot('b/low')).toBeInTheDocument();
    expect(screen.getByText('2 of 4 candidates shown')).toBeInTheDocument();
  });

  it('a throwing localStorage still renders the chart and toggling stays safe', () => {
    localStorageMock.getItem.mockImplementationOnce(() => {
      throw new Error('storage blocked');
    });
    localStorageMock.setItem.mockImplementationOnce(() => {
      throw new Error('QuotaExceededError');
    });

    renderPanel();

    expect(dot('a/top')).toBeInTheDocument();
    expect(() => fireEvent.click(screen.getByRole('button', { name: 'a 2' }))).not.toThrow();
    expect(dot('a/top')).not.toBeInTheDocument();
    expect(dot('b/low')).toBeInTheDocument();
  });

  it('reports a typed headroom and marks an invalid one', () => {
    const { p, rerender } = renderPanel();
    const input = screen.getByRole('spinbutton', { name: 'Price headroom' });
    expect(input).not.toHaveAttribute('aria-invalid');

    fireEvent.change(input, { target: { value: '2' } });
    expect(p.onHeadroomChange).toHaveBeenLastCalledWith(2);
    fireEvent.change(input, { target: { value: '' } });
    expect(p.onHeadroomChange).toHaveBeenLastCalledWith(NaN);

    for (const bad of [0.5, NaN, Infinity]) {
      rerender(<LadderPanel {...p} headroom={bad} />);
      expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveAttribute('aria-invalid', 'true');
    }
  });
});
