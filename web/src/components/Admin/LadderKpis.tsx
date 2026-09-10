import type { CSSProperties, ReactNode } from 'react';
import type { SelectorCandidate, SelectorLadders, SelectorPreview } from '../../types';
import { formatBar, panelPrice, shortSlug, usdPerMillion } from './ladder';

interface LadderKpisProps {
  candidates: SelectorCandidate[];
  ladders: SelectorLadders;
  preview: SelectorPreview | null;
}

interface TileProps {
  id: string;
  label: string;
  badge: string;
  value: ReactNode;
  sub: string;
  accent: string;
}

/** Mirrors the dashboard KpiTile markup so the tiles read as one system. */
function Tile({ id, label, badge, value, sub, accent }: TileProps) {
  return (
    <div className="apd-kpi" style={{ '--apd-acc': accent } as CSSProperties} data-testid={`tl-kpi-${id}`}>
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

export function LadderKpis({ candidates, ladders, preview }: LadderKpisProps) {
  const complexBar = ladders.reviewer.complex;
  const reviewersClearing = candidates.filter((c) => c.reviewer_prior >= complexBar).length;

  const cheapest = preview?.tiers.complex.reviewer.pick;
  const cheapestOK = cheapest !== undefined && cheapest.ok;

  const panel = preview?.tiers.complex.panel ?? [];
  const walked = panel.some((s) => s.walked);

  const coder = preview?.tiers.moderate.coder.pick;
  const coderOK = coder !== undefined && coder.ok;

  return (
    <div className="tl-kpis">
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
        label="Cheapest complex reviewer"
        badge="seat 1"
        value={cheapestOK ? usdPerMillion(cheapest.price_per_tok) : NONE}
        sub={cheapestOK ? shortSlug(cheapest.model) : ''}
        accent="var(--aqua)"
      />
      <Tile
        id="panel"
        label="Review panel, complex"
        badge="3 seats · per M tok"
        value={panel.length > 0 ? usdPerMillion(panelPrice(panel)) : NONE}
        sub={panel.map((s) => shortSlug(s.pick.model)).join(' · ')}
        accent={walked ? 'var(--orange)' : 'var(--green)'}
      />
      <Tile
        id="coder"
        label="Coder at moderate"
        badge={`bar ${formatBar(ladders.coder.moderate)}`}
        value={coderOK ? usdPerMillion(coder.price_per_tok) : NONE}
        sub={coderOK ? shortSlug(coder.model) : ''}
        accent="var(--blue)"
      />
    </div>
  );
}
