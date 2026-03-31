import type { DashboardData } from '../../types';

interface ProjectCardProps {
  name: string;
  summary?: DashboardData;
  isActive: boolean;
}

const STATE_COLORS: Record<string, string> = {
  todo: 'var(--grey2)',
  in_progress: 'var(--yellow)',
  blocked: 'var(--red)',
  review: 'var(--purple)',
  done: 'var(--green)',
  stalled: 'var(--orange)',
};

export function ProjectCard({ name, summary, isActive }: ProjectCardProps) {
  const activeAgentCount = summary?.active_agents.length ?? 0;
  const totalCards = summary
    ? Object.values(summary.state_counts).reduce((a, b) => a + b, 0)
    : 0;

  return (
    <div
      className="px-3 py-2 rounded transition-colors cursor-pointer"
      style={{ backgroundColor: isActive ? 'var(--bg2)' : 'transparent' }}
    >
      <div className="flex items-center justify-between">
        <span
          className="text-sm font-medium truncate"
          style={{ color: isActive ? 'var(--fg)' : 'var(--grey2)' }}
        >
          {name}
        </span>
        {activeAgentCount > 0 && (
          <span className="text-xs px-1.5 rounded-full" style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}>
            {activeAgentCount}
          </span>
        )}
      </div>

      {summary && totalCards > 0 && (
        <div className="flex gap-1.5 mt-1.5">
          {Object.entries(summary.state_counts)
            .filter(([, count]) => count > 0)
            .map(([state, count]) => (
              <span
                key={state}
                className="text-xs px-1 rounded"
                style={{
                  backgroundColor: 'var(--bg0)',
                  color: STATE_COLORS[state] || 'var(--grey1)',
                }}
                title={`${state}: ${count}`}
              >
                {count}
              </span>
            ))}
        </div>
      )}
    </div>
  );
}
