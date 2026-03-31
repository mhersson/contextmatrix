import { NavLink } from 'react-router-dom';
import { ThemeToggle } from './ThemeToggle';

interface AppHeaderProps {
  project: string;
  connected: boolean;
}

const VIEWS = [
  { label: 'Board', to: '' },
  { label: 'Dashboard', to: '/dashboard' },
  { label: 'Settings', to: '/settings' },
] as const;

export function AppHeader({ project, connected }: AppHeaderProps) {
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
        <ThemeToggle />
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
