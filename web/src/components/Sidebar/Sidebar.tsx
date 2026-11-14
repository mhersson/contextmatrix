import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { NavLink, useLocation } from 'react-router';
import { sortProjectsForSidebar } from '../../lib/projectOrder';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummariesContext } from '../../hooks/ProjectSummariesProvider';
import { useTheme } from '../../hooks/useTheme';
import { useSync } from '../../hooks/useSync';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useRepoSectionsCollapsed } from '../../hooks/useRepoSectionsCollapsed';
import { formatVersionWithLocalTime } from '../../utils/formatVersion';
import { boardsRepoName, type ProjectConfig, type SessionKind } from '../../types';
import { ProjectCard } from './ProjectCard';
import { PlaybookSection } from './PlaybookSection';
import { SessionSections } from './SessionSections';
import { UserMenu } from './UserMenu';
import { SettingsMenu } from './SettingsMenu';
import { RepoSection } from './RepoSection';
import { LLMChannelRailDot } from './LLMChannelChip';
import { SentinelRailIcon } from './SentinelChip';
import { StatusRow } from './StatusRow';
import { WaitingSection } from './WaitingSection';
import { WaitBadge } from '../WaitBadge';
import { useAttention } from '../../hooks/useAttention';

interface SidebarProps {
  onNewProject: () => void;
  onNewSession: (kind: SessionKind) => void;
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

interface BoardsNavLinkProps {
  to: string;
  label: string;
  icon: ReactNode;
  onNavigate?: () => void;
}

function BoardsNavLink({ to, label, icon, onNavigate }: BoardsNavLinkProps) {
  const { pathname } = useLocation();
  const isActive = pathname === to || pathname.startsWith(`${to}/`);
  return (
    <NavLink to={to} className="block" onClick={onNavigate}>
      <div
        className={`sb-navrow${isActive ? ' active' : ''} flex items-center gap-2 px-3 py-1 rounded text-[13.5px] transition-colors`}
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

export function Sidebar({ onNewProject, onNewSession, mobileOpen = false, onMobileClose }: SidebarProps) {
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
    () => sortProjectsForSidebar(projects, boardsRepos),
    [projects, boardsRepos]
  );
  const { summaries } = useProjectSummariesContext();
  const { items: waiting } = useAttention();

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
          <ProjectCard name={p.name} displayName={p.display_name} summary={summaries.get(p.name)} isActive={isActive} poc={p.poc} />
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
      <div className="relative px-4 py-4 border-b" style={{ borderColor: 'var(--bg3)' }}>
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <h1
              className="truncate"
              style={{
                color: 'var(--fg)',
                fontFamily: 'var(--font-sans)',
                fontWeight: 500,
                fontSize: '28px',
                letterSpacing: '-0.02em',
                lineHeight: 1.15,
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
        <StatusRow />
      </div>

      <nav aria-label="Projects" className="flex-1 overflow-y-auto py-1.5">
        <WaitingSection />
        <div className="sb-eyebrow flex items-baseline justify-between px-5 pt-2 pb-0.5">
          Work
          <span className="normal-case tracking-normal" style={{ fontSize: '10px' }}>chat → blueprint → playbook</span>
        </div>
        <SessionSections onNewSession={onNewSession} />
        <PlaybookSection />

        <div className="px-2">
          <div className="sb-eyebrow px-3 pt-2 pb-1">Boards</div>
          <BoardsNavLink
            to="/all"
            label="Dashboard"
            icon={<DashboardIcon />}
            onNavigate={mobileOpen ? onMobileClose : undefined}
          />
        </div>

        <div className="px-2">
          {multiRepo ? (
            boardsRepos.map((repo) => {
              const items = sortedProjects.filter((p) => boardsRepoName(p.boards_repo, boardsRepos) === repo.name);
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
          : <SettingsMenu onNavigate={mobileOpen ? onMobileClose : undefined} />}
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
        {waiting.length > 0 && (
          <button
            type="button"
            onClick={() => setCollapsed(false)}
            className="relative mt-2 p-1 rounded hover:opacity-80"
            style={{ color: 'var(--yellow)' }}
            title={`${waiting.length} waiting for you`}
            aria-label={`${waiting.length} waiting for you`}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
              <path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" />
            </svg>
            <span className="absolute -top-0.5 -right-1">
              <WaitBadge count={waiting.length} />
            </span>
          </button>
        )}
        <LLMChannelRailDot />
        <SentinelRailIcon />
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
