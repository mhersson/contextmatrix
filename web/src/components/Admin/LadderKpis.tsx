import type { CSSProperties, ReactNode } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorPreview } from '../../types';
import { formatBar, panelPrice, shortSlug, usdPerMillion } from './ladder';

interface LadderKpisProps {
  candidates: SelectorCandidate[];
  ladders: SelectorLadders;
  preview: SelectorPreview | null;
  /** A newer preview is in flight: the pick-backed tiles are one step behind. */
  pending: boolean;
}

interface TileProps {
  id: string;
  label: string;
  badge: string;
  value: ReactNode;
  sub: string;
  accent: string;
  /** The tile reads from the preview, so it dims while one is in flight. */
  stale?: boolean;
}

/** Mirrors the dashboard KpiTile markup so the tiles read as one system. */
function Tile({ id, label, badge, value, sub, accent, stale = false }: TileProps) {
  return (
    <div className={`apd-kpi${stale ? ' pending' : ''}`} style={{ '--apd-acc': accent } as CSSProperties} data-testid={`tl-kpi-${id}`}>
      <div className="apd-kpi-label">
        <span>{label}</span>
        <span className="apd-kpi-badge">{badge}</span>
      </div>
      <div className="apd-kpi-value-row">
        <span className="apd-kpi-value" style={{ color: accent }}>
          {value}
        </span>
        <span className="apd-kpi-sub">{sub}</span>
      </div>
    </div>
  );
}

const NONE = '—';

export function LadderKpis({ candidates, ladders, preview, pending }: LadderKpisProps) {
  const complexBar = ladders.reviewer.complex;
  const reviewersClearing = candidates.filter((c) => c.reviewer_prior >= complexBar).length;

  const cheapest = preview?.tiers.complex.reviewer.pick;
  const cheapestOK = cheapest !== undefined && cheapest.ok;

  const panel = preview?.tiers.complex.panel ?? [];
  const walked = panel.some((s) => s.walked);

  const coder = preview?.tiers.moderate.coder.pick;
  const coderOK = coder !== undefined && coder.ok;

  return (
    <div className="tl-kpis" aria-busy={pending}>
      <Tile
        id="reviewers"
        label="Reviewers clearing complex"
        badge={`bar ${formatBar(complexBar)}`}
        value={reviewersClearing}
        sub={`of ${candidates.length} candidates`}
        accent="var(--yellow)"
      />
      <Tile
        id="cheapest"
        stale={pending}
        label="Cheapest complex reviewer"
        badge="seat 1"
        value={cheapestOK ? usdPerMillion(cheapest.price_per_tok) : NONE}
        sub={cheapestOK ? shortSlug(cheapest.model) : ''}
        accent="var(--aqua)"
      />
      <Tile
        id="panel"
        stale={pending}
        label="Review panel, complex"
        badge="3 seats · per M tok"
        value={panel.length > 0 ? usdPerMillion(panelPrice(panel)) : NONE}
        sub={panel.map((s) => shortSlug(s.pick.model)).join(' · ')}
        accent={walked ? 'var(--orange)' : 'var(--green)'}
      />
      <Tile
        id="coder"
        stale={pending}
        label="Coder at moderate"
        badge={`bar ${formatBar(ladders.coder.moderate)}`}
        value={coderOK ? usdPerMillion(coder.price_per_tok) : NONE}
        sub={coderOK ? shortSlug(coder.model) : ''}
        accent="var(--blue)"
      />
    </div>
  );
}
