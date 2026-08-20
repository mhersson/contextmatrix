import { useMobileSidebar } from '../../context/MobileSidebarContext';

/** Slim mobile-only bar giving the Playbooks page the same hamburger
 *  affordance as UtilityBar - no sync pill or clock, just sidebar access. */
export function PlaybooksBar() {
  const { toggle } = useMobileSidebar();

  return (
    <div className="md:hidden flex items-center gap-2 px-4 py-2 border-b" style={{ borderColor: 'var(--bg3)' }}>
      <button
        type="button"
        onClick={toggle}
        aria-label="Open menu"
        className="shrink-0 inline-flex items-center justify-center p-1.5 border-0 bg-transparent cursor-pointer rounded transition-[opacity,background-color] hover:opacity-90 hover:bg-[var(--bg1)] focus-visible:ring-2 focus-visible:ring-[var(--aqua)] focus-visible:outline-none"
        style={{ color: 'var(--grey2)' }}
      >
        <svg
          width={20}
          height={20}
          viewBox="0 0 20 20"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
          aria-hidden="true"
        >
          <rect x="2" y="4" width="16" height="2" rx="1" fill="currentColor" />
          <rect x="2" y="9" width="16" height="2" rx="1" fill="currentColor" />
          <rect x="2" y="14" width="16" height="2" rx="1" fill="currentColor" />
        </svg>
      </button>
      <span className="truncate" style={{ color: 'var(--fg)' }}>Playbooks</span>
    </div>
  );
}
