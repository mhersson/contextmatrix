interface PageHeaderProps {
  summary: string;
  projectCount: number;
  onRefresh: () => void;
  onNewProject: () => void;
  refreshing: boolean;
  /** UX honesty: hidden for non-admins in multi mode (API 403s anyway). Defaults to visible. */
  showNewProject?: boolean;
}

export function PageHeader({
  summary,
  projectCount,
  onRefresh,
  onNewProject,
  refreshing,
  showNewProject = true,
}: PageHeaderProps) {
  return (
    <header className="apd-page-header">
      <div className="apd-page-title-block min-w-0">
        <p className="apd-eyebrow">
          All Projects · <b>Operations Overview</b>
        </p>
        <h1 className="apd-title-display">
          Operations<span style={{ color: 'var(--aqua)' }}>.</span>
        </h1>
        <p className="apd-subtitle">{summary}</p>
      </div>
      <div className="apd-header-actions">
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
          title={projectCount === 0 ? 'No projects to refresh' : 'Refresh dashboard'}
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
