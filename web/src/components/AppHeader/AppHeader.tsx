import type { ProjectConfig } from '../../types';

export type ViewType = 'board' | 'dashboard' | 'settings';

interface AppHeaderProps {
  projects: ProjectConfig[];
  selectedProject: string;
  onSelectProject: (project: string) => void;
  projectsLoading: boolean;
  connected: boolean;
  view: ViewType;
  onViewChange: (view: ViewType) => void;
  onNewProject: () => void;
}

export function AppHeader({
  projects,
  selectedProject,
  onSelectProject,
  projectsLoading,
  connected,
  view,
  onViewChange,
  onNewProject,
}: AppHeaderProps) {
  return (
    <header
      className="flex items-center justify-between px-6 py-4 border-b"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <div className="flex items-center gap-4">
        <h1
          className="text-xl font-semibold"
          style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}
        >
          ContextMatrix
        </h1>

        <div className="flex items-center gap-1 rounded p-0.5" style={{ backgroundColor: 'var(--bg1)' }}>
          {(['board', 'dashboard', 'settings'] as const).map((v) => (
            <button
              key={v}
              onClick={() => onViewChange(v)}
              className="px-3 py-1 rounded text-sm transition-colors"
              style={{
                backgroundColor: view === v ? 'var(--bg3)' : 'transparent',
                color: view === v ? 'var(--fg)' : 'var(--grey1)',
              }}
            >
              {v.charAt(0).toUpperCase() + v.slice(1)}
            </button>
          ))}
        </div>
      </div>

      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2">
          <span
            className={`w-2 h-2 rounded-full ${connected ? 'animate-pulse' : ''}`}
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
          />
          <span className="text-sm" style={{ color: 'var(--grey1)' }}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>

        <div className="flex items-center gap-2">
          <select
            value={selectedProject}
            onChange={(e) => onSelectProject(e.target.value)}
            disabled={projectsLoading || projects.length === 0}
            className="px-3 py-1.5 rounded text-sm border"
            style={{
              backgroundColor: 'var(--bg1)',
              borderColor: 'var(--bg3)',
              color: 'var(--fg)',
            }}
          >
            {projectsLoading && <option>Loading...</option>}
            {!projectsLoading && projects.length === 0 && (
              <option>No projects</option>
            )}
            {projects.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>

          <button
            onClick={onNewProject}
            className="px-2 py-1.5 rounded text-sm transition-colors hover:opacity-80"
            style={{ backgroundColor: 'var(--bg1)', color: 'var(--green)' }}
            title="New Project"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
          </button>
        </div>
      </div>
    </header>
  );
}
