import { useState, useRef, useEffect } from 'react';
import { useTheme } from '../../hooks/useTheme';

type Palette = 'everforest' | 'radix' | 'catppuccin';

const PALETTES: { id: Palette; label: string }[] = [
  { id: 'everforest', label: 'Everforest' },
  { id: 'radix', label: 'Radix' },
  { id: 'catppuccin', label: 'Catppuccin' },
];

export function PaletteSelector() {
  const { palette, setPalette } = useTheme();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleMouseDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setOpen(false);
      }
    }

    document.addEventListener('mousedown', handleMouseDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleMouseDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  function handleSelect(id: Palette) {
    setPalette(id);
    setOpen(false);
  }

  return (
    <div ref={containerRef} style={{ position: 'relative' }}>
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        aria-label="Select color palette"
        title="Select color palette"
        className="flex items-center justify-center w-8 h-8 rounded transition-colors"
        style={{ color: 'var(--grey1)', background: 'transparent' }}
        onMouseEnter={(e) => {
          (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)';
        }}
        onMouseLeave={(e) => {
          (e.currentTarget as HTMLButtonElement).style.color = 'var(--grey1)';
        }}
      >
        {/* Palette icon */}
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="13.5" cy="6.5" r=".5" fill="currentColor" />
          <circle cx="17.5" cy="10.5" r=".5" fill="currentColor" />
          <circle cx="8.5" cy="7.5" r=".5" fill="currentColor" />
          <circle cx="6.5" cy="12.5" r=".5" fill="currentColor" />
          <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z" />
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
            minWidth: '140px',
            boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
            zIndex: 100,
            overflow: 'hidden',
          }}
        >
          {PALETTES.map(({ id, label }) => {
            const isActive = palette === id;
            return (
              <button
                key={id}
                type="button"
                role="menuitem"
                onClick={() => handleSelect(id)}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-left transition-colors"
                style={{
                  color: isActive ? 'var(--fg)' : 'var(--grey1)',
                  backgroundColor: 'transparent',
                  fontWeight: isActive ? 600 : 400,
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--bg3)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                }}
              >
                <span
                  style={{
                    width: '1em',
                    display: 'inline-block',
                    color: 'var(--green)',
                  }}
                >
                  {isActive ? '✓' : ''}
                </span>
                {label}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
