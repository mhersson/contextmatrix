interface MetricsRibbonProps {
  activeAgents: number;
  inFlight: number;
  inFlightSubtasks?: number;
  stalled: number;
  stalledSubtasks?: number;
  shippedToday: number;
  shippedTodaySubtasks?: number;
  shipped7d?: number;
  shipped7dSubtasks?: number;
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
 * Metric tiles surfaced from DashboardData fields. Headlines and sparklines
 * consume parent-only counts so subtasks do not inflate the headline numbers.
 * When subtasks exist (total − parents > 0), a muted "+N sub" suffix is shown
 * next to the headline. The 7d tile is shown only when shipped7d is provided;
 * the delta renders +X% / -X% colored green/red using parent-only prior values.
 * Active agents tile is unchanged.
 */
export function MetricsRibbon({
  activeAgents,
  inFlight,
  inFlightSubtasks,
  stalled,
  stalledSubtasks,
  shippedToday,
  shippedTodaySubtasks,
  shipped7d,
  shipped7dSubtasks,
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
          {inFlightSubtasks !== undefined && inFlightSubtasks > 0 && (
            <span className="metric-tile__sub">+{inFlightSubtasks} sub</span>
          )}
        </span>
        <Sparkline values={inFlightSeries} color="var(--blue)" />
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">Stalled</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{stalled}</span>
          {stalledSubtasks !== undefined && stalledSubtasks > 0 && (
            <span className="metric-tile__sub">+{stalledSubtasks} sub</span>
          )}
        </span>
        <Sparkline values={stalledSeries} color="var(--red)" />
      </div>
      {/*
        "Shipped today" is a point-in-time count and intentionally has no
        sparkline — the trend belongs on the 7d tile, where the series
        covers the same window the number measures. Rendering the 7d
        series under both tiles produced identical sparklines side-by-side.
      */}
      <div className="metric-tile">
        <span className="metric-tile__label">Shipped today</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{shippedToday}</span>
          {shippedTodaySubtasks !== undefined && shippedTodaySubtasks > 0 && (
            <span className="metric-tile__sub">+{shippedTodaySubtasks} sub</span>
          )}
        </span>
      </div>
      {showShipped7d && (
        <div className="metric-tile">
          <span className="metric-tile__label">Shipped · 7d</span>
          <span className="metric-tile__value">
            <span className="metric-tile__num">{shipped7d}</span>
            {shipped7dSubtasks !== undefined && shipped7dSubtasks > 0 && (
              <span className="metric-tile__sub">+{shipped7dSubtasks} sub</span>
            )}
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
