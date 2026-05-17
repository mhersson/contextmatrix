interface MetricsRibbonProps {
  activeAgents: number;
  inFlight: number;
  stalled: number;
  shippedToday: number;
  shipped7d?: number;
  shipped7dPrior?: number;
  activeAgentsSeries?: number[];
  inFlightSeries?: number[];
  stalledSeries?: number[];
  shippedSeries?: number[];
}

function Sparkline({ values, color }: { values?: number[]; color: string }) {
  if (!values || values.length < 2) return null;
  const max = Math.max(...values, 1);
  const w = 80;
  const h = 22;
  const step = w / (values.length - 1);
  const points = values
    .map((v, i) => {
      // Clamp so a constant-zero series sits on the baseline rather than the top.
      const y = max === 0 ? h : h - (v / max) * h;
      return `${(i * step).toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <polyline points={points} fill="none" stroke={color} strokeWidth="1.5" />
    </svg>
  );
}

/**
 * Metric tiles surfaced from DashboardData fields. The 7d tile is shown only
 * when both shipped7d and shipped7dPrior are provided (Phase 2 backend
 * additions); the delta renders +X% / -X% colored green/red. Sparklines
 * mirror the playground 7-day series; the shipped series is accurate while
 * the other three are best-effort approximations (see service_dashboard.go).
 */
export function MetricsRibbon({
  activeAgents,
  inFlight,
  stalled,
  shippedToday,
  shipped7d,
  shipped7dPrior,
  activeAgentsSeries,
  inFlightSeries,
  stalledSeries,
  shippedSeries,
}: MetricsRibbonProps) {
  const showShipped7d = shipped7d !== undefined;
  const hasDelta = showShipped7d && shipped7dPrior !== undefined && shipped7dPrior > 0;
  const deltaPct = hasDelta ? Math.round(((shipped7d! - shipped7dPrior!) / shipped7dPrior!) * 100) : 0;
  const deltaUp = hasDelta && shipped7d! >= shipped7dPrior!;

  return (
    <div className="metrics-ribbon">
      <div className="metric-tile">
        <span className="metric-tile__label">Active agents</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{activeAgents}</span>
        </span>
        <Sparkline values={activeAgentsSeries} color="var(--aqua)" />
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">In flight</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{inFlight}</span>
        </span>
        <Sparkline values={inFlightSeries} color="var(--blue)" />
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">Stalled</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{stalled}</span>
        </span>
        <Sparkline values={stalledSeries} color="var(--red)" />
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">Shipped today</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{shippedToday}</span>
        </span>
        <Sparkline values={shippedSeries} color="var(--green)" />
      </div>
      {showShipped7d && (
        <div className="metric-tile">
          <span className="metric-tile__label">Shipped · 7d</span>
          <span className="metric-tile__value">
            <span className="metric-tile__num">{shipped7d}</span>
            {hasDelta && (
              <span className={`metric-tile__delta ${deltaUp ? 'metric-tile__delta--up' : 'metric-tile__delta--down'}`}>
                {deltaUp ? '+' : ''}{deltaPct}%
              </span>
            )}
          </span>
          <Sparkline values={shippedSeries} color="var(--green)" />
        </div>
      )}
    </div>
  );
}
