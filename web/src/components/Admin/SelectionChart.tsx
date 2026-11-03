import { useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorRole, SelectorTier, SelectorTierPreview } from '../../types';
import { usePaneWidth } from '../Blueprint/usePaneWidth';
import { CHART_LAYOUT, formatHandle, placeLabels, priceScale, qualityScale, stackHandles } from './chart';
import { ROLES, blendedPrice, clearedTiers, formatBar, money, priorOf, shortSlug } from './ladder';
import { useLadderDrag } from './useLadderDrag';

export interface SelectionChartProps {
  /** All candidates: the scales never change with the filter. */
  candidates: SelectorCandidate[];
  /** Display-only provider filter. */
  visible: (c: SelectorCandidate) => boolean;
  ladders: SelectorLadders;
  /** Two ladders on screen. */
  separate: boolean;
  floor: number;
  /** The normalizer behind prior 1.00; 0 means no AA-point axis or AA score anywhere. */
  bestAAScore: number;
  indexVersion: string;
  selectedTier: SelectorTier;
  /** The preview of the selected tier. */
  tierPreview: SelectorTierPreview | null;
  /** The headroom the shown preview used. */
  appliedHeadroom: number | null;
  blacklist: ReadonlySet<string>;
  onChange: (next: SelectorLadders) => void;
  /** Right-click on a dot: the slug and the pointer's viewport position. Unset leaves the browser menu alone. */
  onModelMenu?: (slug: string, x: number, y: number) => void;
}

const FALLBACK_WIDTH = 760;
/** Narrower panes scale this viewBox down instead of squeezing the plot. */
const MIN_WIDTH = 420;
const LABEL_GAP = 11;
/** placeLabels' line height: label baselines sit at least this far apart. */
const LABEL_LINE = 12;

type Mark = 'coder-pick' | 'reviewer-pick' | 'seat' | 'blacklisted';

interface Picks {
  coder: string | null;
  reviewer: string | null;
  seats: ReadonlySet<string>;
}

/** One role's prior of a split candidate, or the prior both roles share. */
interface Dot {
  id: string;
  c: SelectorCandidate;
  roles: readonly SelectorRole[];
  cx: number;
  cy: number;
  marks: Mark[];
}

interface DotStyle {
  fill: string;
  stroke: string;
  r: number;
  dash?: string;
}

interface Tip {
  id: string;
  x: number;
  y: number;
}

function picksOf(tp: SelectorTierPreview | null): Picks {
  if (!tp) return { coder: null, reviewer: null, seats: new Set() };
  return {
    coder: tp.coder.pick.ok ? tp.coder.pick.model : null,
    reviewer: tp.reviewer.pick.ok ? tp.reviewer.pick.model : null,
    seats: new Set(tp.panel.filter((s) => s.pick.ok).map((s) => s.pick.model)),
  };
}

function marksOf(slug: string, roles: readonly SelectorRole[], picks: Picks, blacklisted: boolean): Mark[] {
  const marks: Mark[] = [];
  if (roles.includes('coder') && picks.coder === slug) marks.push('coder-pick');
  if (roles.includes('reviewer') && picks.reviewer === slug) marks.push('reviewer-pick');
  if (roles.includes('reviewer') && picks.seats.has(slug)) marks.push('seat');
  if (blacklisted) marks.push('blacklisted');
  return marks;
}

function dotStyle(marks: readonly Mark[]): DotStyle {
  const s: DotStyle = { fill: 'var(--grey2)', stroke: 'var(--grey2)', r: 5 };
  if (marks.includes('seat')) Object.assign(s, { stroke: 'var(--tl-reviewer)', r: 7, dash: '3 2' });
  if (marks.includes('reviewer-pick')) Object.assign(s, { fill: 'var(--tl-reviewer)', stroke: 'var(--tl-reviewer)', r: 7, dash: undefined });
  if (marks.includes('coder-pick')) {
    Object.assign(s, { stroke: 'var(--tl-coder)', r: 8, dash: undefined });
    if (!marks.includes('reviewer-pick')) s.fill = 'var(--tl-coder)';
  }
  return s;
}

