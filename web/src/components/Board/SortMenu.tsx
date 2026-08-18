import { useRef, useState, useCallback } from 'react';
import type { SortMode } from '../../types';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';

const SORT_OPTIONS: { mode: SortMode; label: string }[] = [
  { mode: 'recent', label: 'Recent' },
  { mode: 'id-asc', label: 'ID \u2191' },
  { mode: 'id-desc', label: 'ID \u2193' },
  { mode: 'priority', label: 'Priority' },
  { mode: 'type', label: 'Type' },
];

export interface SortMenuProps {
  current: SortMode;
  onChange: (mode: SortMode) => void;
}

export function SortMenu({ current, onChange }: SortMenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useMenuDismiss(containerRef, open, () => setOpen(false));

  const handleSelect = useCallback(
    (mode: SortMode) => {
      onChange(mode);
      setOpen(false);
    },
    [onChange],
  );

  return (
    <div ref={containerRef} className="relative inline-flex">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label="Select sort order"
        title="Sort cards"
        aria-haspopup="menu"
        aria-expanded={open}
        className="w-5 h-5 flex items-center justify-center rounded text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg2)] transition-colors"
      >
        {/* Sort icon: two arrows stacked vertically */}
        <svg
          className="w-3.5 h-3.5"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M4 2v10M2 10l2 2 2-2M12 14V4M10 6l2-2 2 2" />
        </svg>
      </button>

      {open && (
        <div
          role="menu"
          style={{
            position: 'absolute',
            right: 0,
            top: 'calc(100% + 4px)',
            backgroundColor: 'var(--bg2)',
            border: '1px solid var(--bg3)',
            borderRadius: '6px',
            minWidth: '120px',
            boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
            zIndex: 100,
            overflow: 'hidden',
          }}
        >
          {SORT_OPTIONS.map(({ mode, label }) => {
            const isActive = current === mode;
            return (
              <button
                key={mode}
                type="button"
                role="menuitemradio"
                aria-checked={isActive}
                onClick={() => handleSelect(mode)}
                className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left transition-colors hover:opacity-80"
                style={{
                  color: isActive ? 'var(--fg)' : 'var(--grey1)',
                  fontWeight: isActive ? 600 : 400,
                }}
              >
                {isActive ? (
                  <span style={{ width: '1em', display: 'inline-block', color: 'var(--green)' }}>
                    {'\u25CF'}
                  </span>
                ) : (
                  <span style={{ width: '1em', display: 'inline-block' }} />
                )}
                {label}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
