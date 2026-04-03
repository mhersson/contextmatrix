import type { DashboardData } from '../../types';

interface ProjectCardProps {
  name: string;
  summary?: DashboardData;
  isActive: boolean;
}

export function ProjectCard({ name, summary, isActive }: ProjectCardProps) {
  const activeAgentCount = summary?.active_agents.length ?? 0;
  const inProgressCount = summary?.state_counts?.in_progress ?? 0;

  return (
    <div
      className="px-3 py-2 rounded transition-colors cursor-pointer"
      style={{ backgroundColor: isActive ? 'var(--bg2)' : 'transparent' }}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 min-w-0">
          <span
            className="text-sm font-medium truncate"
            style={{ color: isActive ? 'var(--fg)' : 'var(--grey2)' }}
          >
            {name}
          </span>
          {inProgressCount > 0 && (
            <svg
              className="animate-spin flex-shrink-0"
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              xmlns="http://www.w3.org/2000/svg"
              aria-label="Tasks in progress"
            >
              <circle
                cx="6"
                cy="6"
                r="4.5"
                stroke="var(--yellow)"
                strokeOpacity="0.3"
                strokeWidth="1.5"
              />
              <path
                d="M6 1.5A4.5 4.5 0 0 1 10.5 6"
                stroke="var(--yellow)"
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </svg>
          )}
        </div>
        {activeAgentCount > 0 && (
          <span className="text-xs px-1.5 rounded-full flex-shrink-0" style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}>
            {activeAgentCount}
          </span>
        )}
      </div>
    </div>
  );
}
