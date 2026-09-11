import { beforeAll, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { SelectorCandidate, SelectorLadders } from '../../types';
import { TierLadder } from './TierLadder';

const DEFAULTS = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function ladders(): SelectorLadders {
  return { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } };
}

function cand(slug: string, coder: number, reviewer: number, price = 1e-6): SelectorCandidate {
  return { slug, creator: slug.split('/')[0], coder_prior: coder, reviewer_prior: reviewer, prompt_price_per_tok: price, completion_price_per_tok: price, context_window: 200000, price_source: 'gateway', scored_from: '' };
}

const CANDIDATES = [cand('a/top', 0.95, 0.92), cand('a/mid', 0.85, 0.84), cand('b/low', 0.7, 0.8), cand('c/floor', 0.5, 0.66)];

const EMPTY = new Set<string>();

beforeAll(() => {
  // jsdom has neither pointer capture nor layout; the rail is 650px tall so
  // a client y of (1 - v) * 1000 lands on prior v.
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', { value: vi.fn(), configurable: true });
  Object.defineProperty(HTMLElement.prototype, 'releasePointerCapture', { value: vi.fn(), configurable: true });
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    top: 0, height: 650, left: 0, width: 300, bottom: 650, right: 300, x: 0, y: 0, toJSON: () => ({}),
  } as DOMRect);
});

function renderLadder(overrides: Partial<Parameters<typeof TierLadder>[0]> = {}) {
  const onChange = vi.fn();
  const onLinkedChange = vi.fn();
  const onHeadroomChange = vi.fn();
  const utils = render(
    <TierLadder
      candidates={CANDIDATES}
      ladders={ladders()}
      linked={false}
      onLinkedChange={onLinkedChange}
      onChange={onChange}
      floor={0.65}
      blacklist={EMPTY}
      picks={{ coder: EMPTY, reviewer: EMPTY }}
      seats={EMPTY}
      meta="4 candidates"
      headroom={1.5}
      onHeadroomChange={onHeadroomChange}
      {...overrides}
    />,
  );
  return { onChange, onLinkedChange, onHeadroomChange, ...utils };
}

function drag(name: string, from: number, to: number) {
  const handle = screen.getByRole('slider', { name });
  fireEvent.pointerDown(handle, { pointerId: 1, clientY: from, button: 0 });
  fireEvent.pointerMove(handle, { pointerId: 1, clientY: to });
  fireEvent.pointerUp(handle, { pointerId: 1, clientY: to });
}

describe('TierLadder - rendering', () => {
  it('draws a pill per candidate in each column and counts band members', () => {
    renderLadder();

    expect(screen.getByTestId('tl-dot-coder-a/top')).toHaveTextContent('top');
    expect(screen.getByTestId('tl-dot-reviewer-c/floor')).toHaveTextContent('0.660');
    expect(screen.getByTestId('tl-band-coder-critical')).toHaveTextContent('1 model');
    expect(screen.getByTestId('tl-band-coder-complex')).toHaveTextContent('1 model');
    expect(screen.getByTestId('tl-band-reviewer-simple')).toHaveTextContent('1 model');
    expect(screen.getByTestId('tl-band-coder-floor')).toHaveTextContent('below floor');
  });

  it('marks picks, seats and blacklisted models', () => {
    renderLadder({
      picks: { coder: new Set(['a/top']), reviewer: new Set(['a/mid']) },
      seats: new Set(['b/low']),
      blacklist: new Set(['c/floor']),
    });

    expect(screen.getByTestId('tl-dot-coder-a/top')).toHaveClass('picked');
    expect(screen.getByTestId('tl-dot-reviewer-a/top')).not.toHaveClass('picked');
    expect(screen.getByTestId('tl-dot-reviewer-b/low')).toHaveClass('seat');
    expect(screen.getByTestId('tl-dot-coder-b/low')).not.toHaveClass('seat');
    expect(screen.getByTestId('tl-dot-coder-c/floor')).toHaveClass('banned');
  });

  it('shows both values on the coder handle when a tier differs between the ladders', () => {
    renderLadder({ ladders: { coder: { ...DEFAULTS, complex: 0.9 }, reviewer: { ...DEFAULTS } } });

    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.90 · 0.82');
    expect(screen.getByRole('slider', { name: 'reviewer complex bar' })).toHaveTextContent('0.82');
    expect(screen.getByRole('slider', { name: 'coder critical bar' })).toHaveTextContent('0.90');
  });

  it('names the AA row a candidate was scored from in the pill tooltip', () => {
    renderLadder({ candidates: [{ ...cand('a/top', 0.95, 0.92), scored_from: 'top-1-medium' }, cand('a/mid', 0.85, 0.84)] });
    expect(screen.getByTestId('tl-dot-coder-a/top')).toHaveAttribute('title', expect.stringContaining('scored from top-1-medium'));
    expect(screen.getByTestId('tl-dot-coder-a/mid')).toHaveAttribute('title', expect.not.stringContaining('scored from'));
  });
});

