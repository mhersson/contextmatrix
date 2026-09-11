import { useMemo, useRef } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorRole, SelectorTier } from '../../types';
import {
  BELOW_FLOOR_COLOR,
  ROLES,
  TIERS_DESC,
  TIER_COLOR,
  bandsFor,
  blendedPrice,
  formatBar,
  priorOf,
  railY,
  shortSlug,
  tierOf,
  usdPerMillion,
} from './ladder';
import { useLadderDrag } from './useLadderDrag';

export interface TierLadderProps {
  candidates: SelectorCandidate[];
  ladders: SelectorLadders;
  linked: boolean;
  onLinkedChange: (linked: boolean) => void;
  onChange: (next: SelectorLadders) => void;
  /** The price headroom being edited; NaN while the field is emptied. */
  headroom: number;
  onHeadroomChange: (headroom: number) => void;
  /** Catalog quality floor: no bar may be dragged below it. */
  floor: number;
  blacklist: ReadonlySet<string>;
  /** Slugs that are the pick at some rung, per role. */
  picks: Record<SelectorRole, ReadonlySet<string>>;
  /** Slugs holding a review panel seat at some tier (reviewer column only). */
  seats: ReadonlySet<string>;
  /** Panel-head meta from the API: candidate count and catalog freshness. */
  meta: ReactNode;
  /** Right-click on a pill: the slug and the pointer's viewport position. Unset leaves the browser menu alone. */
  onModelMenu?: (slug: string, x: number, y: number) => void;
}

const TICKS = [0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1];

/** Pills within 2.4% of each other step across three lanes so labels stay legible. */
const LANE_GAP_PCT = 2.4;
const LANES = 3;
/** Reviewer pills start further right to leave room for that column's handles. */
const LANE_START_PCT: Record<SelectorRole, number> = { coder: 3, reviewer: 18 };
const LANE_STEP_PCT: Record<SelectorRole, number> = { coder: 30, reviewer: 27 };

interface PillLayout {
  c: SelectorCandidate;
  prior: number;
  y: number;
  left: number;
}

function layoutPills(candidates: SelectorCandidate[], role: SelectorRole): PillLayout[] {
  const sorted = [...candidates].sort((a, b) => priorOf(b, role) - priorOf(a, role));
  let lastY = -99;
  let lane = 0;
  return sorted.map((c) => {
    const prior = priorOf(c, role);
    const y = railY(prior);
    lane = y - lastY < LANE_GAP_PCT ? (lane + 1) % LANES : 0;
    lastY = y;
    return { c, prior, y, left: LANE_START_PCT[role] + lane * LANE_STEP_PCT[role] };
  });
}

