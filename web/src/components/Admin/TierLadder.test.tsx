import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { SelectorCandidate, SelectorLadders } from '../../types';
import { TierLadder } from './TierLadder';

const DEFAULTS = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function ladders(): SelectorLadders {
  return { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } };
}

function cand(slug: string, coder: number, reviewer: number, price = 1e-6, creator?: string): SelectorCandidate {
  return { slug, creator: creator ?? slug.split('/')[0], coder_prior: coder, reviewer_prior: reviewer, prompt_price_per_tok: price, completion_price_per_tok: price, context_window: 200000, price_source: 'gateway', scored_from: '', coder_prior_estimated: false };
}

// Real backing store + spy-able methods, matching providerFilter.test.tsx, so
// remount and throwing-storage tests can drive one object.
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

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
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

describe('TierLadder - estimated coder priors', () => {
  it('marks the estimated coder pill but not its reviewer pill', () => {
    renderLadder({ candidates: [{ ...cand('a/top', 0.95, 0.92), coder_prior_estimated: true }, ...CANDIDATES.slice(1)] });

    const coderPill = screen.getByTestId('tl-dot-coder-a/top');
    expect(coderPill).toHaveClass('est');
    expect(within(coderPill).getByText('~0.950')).toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-reviewer-a/top')).not.toHaveClass('est');
    expect(screen.getByTestId('tl-dot-coder-a/mid')).not.toHaveClass('est');
  });

  it('shows the estimated count in the panel meta when some candidates are estimated', () => {
    renderLadder({ candidates: [{ ...cand('a/top', 0.95, 0.92), coder_prior_estimated: true }, { ...cand('a/mid', 0.85, 0.84), coder_prior_estimated: true }, ...CANDIDATES.slice(2)] });

    expect(screen.getByText('2 estimated')).toBeInTheDocument();
  });

  it('renders no marker and no count when nothing is estimated', () => {
    renderLadder();

    expect(screen.queryByText(/\d estimated/)).not.toBeInTheDocument();
    expect(within(screen.getByTestId('tl-col-coder')).queryByText(/^~/)).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-a/top')).not.toHaveClass('est');
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

describe('TierLadder - provider filter', () => {
  it('unchecking a provider removes its pills from both columns and leaves the others', () => {
    renderLadder();

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));

    expect(screen.queryByTestId('tl-dot-coder-a/top')).not.toBeInTheDocument();
    expect(screen.queryByTestId('tl-dot-reviewer-a/mid')).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-b/low')).toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-reviewer-c/floor')).toBeInTheDocument();
  });

  it('shows the shown-of-total count in the panel meta while filtered', () => {
    const { rerender } = renderLadder();

    expect(screen.queryByText(/\d+ of \d+ candidates shown/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));
    expect(screen.getByText('2 of 4 candidates shown')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));
    expect(screen.queryByText(/\d+ of \d+ candidates shown/)).not.toBeInTheDocument();

    rerender(
      <TierLadder
        candidates={CANDIDATES}
        ladders={ladders()}
        linked={false}
        onLinkedChange={vi.fn()}
        onChange={vi.fn()}
        floor={0.65}
        blacklist={EMPTY}
        picks={{ coder: EMPTY, reviewer: EMPTY }}
        seats={EMPTY}
        meta="4 candidates"
        headroom={1.5}
        onHeadroomChange={vi.fn()}
      />,
    );
    expect(screen.queryByText(/\d+ of \d+ candidates shown/)).not.toBeInTheDocument();
  });

  it('keeps the tier band counts identical before and after filtering', () => {
    renderLadder();

    const bands = within(screen.getByTestId('tl-ladder')).getAllByText(/model/).map((n) => n.textContent);
    expect(bands).toContain('1 model');

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));

    expect(within(screen.getByTestId('tl-ladder')).getAllByText(/model/).map((n) => n.textContent)).toEqual(bands);
    expect(screen.getByTestId('tl-band-coder-critical')).toHaveTextContent('1 model');
    expect(screen.getByTestId('tl-band-reviewer-simple')).toHaveTextContent('1 model');
  });

  it('none followed by checking one provider shows only that provider', () => {
    renderLadder();

    fireEvent.click(screen.getByRole('button', { name: 'none' }));
    expect(screen.queryByTestId(/^tl-dot-/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('checkbox', { name: 'b 1' }));

    expect(screen.getByTestId('tl-dot-coder-b/low')).toBeInTheDocument();
    expect(screen.queryByTestId(/^tl-dot-(coder|reviewer)-a\//)).not.toBeInTheDocument();
    expect(screen.queryByTestId(/^tl-dot-(coder|reviewer)-c\//)).not.toBeInTheDocument();
  });

  it('groups a candidate with an empty creator under its slug prefix', () => {
    renderLadder({ candidates: [cand('zhipu/glm', 0.9, 0.9, 1e-6, ''), ...CANDIDATES.slice(1)] });

    expect(screen.getByRole('checkbox', { name: 'zhipu 1' })).toBeChecked();
    fireEvent.click(screen.getByRole('checkbox', { name: 'zhipu 1' }));
    expect(screen.queryByTestId('tl-dot-coder-zhipu/glm')).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-a/mid')).toBeInTheDocument();
  });

  it('a rerender after filtering still marks picked and seat classes on the visible pills', () => {
    const props: Parameters<typeof TierLadder>[0] = {
      candidates: CANDIDATES,
      ladders: ladders(),
      linked: false,
      onLinkedChange: vi.fn(),
      onChange: vi.fn(),
      floor: 0.65,
      blacklist: EMPTY,
      picks: { coder: new Set(['b/low']), reviewer: new Set(['a/top']) },
      seats: new Set(['b/low']),
      meta: '4 candidates',
      headroom: 1.5,
      onHeadroomChange: vi.fn(),
    };
    const { rerender } = renderLadder(props);

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));
    rerender(<TierLadder {...props} />);

    expect(screen.getByTestId('tl-dot-coder-b/low')).toHaveClass('picked');
    expect(screen.getByTestId('tl-dot-reviewer-b/low')).toHaveClass('seat');
    expect(screen.queryByTestId('tl-dot-reviewer-a/top')).not.toBeInTheDocument();
  });

  it('the filter never calls onChange or onHeadroomChange', () => {
    const { onChange, onHeadroomChange } = renderLadder();

    fireEvent.click(screen.getByRole('button', { name: 'none' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));

    expect(onChange).not.toHaveBeenCalled();
    expect(onHeadroomChange).not.toHaveBeenCalled();
  });

  it('the selection survives a remount through localStorage', () => {
    const { unmount } = renderLadder();
    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));
    unmount();

    renderLadder();

    expect(screen.queryByTestId('tl-dot-coder-a/top')).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-b/low')).toBeInTheDocument();
    expect(screen.getByText('2 of 4 candidates shown')).toBeInTheDocument();
  });

  it('a throwing localStorage still renders the ladder and toggling stays safe', () => {
    localStorageMock.getItem.mockImplementationOnce(() => {
      throw new Error('storage blocked');
    });
    localStorageMock.setItem.mockImplementationOnce(() => {
      throw new Error('QuotaExceededError');
    });

    renderLadder();

    expect(screen.getByTestId('tl-dot-coder-a/top')).toBeInTheDocument();
    expect(screen.queryByText(/\d+ of \d+ candidates shown/)).not.toBeInTheDocument();

    // Unchecking a provider hits the throwing setItem; the view keeps working.
    expect(() => fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }))).not.toThrow();
    expect(screen.queryByTestId('tl-dot-coder-a/top')).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-b/low')).toBeInTheDocument();
  });

  it('the all toggle restores every pill after filtering', () => {
    renderLadder();

    fireEvent.click(screen.getByRole('checkbox', { name: 'a 2' }));
    fireEvent.click(screen.getByRole('button', { name: 'all' }));
    expect(screen.queryByText(/\d+ of \d+ candidates shown/)).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-a/top')).toBeInTheDocument();
  });
});
