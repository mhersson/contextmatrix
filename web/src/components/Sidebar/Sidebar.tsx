import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { NavLink, useLocation } from 'react-router';
import { api } from '../../api/client';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummariesContext } from '../../hooks/ProjectSummariesProvider';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useTheme } from '../../hooks/useTheme';
import { useSync } from '../../hooks/useSync';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useRepoSectionsCollapsed } from '../../hooks/useRepoSectionsCollapsed';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';
import type { PlaybookSummary, ProjectConfig } from '../../types';
import { ProjectCard } from './ProjectCard';
import { ChatSection } from './ChatSection';
import { UserMenu } from './UserMenu';
import { AppearanceMenu } from './AppearanceMenu';
import { RepoSection } from './RepoSection';

interface SidebarProps {
  onNewProject: () => void;
  onNewChat: () => void;
  mobileOpen?: boolean;
  onMobileClose?: () => void;
}

function DashboardIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <rect x="3.5" y="3.5" width="7" height="7" rx="1" />
      <rect x="13.5" y="3.5" width="7" height="7" rx="1" />
      <rect x="3.5" y="13.5" width="7" height="7" rx="1" />
      <rect x="13.5" y="13.5" width="7" height="7" rx="1" />
    </svg>
  );
}

function PlaybooksIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <circle cx="6" cy="5" r="2" />
      <path d="M6 7v7a4 4 0 0 0 4 4h4" />
      <path d="M18 15.2 20.8 18 18 20.8 15.2 18z" />
    </svg>
  );
}

// With exactly one playbook the sidebar link jumps straight to its detail
// page; the list stays reachable via the detail page's back link, so the
// "New playbook" action is never trapped behind the shortcut.
function usePlaybooksLinkTarget(): string {
  const [playbooks, setPlaybooks] = useState<PlaybookSummary[] | null>(null);
  const { subscribe, reconnectEpoch } = useSSEBus();
  const fetchAll = useCallback(() => {
    api.listPlaybooks().then(setPlaybooks).catch(() => setPlaybooks(null));
  }, []);
  useEffect(() => { fetchAll(); }, [fetchAll, reconnectEpoch]);
  useEffect(() => subscribe('playbook.*', fetchAll), [subscribe, fetchAll]);
  return playbooks?.length === 1 ? `/playbooks/${playbooks[0].id}` : '/playbooks';
}

interface WorkspaceNavLinkProps {
  to: string;
  /** Path prefix that marks this link active; defaults to `to`. */
  activeBase?: string;
  label: string;
  icon: ReactNode;
  onNavigate?: () => void;
}

function WorkspaceNavLink({ to, activeBase, label, icon, onNavigate }: WorkspaceNavLinkProps) {
  const { pathname } = useLocation();
  const base = activeBase ?? to;
  const isActive = pathname === base || pathname.startsWith(`${base}/`);
  return (
    <NavLink to={to} className="block" onClick={onNavigate}>
      <div
        className={`sb-navrow${isActive ? ' active' : ''} flex items-center gap-2 px-3 py-1 rounded text-[12.5px] transition-colors`}
        style={{ color: isActive ? 'var(--fg)' : 'var(--grey2)' }}
        aria-current={isActive ? 'page' : undefined}
      >
        <span
          className="flex shrink-0"
          style={{ color: isActive ? 'var(--aqua)' : 'var(--grey1)' }}
          aria-hidden="true"
        >
          {icon}
        </span>
        {label}
      </div>
    </NavLink>
  );
}

export function Sidebar({ onNewProject, onNewChat, mobileOpen = false, onMobileClose }: SidebarProps) {
  const { projects } = useProjects();
  const { version, boardsRepos = [] } = useTheme();
  const { statusFor } = useSync();
  const [isCollapsed, toggleCollapsed] = useRepoSectionsCollapsed();
  const multiRepo = boardsRepos.length > 1;
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
  const playbooksTarget = usePlaybooksLinkTarget();

  const renderProject = (p: ProjectConfig) => (
    <NavLink
      key={p.name}
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
  );

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

      <nav aria-label="Projects" className="flex-1 overflow-y-auto py-1.5">
        <div className="px-2">
          <div className="sb-eyebrow px-3 pt-2 pb-1">Workspace</div>
          <WorkspaceNavLink
            to="/all"
            label="Dashboard"
            icon={<DashboardIcon />}
            onNavigate={mobileOpen ? onMobileClose : undefined}
          />
          <WorkspaceNavLink
            to={playbooksTarget}
            activeBase="/playbooks"
            label="Playbooks"
            icon={<PlaybooksIcon />}
            onNavigate={mobileOpen ? onMobileClose : undefined}
          />
        </div>

        <ChatSection onNewChat={onNewChat} />

        <div className="px-2">
          {multiRepo ? (
            boardsRepos.map((repo) => {
              const items = sortedProjects.filter((p) => (p.boards_repo ?? boardsRepos[0].name) === repo.name);
              return (
                <RepoSection
                  key={repo.name}
                  name={repo.name}
                  shared={repo.shared}
                  status={statusFor(repo.name)}
                  collapsed={isCollapsed(repo.name)}
                  onToggle={() => toggleCollapsed(repo.name)}
                >
                  {items.map(renderProject)}
                  {items.length === 0 && (
                    <div className="px-3 py-2 text-xs text-center" style={{ color: 'var(--grey0)' }}>
                      No projects
                    </div>
                  )}
                </RepoSection>
              );
            })
          ) : (
            <>
              <div className="sb-eyebrow px-3 pt-2 pb-1">Projects</div>
              {sortedProjects.map(renderProject)}
              {sortedProjects.length === 0 && (
                <div className="px-3 py-4 text-sm text-center" style={{ color: 'var(--grey0)' }}>
                  No projects
                </div>
              )}
            </>
          )}
        </div>
      </nav>

      <div className="px-3 py-3 border-t flex flex-col gap-2" style={{ borderColor: 'var(--bg3)' }}>
        {auth?.mode === 'multi'
          ? <UserMenu onNavigate={mobileOpen ? onMobileClose : undefined} />
          : <AppearanceMenu />}
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
