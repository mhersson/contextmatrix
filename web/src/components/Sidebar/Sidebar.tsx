import { useEffect, useMemo, useRef, useState } from 'react';
import { NavLink } from 'react-router-dom';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummariesContext } from '../../hooks/ProjectSummariesProvider';
import { useTheme } from '../../hooks/useTheme';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';
import { ProjectCard } from './ProjectCard';
import { ChatSection } from './ChatSection';
import { UserMenu } from './UserMenu';

interface SidebarProps {
  onNewProject: () => void;
  onNewChat: () => void;
  mobileOpen?: boolean;
  onMobileClose?: () => void;
}

export function Sidebar({ onNewProject, onNewChat, mobileOpen = false, onMobileClose }: SidebarProps) {
  const { projects } = useProjects();
  const { version } = useTheme();
  const auth = useOptionalAuth();
  const isAdmin = Boolean(auth?.user?.is_admin);
  // UX honesty, not a security boundary - the API 403s a non-admin project
  // create anyway (multi mode is admin-gated). None mode (auth?.mode !==
  // 'multi', including no AuthProvider at all) always shows the button.
  const canCreateProject = !(auth?.mode === 'multi' && !isAdmin);
  const [collapsed, setCollapsed] = useState(false);
  const drawerRef = useRef<HTMLDivElement>(null);
  const sortedProjects = useMemo(
    () => [...projects].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })),
    [projects]
  );
  const { summaries } = useProjectSummariesContext();

  // Mobile drawer: trap focus and close on Escape.
  useFocusTrap(drawerRef, mobileOpen);
  useEffect(() => {
    if (!mobileOpen || !onMobileClose) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onMobileClose?.();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [mobileOpen, onMobileClose]);

  // Shared panel content used in both desktop and mobile overlay modes
  const panelContent = (
    <>
      <div className="flex items-start justify-between gap-2 px-4 py-4 border-b" style={{ borderColor: 'var(--bg3)' }}>
        <div className="min-w-0 flex-1">
          <h1
            className="truncate"
            style={{
              color: 'var(--fg)',
              fontFamily: 'var(--font-display)',
              fontWeight: 500,
              fontSize: '28px',
              letterSpacing: '-0.02em',
              lineHeight: 1.15,
              // Tune Fraunces' optical-size axis to roughly the rendered
              // size - opsz: 96 is for huge display use and ships
              // delicate strokes that wash out at sidebar size; matching
              // opsz here gives the italic M's left leg solid weight.
              fontVariationSettings: '"opsz" 28',
              // Italic Fraunces leans left; the leftmost stroke gets
              // clipped by the h1's truncate overflow:hidden without a
              // small inset.
              paddingLeft: '5px',
              marginLeft: '-5px',
            }}
          >
            Context<em style={{ fontStyle: 'italic', color: 'var(--aqua)', fontWeight: 400 }}>Matrix</em>
          </h1>
          {version && (
            <p
              className="font-mono truncate"
              style={{
                color: 'var(--grey0)',
                fontSize: '10.5px',
                letterSpacing: '0.02em',
                opacity: 0.75,
                marginTop: '3px',
              }}
              title={formatVersionWithLocalTime(version)}
            >
              {formatVersionWithLocalTime(version)}
            </p>
          )}
        </div>
        {!mobileOpen && (
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
        )}
        {mobileOpen && onMobileClose && (
          <button
            onClick={onMobileClose}
            className="p-1 rounded hover:opacity-80"
            style={{ color: 'var(--grey1)' }}
            title="Close sidebar"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        )}
      </div>

      <nav aria-label="Projects" className="flex-1 overflow-y-auto px-2 py-2 space-y-0.5">
        <ChatSection onNewChat={onNewChat} />
        <div className="my-2 border-t" style={{ borderColor: 'var(--bg3)' }} />
        <NavLink to="/all" className="block" onClick={mobileOpen ? onMobileClose : undefined}>
          {({ isActive }) => (
            <div
              className="px-3 py-2 rounded text-sm transition-colors"
              style={{
                backgroundColor: isActive ? 'var(--bg2)' : 'transparent',
                color: isActive ? 'var(--fg)' : 'var(--grey2)',
              }}
              aria-current={isActive ? 'page' : undefined}
            >
              All Projects
            </div>
          )}
        </NavLink>

        <div className="my-2 border-t" style={{ borderColor: 'var(--bg3)' }} />

        {sortedProjects.map((p, i) => (
          <div key={p.name}>
            {i > 0 && (
              <div className="mx-3 my-0.5 border-t" style={{ borderColor: 'var(--bg3)' }} />
            )}
            <NavLink
              to={`/projects/${p.name}`}
              end={false}
              className="block"
              onClick={mobileOpen ? onMobileClose : undefined}
            >
              {({ isActive }) => (
                <div aria-current={isActive ? 'page' : undefined}>
                  <ProjectCard name={p.name} displayName={p.display_name} summary={summaries.get(p.name)} isActive={isActive} />
                </div>
              )}
            </NavLink>
          </div>
        ))}

        {sortedProjects.length === 0 && (
          <div className="px-3 py-4 text-sm text-center" style={{ color: 'var(--grey0)' }}>
            No projects
          </div>
        )}
      </nav>

      <div className="px-3 py-3 border-t flex flex-col gap-2" style={{ borderColor: 'var(--bg3)' }}>
        <UserMenu onNavigate={mobileOpen ? onMobileClose : undefined} />
        {canCreateProject && (
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
        )}
      </div>
    </>
  );

  // Mobile overlay mode: render backdrop + drawer panel on top of everything
  if (mobileOpen) {
    return (
      <>
        {/* Dark backdrop - clicking it closes the drawer */}
        <div
          className="fixed inset-0 z-50"
          style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}
          onClick={onMobileClose}
          aria-hidden="true"
        />
        {/* Drawer panel */}
        <div
          ref={drawerRef}
          className="fixed left-0 top-0 h-full z-50 flex flex-col"
          style={{ width: 240, backgroundColor: 'var(--bg0)', borderRight: '1px solid var(--bg3)' }}
          role="dialog"
          aria-modal="true"
          aria-label="Sidebar navigation"
        >
          {panelContent}
        </div>
      </>
    );
  }

  // Desktop collapsed state
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

  // Desktop expanded state
  return (
    <div
      className="flex flex-col border-r shrink-0 sidebar"
      style={{ width: 240, backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      {panelContent}
    </div>
  );
}
