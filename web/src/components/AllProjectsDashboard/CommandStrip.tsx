import { useMobileSidebar } from '../../context/MobileSidebarContext';
import { SidebarMenuButton } from '../SidebarMenuButton/SidebarMenuButton';

export interface SummaryStats {
  projectCount: number;
  totalCards: number;
  agentCount: number;
  stalledCount: number;
}

interface CommandStripProps {
  /** null while the first dashboard fetch is in flight - the strip renders
   *  without the summary so the mobile hamburger stays reachable. */
  stats: SummaryStats | null;
  onRefresh: () => void;
  onNewProject: () => void;
  refreshing: boolean;
  /** UX honesty: hidden for non-admins in multi mode (API 403s anyway). Defaults to visible. */
  showNewProject?: boolean;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

export function CommandStrip({
  stats,
  onRefresh,
  onNewProject,
  refreshing,
  showNewProject = true,
}: CommandStripProps) {
  const { toggle } = useMobileSidebar();

  return (
    <header className="apd-strip">
      <SidebarMenuButton onClick={toggle} />
      <div className="min-w-0 shrink-0">
        <p className="apd-strip-eyebrow">
          ContextMatrix <span aria-hidden="true">/</span> All projects
        </p>
        <h1 className="apd-strip-title">Operations</h1>
      </div>
      {stats && (
        <p className="apd-strip-summary truncate min-w-0">
          <b>{plural(stats.projectCount, 'project')}</b>
          {' · '}
          {plural(stats.totalCards, 'card')}
          {' · '}
          <b>{plural(stats.agentCount, 'agent')}</b> active
          {stats.stalledCount > 0 && (
            <>
              {' · '}
              <span className="apd-strip-stalled">{stats.stalledCount} stalled</span>
            </>
          )}
        </p>
      )}
      <div className="apd-strip-actions">
        <button
          type="button"
          onClick={onRefresh}
          disabled={refreshing}
          className="apd-chip"
          style={{
            color: 'var(--grey2)',
            backgroundColor: 'var(--bg1)',
            borderColor: 'var(--bg3)',
            opacity: refreshing ? 0.6 : 1,
          }}
          aria-busy={refreshing}
          title="Refresh dashboard"
        >
          <svg
            viewBox="0 0 14 14"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            width={13}
            height={13}
            aria-hidden="true"
            className={refreshing ? 'apd-spin' : undefined}
          >
            <path d="M2 7a5 5 0 1 0 1.5-3.6" />
            <path d="M2 2v3h3" />
          </svg>
          {refreshing ? 'Refreshing' : 'Refresh'}
        </button>
        {showNewProject && (
          <button
            type="button"
            onClick={onNewProject}
            className="apd-chip apd-chip-primary"
            style={{
              color: 'var(--green)',
              backgroundColor: 'var(--bg-green)',
              borderColor: 'transparent',
            }}
          >
            <svg
              viewBox="0 0 14 14"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.6}
              strokeLinecap="round"
              width={13}
              height={13}
              aria-hidden="true"
            >
              <path d="M7 3v8M3 7h8" />
            </svg>
            New project
          </button>
        )}
      </div>
    </header>
  );
}
