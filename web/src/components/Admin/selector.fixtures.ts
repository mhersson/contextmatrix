import type { SelectorCandidate, SelectorPick, SelectorPickReport, SelectorPreview, SelectorSeat, SelectorTier, TierBars } from '../../types';

export const DEFAULT_BARS: TierBars = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function cand(slug: string, coder: number, reviewer: number, price: number): SelectorCandidate {
  return { slug, creator: slug.split('/')[0], coder_prior: coder, reviewer_prior: reviewer, prompt_price_per_tok: price / 2, completion_price_per_tok: price / 2, context_window: 200000 };
}

/** The same four models and blended prices as the Go preview fixture. */
export const CANDIDATES: SelectorCandidate[] = [cand('a/cheap', 0.9, 0.85, 2e-6), cand('a/mid', 0.8, 0.86, 4e-6), cand('b/pricey', 0.83, 0.84, 2e-5), cand('c/weak', 0.7, 0.7, 1e-6)];

export function pick(model: string, role: 'coder' | 'reviewer', requested: SelectorTier, met: SelectorTier | '', price: number, extra: Partial<SelectorPick> = {}): SelectorPickReport {
  return {
    pick: { model, context_window: 200000, role, requested_tier: requested, met_tier: met, requested_bar: DEFAULT_BARS[requested], prior: 0.85, has_prior: true, source: 'auto', duplicate: false, ok: model !== '', price_per_tok: price, ...extra },
    report: { rung: met, bar: met ? DEFAULT_BARS[met] : 0, pool: [], filtered_out: [] },
  };
}

export function seat(model: string, price: number, walked: boolean, tier: SelectorTier = 'complex'): SelectorSeat {
  return { ...pick(model, 'reviewer', tier, tier, price), walked };
}

/** Every tier picks a/cheap; the panel is a/cheap, b/pricey (walked), a/mid (walked); critical's reviewer descended and its panel is empty. */
export function previewFixture(): SelectorPreview {
  const tiers = {} as SelectorPreview['tiers'];
  for (const t of ['simple', 'moderate', 'complex', 'critical'] as SelectorTier[]) {
    tiers[t] = { coder: pick('a/cheap', 'coder', t, t, 2e-6), reviewer: pick('a/cheap', 'reviewer', t, t, 2e-6), panel: [seat('a/cheap', 2e-6, false, t), seat('b/pricey', 2e-5, true, t), seat('a/mid', 4e-6, true, t)] };
  }
  tiers.critical.reviewer = pick('a/cheap', 'reviewer', 'critical', 'complex', 2e-6);
  tiers.critical.panel = [];
  return { tiers };
}
