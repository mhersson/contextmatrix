interface MetricsRibbonProps {
  activeAgents: number;
  inFlight: number;
  stalled: number;
  shippedToday: number;
  shipped7d?: number;
  shipped7dPrior?: number;
}

/**
 * Metric tiles surfaced from DashboardData fields. The 7d tile is shown only
 * when both shipped7d and shipped7dPrior are provided (Phase 2 backend
 * additions); the delta renders +X% / -X% colored green/red.
 */
export function MetricsRibbon({
  activeAgents,
  inFlight,
  stalled,
  shippedToday,
  shipped7d,
  shipped7dPrior,
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
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">In flight</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{inFlight}</span>
        </span>
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">Stalled</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{stalled}</span>
        </span>
      </div>
      <div className="metric-tile">
        <span className="metric-tile__label">Shipped today</span>
        <span className="metric-tile__value">
          <span className="metric-tile__num">{shippedToday}</span>
        </span>
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
        </div>
      )}
    </div>
  );
}
