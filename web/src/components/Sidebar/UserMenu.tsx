import { useState } from 'react';
import { useOptionalAuth } from '../../hooks/useAuth';
import { ChangePasswordModal } from '../Auth/ChangePasswordModal';

/**
 * Sidebar-footer user chip for multi mode: display name + a small menu with
 * change-password and sign-out. Renders nothing in none mode or while
 * logged out, so call sites need no conditional.
 *
 * Uses useOptionalAuth (not useAuth) so Sidebar still renders in tests that
 * mount it without an AuthProvider — see deviation note in task-6-report.md.
 * AuthProvider is unconditionally mounted at the App root, so in production
 * this behaves identically to useAuth.
 */
export function UserMenu() {
  const auth = useOptionalAuth();
  const [open, setOpen] = useState(false);
  const [changeOpen, setChangeOpen] = useState(false);

  if (!auth || auth.mode !== 'multi' || !auth.user) return null;

  const { user, logout } = auth;

  const label = user.display_name || user.username;

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 rounded px-2 py-1.5 text-sm"
        style={{ color: 'var(--grey2)', backgroundColor: open ? 'var(--bg2)' : 'transparent' }}
        title={`Signed in as ${user.username}${user.is_admin ? ' (admin)' : ''}`}
      >
        <span
          className="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-semibold"
          style={{ backgroundColor: 'var(--bg-blue)', color: 'var(--aqua)' }}
          aria-hidden="true"
        >
          {label.slice(0, 1).toUpperCase()}
        </span>
        <span className="truncate">{label}</span>
      </button>

      {open && (
        <div
          className="absolute bottom-full left-0 right-0 mb-1 rounded border overflow-hidden z-10"
          style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        >
          <button
            onClick={() => {
              setOpen(false);
              setChangeOpen(true);
            }}
            className="w-full text-left px-3 py-1.5 text-sm hover:opacity-80"
            style={{ color: 'var(--fg)' }}
          >
            Change password
          </button>
          <button
            onClick={() => void logout()}
            className="w-full text-left px-3 py-1.5 text-sm hover:opacity-80"
            style={{ color: 'var(--red)' }}
          >
            Sign out
          </button>
        </div>
      )}

      <ChangePasswordModal open={changeOpen} onClose={() => setChangeOpen(false)} />
    </div>
  );
}
