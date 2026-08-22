import type { Ref } from 'react';

interface HeaderCollapseToggleProps {
  collapsed: boolean;
  onToggle: () => void;
  ref?: Ref<HTMLButtonElement>;
}

/**
 * Icon button that collapses or expands the board header chrome. Rendered by
 * the band that is currently showing - BoardBand while expanded, BoardMicroBand
 * while collapsed - so it always sits inside the header it controls. The two
 * bands are different components, so toggling unmounts this button and mounts
 * a fresh one; Board holds `ref` to hand keyboard focus across that swap.
 */
export function HeaderCollapseToggle({ collapsed, onToggle, ref }: HeaderCollapseToggleProps) {
  const label = collapsed ? 'Expand board header' : 'Collapse board header';
  return (
    <button
      ref={ref}
      type="button"
      onClick={onToggle}
      aria-expanded={!collapsed}
      aria-label={label}
      title={label}
      className="p-1 rounded transition-colors text-[var(--grey1)] hover:text-[var(--fg)] hover:bg-[var(--bg2)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--aqua)] flex-shrink-0"
    >
      {collapsed ? (
        /* Double chevron down - expand the board header */
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 4l-7 7-7-7" />
        </svg>
      ) : (
        /* Double chevron up - collapse the board header */
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 20l7-7 7 7" />
        </svg>
      )}
    </button>
  );
}
