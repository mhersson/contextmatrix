import { useRef, useState } from 'react';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import { AppearanceMenuItems } from './AppearanceMenuItems';

/**
 * Sidebar-footer chip for none mode, where there is no user chip: opens a
 * menu holding only the APPEARANCE group. Sits in the slot UserMenu occupies
 * in multi mode so theme/palette live in the same place in both modes.
 */
export function AppearanceMenu() {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  // Escape/outside-click unmounts the menu; hand focus back to the chip when
  // it was inside so keyboard users do not drop to <body>.
  useMenuDismiss(containerRef, open, () => {
    if (containerRef.current?.contains(document.activeElement)) triggerRef.current?.focus();
    setOpen(false);
  });

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
            <circle cx="13.5" cy="6.5" r=".5" fill="currentColor" />
            <circle cx="17.5" cy="10.5" r=".5" fill="currentColor" />
            <circle cx="8.5" cy="7.5" r=".5" fill="currentColor" />
            <circle cx="6.5" cy="12.5" r=".5" fill="currentColor" />
            <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z" />
          </svg>
        </span>
        <span className="truncate">Appearance</span>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute bottom-full left-0 right-0 mb-1 rounded border overflow-hidden z-10"
          style={{ backgroundColor: 'var(--bg2)', borderColor: 'var(--bg3)' }}
        >
          <AppearanceMenuItems />
        </div>
      )}
    </div>
  );
}