/** Pushes baselines lifted above min back down, top first, keeping them a line apart. */
function keepBelow(baselines: number[], min: number): number[] {
  const out = [...baselines];
  let prev = -Infinity;
  for (const i of out.map((_, i) => i).sort((a, b) => out[a] - out[b])) {
    out[i] = Math.max(out[i], min, prev + LABEL_LINE);
    prev = out[i];
  }
  return out;
}

function tierList(tiers: SelectorTier[]): string {
  return tiers.length > 0 ? tiers.join(', ') : 'no tier';
}

function formatHeadroom(h: number): string {
  return String(Number(h.toFixed(2)));
}

export function SelectionChart({
  candidates,
  visible,
  ladders,
  separate,
  floor,
  bestAAScore,
  indexVersion,
  selectedTier,
  tierPreview,
  appliedHeadroom,
  blacklist,
  onChange,
  onModelMenu,
}: SelectionChartProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const width = Math.max(MIN_WIDTH, usePaneWidth(wrapRef) ?? FALLBACK_WIDTH);
  const [tip, setTip] = useState<Tip | null>(null);

  const { height, top, handleW, handleH } = CHART_LAYOUT;
  const x0 = CHART_LAYOUT.left;
  const x1 = width - CHART_LAYOUT.right;
  const bottom = height - CHART_LAYOUT.bottom;
  const px = priceScale(candidates.map((c) => blendedPrice(c) * 1e6), x0, x1);
  const q = qualityScale(floor, top, bottom);
  const y = (prior: number): number => q.y(Number.isFinite(prior) ? prior : q.lo);
  const clampX = (v: number): number => (Number.isNaN(v) ? x0 : Math.min(x1, Math.max(x0, v)));
  const hasAA = bestAAScore > 0;

  // Client px to SVG px, then SVG px to prior units; down is a lower bar.
  const valueDelta = (dy: number, rect: DOMRect): number => -((dy * height) / rect.height) * ((q.hi - q.lo) / (bottom - top));
  const { dragging, handleProps } = useLadderDrag({ ladders, linked: !separate, floor, targetRef: svgRef, valueDelta, onChange });
  const handleBounds: [number, number] = [top + handleH / 2, bottom - handleH / 2];
  const handleRoles: SelectorRole[] = separate ? ROLES : ['coder'];
  const tierSets = handleRoles.map((role) => ({
    role,
    color: separate ? `var(--tl-${role})` : 'var(--tl-shared)',
    slots: stackHandles(ladders[role], y, handleH, handleBounds),
  }));
  // One ladder: each line names its tier and bar. Lowest line first, so a label on a coinciding higher line lifts.
  const lineLabels = separate
    ? []
    : [...tierSets[0].slots].sort((a, b) => b.lineY - a.lineY).map((s) => ({ tier: s.tier, bar: ladders.coder[s.tier], lineY: s.lineY }));
  const lineLabelYs = keepBelow(placeLabels(lineLabels.map((l) => ({ x: x0, y: l.lineY - 8 })), LABEL_LINE), top + LABEL_LINE);

  const picks = picksOf(tierPreview);
  const shown = candidates.filter(visible);
  const dots: Dot[] = shown.flatMap((c) => {
    const cx = px.x(blendedPrice(c) * 1e6);
    const listed = blacklist.has(c.slug);
    const parts: [string, readonly SelectorRole[]][] =
      c.coder_prior !== c.reviewer_prior
        ? [
            [`${c.slug}-coder`, ['coder']],
            [`${c.slug}-reviewer`, ['reviewer']],
          ]
        : [[c.slug, ROLES]];
    return parts.map(([id, roles]) => ({ id, c, roles, cx, cy: y(priorOf(c, roles[0])), marks: marksOf(c.slug, roles, picks, listed) }));
  });
  const links = shown.filter((c) => c.coder_prior !== c.reviewer_prior);

  // Lowest dots first, so higher labels are the ones that lift.
  const labelled = dots.filter((d) => d.marks.some((m) => m !== 'blacklisted')).sort((a, b) => b.cy - a.cy);
  const baselines = placeLabels(labelled.map((d) => ({ x: d.cx, y: d.cy })));

  const bands =
    tierPreview && appliedHeadroom !== null
      ? ROLES.flatMap((role) => {
          const rep = tierPreview[role].report;
          if (rep.pool.length === 0) return [];
          const anchor = Math.min(...rep.pool.map((e) => e.price_per_tok)) * 1e6;
          const left = clampX(px.x(anchor));
          const right = clampX(px.x(anchor * appliedHeadroom));
          const barY = y(rep.bar);
          return [{ role, anchor, bar: rep.bar, rung: rep.rung || selectedTier, x: left, width: Math.max(2, right - left), height: Math.max(0, barY - top) }];
        })
      : [];
  const sameBand = bands.length === 2 && bands[0].anchor === bands[1].anchor && bands[0].bar === bands[1].bar && bands[0].rung === bands[1].rung;
  const headroomText = appliedHeadroom === null ? '' : formatHeadroom(appliedHeadroom);
  const bandLabels = sameBand ? [{ ...bands[0], color: 'var(--tl-shared)' }] : bands.map((b) => ({ ...b, color: `var(--tl-${b.role})` }));

  const floorY = y(floor);
  const leftTitle = !hasAA ? 'prior (0-1)' : indexVersion ? `AA Intelligence Index v${indexVersion}` : 'AA Intelligence Index';

  const tipDot = tip ? dots.find((d) => d.id === tip.id) : undefined;

  return (
    <div className="tl-chart" ref={wrapRef}>
      <svg ref={svgRef} viewBox={`0 0 ${width} ${height}`}>
        <g className="tl-chart-grid">
          {px.ticks.map((t) => (
            <g key={`x${t}`}>
              <line x1={px.x(t)} x2={px.x(t)} y1={top} y2={bottom} />
              <text x={px.x(t)} y={bottom + 16} textAnchor="middle">
                ${t}
              </text>
            </g>
          ))}
          {q.ticks.map((p) => (
            <g key={`y${p}`}>
              <line x1={x0} x2={x1} y1={y(p)} y2={y(p)} />
              {hasAA && (
                <text x={x0 - handleW - 6} y={y(p) + 4} textAnchor="end">
                  {Math.round(p * bestAAScore)}
                </text>
              )}
              <text x={x1 + handleW + 6} y={y(p) + 4}>
                {p.toFixed(1)}
              </text>
            </g>
          ))}
        </g>
        <text x={(x0 + x1) / 2} y={height - 6} textAnchor="middle">
          price per million tokens, prompt + completion (log)
        </text>
        <text transform={`translate(12 ${(top + bottom) / 2}) rotate(-90)`} textAnchor="middle">
          {leftTitle}
        </text>
        {px.gutter && (
          <g>
            <rect className="tl-chart-gutter" x={px.gutter[0]} y={top} width={px.gutter[1] - px.gutter[0]} height={bottom - top} />
            <text x={(px.gutter[0] + px.gutter[1]) / 2} y={bottom + 28} textAnchor="middle">
              no price
            </text>
          </g>
        )}
        {bands.map((b) => (
          <rect
            key={b.role}
            className="tl-chart-band"
            data-testid={`tl-band-${b.role}`}
            x={b.x}
            y={top}
            width={b.width}
            height={b.height}
            style={{ fill: `var(--tl-${b.role})` }}
          />
        ))}
        {bandLabels.map((b, i) => {
          // Near the right edge the label ends at the band instead of starting there.
          const flip = b.x > x1 - 150;
          return (
            <text
              key={b.role}
              className="tl-chart-band-label"
              x={flip ? b.x + b.width - 3 : b.x + 3}
              y={top + 12 + i * 13}
              textAnchor={flip ? 'end' : 'start'}
              style={{ fill: b.color }}
            >
              {b.rung} price band ×{headroomText}
            </text>
          );
        })}
        <rect className="tl-chart-floor-area" x={x0} y={floorY} width={x1 - x0} height={Math.max(0, y(q.lo) - floorY)} />
        <line className="tl-chart-floor" data-testid="tl-floor" x1={x0} x2={x1} y1={floorY} y2={floorY} />
        <text className="tl-chart-floor-label" x={x0 + 4} y={floorY + 13}>
          quality floor {formatBar(floor)} (config)
        </text>
        {tierSets.flatMap(({ role, color, slots }) =>
          slots.map((s) => (
            <line
              key={`${role}-${s.tier}`}
              className={s.tier === selectedTier ? 'tl-chart-tier selected' : 'tl-chart-tier'}
              data-testid={`tl-line-${role}-${s.tier}`}
              x1={x0}
              x2={x1}
              y1={s.lineY}
              y2={s.lineY}
              strokeDasharray={role === 'reviewer' ? '6 4' : undefined}
              style={{ stroke: color }}
            />
          )),
        )}
        {lineLabels.map((l, i) => (
          <text key={l.tier} className="tl-chart-tier-label" x={x0 + 4} y={lineLabelYs[i]}>
            {l.tier} ≥ {hasAA ? `${(l.bar * bestAAScore).toFixed(1)} AA · ${formatBar(l.bar)}` : formatBar(l.bar)}
          </text>
        ))}
        {links.map((c) => {
          const cx = px.x(blendedPrice(c) * 1e6);
          return <line key={c.slug} className="tl-chart-link" x1={cx} x2={cx} y1={y(c.coder_prior)} y2={y(c.reviewer_prior)} />;
        })}
        {dots.map((d) => {
          const s = dotStyle(d.marks);
          const listed = d.marks.includes('blacklisted');
          return (
            <g key={d.id}>
              <circle
                className={onModelMenu ? 'tl-chart-pt has-menu' : 'tl-chart-pt'}
                data-testid={`tl-pt-${d.id}`}
                data-marks={d.marks.join(' ')}
                cx={d.cx}
                cy={d.cy}
                r={s.r}
                strokeDasharray={s.dash}
                opacity={listed ? 0.35 : 1}
                style={{ fill: s.fill, stroke: s.stroke }}
                onPointerEnter={(e) => setTip({ id: d.id, x: e.clientX, y: e.clientY })}
                onPointerLeave={() => setTip(null)}
                onContextMenu={
                  onModelMenu &&
                  ((e) => {
                    e.preventDefault();
                    onModelMenu(d.c.slug, e.clientX, e.clientY);
                  })
                }
              />
              {listed && <line className="tl-chart-strike" x1={d.cx - 8} x2={d.cx + 8} y1={d.cy} y2={d.cy} />}
            </g>
          );
        })}
        {labelled.map((d, i) => {
          const flip = d.cx > x1 - 80;
          return (
            <text key={d.id} className="tl-chart-label" x={flip ? d.cx - LABEL_GAP : d.cx + LABEL_GAP} y={baselines[i]} textAnchor={flip ? 'end' : 'start'}>
              {shortSlug(d.c.slug)}
            </text>
          );
        })}
        {tierSets.flatMap(({ role, color, slots }) =>
          slots
            .filter((s) => Math.abs(s.handleY - s.lineY) > 0.5)
            .map((s) => {
              const [edge, onLine] = role === 'coder' ? [x0 - 2, x0 + 8] : [x1 + 2, x1 - 8];
              return <line key={`${role}-${s.tier}`} className="tl-chart-connector" x1={edge} y1={s.handleY} x2={onLine} y2={s.lineY} style={{ stroke: color }} />;
            }),
        )}
        {tierSets.flatMap(({ role, color, slots }) =>
          slots.map((s) => {
            const bar = ladders[role][s.tier];
            const hx = role === 'coder' ? x0 - 2 - handleW : x1 + 2;
            const active = dragging?.role === role && dragging.tier === s.tier;
            return (
              <g
                key={`${role}-${s.tier}`}
                role="slider"
                tabIndex={0}
                className={active ? 'tl-chart-handle dragging' : 'tl-chart-handle'}
                aria-label={`${role} ${s.tier} bar`}
                aria-valuemin={floor}
                aria-valuemax={1}
                aria-valuenow={bar}
                aria-valuetext={formatBar(bar)}
                transform={`translate(${hx} ${s.handleY - handleH / 2})`}
                style={{ '--handle-c': color } as CSSProperties}
                {...handleProps(role, s.tier)}
              >
                <rect width={handleW} height={handleH} rx={4} />
                <text x={handleW / 2} y={handleH / 2 + 3.5} textAnchor="middle">
                  {formatHandle(s.tier, bar)}
                </text>
              </g>
            );
          }),
        )}
      </svg>
      {tip && tipDot && (
        <ChartTip
          c={tipDot.c}
          role={tipDot.roles[0]}
          x={tip.x}
          y={tip.y}
          ladders={ladders}
          bestAAScore={bestAAScore}
          indexVersion={indexVersion}
          blacklisted={blacklist.has(tipDot.c.slug)}
        />
      )}
    </div>
  );
}

