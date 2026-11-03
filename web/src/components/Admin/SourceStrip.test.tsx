import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import type { SelectorCandidatesResponse } from '../../types';
import { SourceStrip } from './SourceStrip';

function catalog(over: Partial<SelectorCandidatesResponse> = {}): SelectorCandidatesResponse {
  const c = { slug: 'a/x', creator: 'a', coder_prior: 0.9, reviewer_prior: 0.9, prompt_price_per_tok: 1e-6, completion_price_per_tok: 1e-6, context_window: 1, price_source: 'gateway' as const, scored_from: '', prior_source: 'aa' as const };
  return {
    candidates: [c, { ...c, slug: 'a/y' }, { ...c, slug: 'a/z' }],
    favorites: [],
    blacklist: [],
    quality_floor: 0.65,
    catalog_refreshed_at: new Date().toISOString(),
    reasoning_effort: '',
    intelligence_index_version: '5',
    aa_scored_models: 411,
    served_scored_models: 273,
    best_aa_score: 59.4,
    ...over,
  };
}

describe('SourceStrip', () => {
  it('states the API numbers', () => {
    render(<SourceStrip catalog={catalog()} ready loading={false} />);
    const strip = within(screen.getByTestId('tl-source'));
    for (const n of ['411', '273', '3', '59.4']) expect(strip.getByText(n, { selector: 'b' })).toBeInTheDocument();
    expect(strip.getByText(/ v5$/, { selector: 'b' })).toBeInTheDocument();
    expect(strip.getByText(/\b0\.65$/)).toBeInTheDocument();
  });

  it('claims no version when AA sent none', () => {
    render(<SourceStrip catalog={catalog({ intelligence_index_version: '' })} ready loading={false} />);
    expect(screen.getByTestId('tl-source').textContent).not.toMatch(/\bv\d/);
  });

  it('omits the normalizer when there is no best score', () => {
    render(<SourceStrip catalog={catalog({ best_aa_score: 0 })} ready loading={false} />);
    expect(screen.getByTestId('tl-source').textContent).not.toMatch(/prior 1\.00/);
  });

  it('claims no counts while loading or unavailable', () => {
    for (const loading of [true, false]) {
      const { unmount } = render(<SourceStrip catalog={catalog()} ready={false} loading={loading} />);
      const text = screen.getByTestId('tl-source').textContent ?? '';
      for (const n of ['411', '273', '59.4', '0.65']) expect(text).not.toContain(n);
      unmount();
    }
  });

  it('links to Artificial Analysis', () => {
    render(<SourceStrip catalog={catalog()} ready={false} loading={false} />);
    const link = screen.getByRole('link', { name: /Artificial Analysis/ });
    expect(link).toHaveAttribute('href', 'https://artificialanalysis.ai/');
    expect(link.getAttribute('rel')).toContain('noopener');
  });
});
