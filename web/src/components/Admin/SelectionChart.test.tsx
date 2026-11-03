import { beforeAll, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { SelectorCandidate, SelectorLadders, SelectorPickReport, SelectorPoolEntry, SelectorRole, SelectorSeat, SelectorTierPreview } from '../../types';
import { CHART_LAYOUT, qualityScale } from './chart';
import { snapBar } from './ladder';
import { SelectionChart, type SelectionChartProps } from './SelectionChart';

function cand(slug: string, coder: number, reviewer: number, perMillion: number, prior_source: SelectorCandidate['prior_source'] = 'aa'): SelectorCandidate {
  const blended = perMillion / 1e6;
  return {
    slug,
    creator: slug.split('/')[0],
    coder_prior: coder,
    reviewer_prior: reviewer,
    prompt_price_per_tok: blended / 4,
    completion_price_per_tok: (blended * 3) / 4,
    context_window: 200000,
    price_source: 'gateway',
    scored_from: '',
    prior_source,
  };
}

const CANDIDATES: SelectorCandidate[] = [
  cand('a/cheap', 0.9, 0.9, 2),
  cand('a/mid', 0.8, 0.8, 4),
  cand('b/pricey', 0.83, 0.83, 20),
  cand('z/free', 0.75, 0.75, 0),
  cand('op/custom', 0.9, 0.4, 3, 'model_priors'),
];

const LADDERS: SelectorLadders = {
  coder: { simple: 0.65, moderate: 0.75, complex: 0.84, critical: 0.94 },
  reviewer: { simple: 0.9, moderate: 0.9, complex: 0.9, critical: 0.92 },
};

function pool(...entries: [string, number][]): SelectorPoolEntry[] {
  return entries.map(([model, perMillion]) => ({ model, prior: 0.9, price_per_tok: perMillion / 1e6, outcome: 'in-band' }));
}

function report(model: string, role: SelectorRole, bar: number, poolEntries: SelectorPoolEntry[] = [], ok = model !== ''): SelectorPickReport {
  return {
    pick: { model, context_window: 200000, role, requested_tier: 'complex', met_tier: ok ? 'complex' : '', requested_bar: bar, prior: 0.9, has_prior: true, source: 'auto', duplicate: false, ok, price_per_tok: 2e-6, price_source: 'gateway' },
    report: { rung: ok ? 'complex' : '', bar, pool: poolEntries, filtered_out: [] },
  };
}

function seatOf(model: string, ok = true): SelectorSeat {
  return { ...report(model, 'reviewer', 0.9, [], ok), walked: false };
}

function tierPreview(over: Partial<SelectorTierPreview> = {}): SelectorTierPreview {
  return {
    coder: report('a/cheap', 'coder', 0.84),
    reviewer: report('a/mid', 'reviewer', 0.9),
    panel: [seatOf('a/mid'), seatOf('b/pricey'), seatOf('z/free', false)],
    ...over,
  };
}

function renderChart(over: Partial<SelectionChartProps> = {}) {
  const props: SelectionChartProps = {
    candidates: CANDIDATES,
    visible: () => true,
    ladders: LADDERS,
    separate: true,
    floor: 0.65,
    bestAAScore: 59.4,
    indexVersion: '5',
    selectedTier: 'complex',
    tierPreview: null,
    appliedHeadroom: 1.5,
    blacklist: new Set<string>(),
    onChange: vi.fn(),
    ...over,
  };
  const utils = render(<SelectionChart {...props} />);
  return { ...utils, props, rerenderWith: (next: Partial<SelectionChartProps>) => utils.rerender(<SelectionChart {...props} {...next} />) };
}

function num(el: Element, attr: string): number {
  const v = el.getAttribute(attr);
  if (v === null) throw new Error(`${attr} missing on ${el.tagName}`);
  return Number(v);
}

const dot = (id: string) => screen.getByTestId(`tl-pt-${id}`);

describe('SelectionChart', () => {
  it('places a dot per visible candidate by price and prior', () => {
    const { rerenderWith } = renderChart();
    const allCheapX = num(dot('a/cheap'), 'cx');
    expect(num(dot('a/cheap'), 'cy')).toBeLessThan(num(dot('a/mid'), 'cy'));
    expect(num(dot('a/mid'), 'cx')).toBeGreaterThan(num(dot('a/cheap'), 'cx'));
    expect(num(dot('b/pricey'), 'cx')).toBeGreaterThan(num(dot('a/mid'), 'cx'));

    rerenderWith({ visible: (c) => c.creator !== 'b' });

    expect(screen.queryByTestId('tl-pt-b/pricey')).not.toBeInTheDocument();
    expect(dot('a/mid')).toBeInTheDocument();
    // The filter is display-only: hiding the priciest model must not rescale the price axis.
    expect(num(dot('a/cheap'), 'cx')).toBe(allCheapX);
  });

  it('puts an unpriced candidate in the no-price gutter with finite coordinates', () => {
    const { container } = renderChart({
      tierPreview: tierPreview({ coder: report('z/free', 'coder', 0.65, pool(['z/free', 0], ['a/cheap', 2])) }),
    });

    const freeX = num(dot('z/free'), 'cx');
    for (const slug of ['a/cheap', 'a/mid', 'b/pricey', 'op/custom-coder']) {
      expect(freeX).toBeLessThan(num(dot(slug), 'cx'));
    }
    // A pool anchored on a free model still draws its band.
    expect(screen.getByTestId('tl-band-coder')).toBeInTheDocument();

    const svg = container.querySelector('svg')!;
    for (const el of [svg, ...Array.from(svg.querySelectorAll('*'))]) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.value, `${el.tagName} ${attr.name}`).not.toMatch(/NaN|Infinity/);
      }
      for (const name of ['cx', 'cy', 'r', 'x', 'y', 'x1', 'x2', 'y1', 'y2', 'width', 'height']) {
        const v = el.getAttribute(name);
        if (v !== null) expect(Number.isFinite(Number(v)), `${el.tagName} ${name}=${v}`).toBe(true);
      }
    }
  });

  it('draws a model_priors candidate with split priors as two joined dots, the low one under the floor', () => {
    const { container } = renderChart();

    expect(screen.queryByTestId('tl-pt-op/custom')).not.toBeInTheDocument();
    const coder = dot('op/custom-coder');
    const reviewer = dot('op/custom-reviewer');
    const cx = num(coder, 'cx');
    expect(num(reviewer, 'cx')).toBe(cx);

    const floorY = num(screen.getByTestId('tl-floor'), 'y1');
    expect(num(reviewer, 'cy')).toBeGreaterThan(floorY);
    expect(num(coder, 'cy')).toBeLessThan(floorY);

    const [top, bottom] = [num(coder, 'cy'), num(reviewer, 'cy')];
    const link = Array.from(container.querySelectorAll('line')).find(
      (l) => num(l, 'x1') === cx && num(l, 'x2') === cx && Math.min(num(l, 'y1'), num(l, 'y2')) === top && Math.max(num(l, 'y1'), num(l, 'y2')) === bottom,
    );
    expect(link).toBeDefined();

    for (const d of [coder, reviewer]) {
      fireEvent.pointerEnter(d, { clientX: 100, clientY: 100 });
      const tip = screen.getByRole('tooltip');
      expect(tip).toHaveTextContent('op/custom');
      expect(tip).toHaveTextContent('operator prior');
      expect(tip).toHaveTextContent('0.90');
      expect(tip).toHaveTextContent('0.40');
      expect(tip).not.toHaveTextContent(/AA Intelligence Index/);
      // Neither prior × best AA score (53.5, 23.8) is shown for an operator prior.
      expect(tip).not.toHaveTextContent('53.5');
      expect(tip).not.toHaveTextContent('23.8');
      fireEvent.pointerLeave(d);
    }
  });

  it('shows prior × best AA score with the index version for an AA-scored candidate', () => {
    const { container, rerenderWith } = renderChart();

    fireEvent.pointerEnter(dot('a/cheap'), { clientX: 100, clientY: 100 });
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent('a/cheap');
    expect(tip).toHaveTextContent('53.5');
    expect(tip).toHaveTextContent('v5');
    expect(tip).toHaveTextContent(/complex/);
    expect(tip).not.toHaveTextContent(/critical/);

    fireEvent.pointerLeave(dot('a/cheap'));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();

    rerenderWith({ indexVersion: '' });
    fireEvent.pointerEnter(dot('a/cheap'), { clientX: 100, clientY: 100 });
    expect(screen.getByRole('tooltip')).toHaveTextContent('53.5');
    expect(screen.getByRole('tooltip')).not.toHaveTextContent('v5');
    expect(container).not.toHaveTextContent('v5');
  });

  it('draws no AA-point axis without a best AA score', () => {
    const { container } = renderChart({ bestAAScore: 0 });

    fireEvent.pointerEnter(dot('a/cheap'), { clientX: 100, clientY: 100 });
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent('0.90');
    expect(tip).not.toHaveTextContent('53.5');

    const integerLabels = Array.from(container.querySelectorAll('svg text')).filter((t) => /^\d+$/.test(t.textContent ?? ''));
    expect(integerLabels).toEqual([]);
  });

  it('labels the AA-point axis when a best AA score is known', () => {
    const { container } = renderChart();
    const labels = Array.from(container.querySelectorAll('svg text')).map((t) => t.textContent);
    // Prior 1.0 sits at the best AA score, rounded.
    expect(labels).toContain('59');
  });

  it('marks only the selected tier picks and seats, and labels only those', () => {
    const { container, rerenderWith } = renderChart({ tierPreview: tierPreview() });
    const svg = within(container.querySelector('svg') as unknown as HTMLElement);

    expect(dot('a/cheap')).toHaveAttribute('data-marks', 'coder-pick');
    expect(dot('a/mid')).toHaveAttribute('data-marks', 'reviewer-pick seat');
    expect(dot('b/pricey')).toHaveAttribute('data-marks', 'seat');
    // A seat whose pick is not ok carries no mark.
    expect(dot('z/free')).toHaveAttribute('data-marks', '');

    expect(svg.getByText('cheap')).toBeInTheDocument();
    expect(svg.getByText('mid')).toBeInTheDocument();
    expect(svg.getByText('pricey')).toBeInTheDocument();
    expect(svg.queryByText('free')).not.toBeInTheDocument();
    expect(svg.queryByText('custom')).not.toBeInTheDocument();

    // A split candidate takes coder marks on its coder dot and reviewer marks on its reviewer dot.
    rerenderWith({ tierPreview: tierPreview({ coder: report('op/custom', 'coder', 0.84), reviewer: report('op/custom', 'reviewer', 0.9), panel: [seatOf('op/custom')] }) });
    expect(dot('op/custom-coder')).toHaveAttribute('data-marks', 'coder-pick');
    expect(dot('op/custom-reviewer')).toHaveAttribute('data-marks', 'reviewer-pick seat');
    expect(dot('a/cheap')).toHaveAttribute('data-marks', '');

    // A failed pick marks nothing.
    rerenderWith({ tierPreview: tierPreview({ coder: report('a/cheap', 'coder', 0.84, [], false), reviewer: report('', 'reviewer', 0), panel: [] }) });
    expect(dot('a/cheap')).toHaveAttribute('data-marks', '');
    expect(svg.queryByText('cheap')).not.toBeInTheDocument();
  });

  it('draws each role band from the pool anchor times the applied headroom', () => {
    const preview = tierPreview({
      coder: report('a/mid', 'coder', 0.84, pool(['a/cheap', 2], ['a/mid', 4], ['b/pricey', 20])),
      reviewer: report('a/cheap', 'reviewer', 0.9, pool(['a/cheap', 2], ['b/pricey', 20])),
    });
    const { rerenderWith } = renderChart({ tierPreview: preview, appliedHeadroom: 2.5 });

    const band = screen.getByTestId('tl-band-coder');
    const left = num(band, 'x');
    const right = left + num(band, 'width');
    const bandBottom = num(band, 'y') + num(band, 'height');
    for (const slug of ['a/cheap', 'a/mid']) {
      const cx = num(dot(slug), 'cx');
      expect(cx).toBeGreaterThanOrEqual(left - 1e-6);
      expect(cx).toBeLessThanOrEqual(right + 1e-6);
    }
    expect(num(dot('b/pricey'), 'cx')).toBeGreaterThan(right);
    // The band sits above the coder bar (0.84): a/cheap (0.90) is inside it, a/mid (0.80) below it.
    expect(num(dot('a/cheap'), 'cy')).toBeLessThan(bandBottom);
    expect(num(dot('a/mid'), 'cy')).toBeGreaterThan(bandBottom);

    const reviewerBand = screen.getByTestId('tl-band-reviewer');
    expect(num(reviewerBand, 'y') + num(reviewerBand, 'height')).toBeLessThan(bandBottom);

    rerenderWith({ tierPreview: preview, appliedHeadroom: null });
    expect(screen.queryByTestId('tl-band-coder')).not.toBeInTheDocument();
    expect(screen.queryByTestId('tl-band-reviewer')).not.toBeInTheDocument();

    rerenderWith({ tierPreview: tierPreview(), appliedHeadroom: 2.5 });
    expect(screen.queryByTestId('tl-band-coder')).not.toBeInTheDocument();
  });

  it('right-clicking a dot reports the slug and pointer', () => {
    const onModelMenu = vi.fn();
    renderChart({ onModelMenu });

    expect(fireEvent.contextMenu(dot('a/mid'), { clientX: 120, clientY: 80 })).toBe(false);
    expect(onModelMenu).toHaveBeenLastCalledWith('a/mid', 120, 80);

    expect(fireEvent.contextMenu(dot('op/custom-reviewer'), { clientX: 5, clientY: 6 })).toBe(false);
    expect(onModelMenu).toHaveBeenLastCalledWith('op/custom', 5, 6);
  });

  it('leaves the browser menu alone without a handler', () => {
    renderChart();
    expect(fireEvent.contextMenu(dot('a/mid'), { clientX: 120, clientY: 80 })).toBe(true);
  });

  it('strikes a blacklisted dot', () => {
    renderChart({ blacklist: new Set(['b/pricey', 'op/custom']), tierPreview: tierPreview() });

    expect(dot('b/pricey').getAttribute('data-marks')?.split(' ')).toEqual(['seat', 'blacklisted']);
    expect(dot('op/custom-coder')).toHaveAttribute('data-marks', 'blacklisted');
    expect(dot('op/custom-reviewer')).toHaveAttribute('data-marks', 'blacklisted');
    expect(dot('a/mid').getAttribute('data-marks')).not.toContain('blacklisted');
  });
});

