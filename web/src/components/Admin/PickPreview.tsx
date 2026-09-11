import type { CSSProperties, MouseEvent } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorPickReport, SelectorPreview, SelectorSeat, SelectorTier } from '../../types';
import { TIERS_DESC, TIER_COLOR, formatBar, panelHasListPrice, panelPrice, priorOf, shortSlug, usdPerMillion } from './ladder';

interface PickPreviewProps {
  preview: SelectorPreview | null;
  pending: boolean;
  /** No preview can be requested: the inputs the server needs are missing. */
  disabled: boolean;
  error: string | null;
  ladders: SelectorLadders;
  candidates: SelectorCandidate[];
  headroom: number;
  /** Right-click on a pick or a seat: the slug and the pointer's viewport position. Unset leaves the browser menu alone. */
  onModelMenu?: ModelMenuHandler;
}

export type ModelMenuHandler = (slug: string, x: number, y: number) => void;

/** onContextMenu for one model, or undefined so the browser menu stays. */
function contextMenuFor(slug: string, onModelMenu: ModelMenuHandler | undefined) {
  if (!onModelMenu) return undefined;
  return (e: MouseEvent) => {
    e.preventDefault();
    onModelMenu(slug, e.clientX, e.clientY);
  };
}

const LIST_PRICE_TITLE = 'Artificial Analysis list price: the gateway publishes no price for this model';

/** A dollar figure, marked "list" when it is the AA list price and not the gateway's. */
function Price({ perTok, list }: { perTok: number; list: boolean }) {
  return (
    <>
      {usdPerMillion(perTok)}
      {list && (
        <abbr className="tl-pv-list" title={LIST_PRICE_TITLE}>
          list
        </abbr>
      )}
    </>
  );
}

function PickRow({ who, pr, tier, onModelMenu }: { who: string; pr: SelectorPickReport; tier: SelectorTier; onModelMenu?: ModelMenuHandler }) {
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
        <span className="tl-pv-name" onContextMenu={contextMenuFor(p.model, onModelMenu)}>
          {p.model}
        </span>
        {p.source === 'favorite' && <span className="tl-pv-src">favorite</span>}
      </span>
      <span className="tl-pv-price">
        <Price perTok={p.price_per_tok} list={p.price_source === 'aa'} />
      </span>
    </div>
  );
}

function SeatRow({ seats, tier, onModelMenu }: { seats: SelectorSeat[]; tier: SelectorTier; onModelMenu?: ModelMenuHandler }) {
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
            onContextMenu={s.pick.ok ? contextMenuFor(s.pick.model, onModelMenu) : undefined}
          >
            <b>{i + 1}</b>
            {shortSlug(s.pick.model)}
            <span className="tl-seat-px">
              <Price perTok={s.pick.price_per_tok} list={s.pick.price_source === 'aa'} />
            </span>
            {s.walked && <span className="tl-walked">walked</span>}
            {s.pick.duplicate && <span className="tl-walked">duplicate</span>}
          </span>
        ))}
      </span>
      <span className="tl-pv-price">{seats.length > 0 && <Price perTok={panelPrice(seats)} list={panelHasListPrice(seats)} />}</span>
    </div>
  );
}

export function PickPreview({ preview, pending, disabled, error, ladders, candidates, headroom, onModelMenu }: PickPreviewProps) {
  const clearing = (tier: SelectorTier, role: 'coder' | 'reviewer') =>
    candidates.filter((c) => priorOf(c, role) >= ladders[role][tier]).length;

  const busy = pending && !disabled;
  const empty = error ? 'No preview yet.' : disabled ? 'Preview needs the candidate catalog.' : 'Waiting for the first preview…';

  return (
    <section className="apd-panel" style={{ '--apd-acc': 'var(--purple)' } as CSSProperties}>
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">Pick preview</h2>
        <span className="apd-panel-meta">
          <span>{`headroom ${Number.isFinite(headroom) ? headroom : '?'}× · favorites and blacklist applied`}</span>
          {busy && <span className="tl-pending">computing…</span>}
        </span>
      </div>
      <div className="apd-panel-body tl-pv" aria-busy={busy} data-testid="tl-preview">
        {error && (
          <p className="tl-pv-error" role="alert">
            {error}
          </p>
        )}
        {preview === null ? (
          <div className="apd-panel-empty">{empty}</div>
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
                <PickRow who="coder" pr={t.coder} tier={tier} onModelMenu={onModelMenu} />
                <PickRow who="reviewer" pr={t.reviewer} tier={tier} onModelMenu={onModelMenu} />
                <SeatRow seats={t.panel} tier={tier} onModelMenu={onModelMenu} />
              </div>
            );
          })
        )}
      </div>
      <p className="tl-hint">
        Computed with the selection rules the agent runs, from the current catalog, the backend favorites and the blacklist. Single picks are
        vendor-blind with no in-run exclusions. A seat marked <em>walked</em> re-anchored the price band because nothing cheaper was left at
        that rung. Project favorites are not applied here.
      </p>
    </section>
  );
}
