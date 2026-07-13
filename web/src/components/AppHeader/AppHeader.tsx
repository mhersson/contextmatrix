import { useState } from 'react';
import { NavLink } from 'react-router-dom';
import { ThemeToggle } from './ThemeToggle';
import { PaletteSelector } from './PaletteSelector';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { useMobileSidebar } from '../../context/MobileSidebarContext';

interface AppHeaderProps {
  project: string;
  hasActiveWorkers?: boolean;
  onStopAll?: () => void;
  taskBackendConfigured?: boolean;
  consoleOpen?: boolean;
  onToggleConsole?: () => void;
}

export function AppHeader({ project, hasActiveWorkers, onStopAll, taskBackendConfigured, consoleOpen, onToggleConsole }: AppHeaderProps) {
  const base = `/projects/${project}`;
  const { toggle } = useMobileSidebar();
  const [confirmStopAllOpen, setConfirmStopAllOpen] = useState(false);
  return (
    <>
    <header
      className="flex items-center justify-between px-3 py-2 sm:px-6 sm:py-3 border-b"
      style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
    >
      <div className="flex items-center gap-2 sm:gap-4">
        <button
          type="button"
          onClick={toggle}
          className="md:hidden p-1 rounded hover:opacity-80 transition-opacity"
          style={{ color: 'var(--fg)' }}
          aria-label="Toggle sidebar"
        >
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
            <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
            <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
          </svg>
        </button>
        <nav aria-label="Primary navigation" className="flex items-center gap-1 rounded p-0.5" style={{ backgroundColor: 'var(--bg1)' }}>
          <NavLink
            key="Board"
            to={`${base}`}
            end
            className="px-2 py-1 sm:px-3 rounded text-sm transition-colors"
            style={({ isActive }) => ({
              backgroundColor: isActive ? 'var(--bg3)' : 'transparent',
              color: isActive ? 'var(--fg)' : 'var(--grey1)',
            })}
          >
            Board
          </NavLink>
          {taskBackendConfigured && (
            <button
              type="button"
              onClick={onToggleConsole}
              aria-pressed={consoleOpen ?? false}
              className="px-2 py-1 sm:px-3 rounded text-sm transition-colors flex items-center gap-1"
              style={{
                backgroundColor: consoleOpen ? 'var(--bg3)' : 'transparent',
                color: consoleOpen ? 'var(--fg)' : 'var(--grey1)',
              }}
              title="Toggle worker console (c)"
            >
              <span aria-hidden="true" style={{ fontFamily: 'var(--font-mono)', fontSize: '0.7rem' }}>{'>_'}</span>
              Console
            </button>
          )}
          <NavLink
            key="Settings"
            to={`${base}/settings`}
            end
            className="px-2 py-1 sm:px-3 rounded text-sm transition-colors"
            style={({ isActive }) => ({
              backgroundColor: isActive ? 'var(--bg3)' : 'transparent',
              color: isActive ? 'var(--fg)' : 'var(--grey1)',
            })}
          >
            Settings
          </NavLink>
        </nav>
      </div>

      <div className="flex items-center gap-2 sm:gap-3">
        {hasActiveWorkers && onStopAll && (
          <button
            type="button"
            onClick={() => setConfirmStopAllOpen(true)}
            className="px-2 py-1 rounded text-xs font-medium hover:opacity-90 transition-opacity"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
            title="Stop all running tasks"
          >
            Stop All
          </button>
        )}
        <PaletteSelector />
        <ThemeToggle />
      </div>
    </header>

    <ConfirmModal
      open={confirmStopAllOpen}
      title="Stop all running tasks?"
      message="All active worker containers will be destroyed and uncommitted work discarded."
      confirmLabel="Stop all"
      variant="danger"
      onConfirm={() => {
        setConfirmStopAllOpen(false);
        onStopAll?.();
      }}
      onCancel={() => setConfirmStopAllOpen(false)}
    />
    </>
  );
}
