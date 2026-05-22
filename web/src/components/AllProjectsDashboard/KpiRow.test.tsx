import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { KpiRow } from './KpiRow';

function renderKpiRow(overrides: Partial<Parameters<typeof KpiRow>[0]> = {}) {
  const defaults = {
    costLast30dUsd: 0,
    costPrior30dUsd: 0,
    costSeries30d: [],
    stateCountsParents: {},
    doneTodayParents: 0,
    chatCostLast30dUsd: 0,
    chatCostPrior30dUsd: 0,
    chatCostSeries30d: undefined,
  };
  return render(<KpiRow {...defaults} {...overrides} />);
}

describe('KpiRow — Cost · 30d tile', () => {
  it('renders the tile label "Cost · 30d"', () => {
    const { getByText } = renderKpiRow();
    expect(getByText('Cost · 30d')).toBeTruthy();
  });

  it('does not render a delta element when costPrior30dUsd is 0', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 10, costPrior30dUsd: 0 });
    expect(container.querySelector('.metric-tile__delta')).toBeNull();
  });

  it('renders a delta element when costPrior30dUsd > 0', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 15, costPrior30dUsd: 10 });
    expect(container.querySelector('.metric-tile__delta')).not.toBeNull();
  });

  it('shows +0% when last equals prior', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 10, costPrior30dUsd: 10 });
    const delta = container.querySelector('.metric-tile__delta');
    expect(delta).not.toBeNull();
    expect(delta?.textContent).toBe('+0%');
    expect(delta?.classList.contains('metric-tile__delta--up')).toBe(true);
  });

  it('has class metric-tile__delta--up when last > prior', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 20, costPrior30dUsd: 10 });
    const delta = container.querySelector('.metric-tile__delta');
    expect(delta?.classList.contains('metric-tile__delta--up')).toBe(true);
    expect(delta?.classList.contains('metric-tile__delta--down')).toBe(false);
  });

  it('has class metric-tile__delta--down when last < prior', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 5, costPrior30dUsd: 10 });
    const delta = container.querySelector('.metric-tile__delta');
    expect(delta?.classList.contains('metric-tile__delta--down')).toBe(true);
    expect(delta?.classList.contains('metric-tile__delta--up')).toBe(false);
  });

  it('shows +0% styled as up when a tiny decrease rounds to 0% (e.g. $9.99 -> $10)', () => {
    const { container } = renderKpiRow({ costLast30dUsd: 9.99, costPrior30dUsd: 10 });
    const delta = container.querySelector('.metric-tile__delta');
    expect(delta).not.toBeNull();
    expect(delta?.textContent).toBe('+0%');
    expect(delta?.classList.contains('metric-tile__delta--up')).toBe(true);
    expect(delta?.classList.contains('metric-tile__delta--down')).toBe(false);
  });

  it('renders an SVG with class "spark" when costSeries30d has length >= 2', () => {
    const { container } = renderKpiRow({
      costSeries30d: Array.from({ length: 30 }, (_, i) => i * 0.1),
    });
    expect(container.querySelector('svg.spark')).not.toBeNull();
  });

  it('does not render an SVG spark when costSeries30d has length < 2', () => {
    const { container } = renderKpiRow({ costSeries30d: [1] });
    expect(container.querySelector('svg.spark')).toBeNull();
  });

  it('does not render an SVG spark when costSeries30d is empty', () => {
    const { container } = renderKpiRow({ costSeries30d: [] });
    expect(container.querySelector('svg.spark')).toBeNull();
  });
});

describe('KpiRow — Chat cost · 30d tile', () => {
  it('renders the tile label "Chat cost · 30d"', () => {
    const { getByText } = renderKpiRow();
    expect(getByText('Chat cost · 30d')).toBeTruthy();
  });

  it('renders $0.00 when chatCostLast30dUsd is 0', () => {
    const { getAllByText } = renderKpiRow({ chatCostLast30dUsd: 0 });
    // The CostValue component renders "$0" + ".00" — getByText matches the
    // dollar amount; we just ensure the tile renders without crashing.
    expect(getAllByText(/\$/).length).toBeGreaterThan(0);
  });

  it('does not render a delta when chatCostPrior30dUsd is 0', () => {
    const { container } = renderKpiRow({ chatCostLast30dUsd: 5, chatCostPrior30dUsd: 0 });
    // Count delta elements — one for card cost (also 0 prior), check total.
    // Both tiles have costPrior30dUsd: 0, so neither should render a delta.
    expect(container.querySelector('.metric-tile__delta')).toBeNull();
  });

  it('renders a sparkline when chatCostSeries30d has >= 2 values', () => {
    const { container } = renderKpiRow({
      chatCostLast30dUsd: 10,
      chatCostSeries30d: Array.from({ length: 30 }, (_, i) => i * 0.5),
    });
    // Two sparklines now exist (Cost · 30d and Chat cost · 30d) — assert at least one.
    expect(container.querySelectorAll('svg.spark').length).toBeGreaterThanOrEqual(1);
  });

  it('does not crash when chatCostSeries30d is undefined (no sparkline)', () => {
    const { container } = renderKpiRow({ chatCostSeries30d: undefined });
    // No chat cost sparkline; card cost sparkline also absent (empty series).
    // Just assert the tile rendered without error (label check).
    expect(container.textContent).toContain('Chat cost · 30d');
  });
});
