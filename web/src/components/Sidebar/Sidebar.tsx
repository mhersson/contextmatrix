import { useMemo, useState } from 'react';
import { NavLink } from 'react-router-dom';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummaries } from '../../hooks/useProjectSummaries';
import { ProjectCard } from './ProjectCard';

interface SidebarProps {
  onNewProject: () => void;
}

export function Sidebar({ onNewProject }: SidebarProps) {
  const { projects, connected } = useProjects();
  const [collapsed, setCollapsed] = useState(false);
  const sortedProjects = useMemo(
    () => [...projects].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })),
    [projects]
  );
  const projectNames = sortedProjects.map((p) => p.name);
  const { summaries } = useProjectSummaries(projectNames);

  if (collapsed) {
    return (
      <div
        className="flex flex-col items-center py-4 border-r shrink-0"
        style={{ width: 48, backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
      >
        <button
          onClick={() => setCollapsed(false)}
          className="p-1 rounded hover:opacity-80"
          style={{ color: 'var(--grey2)' }}
          title="Expand sidebar"
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
        </button>
      </div>
    );
  }

  return (
    <div
      className="flex flex-col border-r shrink-0 sidebar"
      style={{ width: 240, backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <div className="flex items-center justify-between px-4 py-4 border-b" style={{ borderColor: 'var(--bg3)' }}>
        <h1
          className="text-lg font-semibold truncate"
          style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}
        >
          ContextMatrix
        </h1>
        <button
          onClick={() => setCollapsed(true)}
          className="p-1 rounded hover:opacity-80"
          style={{ color: 'var(--grey1)' }}
          title="Collapse sidebar"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
        </button>
      </div>

      <div className="flex-1 overflow-y-auto px-2 py-2 space-y-0.5">
        <NavLink to="/all" className="block">
          {({ isActive }) => (
            <div
              className="px-3 py-2 rounded text-sm transition-colors"
              style={{
                backgroundColor: isActive ? 'var(--bg2)' : 'transparent',
                color: isActive ? 'var(--fg)' : 'var(--grey2)',
              }}
            >
              All Projects
            </div>
          )}
        </NavLink>

        <div className="my-2 border-t" style={{ borderColor: 'var(--bg3)' }} />

        {sortedProjects.map((p) => (
          <NavLink key={p.name} to={`/projects/${p.name}`} end={false} className="block">
            {({ isActive }) => (
              <ProjectCard name={p.name} summary={summaries.get(p.name)} isActive={isActive} />
            )}
          </NavLink>
        ))}

        {sortedProjects.length === 0 && (
          <div className="px-3 py-4 text-sm text-center" style={{ color: 'var(--grey0)' }}>
            No projects
          </div>
        )}
      </div>

      <div className="px-3 py-3 border-t space-y-2" style={{ borderColor: 'var(--bg3)' }}>
        <button
          onClick={onNewProject}
          className="w-full flex items-center justify-center gap-1.5 px-3 py-1.5 rounded text-sm transition-colors hover:opacity-80"
          style={{ backgroundColor: 'var(--bg1)', color: 'var(--green)' }}
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New Project
        </button>
        <div className="flex items-center gap-2 px-1">
          <span
            className={`w-2 h-2 rounded-full ${connected ? 'animate-pulse' : ''}`}
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
          />
          <span className="text-xs" style={{ color: 'var(--grey0)' }}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
    </div>
  );
}