interface ChartTipProps {
  c: SelectorCandidate;
  /** The hovered dot's role; a shared dot reads as coder, whose prior equals the reviewer's. */
  role: SelectorRole;
  x: number;
  y: number;
  ladders: SelectorLadders;
  bestAAScore: number;
  indexVersion: string;
  blacklisted: boolean;
}

const TIP_ROOM_X = 300;
const TIP_ROOM_Y = 160;

function ChartTip({ c, role, x, y, ladders, bestAAScore, indexVersion, blacklisted }: ChartTipProps) {
  let score: string;
  if (c.prior_source === 'model_priors') {
    score = `operator prior · coder ${c.coder_prior.toFixed(2)} · reviewer ${c.reviewer_prior.toFixed(2)}`;
  } else {
    const prior = priorOf(c, role);
    const index = indexVersion ? `AA Intelligence Index v${indexVersion}` : 'AA Intelligence Index';
    score = bestAAScore > 0 ? `${index} ${(prior * bestAAScore).toFixed(1)} · prior ${prior.toFixed(2)}` : `prior ${prior.toFixed(2)}`;
  }

  const priced = c.prompt_price_per_tok > 0 || c.completion_price_per_tok > 0;
  const price = priced
    ? `${money(c.prompt_price_per_tok)} in / ${money(c.completion_price_per_tok)} out per Mtok${c.price_source === 'aa' ? ' · list price' : ''}`
    : 'no price';

  const coderTiers = clearedTiers(ladders.coder, c.coder_prior);
  const reviewerTiers = clearedTiers(ladders.reviewer, c.reviewer_prior);
  // One list would be untrue for a role whenever the two differ, whether the ladders or the priors split them.
  const tiers =
    coderTiers.join() === reviewerTiers.join()
      ? [`clears: ${tierList(coderTiers)}`]
      : [`coder clears: ${tierList(coderTiers)}`, `reviewer clears: ${tierList(reviewerTiers)}`];

  const flipX = x + TIP_ROOM_X > window.innerWidth;
  const flipY = y + TIP_ROOM_Y > window.innerHeight;
  const style: CSSProperties = {
    left: flipX ? x - 14 : x + 14,
    top: flipY ? y - 10 : y + 10,
    transform: `translate(${flipX ? '-100%' : '0'}, ${flipY ? '-100%' : '0'})`,
  };

  return (
    <div role="tooltip" className="tl-chart-tip" style={style}>
      <b>{c.slug}</b>
      <div>{score}</div>
      <div>{price}</div>
      {tiers.map((t) => (
        <div key={t}>{t}</div>
      ))}
      {c.scored_from && <div>scored from {c.scored_from}</div>}
      {blacklisted && <div className="tl-chart-tip-bl">blacklisted</div>}
    </div>
  );
}
