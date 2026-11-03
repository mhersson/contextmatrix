import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { SelectorCandidate, SelectorLadders, SelectorPickReport, SelectorPoolEntry, SelectorRole, SelectorSeat, SelectorTierPreview } from '../../types';
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
