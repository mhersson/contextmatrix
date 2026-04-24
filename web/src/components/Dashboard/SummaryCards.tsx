interface SummaryCardsProps {
  stateCounts: Record<string, number>;
  totalCost: number;
  completedToday: number;
}

function StatTile({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div
      className="rounded-lg p-4"
      style={{ backgroundColor: 'var(--bg1)' }}
    >
      <div
        className="truncate"
        style={{
          color,
          fontFamily: 'var(--font-display)',
          fontWeight: 500,
          fontSize: '28px',
          letterSpacing: '-0.02em',
          lineHeight: 1.1,
        }}
      >
        {value}
      </div>
      <div className="section-eyebrow mt-2">{label}</div>
    </div>
  );
}

export function SummaryCards({ stateCounts, totalCost, completedToday }: SummaryCardsProps) {
  const openTasks = Object.entries(stateCounts)
    .filter(([state]) => state !== 'done' && state !== 'stalled' && state !== 'not_planned')
    .reduce((sum, [, count]) => sum + count, 0);

  return (
    <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
      <StatTile label="Open Tasks" value={String(openTasks)} color="var(--blue)" />
      <StatTile label="In Progress" value={String(stateCounts['in_progress'] ?? 0)} color="var(--yellow)" />
      <StatTile label="Done Today" value={String(completedToday)} color="var(--green)" />
      <StatTile label="Total Cost" value={`$${totalCost.toFixed(2)}`} color="var(--purple)" />
    </div>
  );
}
