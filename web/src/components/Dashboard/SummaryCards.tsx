import { SubCount } from '../Board/MetricsRibbon';

interface SummaryCardsProps {
  stateCounts: Record<string, number>;
  totalCost: number;
  completedToday: number;
  completedTodayParents?: number;
}

function StatTile({
  label,
  value,
  color,
  subCount,
}: {
  label: string;
  value: string;
  color: string;
  subCount?: number;
}) {
  return (
    <div
      className="rounded-lg p-4"
      style={{ backgroundColor: 'var(--bg1)' }}
    >
      <div
        className="flex items-baseline"
        style={{
          color,
          fontFamily: 'var(--font-display)',
          fontWeight: 500,
          fontSize: '28px',
          letterSpacing: '-0.02em',
          lineHeight: 1.1,
        }}
      >
        <span className="truncate min-w-0">{value}</span>
        <SubCount n={subCount} />
      </div>
      <div className="section-eyebrow mt-2">{label}</div>
    </div>
  );
}

export function SummaryCards({
  stateCounts,
  totalCost,
  completedToday,
  completedTodayParents,
}: SummaryCardsProps) {
  const openTasks = Object.entries(stateCounts)
    .filter(([state]) => state !== 'done' && state !== 'stalled' && state !== 'not_planned')
    .reduce((sum, [, count]) => sum + count, 0);

  const doneHeadline = completedTodayParents ?? completedToday;
  const doneSubtasks =
    completedTodayParents !== undefined ? completedToday - completedTodayParents : undefined;

  return (
    <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
      <StatTile label="Open Tasks" value={String(openTasks)} color="var(--blue)" />
      <StatTile label="In Progress" value={String(stateCounts['in_progress'] ?? 0)} color="var(--yellow)" />
      <StatTile
        label="Done Today"
        value={String(doneHeadline)}
        color="var(--green)"
        subCount={doneSubtasks}
      />
      <StatTile label="Total Cost" value={`$${totalCost.toFixed(2)}`} color="var(--purple)" />
    </div>
  );
}
