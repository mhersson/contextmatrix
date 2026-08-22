import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import { ChangePasswordModal } from '../Auth/ChangePasswordModal';
import { AppearanceMenuItems } from './AppearanceMenuItems';

/**
 * Sidebar-footer user chip for multi mode: display name + a small menu with
 * the admin pages, the appearance radios, change-password and sign-out.
 * Renders nothing in none mode or while logged out, so call sites need no
 * conditional (none mode gets AppearanceMenu in the same slot instead).
 *
 * Uses useOptionalAuth (not useAuth) so Sidebar still renders in tests that
 * mount it without an AuthProvider - see deviation note in task-6-report.md.
 * AuthProvider is unconditionally mounted at the App root, so in production
 * this behaves identically to useAuth.
 */
export function UserMenu({ onNavigate }: { onNavigate?: () => void } = {}) {
  const auth = useOptionalAuth();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [changeOpen, setChangeOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const goto = (path: string) => {
    setOpen(false);
    onNavigate?.();
    navigate(path);
  };

  useMenuDismiss(containerRef, open, () => {
    if (containerRef.current?.contains(document.activeElement)) triggerRef.current?.focus();
    setOpen(false);
  });

  if (!auth || auth.mode !== 'multi' || !auth.user) return null;

  const { user, logout } = auth;

  const label = user.display_name || user.username;

  return (
    <div ref={containerRef} className="relative">
      <button
        ref={triggerRef}
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
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
          role="menu"
          className="absolute bottom-full left-0 right-0 mb-1 rounded border overflow-hidden z-10"
          style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        >
          {user.is_admin && (
            <>
              <div
                className="px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wide"
                style={{ color: 'var(--grey0)' }}
              >
                ADMIN
              </div>
              {[
                { label: 'Users', path: '/admin/users' },
                { label: 'Credentials', path: '/admin/credentials' },
                { label: 'Chats', path: '/admin/chats' },
                { label: 'Model selection', path: '/admin/model-selection' },
              ].map((item) => (
                <button
                  key={item.path}
                  role="menuitem"
                  onClick={() => goto(item.path)}
                  className="w-full text-left px-3 py-1.5 text-sm hover:opacity-80"
                  style={{ color: 'var(--fg)' }}
                >
                  {item.label}
                </button>
              ))}
              <div className="border-t my-1" style={{ borderColor: 'var(--bg3)' }} aria-hidden="true" />
            </>
          )}
          <AppearanceMenuItems />
          <div className="border-t my-1" style={{ borderColor: 'var(--bg3)' }} aria-hidden="true" />
          <button
            role="menuitem"
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
            role="menuitem"
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
