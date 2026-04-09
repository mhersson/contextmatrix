import { NavLink } from 'react-router-dom';
import { ThemeToggle } from './ThemeToggle';
import { SyncIndicator } from './SyncIndicator';
import { useMobileSidebar } from '../../context/MobileSidebarContext';
import type { SyncStatus } from '../../types';

interface AppHeaderProps {
  project: string;
  connected: boolean;
  syncStatus?: SyncStatus | null;
  onSyncClick?: () => void;
  hasActiveRunners?: boolean;
  onStopAll?: () => void;
  runnerEnabled?: boolean;
  consoleOpen?: boolean;
  onToggleConsole?: () => void;
}

const VIEWS = [
  { label: 'Board', to: '' },
  { label: 'Dashboard', to: '/dashboard' },
  { label: 'Settings', to: '/settings' },
] as const;

export function AppHeader({ project, connected, syncStatus, onSyncClick, hasActiveRunners, onStopAll, runnerEnabled, consoleOpen, onToggleConsole }: AppHeaderProps) {
  const base = `/projects/${project}`;
  const { toggle } = useMobileSidebar();
  return (
    <header
      className="flex items-center justify-between px-3 py-2 sm:px-6 sm:py-3 border-b"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <div className="flex items-center gap-2 sm:gap-4">
        <button
          type="button"
          onClick={toggle}
          className="md:hidden p-1 rounded hover:opacity-80 transition-opacity"
          style={{ color: 'var(--fg1)' }}
          aria-label="Toggle sidebar"
        >
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
            <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
          </svg>
        </button>
        <span className="text-sm font-medium" style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}>
          {project}
        </span>

        <div className="flex items-center gap-1 rounded p-0.5" style={{ backgroundColor: 'var(--bg1)' }}>
          {VIEWS.slice(0, 1).map((v) => (
            <NavLink
              key={v.label}
              to={`${base}${v.to}`}
              end
              className="px-2 py-1 sm:px-3 rounded text-sm transition-colors"
              style={({ isActive }) => ({
                backgroundColor: isActive ? 'var(--bg3)' : 'transparent',
                color: isActive ? 'var(--fg)' : 'var(--grey1)',
              })}
            >
              {v.label}
            </NavLink>
          ))}
          {runnerEnabled && (
            <button
              type="button"
              onClick={onToggleConsole}
              className="px-2 py-1 sm:px-3 rounded text-sm transition-colors flex items-center gap-1"
              style={{
                backgroundColor: consoleOpen ? 'var(--bg3)' : 'transparent',
                color: consoleOpen ? 'var(--fg)' : 'var(--grey1)',
              }}
              title="Toggle runner console (c)"
            >
              <span aria-hidden="true" style={{ fontFamily: 'var(--font-mono)', fontSize: '0.7rem' }}>{'>_'}</span>
              Console
            </button>
          )}
          {VIEWS.slice(1).map((v) => (
            <NavLink
              key={v.label}
              to={`${base}${v.to}`}
              end
              className="px-2 py-1 sm:px-3 rounded text-sm transition-colors"
              style={({ isActive }) => ({
                backgroundColor: isActive ? 'var(--bg3)' : 'transparent',
                color: isActive ? 'var(--fg)' : 'var(--grey1)',
              })}
            >
              {v.label}
            </NavLink>
          ))}
        </div>
      </div>

      <div className="flex items-center gap-2 sm:gap-3">
        {hasActiveRunners && onStopAll && (
          <button
            type="button"
            onClick={() => { if (window.confirm('Stop all running tasks? Containers will be destroyed.')) onStopAll(); }}
            className="px-2 py-1 rounded text-xs font-medium hover:opacity-90 transition-opacity"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
            title="Stop all running tasks"
          >
            Stop All
          </button>
        )}
        <ThemeToggle />
        <SyncIndicator status={syncStatus} onClick={onSyncClick} />
        <div className="flex items-center gap-2">
          <span
            className="w-2 h-2 rounded-full"
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
          />
          <span className="text-sm hidden sm:inline" style={{ color: 'var(--grey1)' }}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
    </header>
  );
}
