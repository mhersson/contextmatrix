import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { SelectorLadders } from '../../types';
import { LadderKpis } from './LadderKpis';
import { CANDIDATES, DEFAULT_BARS, previewFixture, seat } from './selector.fixtures';

const LADDERS: SelectorLadders = { coder: { ...DEFAULT_BARS }, reviewer: { ...DEFAULT_BARS } };

describe('LadderKpis', () => {
  it('counts reviewers clearing complex from the candidates and prices the picks from the preview', () => {
    render(<LadderKpis candidates={CANDIDATES} ladders={LADDERS} preview={previewFixture()} />);

    const reviewers = screen.getByTestId('tl-kpi-reviewers');
    expect(reviewers).toHaveTextContent('3');
    expect(reviewers).toHaveTextContent('bar 0.82');
    expect(reviewers).toHaveTextContent('of 4 candidates');

    const cheapest = screen.getByTestId('tl-kpi-cheapest');
    expect(cheapest).toHaveTextContent('$2.0/M');
    expect(cheapest).toHaveTextContent('cheap');

    const panel = screen.getByTestId('tl-kpi-panel');
    expect(panel).toHaveTextContent('$26.0/M');
    expect(panel).toHaveTextContent('cheap · pricey · mid');
    expect(panel.style.getPropertyValue('--apd-acc')).toBe('var(--orange)');

    expect(screen.getByTestId('tl-kpi-coder')).toHaveTextContent('$2.0/M');
  });

  it('turns the panel tile green when no seat walked and shows dashes without a preview', () => {
    const p = previewFixture();
    p.tiers.complex.panel = [seat('a/cheap', 2e-6, false), seat('a/mid', 4e-6, false), seat('b/pricey', 2e-5, false)];
    const { rerender } = render(<LadderKpis candidates={CANDIDATES} ladders={LADDERS} preview={p} />);
    expect(screen.getByTestId('tl-kpi-panel').style.getPropertyValue('--apd-acc')).toBe('var(--green)');

    rerender(<LadderKpis candidates={CANDIDATES} ladders={LADDERS} preview={null} />);
    expect(screen.getByTestId('tl-kpi-cheapest')).toHaveTextContent('—');
    expect(screen.getByTestId('tl-kpi-panel')).toHaveTextContent('—');
    expect(screen.getByTestId('tl-kpi-reviewers')).toHaveTextContent('3');
  });
});