describe('TierLadder - context menu', () => {
  it('right-clicking a pill reports the slug and pointer, and suppresses the browser menu', () => {
    const onModelMenu = vi.fn();
    renderLadder({ onModelMenu });

    const pill = screen.getByTestId('tl-dot-reviewer-a/mid');
    const evt = fireEvent.contextMenu(pill, { clientX: 77, clientY: 123 });

    expect(onModelMenu).toHaveBeenCalledWith('a/mid', 77, 123);
    expect(evt).toBe(false);
  });

  it('leaves the browser menu alone when no handler is wired', () => {
    renderLadder();

    expect(fireEvent.contextMenu(screen.getByTestId('tl-dot-coder-a/top'), { clientX: 1, clientY: 2 })).toBe(true);
  });

  it('tells the operator a pill can be right-clicked', () => {
    renderLadder({ onModelMenu: vi.fn() });

    expect(screen.getByText(/right-click a pill to blacklist/)).toBeInTheDocument();
  });
});

describe('TierLadder - drag', () => {
  it('moves one ladder in 0.005 steps when unlinked', () => {
    const { onChange } = renderLadder();

    drag('coder complex bar', 180, 145);

    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.855, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.82, 9);
  });

  it('moves both ladders when linked, from either handle', () => {
    const { onChange } = renderLadder({ linked: true });

    drag('reviewer complex bar', 180, 145);

    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.855, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.855, 9);
  });

  it('clamps between neighbours and above the floor', () => {
    // Floor 0.6 sits below the simple bar so a pull below it has room to clamp.
    const { onChange } = renderLadder({ floor: 0.6 });

    drag('coder complex bar', 180, 50);
    expect((onChange.mock.calls[0][0] as SelectorLadders).coder.complex).toBeCloseTo(0.9, 9);

    drag('coder complex bar', 180, 300);
    expect((onChange.mock.calls[1][0] as SelectorLadders).coder.complex).toBeCloseTo(0.76, 9);

    drag('coder simple bar', 350, 500);
    expect((onChange.mock.calls[2][0] as SelectorLadders).coder.simple).toBeCloseTo(0.6, 9);
  });

  it('turning linked on snaps nothing; only the next drag equalises the tier', () => {
    const differing: SelectorLadders = { coder: { ...DEFAULTS, complex: 0.9 }, reviewer: { ...DEFAULTS } };
    const { onChange, onLinkedChange, rerender } = renderLadder({ ladders: differing });

    fireEvent.click(screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' }));
    expect(onLinkedChange).toHaveBeenCalledWith(true);
    expect(onChange).not.toHaveBeenCalled();

    rerender(
      <TierLadder
        candidates={CANDIDATES}
        ladders={differing}
        linked
        onLinkedChange={onLinkedChange}
        onChange={onChange}
        floor={0.65}
        blacklist={EMPTY}
        picks={{ coder: EMPTY, reviewer: EMPTY }}
        seats={EMPTY}
        meta="4 candidates"
        headroom={1.5}
        onHeadroomChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.90 · 0.82');
    expect(onChange).not.toHaveBeenCalled();

    drag('coder complex bar', 100, 145);
    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.855, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.855, 9);
  });

  it('moves a bar from the keyboard, so the handles are operable without a pointer', () => {
    const { onChange } = renderLadder();

    fireEvent.keyDown(screen.getByRole('slider', { name: 'coder complex bar' }), { key: 'ArrowUp' });

    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.825, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.82, 9);
  });

  it('keeps the handle mounted across a drag', () => {
    renderLadder();
    const handle = screen.getByRole('slider', { name: 'coder complex bar' });

    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 180, button: 0 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 145 });

    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toBe(handle);
    expect(within(screen.getByTestId('tl-col-coder')).getAllByRole('slider')).toHaveLength(4);
  });
});

describe('TierLadder - headroom', () => {
  it('reports a typed headroom as a number and an emptied field as NaN', () => {
    const { onHeadroomChange } = renderLadder();
    const input = screen.getByRole('spinbutton', { name: 'Price headroom' });

    fireEvent.change(input, { target: { value: '2' } });
    expect(onHeadroomChange).toHaveBeenLastCalledWith(2);

    fireEvent.change(input, { target: { value: '' } });
    expect(onHeadroomChange).toHaveBeenLastCalledWith(NaN);
  });

  it('marks a headroom below 1 invalid', () => {
    renderLadder({ headroom: 0.5 });
    expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveAttribute('aria-invalid', 'true');
  });

  it('marks a non-finite headroom invalid, matching the page gate', () => {
    renderLadder({ headroom: Infinity });
    expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveAttribute('aria-invalid', 'true');
  });
});
