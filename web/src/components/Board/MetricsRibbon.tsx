interface Tile {
  label: string;
  value: number;
}

interface MetricsRibbonProps {
  activeAgents: number;
  inFlight: number;
  stalled: number;
  shippedToday: number;
}

/**
 * Four metric tiles surfaced from existing DashboardData fields:
 *   - active_agents.length
 *   - in_progress + review counts (in flight)
 *   - stalled count
 *   - cards_completed_today
 *
 * Sparklines and cycle-time-p50 are deliberately not included — they
 * would require backend time-series support.
 */
export function MetricsRibbon({ activeAgents, inFlight, stalled, shippedToday }: MetricsRibbonProps) {
  const tiles: Tile[] = [
    { label: 'Active agents', value: activeAgents },
    { label: 'In flight', value: inFlight },
    { label: 'Stalled', value: stalled },
    { label: 'Shipped today', value: shippedToday },
  ];
  return (
    <div className="metrics-ribbon">
      {tiles.map((t) => (
        <div className="metric-tile" key={t.label}>
          <span className="metric-tile__label">{t.label}</span>
          <span className="metric-tile__value">
            <span className="metric-tile__num">{t.value}</span>
          </span>
        </div>
      ))}
    </div>
  );
}
