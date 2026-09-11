import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import { AppearanceMenuItems } from './AppearanceMenuItems';

/**
 * Sidebar-footer chip for none mode, where there is no user chip: opens a
 * menu with the admin pages that exist without accounts (Chats, Model
 * selection) and the APPEARANCE group. Sits in the slot UserMenu occupies
 * in multi mode so settings live in the same place in both modes. Users
 * and Credentials are deliberately absent: they are multi-mode concepts.
 */
export function SettingsMenu({ onNavigate }: { onNavigate?: () => void } = {}) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const goto = (path: string) => {
    setOpen(false);
    onNavigate?.();
    navigate(path);
  };

  useMenuDismiss(containerRef, open, () => setOpen(false), triggerRef);

  return (
    <div ref={containerRef} className="relative">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="w-full flex items-center gap-2 rounded px-2 py-1.5 text-sm"
        style={{ color: 'var(--grey2)', backgroundColor: open ? 'var(--bg2)' : 'transparent' }}
      >
        <span className="w-5 h-5 flex items-center justify-center" style={{ color: 'var(--grey1)' }} aria-hidden="true">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.01a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
        </span>
        <span className="truncate">Settings</span>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute bottom-full left-0 right-0 mb-1 rounded border overflow-hidden z-10"
          style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        >
          <div className="px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wide" style={{ color: 'var(--grey0)' }}>
            ADMIN
          </div>
          {[
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
          <AppearanceMenuItems />
        </div>
      )}
    </div>
  );
}
