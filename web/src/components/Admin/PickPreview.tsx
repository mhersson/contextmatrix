import type { CSSProperties } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorPickReport, SelectorPreview, SelectorSeat, SelectorTier } from '../../types';
import { TIERS_DESC, TIER_COLOR, formatBar, panelPrice, priorOf, shortSlug, usdPerMillion } from './ladder';

interface PickPreviewProps {
  preview: SelectorPreview | null;
  pending: boolean;
  error: string | null;
  ladders: SelectorLadders;
  candidates: SelectorCandidate[];
  headroom: number;
}

function PickRow({ who, pr, tier }: { who: string; pr: SelectorPickReport; tier: SelectorTier }) {
  const p = pr.pick;
  if (!p.ok) {
    return (
      <div className="tl-pv-row">
        <span className="tl-pv-who">{who}</span>
        <span className="tl-pv-none">nothing clears any rung</span>
        <span />
      </div>
    );
  }
  const descended = p.met_tier !== tier;
  return (
    <div className="tl-pv-row">
      <span className="tl-pv-who">{who}</span>
      <span className="tl-pv-model">
        <span className="chip-pill tl-pv-chip">{descended ? `↓ ${p.met_tier}` : 'at bar'}</span>
        <span className="tl-pv-name">{p.model}</span>
        {p.source === 'favorite' && <span className="tl-pv-src">favorite</span>}
      </span>
      <span className="tl-pv-price">{usdPerMillion(p.price_per_tok)}</span>
    </div>
  );
}

function SeatRow({ seats, tier }: { seats: SelectorSeat[]; tier: SelectorTier }) {
  return (
    <div className="tl-pv-row">
      <span className="tl-pv-who">panel ×3</span>
      <span className="tl-seats">
        {seats.length === 0 && <span className="tl-pv-none">no seat can be filled</span>}
        {seats.map((s, i) => (
          <span
            key={`${s.pick.model}-${i}`}
            className={`tl-seat${s.walked ? ' walk' : ''}${s.pick.duplicate ? ' dup' : ''}`}
            data-testid={`tl-seat-${tier}-${i}`}
          >
            <b>{i + 1}</b>
            {shortSlug(s.pick.model)}
            <span className="tl-seat-px">{usdPerMillion(s.pick.price_per_tok)}</span>
            {s.walked && <span className="tl-walked">walked</span>}
            {s.pick.duplicate && <span className="tl-walked">duplicate</span>}
          </span>
        ))}
      </span>
      <span className="tl-pv-price">{seats.length > 0 ? usdPerMillion(panelPrice(seats)) : ''}</span>
    </div>
  );
}

export function PickPreview({ preview, pending, error, ladders, candidates, headroom }: PickPreviewProps) {
  const clearing = (tier: SelectorTier, role: 'coder' | 'reviewer') =>
    candidates.filter((c) => priorOf(c, role) >= ladders[role][tier]).length;

  return (
    <section className="apd-panel" style={{ '--apd-acc': 'var(--purple)' } as CSSProperties}>
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">Pick preview</h2>
        <span className="apd-panel-meta">
          <span>{`headroom ${headroom}× · favorites and blacklist applied`}</span>
          {pending && <span className="tl-pending">computing…</span>}
        </span>
      </div>
      <div className="apd-panel-body tl-pv" aria-busy={pending} data-testid="tl-preview">
        {error && (
          <p className="tl-pv-error" role="alert">
            {error}
          </p>
        )}
        {preview === null ? (
          <div className="apd-panel-empty">{error ? 'No preview yet.' : 'Waiting for the first preview…'}</div>
        ) : (
          TIERS_DESC.map((tier) => {
            const t = preview.tiers[tier];
            return (
              <div key={tier} className="tl-pv-tier" style={{ '--tier-c': TIER_COLOR[tier] } as CSSProperties} data-testid={`tl-pv-${tier}`}>
                <div className="tl-pv-tier-head">
                  <span className="tl-pv-tier-name">{tier}</span>
                  <span className="tl-pv-tier-bar">
                    {`bar c ${formatBar(ladders.coder[tier])} · r ${formatBar(ladders.reviewer[tier])} · ${clearing(tier, 'coder')} coders · ${clearing(tier, 'reviewer')} reviewers clear it`}
                  </span>
                </div>
                <PickRow who="coder" pr={t.coder} tier={tier} />
                <PickRow who="reviewer" pr={t.reviewer} tier={tier} />
                <SeatRow seats={t.panel} tier={tier} />
              </div>
            );
          })
        )}
      </div>
      <p className="tl-hint">
        Computed with the selection rules the agent runs, from the current catalog, the backend favorites and the blacklist. Single picks are
        vendor-blind with no in-run exclusions. A seat marked <em>walked</em> re-anchored the price band because nothing cheaper was left at
        that rung. Project favorites and the agent&apos;s own headroom setting are not applied here.
      </p>
    </section>
  );
}
