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
      <div className="text-2xl font-bold" style={{ color }}>{value}</div>
      <div className="text-sm mt-1" style={{ color: 'var(--grey1)' }}>{label}</div>
    </div>
  );
}

export function SummaryCards({ stateCounts, totalCost, completedToday }: SummaryCardsProps) {
  const openTasks = Object.entries(stateCounts)
    .filter(([state]) => state !== 'done' && state !== 'stalled' && state !== 'not_planned')
    .reduce((sum, [, count]) => sum + count, 0);

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
      <StatTile label="Open Tasks" value={String(openTasks)} color="var(--blue)" />
      <StatTile label="In Progress" value={String(stateCounts['in_progress'] ?? 0)} color="var(--yellow)" />
      <StatTile label="Done Today" value={String(completedToday)} color="var(--green)" />
      <StatTile label="Total Cost" value={`$${totalCost.toFixed(2)}`} color="var(--purple)" />
    </div>
  );
}