// jsdom has neither layout nor pointer capture. The chart draws at its own
// size, so one client px is one SVG px. Plain stubs, not spies, so a
// vi.resetAllMocks() elsewhere cannot restore the zero-height jsdom box.
beforeAll(() => {
  Object.defineProperty(SVGElement.prototype, 'getBoundingClientRect', {
    value: () =>
      ({
        top: 0, left: 0, height: CHART_LAYOUT.height, width: 760, bottom: CHART_LAYOUT.height, right: 760, x: 0, y: 0, toJSON: () => ({}),
      }) as DOMRect,
    configurable: true,
  });
  Object.defineProperty(Element.prototype, 'setPointerCapture', { value: () => {}, configurable: true });
  Object.defineProperty(Element.prototype, 'releasePointerCapture', { value: () => {}, configurable: true });
});

/** Prior change for a pointer travel of dy px (down is positive). */
function travel(dy: number, floor = 0.65): number {
  const q = qualityScale(floor, CHART_LAYOUT.top, CHART_LAYOUT.height - CHART_LAYOUT.bottom);
  return q.prior(200 + dy) - q.prior(200);
}

function translate(el: Element): [number, number] {
  const m = /translate\(\s*([-\d.e]+)[\s,]+([-\d.e]+)\s*\)/.exec(el.getAttribute('transform') ?? '');
  if (!m) throw new Error(`no translate on ${el.tagName}`);
  return [Number(m[1]), Number(m[2])];
}

