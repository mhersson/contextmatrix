import { NavLink } from 'react-router-dom';
import { ThemeToggle } from './ThemeToggle';
import { SyncIndicator } from './SyncIndicator';
import type { SyncStatus } from '../../types';

interface AppHeaderProps {
  project: string;
  connected: boolean;
  syncStatus?: SyncStatus | null;
  onSyncClick?: () => void;
  hasActiveRunners?: boolean;
  onStopAll?: () => void;
}

const VIEWS = [
  { label: 'Board', to: '' },
  { label: 'Dashboard', to: '/dashboard' },
  { label: 'Settings', to: '/settings' },
] as const;

export function AppHeader({ project, connected, syncStatus, onSyncClick, hasActiveRunners, onStopAll }: AppHeaderProps) {
  const base = `/projects/${project}`;
  return (
    <header
      className="flex items-center justify-between px-6 py-3 border-b"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <div className="flex items-center gap-4">
        <span className="text-sm font-medium" style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}>
          {project}
        </span>

        <div className="flex items-center gap-1 rounded p-0.5" style={{ backgroundColor: 'var(--bg1)' }}>
          {VIEWS.map((v) => (
            <NavLink
              key={v.label}
              to={`${base}${v.to}`}
              end
              className="px-3 py-1 rounded text-sm transition-colors"
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

      <div className="flex items-center gap-3">
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
            className={`w-2 h-2 rounded-full ${connected ? 'animate-pulse' : ''}`}
            style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
          />
          <span className="text-sm" style={{ color: 'var(--grey1)' }}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
    </header>
  );
}