export function TierLadder({
  candidates,
  ladders,
  linked,
  onLinkedChange,
  onChange,
  headroom,
  onHeadroomChange,
  floor,
  blacklist,
  picks,
  seats,
  meta,
  onModelMenu,
}: TierLadderProps) {
  const railRef = useRef<HTMLDivElement>(null);
  const { dragging, handleProps } = useLadderDrag({ ladders, linked, floor, railRef, onChange });

  const columns = useMemo(
    () =>
      ROLES.map((role) => ({
        role,
        bands: bandsFor(ladders[role], candidates, role),
        pills: layoutPills(candidates, role),
      })),
    [candidates, ladders],
  );

  const handleLabel = (role: SelectorRole, tier: SelectorTier): string => {
    const own = ladders[role][tier];
    const other = ladders[role === 'coder' ? 'reviewer' : 'coder'][tier];
    if (role === 'coder' && Math.abs(own - other) > 1e-9) return `${formatBar(own)} · ${formatBar(other)}`;
    return formatBar(own);
  };

  return (
    <section className="apd-panel" style={{ '--apd-acc': 'var(--aqua)' } as CSSProperties}>
      <div className="apd-panel-head">
        <h2 className="apd-panel-title">Ladders</h2>
        <div className="apd-panel-meta">
          <span>{meta}</span>
          <label className="tl-headroom">
            price headroom
            <input
              type="number"
              min={1}
              step={0.1}
              value={Number.isNaN(headroom) ? '' : headroom}
              aria-label="Price headroom"
              aria-invalid={Number.isFinite(headroom) && headroom >= 1 ? undefined : true}
              onChange={(e) => onHeadroomChange(e.target.value === '' ? NaN : Number(e.target.value))}
            />
            ×
          </label>
          <button
            type="button"
            role="switch"
            aria-checked={linked}
            aria-label="Link the coder and reviewer ladders"
            className={`tl-switch${linked ? ' on' : ''}`}
            onClick={() => onLinkedChange(!linked)}
          >
            <span className="tl-switch-knob" aria-hidden="true" />
            linked
          </button>
        </div>
      </div>
      <div className="tl-ladder-wrap">
        <div className="tl-ladder-head">
          <div className="tl-rail-h">bars · drag</div>
          <div className="tl-role-h">
            <span className="tl-role-tag coder">coder</span> coding index <small>· subtask coders, fix coders</small>
          </div>
          <div className="tl-role-h">
            <span className="tl-role-tag reviewer">reviewer</span> intelligence index <small>· panels, seats, judges</small>
          </div>
        </div>
        <div className="tl-ladder" ref={railRef} data-testid="tl-ladder">
          <div className="tl-rail" aria-hidden="true">
            {TICKS.map((v) => (
              <div key={v} className="tl-tick" style={{ top: `${railY(v)}%` }}>
                {v.toFixed(1)}
              </div>
            ))}
          </div>
          {columns.map(({ role, bands, pills }) => (
            <div key={role} className={`tl-col tl-col-${role}`} data-testid={`tl-col-${role}`}>
              {bands.map((b, i) => {
                const top = railY(b.top);
                const height = Math.max(0, railY(b.bottom) - top);
                const color = b.tier ? TIER_COLOR[b.tier] : BELOW_FLOOR_COLOR;
                return (
                  <div
                    key={b.tier ?? 'floor'}
                    className={`tl-band${i === 0 ? ' first' : ''}${b.tier ? '' : ' floor'}`}
                    style={{ top: `${top}%`, height: `${height}%`, '--band-c': color } as CSSProperties}
                    data-testid={`tl-band-${role}-${b.tier ?? 'floor'}`}
                  >
                    {height >= 4 && (
                      <span className="tl-band-label">
                        {b.tier ?? 'below floor'}
                        {b.tier && (
                          <small>
                            {b.count} model{b.count === 1 ? '' : 's'}
                          </small>
                        )}
                      </span>
                    )}
                  </div>
                );
              })}
              {pills.map(({ c, prior, y, left }) => {
                const tier = tierOf(ladders[role], prior);
                const cls =
                  (picks[role].has(c.slug) ? ' picked' : '') +
                  (role === 'reviewer' && seats.has(c.slug) ? ' seat' : '') +
                  (blacklist.has(c.slug) ? ' banned' : '');
                return (
                  <span
                    key={c.slug}
                    className={`tl-dot${cls}`}
                    style={{ top: `${y}%`, left: `${left}%`, '--tier-c': tier ? TIER_COLOR[tier] : BELOW_FLOOR_COLOR } as CSSProperties}
                    title={`${c.slug} · ${role} prior ${prior.toFixed(3)} · ${usdPerMillion(blendedPrice(c))}${c.price_source === 'aa' ? ' (list price)' : ''} · ${tier ?? 'below floor'}${c.scored_from ? ` · scored from ${c.scored_from}` : ''}`}
                    data-testid={`tl-dot-${role}-${c.slug}`}
                    onContextMenu={
                      onModelMenu &&
                      ((e) => {
                        e.preventDefault();
                        onModelMenu(c.slug, e.clientX, e.clientY);
                      })
                    }
                  >
                    <i aria-hidden="true" />
                    <span className="tl-dot-n">{shortSlug(c.slug)}</span>
                    <span className="tl-dot-p">{prior.toFixed(3)}</span>
                  </span>
                );
              })}
              {TIERS_DESC.map((tier) => (
                <div
                  key={tier}
                  className={`tl-bar${dragging?.role === role && dragging.tier === tier ? ' dragging' : ''}`}
                  style={{ top: `${railY(ladders[role][tier])}%`, '--bar-c': TIER_COLOR[tier] } as CSSProperties}
                >
                  <div className="tl-bar-line" aria-hidden="true" />
                  <div
                    className="tl-handle"
                    role="slider"
                    tabIndex={0}
                    aria-label={`${role} ${tier} bar`}
                    aria-valuemin={floor}
                    aria-valuemax={1}
                    aria-valuenow={ladders[role][tier]}
                    aria-valuetext={handleLabel(role, tier)}
                    {...handleProps(role, tier)}
                  >
                    <span className="tl-grip" aria-hidden="true" />
                    {role === 'coder' && <span className="tl-tname">{tier}</span>}
                    <span className="tl-tval">{handleLabel(role, tier)}</span>
                  </div>
                </div>
              ))}
            </div>
          ))}
        </div>
        <div className="tl-legend">
          {TIERS_DESC.map((tier) => (
            <span key={tier}>
              <span className="tl-sw" style={{ '--sw-c': TIER_COLOR[tier] } as CSSProperties} aria-hidden="true" />
              {tier}
            </span>
          ))}
          <span>
            <span className="tl-sw" style={{ '--sw-c': BELOW_FLOOR_COLOR } as CSSProperties} aria-hidden="true" />
            below floor · never selected
          </span>
          <span className="tl-legend-note">
            filled pill = the pick at its rung · dashed = a panel seat · struck = blacklisted
            {onModelMenu && ' · right-click a pill to blacklist or delist'}
          </span>
        </div>
      </div>
    </section>
  );
}