function drag(name: string, from: number, to: number) {
  const handle = screen.getByRole('slider', { name });
  fireEvent.pointerDown(handle, { pointerId: 1, clientY: from, button: 0 });
  fireEvent.pointerMove(handle, { pointerId: 1, clientY: to });
  fireEvent.pointerUp(handle, { pointerId: 1, clientY: to });
}

function lastChange(onChange: SelectionChartProps['onChange']): SelectorLadders {
  const calls = vi.mocked(onChange).mock.calls;
  expect(calls.length).toBeGreaterThan(0);
  return calls[calls.length - 1][0];
}

const TIERS = ['simple', 'moderate', 'complex', 'critical'] as const;

describe('SelectionChart tier handles', () => {
  it('shows coder handles left and reviewer handles right when separate, one shared set otherwise', () => {
    const { rerenderWith } = renderChart();

    expect(screen.getAllByRole('slider')).toHaveLength(8);
    const q = qualityScale(0.65, CHART_LAYOUT.top, CHART_LAYOUT.height - CHART_LAYOUT.bottom);
    for (const tier of TIERS) {
      const coderLine = screen.getByTestId(`tl-line-coder-${tier}`);
      const reviewerLine = screen.getByTestId(`tl-line-reviewer-${tier}`);
      expect(num(coderLine, 'y1')).toBeCloseTo(q.y(LADDERS.coder[tier]), 6);
      expect(num(reviewerLine, 'y1')).toBeCloseTo(q.y(LADDERS.reviewer[tier]), 6);
      expect(coderLine).not.toHaveAttribute('stroke-dasharray');
      expect(reviewerLine).toHaveAttribute('stroke-dasharray');

      const [coderX] = translate(screen.getByRole('slider', { name: `coder ${tier} bar` }));
      const [reviewerX] = translate(screen.getByRole('slider', { name: `reviewer ${tier} bar` }));
      expect(coderX + CHART_LAYOUT.handleW).toBeLessThanOrEqual(num(coderLine, 'x1'));
      expect(reviewerX).toBeGreaterThanOrEqual(num(reviewerLine, 'x2'));
    }

    rerenderWith({ separate: false });

    expect(screen.getAllByRole('slider')).toHaveLength(4);
    expect(screen.queryByRole('slider', { name: /^reviewer / })).not.toBeInTheDocument();
    expect(screen.queryByTestId('tl-line-reviewer-complex')).not.toBeInTheDocument();
  });

  it('labels each shared line with its bar, and with AA points only when a best AA score is known', () => {
    const shared: SelectorLadders = { coder: { ...LADDERS.coder }, reviewer: { ...LADDERS.coder } };
    const { container, rerenderWith } = renderChart({ ladders: shared, separate: false });
    const svgText = () => container.querySelector('svg')!.textContent;

    // complex 0.84 × 59.4 = 49.9 AA points.
    expect(svgText()).toContain('49.9');
    expect(svgText()).toContain('0.84');

    rerenderWith({ bestAAScore: 0 });
    expect(svgText()).not.toContain('49.9');
    expect(svgText()).toContain('0.84');
  });

  it('dragging a coder handle moves only the coder ladder when separate', () => {
    const { props } = renderChart();

    drag('coder complex bar', 200, 170);

    expect(props.onChange).toHaveBeenCalledTimes(1);
    const next = lastChange(props.onChange);
    expect(next.coder.complex).toBeCloseTo(snapBar(0.84 + travel(-30)), 9);
    expect(next.coder.complex).not.toBeCloseTo(0.84, 3);
    expect(next.reviewer).toEqual(LADDERS.reviewer);
  });

  it('dragging with one ladder moves both roles', () => {
    const shared: SelectorLadders = { coder: { ...LADDERS.coder }, reviewer: { ...LADDERS.coder } };
    const { props } = renderChart({ ladders: shared, separate: false });

    drag('coder complex bar', 200, 170);

    const next = lastChange(props.onChange);
    const want = snapBar(0.84 + travel(-30));
    expect(next.coder.complex).toBeCloseTo(want, 9);
    expect(next.reviewer.complex).toBeCloseTo(want, 9);
  });

  it('every stacked handle of the default reviewer ladder is its own reachable slider', () => {
    const { props } = renderChart();
    const handles = TIERS.map((t) => screen.getByRole('slider', { name: `reviewer ${t} bar` }));

    const ys = handles.map((h) => translate(h)[1]);
    expect(new Set(handles.map((h) => h.getAttribute('transform'))).size).toBe(4);
    const sorted = [...ys].sort((a, b) => a - b);
    for (let i = 1; i < sorted.length; i++) expect(sorted[i] - sorted[i - 1]).toBeGreaterThanOrEqual(CHART_LAYOUT.handleH - 1e-6);

    // A grab on a fanned handle, off its line, with no travel leaves every bar alone.
    handles.forEach((h, i) => {
      const at = ys[i] + CHART_LAYOUT.handleH / 2;
      fireEvent.pointerDown(h, { pointerId: 1, clientY: at, button: 0 });
      fireEvent.pointerMove(h, { pointerId: 1, clientY: at });
      fireEvent.pointerUp(h, { pointerId: 1, clientY: at });
    });
    expect(props.onChange).not.toHaveBeenCalled();

    drag('reviewer complex bar', 200, 192);

    const next = lastChange(props.onChange);
    expect(next.reviewer.complex).toBeCloseTo(snapBar(0.9 + travel(-8)), 9);
    expect(next.reviewer.complex).toBeGreaterThan(0.9);
    for (const t of ['simple', 'moderate', 'critical'] as const) expect(next.reviewer[t]).toBe(LADDERS.reviewer[t]);
    expect(next.coder).toEqual(LADDERS.coder);
  });

  it('steps a bar from the keyboard', () => {
    const { props } = renderChart();
    const handle = screen.getByRole('slider', { name: 'coder complex bar' });
    expect(handle).toHaveAttribute('tabindex', '0');

    fireEvent.keyDown(handle, { key: 'ArrowUp' });

    const next = lastChange(props.onChange);
    expect(next.coder.complex).toBeCloseTo(0.845, 9);
    expect(next.reviewer).toEqual(LADDERS.reviewer);
  });

  it('never drags a bar below the floor', () => {
    const { props } = renderChart({ floor: 0.6 });
    expect(screen.getByRole('slider', { name: 'coder simple bar' })).toHaveAttribute('aria-valuemin', '0.6');

    drag('coder simple bar', 200, 400);

    expect(lastChange(props.onChange).coder.simple).toBeCloseTo(0.6, 9);
  });

  it('keeps the handle mounted across a drag', () => {
    const { props, rerenderWith } = renderChart();
    const handle = screen.getByRole('slider', { name: 'coder complex bar' });
    const before = handle.getAttribute('transform');

    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 200, button: 0 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 170 });
    rerenderWith({ ladders: lastChange(props.onChange) });

    const moved = screen.getByRole('slider', { name: 'coder complex bar' });
    expect(moved).toBe(handle);
    expect(moved.getAttribute('transform')).not.toBe(before);

    // The next move still counts from the grab, not from the re-rendered bar.
    fireEvent.pointerMove(moved, { pointerId: 1, clientY: 160 });
    fireEvent.pointerUp(moved, { pointerId: 1, clientY: 160 });
    expect(lastChange(props.onChange).coder.complex).toBeCloseTo(snapBar(0.84 + travel(-40)), 9);
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toBe(handle);
  });
});
